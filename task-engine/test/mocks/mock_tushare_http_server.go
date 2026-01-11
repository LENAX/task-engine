// Package mocks 提供测试用的模拟服务器
package mocks

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"time"
)

// RealtimeTick 实时成交数据（参考 Tushare realtime_tick API）
// https://tushare.pro/document/2?doc_id=316
type RealtimeTick struct {
	Time   string  `json:"time"`   // 交易时间
	Price  float64 `json:"price"`  // 现价
	Change float64 `json:"change"` // 价格变动
	Volume int     `json:"volume"` // 成交量（单位：手）
	Amount int     `json:"amount"` // 成交金额（元）
	Type   string  `json:"type"`   // 类型：买入/卖出/中性
}

// RealtimeQuoteHTTP HTTP 轮询的实时行情数据
// 参考 Tushare realtime_quote API 的字段
type RealtimeQuoteHTTP struct {
	TSCode   string  `json:"ts_code"`   // 股票代码
	Name     string  `json:"name"`      // 股票名称
	Open     float64 `json:"open"`      // 今日开盘价
	PreClose float64 `json:"pre_close"` // 昨日收盘价
	Price    float64 `json:"price"`     // 当前价格
	High     float64 `json:"high"`      // 今日最高价
	Low      float64 `json:"low"`       // 今日最低价
	Bid      float64 `json:"bid"`       // 竞买价
	Ask      float64 `json:"ask"`       // 竞卖价
	Volume   int64   `json:"volume"`    // 成交量
	Amount   float64 `json:"amount"`    // 成交金额
	Date     string  `json:"date"`      // 日期
	Time     string  `json:"time"`      // 时间
	// 五档买卖盘
	B1V int     `json:"b1_v"` // 买一量
	B1P float64 `json:"b1_p"` // 买一价
	B2V int     `json:"b2_v"` // 买二量
	B2P float64 `json:"b2_p"` // 买二价
	B3V int     `json:"b3_v"` // 买三量
	B3P float64 `json:"b3_p"` // 买三价
	B4V int     `json:"b4_v"` // 买四量
	B4P float64 `json:"b4_p"` // 买四价
	B5V int     `json:"b5_v"` // 买五量
	B5P float64 `json:"b5_p"` // 买五价
	A1V int     `json:"a1_v"` // 卖一量
	A1P float64 `json:"a1_p"` // 卖一价
	A2V int     `json:"a2_v"` // 卖二量
	A2P float64 `json:"a2_p"` // 卖二价
	A3V int     `json:"a3_v"` // 卖三量
	A3P float64 `json:"a3_p"` // 卖三价
	A4V int     `json:"a4_v"` // 卖四量
	A4P float64 `json:"a4_p"` // 卖四价
	A5V int     `json:"a5_v"` // 卖五量
	A5P float64 `json:"a5_p"` // 卖五价
}

// HTTPResponse API 响应结构
type HTTPResponse struct {
	Code    int         `json:"code"`    // 状态码
	Message string      `json:"message"` // 消息说明
	Data    interface{} `json:"data"`    // 数据内容
}

// MockTushareHTTPServer 模拟 Tushare HTTP API 服务器
type MockTushareHTTPServer struct {
	server *httptest.Server

	// 股票基础数据
	stockData map[string]*stockState

	// 控制选项
	responseDelay    time.Duration // 响应延迟
	simulateFailure  bool          // 是否模拟失败
	failureRate      float64       // 失败率 (0.0-1.0)
	errorCode        int           // 失败时的错误码
	errorMessage     string        // 失败时的错误消息
	maxTicksPerCall  int           // 每次调用返回的最大 tick 数

	// 统计
	totalRequests  int64
	failedRequests int64

	mu sync.RWMutex
}

// stockState 股票状态
type stockState struct {
	Name         string
	BasePrice    float64
	CurrentPrice float64
	Open         float64
	High         float64
	Low          float64
	PreClose     float64
	Volume       int64
	Amount       float64
	TickSequence int // tick 序列号
}

// NewMockTushareHTTPServer 创建模拟 HTTP 服务器
func NewMockTushareHTTPServer() *MockTushareHTTPServer {
	s := &MockTushareHTTPServer{
		stockData:       make(map[string]*stockState),
		maxTicksPerCall: 100,
	}

	// 初始化股票数据
	s.initStockData()

	return s
}

// initStockData 初始化股票数据
func (s *MockTushareHTTPServer) initStockData() {
	stocks := map[string]struct {
		name  string
		price float64
	}{
		"000001.SZ": {"平安银行", 12.50},
		"000002.SZ": {"万科A", 15.80},
		"600000.SH": {"浦发银行", 8.20},
		"600036.SH": {"招商银行", 35.60},
		"000858.SZ": {"五粮液", 165.00},
		"002415.SZ": {"海康威视", 32.50},
		"600519.SH": {"贵州茅台", 1650.00},
		"601318.SH": {"中国平安", 48.80},
	}

	for code, info := range stocks {
		s.stockData[code] = &stockState{
			Name:         info.name,
			BasePrice:    info.price,
			CurrentPrice: info.price,
			Open:         info.price * (1 + (rand.Float64()-0.5)*0.01),
			High:         info.price * (1 + rand.Float64()*0.02),
			Low:          info.price * (1 - rand.Float64()*0.02),
			PreClose:     info.price,
			Volume:       0,
			Amount:       0,
			TickSequence: 0,
		}
	}
}

// Start 启动服务器
func (s *MockTushareHTTPServer) Start() string {
	mux := http.NewServeMux()

	// 实时行情接口 - 单个/多个股票
	mux.HandleFunc("/api/realtime_quote", s.handleRealtimeQuote)

	// 实时成交数据接口
	mux.HandleFunc("/api/realtime_tick", s.handleRealtimeTick)

	// 批量行情接口 - 所有 A 股
	mux.HandleFunc("/api/realtime_list", s.handleRealtimeList)

	// 健康检查
	mux.HandleFunc("/health", s.handleHealth)

	s.server = httptest.NewServer(mux)
	return s.server.URL
}

// Stop 停止服务器
func (s *MockTushareHTTPServer) Stop() {
	if s.server != nil {
		s.server.Close()
	}
}

// URL 获取服务器 URL
func (s *MockTushareHTTPServer) URL() string {
	if s.server == nil {
		return ""
	}
	return s.server.URL
}

// SetResponseDelay 设置响应延迟
func (s *MockTushareHTTPServer) SetResponseDelay(delay time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responseDelay = delay
}

// SimulateFailure 模拟失败
func (s *MockTushareHTTPServer) SimulateFailure(rate float64, code int, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.simulateFailure = true
	s.failureRate = rate
	s.errorCode = code
	s.errorMessage = message
}

// DisableFailure 禁用失败模拟
func (s *MockTushareHTTPServer) DisableFailure() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.simulateFailure = false
}

// SetMaxTicksPerCall 设置每次调用返回的最大 tick 数
func (s *MockTushareHTTPServer) SetMaxTicksPerCall(max int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxTicksPerCall = max
}

// GetStats 获取统计信息
func (s *MockTushareHTTPServer) GetStats() (totalRequests, failedRequests int64) {
	return atomic.LoadInt64(&s.totalRequests), atomic.LoadInt64(&s.failedRequests)
}

// handleRealtimeQuote 处理实时行情请求
// 参考 Tushare realtime_quote 接口
// GET /api/realtime_quote?ts_code=000001.SZ,600000.SH&src=sina
func (s *MockTushareHTTPServer) handleRealtimeQuote(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&s.totalRequests, 1)

	s.mu.RLock()
	delay := s.responseDelay
	simulateFailure := s.simulateFailure
	failureRate := s.failureRate
	errorCode := s.errorCode
	errorMessage := s.errorMessage
	s.mu.RUnlock()

	// 模拟延迟
	if delay > 0 {
		time.Sleep(delay)
	}

	// 模拟失败
	if simulateFailure && rand.Float64() < failureRate {
		atomic.AddInt64(&s.failedRequests, 1)
		s.sendError(w, errorCode, errorMessage)
		return
	}

	// 获取请求参数
	tsCodes := r.URL.Query().Get("ts_code")
	if tsCodes == "" {
		s.sendError(w, 400, "ts_code is required")
		return
	}

	// 解析股票代码
	var quotes []RealtimeQuoteHTTP
	codes := splitCodes(tsCodes)

	now := time.Now()
	dateStr := now.Format("20060102")
	timeStr := now.Format("15:04:05")

	s.mu.Lock()
	for _, code := range codes {
		state, exists := s.stockData[code]
		if !exists {
			continue
		}

		// 更新价格（模拟价格波动）
		change := (rand.Float64() - 0.5) * 0.002 * state.BasePrice
		state.CurrentPrice += change
		if state.CurrentPrice > state.High {
			state.High = state.CurrentPrice
		}
		if state.CurrentPrice < state.Low {
			state.Low = state.CurrentPrice
		}
		state.Volume += rand.Int63n(10000)
		state.Amount += float64(rand.Int63n(1000000))

		quote := s.buildQuote(code, state, dateStr, timeStr)
		quotes = append(quotes, quote)
	}
	s.mu.Unlock()

	s.sendSuccess(w, quotes)
}

// handleRealtimeTick 处理实时成交数据请求
// 参考 Tushare realtime_tick 接口
// GET /api/realtime_tick?ts_code=600000.SH&src=sina
func (s *MockTushareHTTPServer) handleRealtimeTick(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&s.totalRequests, 1)

	s.mu.RLock()
	delay := s.responseDelay
	simulateFailure := s.simulateFailure
	failureRate := s.failureRate
	errorCode := s.errorCode
	errorMessage := s.errorMessage
	maxTicks := s.maxTicksPerCall
	s.mu.RUnlock()

	// 模拟延迟
	if delay > 0 {
		time.Sleep(delay)
	}

	// 模拟失败
	if simulateFailure && rand.Float64() < failureRate {
		atomic.AddInt64(&s.failedRequests, 1)
		s.sendError(w, errorCode, errorMessage)
		return
	}

	// 获取请求参数
	tsCode := r.URL.Query().Get("ts_code")
	if tsCode == "" {
		s.sendError(w, 400, "ts_code is required")
		return
	}

	s.mu.Lock()
	state, exists := s.stockData[tsCode]
	if !exists {
		s.mu.Unlock()
		s.sendError(w, 404, "Stock not found")
		return
	}

	// 生成 tick 数据
	var ticks []RealtimeTick
	baseTime := time.Now()

	for i := 0; i < maxTicks; i++ {
		state.TickSequence++

		// 模拟价格变动
		change := (rand.Float64() - 0.5) * 0.001 * state.BasePrice
		state.CurrentPrice += change

		// 生成成交量
		volume := rand.Intn(500) + 1 // 1-500 手
		amount := int(state.CurrentPrice * float64(volume) * 100)

		// 确定买卖类型
		var tickType string
		if change > 0 {
			tickType = "买盘"
		} else if change < 0 {
			tickType = "卖盘"
		} else {
			tickType = "中性"
		}

		tick := RealtimeTick{
			Time:   baseTime.Add(-time.Duration(maxTicks-i) * time.Second).Format("15:04:05"),
			Price:  state.CurrentPrice,
			Change: change,
			Volume: volume,
			Amount: amount,
			Type:   tickType,
		}
		ticks = append(ticks, tick)
	}
	s.mu.Unlock()

	s.sendSuccess(w, ticks)
}

// handleRealtimeList 处理批量行情请求
// 参考 Tushare realtime_list 接口
// GET /api/realtime_list?src=dc&page=1
func (s *MockTushareHTTPServer) handleRealtimeList(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&s.totalRequests, 1)

	s.mu.RLock()
	delay := s.responseDelay
	simulateFailure := s.simulateFailure
	failureRate := s.failureRate
	errorCode := s.errorCode
	errorMessage := s.errorMessage
	s.mu.RUnlock()

	// 模拟延迟
	if delay > 0 {
		time.Sleep(delay)
	}

	// 模拟失败
	if simulateFailure && rand.Float64() < failureRate {
		atomic.AddInt64(&s.failedRequests, 1)
		s.sendError(w, errorCode, errorMessage)
		return
	}

	// 生成所有股票的行情数据
	var quotes []RealtimeQuoteHTTP
	now := time.Now()
	dateStr := now.Format("20060102")
	timeStr := now.Format("15:04:05")

	s.mu.Lock()
	for code, state := range s.stockData {
		// 更新价格
		change := (rand.Float64() - 0.5) * 0.002 * state.BasePrice
		state.CurrentPrice += change
		if state.CurrentPrice > state.High {
			state.High = state.CurrentPrice
		}
		if state.CurrentPrice < state.Low {
			state.Low = state.CurrentPrice
		}
		state.Volume += rand.Int63n(10000)
		state.Amount += float64(rand.Int63n(1000000))

		quote := s.buildQuote(code, state, dateStr, timeStr)
		quotes = append(quotes, quote)
	}
	s.mu.Unlock()

	s.sendSuccess(w, quotes)
}

// handleHealth 健康检查
func (s *MockTushareHTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// buildQuote 构建行情数据
func (s *MockTushareHTTPServer) buildQuote(code string, state *stockState, dateStr, timeStr string) RealtimeQuoteHTTP {
	price := state.CurrentPrice
	return RealtimeQuoteHTTP{
		TSCode:   code,
		Name:     state.Name,
		Open:     state.Open,
		PreClose: state.PreClose,
		Price:    price,
		High:     state.High,
		Low:      state.Low,
		Bid:      price - 0.01,
		Ask:      price + 0.01,
		Volume:   state.Volume,
		Amount:   state.Amount,
		Date:     dateStr,
		Time:     timeStr,
		B1V:      rand.Intn(1000) + 100,
		B1P:      price - 0.01,
		B2V:      rand.Intn(1000) + 100,
		B2P:      price - 0.02,
		B3V:      rand.Intn(1000) + 100,
		B3P:      price - 0.03,
		B4V:      rand.Intn(1000) + 100,
		B4P:      price - 0.04,
		B5V:      rand.Intn(1000) + 100,
		B5P:      price - 0.05,
		A1V:      rand.Intn(1000) + 100,
		A1P:      price + 0.01,
		A2V:      rand.Intn(1000) + 100,
		A2P:      price + 0.02,
		A3V:      rand.Intn(1000) + 100,
		A3P:      price + 0.03,
		A4V:      rand.Intn(1000) + 100,
		A4P:      price + 0.04,
		A5V:      rand.Intn(1000) + 100,
		A5P:      price + 0.05,
	}
}

// sendSuccess 发送成功响应
func (s *MockTushareHTTPServer) sendSuccess(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(HTTPResponse{
		Code:    200,
		Message: "success",
		Data:    data,
	})
}

// sendError 发送错误响应
func (s *MockTushareHTTPServer) sendError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	status := http.StatusInternalServerError
	if code == 400 {
		status = http.StatusBadRequest
	} else if code == 404 {
		status = http.StatusNotFound
	} else if code == 429 {
		status = http.StatusTooManyRequests
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(HTTPResponse{
		Code:    code,
		Message: message,
		Data:    nil,
	})
}

// splitCodes 分割股票代码
func splitCodes(codes string) []string {
	var result []string
	current := ""
	for _, c := range codes {
		if c == ',' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

// ResetStats 重置统计信息
func (s *MockTushareHTTPServer) ResetStats() {
	atomic.StoreInt64(&s.totalRequests, 0)
	atomic.StoreInt64(&s.failedRequests, 0)
}

// ResetStockData 重置股票数据
func (s *MockTushareHTTPServer) ResetStockData() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initStockData()
}

// AddStock 添加自定义股票
func (s *MockTushareHTTPServer) AddStock(code, name string, basePrice float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stockData[code] = &stockState{
		Name:         name,
		BasePrice:    basePrice,
		CurrentPrice: basePrice,
		Open:         basePrice * (1 + (rand.Float64()-0.5)*0.01),
		High:         basePrice * (1 + rand.Float64()*0.02),
		Low:          basePrice * (1 - rand.Float64()*0.02),
		PreClose:     basePrice,
		Volume:       0,
		Amount:       0,
		TickSequence: 0,
	}
}

// GetStockPrice 获取当前股价（用于测试验证）
func (s *MockTushareHTTPServer) GetStockPrice(code string) (float64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if state, exists := s.stockData[code]; exists {
		return state.CurrentPrice, true
	}
	return 0, false
}

// MockTusharePollingClient 模拟 Tushare HTTP 轮询客户端
// 用于集成测试
type MockTusharePollingClient struct {
	serverURL     string
	pollInterval  time.Duration
	stopChan      chan struct{}
	dataHandler   func([]RealtimeQuoteHTTP)
	tickHandler   func([]RealtimeTick)
	errorHandler  func(error)
	subscribeCodes []string
	running       int32
	mu            sync.RWMutex
}

// NewMockTusharePollingClient 创建轮询客户端
func NewMockTusharePollingClient(serverURL string, pollInterval time.Duration) *MockTusharePollingClient {
	return &MockTusharePollingClient{
		serverURL:    serverURL,
		pollInterval: pollInterval,
		stopChan:     make(chan struct{}),
	}
}

// Subscribe 订阅股票代码
func (c *MockTusharePollingClient) Subscribe(codes []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subscribeCodes = codes
}

// OnData 设置数据处理回调
func (c *MockTusharePollingClient) OnData(handler func([]RealtimeQuoteHTTP)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dataHandler = handler
}

// OnTick 设置 tick 数据处理回调
func (c *MockTusharePollingClient) OnTick(handler func([]RealtimeTick)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tickHandler = handler
}

// OnError 设置错误处理回调
func (c *MockTusharePollingClient) OnError(handler func(error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errorHandler = handler
}

// Start 启动轮询
func (c *MockTusharePollingClient) Start() {
	if !atomic.CompareAndSwapInt32(&c.running, 0, 1) {
		return // 已经在运行
	}

	go c.pollLoop()
}

// Stop 停止轮询
func (c *MockTusharePollingClient) Stop() {
	if atomic.CompareAndSwapInt32(&c.running, 1, 0) {
		close(c.stopChan)
	}
}

// pollLoop 轮询循环
func (c *MockTusharePollingClient) pollLoop() {
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopChan:
			return
		case <-ticker.C:
			c.poll()
		}
	}
}

// poll 执行一次轮询
func (c *MockTusharePollingClient) poll() {
	c.mu.RLock()
	codes := c.subscribeCodes
	dataHandler := c.dataHandler
	errorHandler := c.errorHandler
	c.mu.RUnlock()

	if len(codes) == 0 {
		return
	}

	// 构建请求 URL
	codesStr := ""
	for i, code := range codes {
		if i > 0 {
			codesStr += ","
		}
		codesStr += code
	}

	url := fmt.Sprintf("%s/api/realtime_quote?ts_code=%s", c.serverURL, codesStr)

	// 发送请求
	resp, err := http.Get(url)
	if err != nil {
		if errorHandler != nil {
			errorHandler(err)
		}
		return
	}
	defer resp.Body.Close()

	// 解析响应
	var result HTTPResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		if errorHandler != nil {
			errorHandler(err)
		}
		return
	}

	if result.Code != 200 {
		if errorHandler != nil {
			errorHandler(fmt.Errorf("API error: %s", result.Message))
		}
		return
	}

	// 解析数据
	if dataHandler != nil && result.Data != nil {
		dataBytes, _ := json.Marshal(result.Data)
		var quotes []RealtimeQuoteHTTP
		if err := json.Unmarshal(dataBytes, &quotes); err == nil {
			dataHandler(quotes)
		}
	}
}

// IsRunning 检查是否正在运行
func (c *MockTusharePollingClient) IsRunning() bool {
	return atomic.LoadInt32(&c.running) == 1
}

