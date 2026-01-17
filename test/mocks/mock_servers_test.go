// Package mocks 包含 mock 服务器的测试
package mocks

import (
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
)

// ============================================
// WebSocket Mock Server Tests
// ============================================

func TestMockTushareWSServer_BasicConnection(t *testing.T) {
	server := NewMockTushareWSServer()
	wsURL := server.Start()
	defer server.Stop()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// 读取连接成功消息
	_, message, err := conn.ReadMessage()
	require.NoError(t, err)

	var msg WSMessage
	err = json.Unmarshal(message, &msg)
	require.NoError(t, err)

	assert.Equal(t, "connected", msg.Type)
	assert.Equal(t, 200, msg.Code)
}

func TestMockTushareWSServer_Subscribe(t *testing.T) {
	server := NewMockTushareWSServer()
	wsURL := server.Start()
	defer server.Stop()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// 读取连接消息
	conn.ReadMessage()

	// 发送订阅请求
	subscribeMsg := WSMessage{
		Type: "subscribe",
		Data: []string{"000001.SZ", "600000.SH"},
	}
	msgData, _ := json.Marshal(subscribeMsg)
	err = conn.WriteMessage(websocket.TextMessage, msgData)
	require.NoError(t, err)

	// 读取订阅确认
	_, message, err := conn.ReadMessage()
	require.NoError(t, err)

	var msg WSMessage
	err = json.Unmarshal(message, &msg)
	require.NoError(t, err)

	assert.Equal(t, "subscribed", msg.Type)
	assert.Equal(t, 200, msg.Code)
}

func TestMockTushareWSServer_ReceiveQuotes(t *testing.T) {
	server := NewMockTushareWSServer()
	server.SetPushInterval(50 * time.Millisecond)
	wsURL := server.Start()
	defer server.Stop()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// 读取连接消息
	conn.ReadMessage()

	// 订阅股票
	subscribeMsg := WSMessage{
		Type: "subscribe",
		Data: []string{"000001.SZ"},
	}
	msgData, _ := json.Marshal(subscribeMsg)
	conn.WriteMessage(websocket.TextMessage, msgData)
	conn.ReadMessage() // 读取订阅确认

	// 收集行情数据
	var quotes []RealtimeQuote
	timeout := time.After(300 * time.Millisecond)

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

			var msg WSMessage
			if err := json.Unmarshal(message, &msg); err != nil {
				continue
			}

			if msg.Type == "quote" {
				quoteData, _ := json.Marshal(msg.Data)
				var quote RealtimeQuote
				if err := json.Unmarshal(quoteData, &quote); err == nil {
					quotes = append(quotes, quote)
				}
			}
		}
	}
done:

	assert.Greater(t, len(quotes), 0, "应该收到行情数据")

	if len(quotes) > 0 {
		quote := quotes[0]
		assert.Equal(t, "000001.SZ", quote.TSCode)
		assert.Equal(t, "平安银行", quote.Name)
		assert.Greater(t, quote.Price, 0.0)
	}
}

func TestMockTushareWSServer_Heartbeat(t *testing.T) {
	server := NewMockTushareWSServer()
	server.SetPushInterval(1 * time.Second) // 长间隔避免干扰
	wsURL := server.Start()
	defer server.Stop()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// 读取连接消息
	conn.ReadMessage()

	// 发送心跳
	heartbeatMsg := WSMessage{Type: "heartbeat"}
	msgData, _ := json.Marshal(heartbeatMsg)
	err = conn.WriteMessage(websocket.TextMessage, msgData)
	require.NoError(t, err)

	// 读取心跳响应
	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, message, err := conn.ReadMessage()
	require.NoError(t, err)

	var msg WSMessage
	json.Unmarshal(message, &msg)
	assert.Equal(t, "heartbeat", msg.Type)
	assert.Equal(t, "pong", msg.Message)
}

func TestMockTushareWSServer_SimulateFailure(t *testing.T) {
	server := NewMockTushareWSServer()
	server.SetPushInterval(20 * time.Millisecond)
	server.SimulateFailure(5)
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
	assert.Greater(t, messageCount, 0)
	assert.LessOrEqual(t, messageCount, 10) // 不应该收到太多消息
}

func TestMockTushareWSServer_GetStats(t *testing.T) {
	server := NewMockTushareWSServer()
	server.SetPushInterval(30 * time.Millisecond)
	wsURL := server.Start()
	defer server.Stop()

	// 初始状态
	totalMsgs, totalConns, currentClients := server.GetStats()
	assert.Equal(t, int64(0), totalMsgs)
	assert.Equal(t, int64(0), totalConns)
	assert.Equal(t, int32(0), currentClients)

	// 连接
	conn, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer conn.Close()

	time.Sleep(100 * time.Millisecond)

	// 检查连接后的状态
	_, totalConns, currentClients = server.GetStats()
	assert.Equal(t, int64(1), totalConns)
	assert.Equal(t, int32(1), currentClients)
}

// ============================================
// HTTP Mock Server Tests
// ============================================

func TestMockTushareHTTPServer_RealtimeQuote(t *testing.T) {
	server := NewMockTushareHTTPServer()
	serverURL := server.Start()
	defer server.Stop()

	resp, err := http.Get(fmt.Sprintf("%s/api/realtime_quote?ts_code=000001.SZ", serverURL))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result HTTPResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, 200, result.Code)
	assert.Equal(t, "success", result.Message)

	dataBytes, _ := json.Marshal(result.Data)
	var quotes []RealtimeQuoteHTTP
	json.Unmarshal(dataBytes, &quotes)

	assert.Len(t, quotes, 1)
	assert.Equal(t, "000001.SZ", quotes[0].TSCode)
	assert.Equal(t, "平安银行", quotes[0].Name)
}

func TestMockTushareHTTPServer_MultipleStocks(t *testing.T) {
	server := NewMockTushareHTTPServer()
	serverURL := server.Start()
	defer server.Stop()

	resp, err := http.Get(fmt.Sprintf("%s/api/realtime_quote?ts_code=000001.SZ,600000.SH", serverURL))
	require.NoError(t, err)
	defer resp.Body.Close()

	var result HTTPResponse
	json.NewDecoder(resp.Body).Decode(&result)

	dataBytes, _ := json.Marshal(result.Data)
	var quotes []RealtimeQuoteHTTP
	json.Unmarshal(dataBytes, &quotes)

	assert.Len(t, quotes, 2)
}

func TestMockTushareHTTPServer_RealtimeTick(t *testing.T) {
	server := NewMockTushareHTTPServer()
	server.SetMaxTicksPerCall(20)
	serverURL := server.Start()
	defer server.Stop()

	resp, err := http.Get(fmt.Sprintf("%s/api/realtime_tick?ts_code=600000.SH", serverURL))
	require.NoError(t, err)
	defer resp.Body.Close()

	var result HTTPResponse
	json.NewDecoder(resp.Body).Decode(&result)

	assert.Equal(t, 200, result.Code)

	dataBytes, _ := json.Marshal(result.Data)
	var ticks []RealtimeTick
	json.Unmarshal(dataBytes, &ticks)

	assert.Len(t, ticks, 20)

	// 验证 tick 字段
	tick := ticks[0]
	assert.NotEmpty(t, tick.Time)
	assert.Greater(t, tick.Price, 0.0)
	assert.Greater(t, tick.Volume, 0)
	assert.Contains(t, []string{"买盘", "卖盘", "中性"}, tick.Type)
}

func TestMockTushareHTTPServer_RealtimeList(t *testing.T) {
	server := NewMockTushareHTTPServer()
	serverURL := server.Start()
	defer server.Stop()

	resp, err := http.Get(fmt.Sprintf("%s/api/realtime_list", serverURL))
	require.NoError(t, err)
	defer resp.Body.Close()

	var result HTTPResponse
	json.NewDecoder(resp.Body).Decode(&result)

	dataBytes, _ := json.Marshal(result.Data)
	var quotes []RealtimeQuoteHTTP
	json.Unmarshal(dataBytes, &quotes)

	// 应该返回所有预设的股票
	assert.GreaterOrEqual(t, len(quotes), 8)
}

func TestMockTushareHTTPServer_MissingTSCode(t *testing.T) {
	server := NewMockTushareHTTPServer()
	serverURL := server.Start()
	defer server.Stop()

	resp, err := http.Get(fmt.Sprintf("%s/api/realtime_quote", serverURL))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var result HTTPResponse
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, 400, result.Code)
}

func TestMockTushareHTTPServer_NonExistentStock(t *testing.T) {
	server := NewMockTushareHTTPServer()
	serverURL := server.Start()
	defer server.Stop()

	resp, err := http.Get(fmt.Sprintf("%s/api/realtime_tick?ts_code=999999.SZ", serverURL))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestMockTushareHTTPServer_SimulateFailure(t *testing.T) {
	server := NewMockTushareHTTPServer()
	server.SimulateFailure(1.0, 500, "Internal Server Error")
	serverURL := server.Start()
	defer server.Stop()

	resp, err := http.Get(fmt.Sprintf("%s/api/realtime_quote?ts_code=000001.SZ", serverURL))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	totalRequests, failedRequests := server.GetStats()
	assert.Equal(t, int64(1), totalRequests)
	assert.Equal(t, int64(1), failedRequests)
}

func TestMockTushareHTTPServer_ResponseDelay(t *testing.T) {
	server := NewMockTushareHTTPServer()
	server.SetResponseDelay(100 * time.Millisecond)
	serverURL := server.Start()
	defer server.Stop()

	start := time.Now()
	resp, err := http.Get(fmt.Sprintf("%s/api/realtime_quote?ts_code=000001.SZ", serverURL))
	elapsed := time.Since(start)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond)
}

func TestMockTushareHTTPServer_AddStock(t *testing.T) {
	server := NewMockTushareHTTPServer()
	server.AddStock("999999.SZ", "测试股票", 100.0)
	serverURL := server.Start()
	defer server.Stop()

	resp, err := http.Get(fmt.Sprintf("%s/api/realtime_quote?ts_code=999999.SZ", serverURL))
	require.NoError(t, err)
	defer resp.Body.Close()

	var result HTTPResponse
	json.NewDecoder(resp.Body).Decode(&result)

	dataBytes, _ := json.Marshal(result.Data)
	var quotes []RealtimeQuoteHTTP
	json.Unmarshal(dataBytes, &quotes)

	assert.Len(t, quotes, 1)
	assert.Equal(t, "999999.SZ", quotes[0].TSCode)
	assert.Equal(t, "测试股票", quotes[0].Name)
}

func TestMockTushareHTTPServer_GetStockPrice(t *testing.T) {
	server := NewMockTushareHTTPServer()
	serverURL := server.Start()
	defer server.Stop()

	// 获取初始价格
	price1, exists := server.GetStockPrice("000001.SZ")
	assert.True(t, exists)
	assert.Greater(t, price1, 0.0)

	// 调用 API 会更新价格
	http.Get(fmt.Sprintf("%s/api/realtime_quote?ts_code=000001.SZ", serverURL))

	// 价格可能有变化
	price2, _ := server.GetStockPrice("000001.SZ")
	assert.Greater(t, price2, 0.0)
}

func TestMockTushareHTTPServer_ResetStats(t *testing.T) {
	server := NewMockTushareHTTPServer()
	serverURL := server.Start()
	defer server.Stop()

	// 发送一些请求
	http.Get(fmt.Sprintf("%s/api/realtime_quote?ts_code=000001.SZ", serverURL))
	http.Get(fmt.Sprintf("%s/api/realtime_quote?ts_code=000001.SZ", serverURL))

	total, _ := server.GetStats()
	assert.Equal(t, int64(2), total)

	// 重置
	server.ResetStats()

	total, _ = server.GetStats()
	assert.Equal(t, int64(0), total)
}

func TestMockTushareHTTPServer_HealthCheck(t *testing.T) {
	server := NewMockTushareHTTPServer()
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

// ============================================
// Polling Client Tests
// ============================================

func TestMockTusharePollingClient_Subscribe(t *testing.T) {
	server := NewMockTushareHTTPServer()
	serverURL := server.Start()
	defer server.Stop()

	client := NewMockTusharePollingClient(serverURL, 100*time.Millisecond)
	client.Subscribe([]string{"000001.SZ", "600000.SH"})

	var receivedData []RealtimeQuoteHTTP
	var mu sync.Mutex

	client.OnData(func(quotes []RealtimeQuoteHTTP) {
		mu.Lock()
		defer mu.Unlock()
		receivedData = append(receivedData, quotes...)
	})

	client.Start()
	time.Sleep(350 * time.Millisecond)
	client.Stop()

	mu.Lock()
	count := len(receivedData)
	mu.Unlock()

	assert.Greater(t, count, 0)
}

func TestMockTusharePollingClient_ErrorHandler(t *testing.T) {
	// 使用一个不存在的服务器地址
	client := NewMockTusharePollingClient("http://localhost:99999", 50*time.Millisecond)
	client.Subscribe([]string{"000001.SZ"})

	var errorCount int32

	client.OnError(func(err error) {
		atomic.AddInt32(&errorCount, 1)
	})

	client.Start()
	time.Sleep(200 * time.Millisecond)
	client.Stop()

	assert.Greater(t, atomic.LoadInt32(&errorCount), int32(0))
}

func TestMockTusharePollingClient_IsRunning(t *testing.T) {
	server := NewMockTushareHTTPServer()
	serverURL := server.Start()
	defer server.Stop()

	client := NewMockTusharePollingClient(serverURL, 100*time.Millisecond)

	assert.False(t, client.IsRunning())

	client.Start()
	assert.True(t, client.IsRunning())

	client.Stop()
	time.Sleep(50 * time.Millisecond)
	assert.False(t, client.IsRunning())
}

// ============================================
// Integration Tests - Continuous Data Acquisition
// ============================================

func TestContinuousDataAcquisition_WebSocket(t *testing.T) {
	server := NewMockTushareWSServer()
	server.SetPushInterval(30 * time.Millisecond)
	wsURL := server.Start()
	defer server.Stop()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// 连接消息
	conn.ReadMessage()

	// 订阅
	subscribeMsg := WSMessage{
		Type: "subscribe",
		Data: []string{"000001.SZ", "600000.SH"},
	}
	msgData, _ := json.Marshal(subscribeMsg)
	conn.WriteMessage(websocket.TextMessage, msgData)
	conn.ReadMessage() // 订阅确认

	// 持续接收数据
	var totalQuotes int32
	done := make(chan struct{})
	readDone := make(chan struct{})

	go func() {
		time.Sleep(300 * time.Millisecond)
		close(done)
	}()

	// 在单独的 goroutine 中读取
	go func() {
		defer close(readDone)
		for {
			select {
			case <-done:
				return
			default:
				conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
				_, message, err := conn.ReadMessage()
				if err != nil {
					// 检查是否应该退出
					select {
					case <-done:
						return
					default:
						continue
					}
				}

				var msg WSMessage
				if json.Unmarshal(message, &msg) == nil && msg.Type == "quote" {
					atomic.AddInt32(&totalQuotes, 1)
				}
			}
		}
	}()

	// 等待完成
	<-done
	<-readDone

	count := atomic.LoadInt32(&totalQuotes)
	assert.Greater(t, count, int32(0))
	t.Logf("WebSocket 持续采集: 收到 %d 条行情", count)
}

func TestContinuousDataAcquisition_HTTP(t *testing.T) {
	server := NewMockTushareHTTPServer()
	serverURL := server.Start()
	defer server.Stop()

	var totalQuotes int
	done := make(chan struct{})

	go func() {
		time.Sleep(300 * time.Millisecond)
		close(done)
	}()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

loop:
	for {
		select {
		case <-done:
			break loop
		case <-ticker.C:
			resp, err := http.Get(fmt.Sprintf("%s/api/realtime_quote?ts_code=000001.SZ", serverURL))
			if err != nil {
				continue
			}

			var result HTTPResponse
			json.NewDecoder(resp.Body).Decode(&result)
			resp.Body.Close()

			if result.Code == 200 {
				dataBytes, _ := json.Marshal(result.Data)
				var quotes []RealtimeQuoteHTTP
				if json.Unmarshal(dataBytes, &quotes) == nil {
					totalQuotes += len(quotes)
				}
			}
		}
	}

	assert.Greater(t, totalQuotes, 0)
	t.Logf("HTTP 持续采集: 收到 %d 条行情", totalQuotes)
}

func TestReconnection_WebSocket(t *testing.T) {
	server := NewMockTushareWSServer()
	server.SetPushInterval(30 * time.Millisecond)
	server.SimulateFailure(5)
	wsURL := server.Start()
	defer server.Stop()

	// 第一次连接
	conn1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)

	var firstBatchCount int
	for {
		conn1.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		_, _, err := conn1.ReadMessage()
		if err != nil {
			break
		}
		firstBatchCount++
	}
	conn1.Close()

	// 禁用失败，重连
	server.DisableFailure()

	conn2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn2.Close()

	// 读取一些消息确认重连成功
	var secondBatchCount int
	timeout := time.After(300 * time.Millisecond)

loop:
	for {
		select {
		case <-timeout:
			break loop
		default:
			conn2.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			_, _, err := conn2.ReadMessage()
			if err != nil {
				// 检查是否超时
				select {
				case <-timeout:
					break loop
				default:
					continue
				}
			}
			secondBatchCount++
		}
	}

	assert.Greater(t, firstBatchCount, 0)
	assert.Greater(t, secondBatchCount, 0)

	_, totalConns, _ := server.GetStats()
	assert.Equal(t, int64(2), totalConns)
}

