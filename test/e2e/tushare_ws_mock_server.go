// Package e2e 提供 Tushare 风格 WebSocket 模拟服务端，协议与 subscribe.py 一致
package e2e

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// TusharePushMessage 服务端推送单条消息格式（与 subscribe.py 接收一致）
type TusharePushMessage struct {
	Status  bool        `json:"status"`
	Message string      `json:"message"`
	Data    *TushareTickData `json:"data"`
}

// TushareTickData data 段：topic、code、record（TsStkBndFnd 数组）
type TushareTickData struct {
	Topic  string        `json:"topic"`
	Code   string        `json:"code"`
	Record []interface{} `json:"record"`
}

// TushareListeningRequest 客户端连接后发送的 listening 请求
type TushareListeningRequest struct {
	Action string            `json:"action"`
	Token  string            `json:"token"`
	Data   map[string][]string `json:"data"` // topic -> codes
}

// TushareWsMockServer Tushare 风格 WebSocket Mock：连接后读 listening，再按 pushInterval 推送
type TushareWsMockServer struct {
	server *httptest.Server
	msgs   []*TusharePushMessage
	mu     sync.RWMutex

	pushInterval     time.Duration
	maxPushRows      int  // 0=全部
	disconnectAfter  int  // 推送 N 条后主动断线，0=不断
}

// NewTushareWsMockServer 从文件加载或使用内置样本。path 为每行一个 JSON 的文件路径，空则用内置
func NewTushareWsMockServer(jsonLinesPath string) (*TushareWsMockServer, error) {
	s := &TushareWsMockServer{
		pushInterval: 20 * time.Millisecond,
		maxPushRows:  0,
	}
	if jsonLinesPath != "" {
		if err := s.loadJSONLines(jsonLinesPath); err != nil {
			return nil, err
		}
	}
	if len(s.msgs) == 0 {
		s.msgs = embeddedTushareSamples()
	}
	return s, nil
}

func (s *TushareWsMockServer) loadJSONLines(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var list []*TusharePushMessage
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		// 兼容 realtime_output.txt 中 "DEBUG - {json}" 格式
		if idx := strings.Index(line, "{\"status\""); idx >= 0 {
			line = line[idx:]
		}
		var m TusharePushMessage
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		if m.Data != nil {
			list = append(list, &m)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.msgs = list
	s.mu.Unlock()
	return nil
}

func embeddedTushareSamples() []*TusharePushMessage {
	return []*TusharePushMessage{
		{
			Status: true, Message: "",
			Data: &TushareTickData{
				Topic: "HQ_STK_TICK", Code: "600863.SH",
				Record: []interface{}{"600863.SH", "华能蒙电", "2026-03-11 11:12:04", 5.32, 5.36, 5.36, 5.36, 5.25, 0, 39167194, 207437940, 23486, 5.32, 509050, 5.31, 3100, 5.33, 372000, 5.3, 778800, 5.34, 477400, 5.29, 647800, 5.35, 653500, 5.28, 1070700, 5.36, 617000, 5.27, 596200, 0},
			},
		},
		{
			Status: true, Message: "",
			Data: &TushareTickData{
				Topic: "HQ_STK_TICK", Code: "601169.SH",
				Record: []interface{}{"601169.SH", "北京银行", "2026-03-11 11:12:05", 5.39, 5.39, 5.4, 5.39, 5.37, 0, 40431165, 217825376, 14427, 5.4, 8457088, 5.39, 6116018, 5.41, 6565080, 5.38, 9255200, 5.42, 6788900, 5.37, 8739700, 5.43, 4545336, 5.36, 7781300, 5.44, 3188112, 5.35, 5103800, 0},
			},
		},
		{
			Status: true, Message: "",
			Data: &TushareTickData{
				Topic: "HQ_STK_TICK", Code: "600503.SH",
				Record: []interface{}{"600503.SH", "华丽家族", "2026-03-11 11:12:04", 2.55, 2.55, 2.56, 2.55, 2.53, 0, 10405500, 26484374, 3784, 2.56, 1316900, 2.55, 332000, 2.57, 461900, 2.54, 1171700, 2.58, 515500, 2.53, 917100, 2.59, 744000, 2.52, 466000, 2.6, 1197900, 2.51, 1050800, 0},
			},
		},
	}
}

// SetPushInterval 设置每条消息推送间隔
func (s *TushareWsMockServer) SetPushInterval(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pushInterval = d
}

// SetMaxPushRows 最多推送条数，0 表示全部
func (s *TushareWsMockServer) SetMaxPushRows(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxPushRows = n
}

// SetDisconnectAfter 推送 N 条后主动关闭连接（模拟断线），0 表示不断
func (s *TushareWsMockServer) SetDisconnectAfter(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.disconnectAfter = n
}

// Start 启动 HTTP 服务，WebSocket 端点 /listening
func (s *TushareWsMockServer) Start() string {
	mux := http.NewServeMux()
	mux.HandleFunc("/listening", s.handleWebSocket)
	s.server = httptest.NewServer(mux)
	return s.server.URL
}

// Stop 停止服务
func (s *TushareWsMockServer) Stop() {
	if s.server != nil {
		s.server.Close()
	}
}

// URL 返回 base URL
func (s *TushareWsMockServer) URL() string {
	if s.server == nil {
		return ""
	}
	return s.server.URL
}

// WsURL 返回 WebSocket URL（ws://...）
func (s *TushareWsMockServer) WsURL() string {
	u := s.URL()
	if u == "" {
		return ""
	}
	if strings.HasPrefix(u, "https://") {
		return "wss" + u[5:]
	}
	return "ws" + u[4:]
}

var tushareUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// handleWebSocket 处理 /listening：升级后读首条 listening，再按间隔推送
func (s *TushareWsMockServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := tushareUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	var req TushareListeningRequest
	if err := conn.ReadJSON(&req); err != nil {
		return
	}
	if req.Action != "listening" {
		return
	}

	s.mu.RLock()
	msgs := make([]*TusharePushMessage, len(s.msgs))
	copy(msgs, s.msgs)
	interval := s.pushInterval
	maxPush := s.maxPushRows
	disconnectAfter := s.disconnectAfter
	s.mu.RUnlock()

	sent := 0
	for {
		if maxPush > 0 && sent >= maxPush {
			break
		}
		idx := sent % len(msgs)
		if err := conn.WriteJSON(msgs[idx]); err != nil {
			return
		}
		sent++
		if interval > 0 {
			time.Sleep(interval)
		}
		if disconnectAfter > 0 && sent >= disconnectAfter {
			_ = conn.WriteMessage(websocket.CloseMessage, nil)
			return
		}
	}
}
