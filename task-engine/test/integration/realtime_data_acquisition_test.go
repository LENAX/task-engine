// Package integration 包含集成测试
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stevelan1995/task-engine/test/mocks"
)

// TestMockTushareWSServer_BasicConnection 测试 WebSocket 服务器基本连接
func TestMockTushareWSServer_BasicConnection(t *testing.T) {
	// 启动 mock WebSocket 服务器
	server := mocks.NewMockTushareWSServer()
	wsURL := server.Start()
	defer server.Stop()

	// 连接到服务器
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// 读取连接成功消息
	_, message, err := conn.ReadMessage()
	require.NoError(t, err)

	var msg mocks.WSMessage
	err = json.Unmarshal(message, &msg)
	require.NoError(t, err)

	assert.Equal(t, "connected", msg.Type)
	assert.Equal(t, 200, msg.Code)
	t.Logf("收到连接消息: %s", msg.Message)
}

// TestMockTushareWSServer_Subscribe 测试订阅股票
func TestMockTushareWSServer_Subscribe(t *testing.T) {
	server := mocks.NewMockTushareWSServer()
	wsURL := server.Start()
	defer server.Stop()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// 读取连接消息
	conn.ReadMessage()

	// 发送订阅请求
	subscribeMsg := mocks.WSMessage{
		Type: "subscribe",
		Data: []string{"000001.SZ", "600000.SH"},
	}
	msgData, _ := json.Marshal(subscribeMsg)
	err = conn.WriteMessage(websocket.TextMessage, msgData)
	require.NoError(t, err)

	// 读取订阅确认
	_, message, err := conn.ReadMessage()
	require.NoError(t, err)

	var msg mocks.WSMessage
	err = json.Unmarshal(message, &msg)
	require.NoError(t, err)

	assert.Equal(t, "subscribed", msg.Type)
	assert.Equal(t, 200, msg.Code)
	t.Logf("订阅成功: %s", msg.Message)
}

// TestMockTushareWSServer_ReceiveQuotes 测试接收行情数据
func TestMockTushareWSServer_ReceiveQuotes(t *testing.T) {
	server := mocks.NewMockTushareWSServer()
	server.SetPushInterval(50 * time.Millisecond)
	wsURL := server.Start()
	defer server.Stop()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// 读取连接消息
	conn.ReadMessage()

	// 订阅股票
	subscribeMsg := mocks.WSMessage{
		Type: "subscribe",
		Data: []string{"000001.SZ"},
	}
	msgData, _ := json.Marshal(subscribeMsg)
	conn.WriteMessage(websocket.TextMessage, msgData)
	conn.ReadMessage() // 读取订阅确认

	// 收集行情数据
	var quotes []mocks.RealtimeQuote
	timeout := time.After(500 * time.Millisecond)

	for {
		select {
		case <-timeout:
			goto done
		default:
			conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			_, message, err := conn.ReadMessage()
			if err != nil {
				continue
			}

			var msg mocks.WSMessage
			if err := json.Unmarshal(message, &msg); err != nil {
				continue
			}

			if msg.Type == "quote" {
				// 解析行情数据
				quoteData, _ := json.Marshal(msg.Data)
				var quote mocks.RealtimeQuote
				if err := json.Unmarshal(quoteData, &quote); err == nil {
					quotes = append(quotes, quote)
				}
			}
		}
	}
done:

	assert.Greater(t, len(quotes), 0, "应该收到行情数据")
	t.Logf("收到 %d 条行情数据", len(quotes))

	// 验证行情数据字段
	if len(quotes) > 0 {
		quote := quotes[0]
		assert.Equal(t, "000001.SZ", quote.TSCode)
		assert.Equal(t, "平安银行", quote.Name)
		assert.Greater(t, quote.Price, 0.0)
		assert.Greater(t, quote.Timestamp, int64(0))
		t.Logf("第一条行情: 代码=%s, 价格=%.2f, 涨跌幅=%.2f%%", quote.TSCode, quote.Price, quote.PctChg)
	}
}

// TestMockTushareWSServer_SimulateDisconnect 测试模拟断连
func TestMockTushareWSServer_SimulateDisconnect(t *testing.T) {
	server := mocks.NewMockTushareWSServer()
	server.SetPushInterval(50 * time.Millisecond)
	server.SimulateFailure(5) // 发送 5 条消息后断开
	wsURL := server.Start()
	defer server.Stop()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// 读取消息直到断开
	messageCount := 0
	for {
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
		messageCount++
	}

	// 应该在收到一些消息后断开
	t.Logf("连接断开前收到 %d 条消息", messageCount)
	assert.Greater(t, messageCount, 0)
}

// TestMockTushareWSServer_Reconnect 测试重连
func TestMockTushareWSServer_Reconnect(t *testing.T) {
	server := mocks.NewMockTushareWSServer()
	server.SetPushInterval(50 * time.Millisecond)
	server.SimulateFailure(3) // 发送 3 条消息后断开
	wsURL := server.Start()
	defer server.Stop()

	// 第一次连接
	conn1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)

	// 读取直到断开
	for {
		conn1.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, _, err := conn1.ReadMessage()
		if err != nil {
			break
		}
	}
	conn1.Close()

	// 禁用失败模拟以允许重连
	server.DisableFailure()

	// 第二次连接（重连）
	conn2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn2.Close()

	// 读取连接消息
	_, message, err := conn2.ReadMessage()
	require.NoError(t, err)

	var msg mocks.WSMessage
	json.Unmarshal(message, &msg)
	assert.Equal(t, "connected", msg.Type)

	// 验证统计信息
	totalMessages, totalConnections, _ := server.GetStats()
	assert.Equal(t, int64(2), totalConnections, "应该有 2 次连接")
	t.Logf("总连接数: %d, 总消息数: %d", totalConnections, totalMessages)
}

// TestMockTushareHTTPServer_RealtimeQuote 测试 HTTP 实时行情接口
func TestMockTushareHTTPServer_RealtimeQuote(t *testing.T) {
	server := mocks.NewMockTushareHTTPServer()
	serverURL := server.Start()
	defer server.Stop()

	// 请求单个股票行情
	resp, err := http.Get(fmt.Sprintf("%s/api/realtime_quote?ts_code=000001.SZ", serverURL))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result mocks.HTTPResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, 200, result.Code)
	assert.Equal(t, "success", result.Message)

	// 解析行情数据
	dataBytes, _ := json.Marshal(result.Data)
	var quotes []mocks.RealtimeQuoteHTTP
	err = json.Unmarshal(dataBytes, &quotes)
	require.NoError(t, err)

	assert.Len(t, quotes, 1)
	quote := quotes[0]
	assert.Equal(t, "000001.SZ", quote.TSCode)
	assert.Equal(t, "平安银行", quote.Name)
	assert.Greater(t, quote.Price, 0.0)
	t.Logf("行情数据: 代码=%s, 名称=%s, 价格=%.2f", quote.TSCode, quote.Name, quote.Price)
}

// TestMockTushareHTTPServer_MultipleStocks 测试多股票行情
func TestMockTushareHTTPServer_MultipleStocks(t *testing.T) {
	server := mocks.NewMockTushareHTTPServer()
	serverURL := server.Start()
	defer server.Stop()

	// 请求多个股票行情
	resp, err := http.Get(fmt.Sprintf("%s/api/realtime_quote?ts_code=000001.SZ,600000.SH,600519.SH", serverURL))
	require.NoError(t, err)
	defer resp.Body.Close()

	var result mocks.HTTPResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	dataBytes, _ := json.Marshal(result.Data)
	var quotes []mocks.RealtimeQuoteHTTP
	json.Unmarshal(dataBytes, &quotes)

	assert.Len(t, quotes, 3)

	// 验证每个股票
	codes := make(map[string]bool)
	for _, q := range quotes {
		codes[q.TSCode] = true
		t.Logf("股票: %s (%s), 价格: %.2f", q.TSCode, q.Name, q.Price)
	}

	assert.True(t, codes["000001.SZ"])
	assert.True(t, codes["600000.SH"])
	assert.True(t, codes["600519.SH"])
}

// TestMockTushareHTTPServer_RealtimeTick 测试实时成交数据
func TestMockTushareHTTPServer_RealtimeTick(t *testing.T) {
	server := mocks.NewMockTushareHTTPServer()
	server.SetMaxTicksPerCall(50)
	serverURL := server.Start()
	defer server.Stop()

	resp, err := http.Get(fmt.Sprintf("%s/api/realtime_tick?ts_code=600000.SH", serverURL))
	require.NoError(t, err)
	defer resp.Body.Close()

	var result mocks.HTTPResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, 200, result.Code)

	dataBytes, _ := json.Marshal(result.Data)
	var ticks []mocks.RealtimeTick
	json.Unmarshal(dataBytes, &ticks)

	assert.Len(t, ticks, 50)

	// 验证 tick 数据
	for i, tick := range ticks[:5] {
		t.Logf("Tick %d: 时间=%s, 价格=%.2f, 成交量=%d, 类型=%s",
			i, tick.Time, tick.Price, tick.Volume, tick.Type)
		assert.NotEmpty(t, tick.Time)
		assert.Greater(t, tick.Price, 0.0)
		assert.Greater(t, tick.Volume, 0)
	}
}

// TestMockTushareHTTPServer_RealtimeList 测试批量行情接口
func TestMockTushareHTTPServer_RealtimeList(t *testing.T) {
	server := mocks.NewMockTushareHTTPServer()
	serverURL := server.Start()
	defer server.Stop()

	resp, err := http.Get(fmt.Sprintf("%s/api/realtime_list", serverURL))
	require.NoError(t, err)
	defer resp.Body.Close()

	var result mocks.HTTPResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, 200, result.Code)

	dataBytes, _ := json.Marshal(result.Data)
	var quotes []mocks.RealtimeQuoteHTTP
	json.Unmarshal(dataBytes, &quotes)

	// 应该返回所有预设的股票
	assert.GreaterOrEqual(t, len(quotes), 8)
	t.Logf("批量行情返回 %d 只股票", len(quotes))

	for _, q := range quotes {
		t.Logf("股票: %s (%s), 价格: %.2f, 成交量: %d",
			q.TSCode, q.Name, q.Price, q.Volume)
	}
}

// TestMockTushareHTTPServer_SimulateFailure 测试模拟失败
func TestMockTushareHTTPServer_SimulateFailure(t *testing.T) {
	server := mocks.NewMockTushareHTTPServer()
	server.SimulateFailure(1.0, 500, "Internal Server Error") // 100% 失败率
	serverURL := server.Start()
	defer server.Stop()

	resp, err := http.Get(fmt.Sprintf("%s/api/realtime_quote?ts_code=000001.SZ", serverURL))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	var result mocks.HTTPResponse
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, 500, result.Code)

	// 检查统计
	totalRequests, failedRequests := server.GetStats()
	assert.Equal(t, int64(1), totalRequests)
	assert.Equal(t, int64(1), failedRequests)
}

// TestMockTushareHTTPServer_ResponseDelay 测试响应延迟
func TestMockTushareHTTPServer_ResponseDelay(t *testing.T) {
	server := mocks.NewMockTushareHTTPServer()
	server.SetResponseDelay(100 * time.Millisecond)
	serverURL := server.Start()
	defer server.Stop()

	start := time.Now()
	resp, err := http.Get(fmt.Sprintf("%s/api/realtime_quote?ts_code=000001.SZ", serverURL))
	elapsed := time.Since(start)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond)
	t.Logf("请求耗时: %v (设置延迟 100ms)", elapsed)
}

// TestMockTusharePollingClient_ContinuousPolling 测试持续轮询
func TestMockTusharePollingClient_ContinuousPolling(t *testing.T) {
	// 启动 HTTP 服务器
	server := mocks.NewMockTushareHTTPServer()
	serverURL := server.Start()
	defer server.Stop()

	// 创建轮询客户端
	client := mocks.NewMockTusharePollingClient(serverURL, 100*time.Millisecond)
	client.Subscribe([]string{"000001.SZ", "600000.SH"})

	// 收集数据
	var receivedQuotes []mocks.RealtimeQuoteHTTP
	var mu sync.Mutex
	var dataCount int32

	client.OnData(func(quotes []mocks.RealtimeQuoteHTTP) {
		mu.Lock()
		defer mu.Unlock()
		receivedQuotes = append(receivedQuotes, quotes...)
		atomic.AddInt32(&dataCount, int32(len(quotes)))
	})

	client.OnError(func(err error) {
		t.Logf("轮询错误: %v", err)
	})

	// 启动轮询
	client.Start()

	// 等待一段时间收集数据
	time.Sleep(500 * time.Millisecond)

	// 停止轮询
	client.Stop()

	// 验证收到数据
	count := atomic.LoadInt32(&dataCount)
	assert.Greater(t, count, int32(0), "应该收到行情数据")
	t.Logf("轮询期间收到 %d 条行情数据", count)

	// 检查服务器统计
	totalRequests, _ := server.GetStats()
	assert.Greater(t, totalRequests, int64(0))
	t.Logf("服务器收到 %d 次请求", totalRequests)
}

// TestContinuousDataAcquisition_WebSocket 测试 WebSocket 持续数据采集
func TestContinuousDataAcquisition_WebSocket(t *testing.T) {
	// 启动 WebSocket 服务器
	server := mocks.NewMockTushareWSServer()
	server.SetPushInterval(50 * time.Millisecond)
	wsURL := server.Start()
	defer server.Stop()

	// 模拟持续数据采集
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// 连接
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// 读取连接消息
	conn.ReadMessage()

	// 订阅
	subscribeMsg := mocks.WSMessage{
		Type: "subscribe",
		Data: []string{"000001.SZ", "600000.SH", "600519.SH"},
	}
	msgData, _ := json.Marshal(subscribeMsg)
	conn.WriteMessage(websocket.TextMessage, msgData)
	conn.ReadMessage() // 订阅确认

	// 持续接收数据
	var totalQuotes int
	var totalVolume int64
	quoteChan := make(chan mocks.RealtimeQuote, 100)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
				_, message, err := conn.ReadMessage()
				if err != nil {
					continue
				}

				var msg mocks.WSMessage
				if err := json.Unmarshal(message, &msg); err != nil {
					continue
				}

				if msg.Type == "quote" {
					quoteData, _ := json.Marshal(msg.Data)
					var quote mocks.RealtimeQuote
					if err := json.Unmarshal(quoteData, &quote); err == nil {
						select {
						case quoteChan <- quote:
						default:
						}
					}
				}
			}
		}
	}()

	// 处理接收到的数据
	for {
		select {
		case <-ctx.Done():
			goto done
		case quote := <-quoteChan:
			totalQuotes++
			totalVolume += quote.Vol
		}
	}
done:

	t.Logf("持续采集结果: 总行情数=%d, 总成交量=%d", totalQuotes, totalVolume)
	assert.Greater(t, totalQuotes, 0)

	// 检查服务器统计
	totalMessages, _, currentClients := server.GetStats()
	t.Logf("服务器统计: 总消息数=%d, 当前客户端数=%d", totalMessages, currentClients)
}

// TestContinuousDataAcquisition_HTTP 测试 HTTP 持续数据采集
func TestContinuousDataAcquisition_HTTP(t *testing.T) {
	server := mocks.NewMockTushareHTTPServer()
	serverURL := server.Start()
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// 持续轮询采集
	var totalQuotes int
	var priceChanges []float64
	var lastPrice float64
	pollInterval := 100 * time.Millisecond

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			goto done
		case <-ticker.C:
			resp, err := http.Get(fmt.Sprintf("%s/api/realtime_quote?ts_code=600519.SH", serverURL))
			if err != nil {
				continue
			}

			var result mocks.HTTPResponse
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				resp.Body.Close()
				continue
			}
			resp.Body.Close()

			dataBytes, _ := json.Marshal(result.Data)
			var quotes []mocks.RealtimeQuoteHTTP
			if err := json.Unmarshal(dataBytes, &quotes); err == nil && len(quotes) > 0 {
				quote := quotes[0]
				totalQuotes++

				if lastPrice > 0 {
					change := quote.Price - lastPrice
					priceChanges = append(priceChanges, change)
				}
				lastPrice = quote.Price
			}
		}
	}
done:

	t.Logf("HTTP 持续采集结果: 采集次数=%d, 价格变化记录=%d", totalQuotes, len(priceChanges))
	assert.Greater(t, totalQuotes, 0)

	// 验证价格有波动
	if len(priceChanges) > 0 {
		var totalChange float64
		for _, c := range priceChanges {
			if c != 0 {
				totalChange += c
			}
		}
		t.Logf("价格累计变化: %.4f", totalChange)
	}
}

// TestBackpressure_Simulation 测试背压场景模拟
func TestBackpressure_Simulation(t *testing.T) {
	// 快速推送数据
	server := mocks.NewMockTushareWSServer()
	server.SetPushInterval(10 * time.Millisecond) // 非常快的推送
	wsURL := server.Start()
	defer server.Stop()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// 读取连接消息
	conn.ReadMessage()

	// 订阅多个股票以增加数据量
	subscribeMsg := mocks.WSMessage{
		Type: "subscribe",
		Data: []string{
			"000001.SZ", "000002.SZ", "600000.SH", "600036.SH",
			"000858.SZ", "002415.SZ", "600519.SH", "601318.SH",
		},
	}
	msgData, _ := json.Marshal(subscribeMsg)
	conn.WriteMessage(websocket.TextMessage, msgData)
	conn.ReadMessage()

	// 模拟慢速消费（造成背压）
	var received int32
	var dropped int32
	buffer := make(chan []byte, 10) // 小缓冲区

	// 生产者 - 快速接收
	go func() {
		for {
			conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			_, message, err := conn.ReadMessage()
			if err != nil {
				continue
			}
			select {
			case buffer <- message:
				atomic.AddInt32(&received, 1)
			default:
				atomic.AddInt32(&dropped, 1) // 缓冲区满，丢弃
			}
		}
	}()

	// 消费者 - 慢速处理
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-buffer:
				// 模拟慢速处理
				time.Sleep(50 * time.Millisecond)
			}
		}
	}()

	<-ctx.Done()

	receivedCount := atomic.LoadInt32(&received)
	droppedCount := atomic.LoadInt32(&dropped)

	t.Logf("背压测试: 接收=%d, 丢弃=%d", receivedCount, droppedCount)
	// 在快速推送场景下应该有一些丢弃
	assert.Greater(t, receivedCount, int32(0))
}

// TestReconnection_WithDataContinuity 测试重连时的数据连续性
func TestReconnection_WithDataContinuity(t *testing.T) {
	server := mocks.NewMockTushareWSServer()
	server.SetPushInterval(50 * time.Millisecond)
	server.SimulateFailure(10) // 10 条消息后断开
	wsURL := server.Start()
	defer server.Stop()

	var allQuotes []mocks.RealtimeQuote
	var mu sync.Mutex

	// 第一次连接
	conn1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	conn1.ReadMessage() // 连接消息

	// 订阅
	subscribeMsg := mocks.WSMessage{Type: "subscribe", Data: []string{"000001.SZ"}}
	msgData, _ := json.Marshal(subscribeMsg)
	conn1.WriteMessage(websocket.TextMessage, msgData)
	conn1.ReadMessage() // 订阅确认

	// 接收数据直到断开
	for {
		conn1.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, message, err := conn1.ReadMessage()
		if err != nil {
			break
		}

		var msg mocks.WSMessage
		if json.Unmarshal(message, &msg) == nil && msg.Type == "quote" {
			quoteData, _ := json.Marshal(msg.Data)
			var quote mocks.RealtimeQuote
			if json.Unmarshal(quoteData, &quote) == nil {
				mu.Lock()
				allQuotes = append(allQuotes, quote)
				mu.Unlock()
			}
		}
	}
	conn1.Close()

	firstBatchCount := len(allQuotes)
	t.Logf("第一次连接收到 %d 条数据", firstBatchCount)

	// 禁用失败，重新连接
	server.DisableFailure()

	conn2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn2.Close()

	conn2.ReadMessage() // 连接消息
	conn2.WriteMessage(websocket.TextMessage, msgData)
	conn2.ReadMessage() // 订阅确认

	// 接收更多数据
	timeout := time.After(300 * time.Millisecond)
loop:
	for {
		select {
		case <-timeout:
			break loop
		default:
			conn2.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			_, message, err := conn2.ReadMessage()
			if err != nil {
				continue
			}

			var msg mocks.WSMessage
			if json.Unmarshal(message, &msg) == nil && msg.Type == "quote" {
				quoteData, _ := json.Marshal(msg.Data)
				var quote mocks.RealtimeQuote
				if json.Unmarshal(quoteData, &quote) == nil {
					mu.Lock()
					allQuotes = append(allQuotes, quote)
					mu.Unlock()
				}
			}
		}
	}

	secondBatchCount := len(allQuotes) - firstBatchCount
	t.Logf("重连后收到 %d 条数据", secondBatchCount)
	t.Logf("总共收到 %d 条数据", len(allQuotes))

	assert.Greater(t, firstBatchCount, 0)
	assert.Greater(t, secondBatchCount, 0)
}

// TestHealthCheck 测试健康检查端点
func TestHealthCheck(t *testing.T) {
	server := mocks.NewMockTushareHTTPServer()
	serverURL := server.Start()
	defer server.Stop()

	resp, err := http.Get(fmt.Sprintf("%s/health", serverURL))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, "ok", result["status"])
}
