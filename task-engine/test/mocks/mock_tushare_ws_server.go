// Package mocks 提供测试用的模拟服务器
package mocks

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// RealtimeQuote 实时行情数据（参考 Tushare ws_quote API）
// https://tushare.pro/document/2?doc_id=315
type RealtimeQuote struct {
	TSCode    string  `json:"ts_code"`   // 股票代码
	Name      string  `json:"name"`      // 股票名称
	Price     float64 `json:"price"`     // 现价
	Change    float64 `json:"change"`    // 涨跌
	PctChg    float64 `json:"pct_chg"`   // 涨跌幅
	Vol       int64   `json:"vol"`       // 成交量
	Amount    float64 `json:"amount"`    // 成交额
	High      float64 `json:"high"`      // 最高价
	Low       float64 `json:"low"`       // 最低价
	Open      float64 `json:"open"`      // 开盘价
	PreClose  float64 `json:"pre_close"` // 昨收价
	Timestamp int64   `json:"timestamp"` // 时间戳（毫秒）
}

// WSMessage WebSocket 消息结构
type WSMessage struct {
	Type    string      `json:"type"`    // 消息类型：subscribe/unsubscribe/quote/error/heartbeat
	Code    int         `json:"code"`    // 状态码
	Message string      `json:"message"` // 消息说明
	Data    interface{} `json:"data"`    // 数据内容
}

// MockTushareWSServer 模拟 Tushare WebSocket 服务器
type MockTushareWSServer struct {
	server   *httptest.Server
	upgrader websocket.Upgrader

	clients    sync.Map // 连接 -> 订阅的股票代码列表
	stockCodes []string // 支持的股票代码列表

	// 控制选项
	pushInterval     time.Duration // 推送间隔
	simulateFailure  bool          // 是否模拟失败
	failureAfter     int           // 发送多少条消息后失败
	reconnectAllowed bool          // 是否允许重连

	// 统计
	totalMessagesSent int64
	totalConnections  int64
	currentClients    int32

	// 控制通道
	stopChan chan struct{}
	mu       sync.RWMutex
}

// NewMockTushareWSServer 创建模拟服务器
func NewMockTushareWSServer() *MockTushareWSServer {
	return &MockTushareWSServer{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
		stockCodes: []string{
			"000001.SZ", "000002.SZ", "600000.SH", "600036.SH",
			"000858.SZ", "002415.SZ", "600519.SH", "601318.SH",
		},
		pushInterval:     100 * time.Millisecond,
		reconnectAllowed: true,
		stopChan:         make(chan struct{}),
	}
}

// Start 启动服务器
func (s *MockTushareWSServer) Start() string {
	s.server = httptest.NewServer(http.HandlerFunc(s.handleWebSocket))
	// 将 http:// 转换为 ws://
	url := "ws" + s.server.URL[4:]
	log.Printf("MockTushareWSServer started at %s", url)
	return url
}

// Stop 停止服务器
func (s *MockTushareWSServer) Stop() {
	close(s.stopChan)
	if s.server != nil {
		s.server.Close()
	}
	log.Println("MockTushareWSServer stopped")
}

// SetPushInterval 设置推送间隔
func (s *MockTushareWSServer) SetPushInterval(interval time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pushInterval = interval
}

// SimulateFailure 模拟失败
func (s *MockTushareWSServer) SimulateFailure(failAfterMessages int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.simulateFailure = true
	s.failureAfter = failAfterMessages
}

// DisableFailure 禁用失败模拟
func (s *MockTushareWSServer) DisableFailure() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.simulateFailure = false
}

// SetReconnectAllowed 设置是否允许重连
func (s *MockTushareWSServer) SetReconnectAllowed(allowed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reconnectAllowed = allowed
}

// GetStats 获取统计信息
func (s *MockTushareWSServer) GetStats() (totalMessages, totalConnections int64, currentClients int32) {
	return atomic.LoadInt64(&s.totalMessagesSent),
		atomic.LoadInt64(&s.totalConnections),
		atomic.LoadInt32(&s.currentClients)
}

// handleWebSocket 处理 WebSocket 连接
func (s *MockTushareWSServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	reconnectAllowed := s.reconnectAllowed
	s.mu.RUnlock()

	if !reconnectAllowed && atomic.LoadInt64(&s.totalConnections) > 0 {
		http.Error(w, "Reconnection not allowed", http.StatusForbidden)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	atomic.AddInt64(&s.totalConnections, 1)
	atomic.AddInt32(&s.currentClients, 1)
	defer atomic.AddInt32(&s.currentClients, -1)

	// 存储客户端订阅信息
	subscribedStocks := make(map[string]bool)
	s.clients.Store(conn, &subscribedStocks)
	defer s.clients.Delete(conn)

	// 发送连接成功消息
	s.sendMessage(conn, &WSMessage{
		Type:    "connected",
		Code:    200,
		Message: "Connection established",
	})

	// 启动消息处理
	go s.handleMessages(conn, &subscribedStocks)

	// 启动行情推送
	s.pushQuotes(conn, &subscribedStocks)
}

// handleMessages 处理客户端消息
func (s *MockTushareWSServer) handleMessages(conn *websocket.Conn, subscribedStocks *map[string]bool) {
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			}
			return
		}

		var msg WSMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			s.sendMessage(conn, &WSMessage{
				Type:    "error",
				Code:    400,
				Message: "Invalid message format",
			})
			continue
		}

		switch msg.Type {
		case "subscribe":
			s.handleSubscribe(conn, subscribedStocks, msg.Data)
		case "unsubscribe":
			s.handleUnsubscribe(conn, subscribedStocks, msg.Data)
		case "heartbeat":
			s.sendMessage(conn, &WSMessage{
				Type:    "heartbeat",
				Code:    200,
				Message: "pong",
			})
		}
	}
}

// handleSubscribe 处理订阅请求
func (s *MockTushareWSServer) handleSubscribe(conn *websocket.Conn, subscribedStocks *map[string]bool, data interface{}) {
	codes, ok := data.([]interface{})
	if !ok {
		// 尝试单个代码
		if code, ok := data.(string); ok {
			codes = []interface{}{code}
		} else {
			s.sendMessage(conn, &WSMessage{
				Type:    "error",
				Code:    400,
				Message: "Invalid subscribe data",
			})
			return
		}
	}

	for _, c := range codes {
		if code, ok := c.(string); ok {
			(*subscribedStocks)[code] = true
		}
	}

	s.sendMessage(conn, &WSMessage{
		Type:    "subscribed",
		Code:    200,
		Message: fmt.Sprintf("Subscribed to %d stocks", len(codes)),
		Data:    codes,
	})
}

// handleUnsubscribe 处理取消订阅请求
func (s *MockTushareWSServer) handleUnsubscribe(conn *websocket.Conn, subscribedStocks *map[string]bool, data interface{}) {
	codes, ok := data.([]interface{})
	if !ok {
		return
	}

	for _, c := range codes {
		if code, ok := c.(string); ok {
			delete(*subscribedStocks, code)
		}
	}

	s.sendMessage(conn, &WSMessage{
		Type:    "unsubscribed",
		Code:    200,
		Message: fmt.Sprintf("Unsubscribed from %d stocks", len(codes)),
	})
}

// pushQuotes 推送行情数据
func (s *MockTushareWSServer) pushQuotes(conn *websocket.Conn, subscribedStocks *map[string]bool) {
	messageCount := 0

	for {
		select {
		case <-s.stopChan:
			return
		default:
		}

		s.mu.RLock()
		interval := s.pushInterval
		simulateFailure := s.simulateFailure
		failureAfter := s.failureAfter
		s.mu.RUnlock()

		// 检查是否需要模拟失败
		if simulateFailure && messageCount >= failureAfter {
			conn.Close()
			return
		}

		time.Sleep(interval)

		// 生成并推送行情数据
		quotes := s.generateQuotes(subscribedStocks)
		if len(quotes) == 0 {
			// 如果没有订阅，使用默认股票
			quotes = s.generateQuotes(&map[string]bool{
				"000001.SZ": true,
				"600000.SH": true,
			})
		}

		for _, quote := range quotes {
			if err := s.sendMessage(conn, &WSMessage{
				Type: "quote",
				Code: 200,
				Data: quote,
			}); err != nil {
				return
			}
			atomic.AddInt64(&s.totalMessagesSent, 1)
			messageCount++
		}
	}
}

// generateQuotes 生成行情数据
func (s *MockTushareWSServer) generateQuotes(subscribedStocks *map[string]bool) []RealtimeQuote {
	var quotes []RealtimeQuote

	stockNames := map[string]string{
		"000001.SZ": "平安银行",
		"000002.SZ": "万科A",
		"600000.SH": "浦发银行",
		"600036.SH": "招商银行",
		"000858.SZ": "五粮液",
		"002415.SZ": "海康威视",
		"600519.SH": "贵州茅台",
		"601318.SH": "中国平安",
	}

	basePrices := map[string]float64{
		"000001.SZ": 12.50,
		"000002.SZ": 15.80,
		"600000.SH": 8.20,
		"600036.SH": 35.60,
		"000858.SZ": 165.00,
		"002415.SZ": 32.50,
		"600519.SH": 1650.00,
		"601318.SH": 48.80,
	}

	for code := range *subscribedStocks {
		name, exists := stockNames[code]
		if !exists {
			name = "未知"
		}

		basePrice, exists := basePrices[code]
		if !exists {
			basePrice = 10.0
		}

		// 生成随机波动
		change := (rand.Float64() - 0.5) * rand.NormFloat64() * basePrice // ±1% 波动
		price := basePrice + change
		pctChg := (change / basePrice) * 100

		quotes = append(quotes, RealtimeQuote{
			TSCode:    code,
			Name:      name,
			Price:     price,
			Change:    change,
			PctChg:    pctChg,
			Vol:       rand.Int63n(1000000) + 100000,
			Amount:    float64(rand.Int63n(100000000)) + 10000000,
			High:      price + rand.Float64()*0.1,
			Low:       price - rand.Float64()*0.1,
			Open:      basePrice + (rand.Float64()-0.5)*0.01*basePrice,
			PreClose:  basePrice,
			Timestamp: time.Now().UnixMilli(),
		})
	}

	return quotes
}

// sendMessage 发送消息
func (s *MockTushareWSServer) sendMessage(conn *websocket.Conn, msg *WSMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}

// URL 获取服务器 URL
func (s *MockTushareWSServer) URL() string {
	if s.server == nil {
		return ""
	}
	return "ws" + s.server.URL[4:]
}
