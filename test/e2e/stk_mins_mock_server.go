// Package e2e 提供基于 stk_mins CSV 的模拟服务端，支持 pull（HTTP 分页）与 push（WebSocket 流式）
package e2e

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// StkMinsRow 分钟线一行数据，与 CSV 列对应
type StkMinsRow struct {
	TsCode      string  `json:"ts_code"`
	TradeTime   string  `json:"trade_time"`
	Open        float64 `json:"open"`
	Close       float64 `json:"close"`
	High        float64 `json:"high"`
	Low         float64 `json:"low"`
	Vol         int64   `json:"vol"`
	Amount      float64 `json:"amount"`
	SyncBatchID string  `json:"sync_batch_id"`
	CreatedAt   string  `json:"created_at"`
}

// StkMinsMockServer 基于 CSV 的模拟服务：Pull = HTTP 分页 GET，Push = WebSocket 逐条推送
type StkMinsMockServer struct {
	server *httptest.Server
	rows   []StkMinsRow
	mu     sync.RWMutex

	// push 模式：每条消息间隔（便于测试不拖太久）
	pushInterval time.Duration
	// 最多推送条数（0=全部），便于 E2E 快速结束
	maxPushRows int
}

// NewStkMinsMockServer 从 CSV 文件路径加载数据并创建服务；若 path 为空或加载失败则使用内置少量示例行
func NewStkMinsMockServer(csvPath string) (*StkMinsMockServer, error) {
	s := &StkMinsMockServer{
		pushInterval: 15 * time.Millisecond,
		maxPushRows:  0,
	}
	if csvPath != "" {
		if err := s.loadCSV(csvPath); err != nil {
			return nil, err
		}
	}
	if len(s.rows) == 0 {
		s.rows = []StkMinsRow{
			{TsCode: "000001.SZ", TradeTime: "2026-03-04 15:00:00", Open: 10.0, Close: 10.1, High: 10.2, Low: 9.9, Vol: 1000, Amount: 10100},
			{TsCode: "000001.SZ", TradeTime: "2026-03-04 14:59:00", Open: 9.98, Close: 10.0, High: 10.05, Low: 9.95, Vol: 800, Amount: 8000},
		}
	}
	return s, nil
}

// SetPushInterval 设置 push 模式每条消息间隔
func (s *StkMinsMockServer) SetPushInterval(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pushInterval = d
}

// SetMaxPushRows 设置 push 模式最多推送条数，0 表示全部
func (s *StkMinsMockServer) SetMaxPushRows(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxPushRows = n
}

func (s *StkMinsMockServer) loadCSV(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := csv.NewReader(f)
	recs, err := r.ReadAll()
	if err != nil {
		return err
	}
	if len(recs) < 2 {
		return nil
	}
	// recs[0] = header
	rows := make([]StkMinsRow, 0, len(recs)-1)
	for i := 1; i < len(recs); i++ {
		rec := recs[i]
		if len(rec) < 8 {
			continue
		}
		open, _ := strconv.ParseFloat(rec[2], 64)
		closeVal, _ := strconv.ParseFloat(rec[3], 64)
		high, _ := strconv.ParseFloat(rec[4], 64)
		low, _ := strconv.ParseFloat(rec[5], 64)
		vol, _ := strconv.ParseInt(rec[6], 10, 64)
		amount, _ := strconv.ParseFloat(rec[7], 64)
		syncBatchID := ""
		createdAt := ""
		if len(rec) > 8 {
			syncBatchID = rec[8]
		}
		if len(rec) > 9 {
			createdAt = rec[9]
		}
		rows = append(rows, StkMinsRow{
			TsCode:      rec[0],
			TradeTime:   rec[1],
			Open:        open,
			Close:       closeVal,
			High:        high,
			Low:         low,
			Vol:         vol,
			Amount:      amount,
			SyncBatchID: syncBatchID,
			CreatedAt:   createdAt,
		})
	}
	s.mu.Lock()
	s.rows = rows
	s.mu.Unlock()
	return nil
}

// Start 启动 HTTP 服务；Pull: GET /api/stk_mins?offset=0&limit=100，Push: WebSocket /ws/stk_mins
func (s *StkMinsMockServer) Start() string {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/stk_mins", s.handlePull)
	mux.HandleFunc("/ws/stk_mins", s.handlePush)
	s.server = httptest.NewServer(mux)
	return s.server.URL
}

// Stop 停止服务
func (s *StkMinsMockServer) Stop() {
	if s.server != nil {
		s.server.Close()
	}
}

// URL 返回 base URL（http://...）
func (s *StkMinsMockServer) URL() string {
	if s.server == nil {
		return ""
	}
	return s.server.URL
}

// WsURL 返回 WebSocket URL（ws://...）
func (s *StkMinsMockServer) WsURL() string {
	u := s.URL()
	if u == "" {
		return ""
	}
	if strings.HasPrefix(u, "https://") {
		return "wss" + u[5:]
	}
	return "ws" + u[4:]
}

// handlePull GET /api/stk_mins?offset=0&limit=100
func (s *StkMinsMockServer) handlePull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	offset := 0
	limit := 100
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	s.mu.RLock()
	total := len(s.rows)
	end := offset + limit
	if end > total {
		end = total
	}
	var slice []StkMinsRow
	if offset < total {
		slice = make([]StkMinsRow, end-offset)
		copy(slice, s.rows[offset:end])
	}
	s.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"data":  slice,
		"total": total,
	})
}

var upgraderStkMins = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// handlePush WebSocket /ws/stk_mins：连接后按 pushInterval 逐条发送 rows
func (s *StkMinsMockServer) handlePush(w http.ResponseWriter, r *http.Request) {
	conn, err := upgraderStkMins.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	s.mu.RLock()
	rows := make([]StkMinsRow, len(s.rows))
	copy(rows, s.rows)
	interval := s.pushInterval
	maxPush := s.maxPushRows
	s.mu.RUnlock()
	if maxPush > 0 && len(rows) > maxPush {
		rows = rows[:maxPush]
	}
	for i := range rows {
		if err := conn.WriteJSON(rows[i]); err != nil {
			return
		}
		if interval > 0 {
			time.Sleep(interval)
		}
	}
}
