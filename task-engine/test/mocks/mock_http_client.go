package mocks

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// MockHTTPClient 模拟HTTP客户端，支持模拟各种网络故障
type MockHTTPClient struct {
	mu                sync.RWMutex
	shouldTimeout     bool
	shouldFailConnect bool
	shouldReturn429   bool
	shouldReturn503   bool
	shouldReturn502   bool
	timeoutDuration   time.Duration
	failCount         int
	currentFailCount  int
}

// NewMockHTTPClient 创建MockHTTPClient
func NewMockHTTPClient() *MockHTTPClient {
	return &MockHTTPClient{
		timeoutDuration: 5 * time.Second,
	}
}

// SetShouldTimeout 设置是否超时
func (m *MockHTTPClient) SetShouldTimeout(shouldTimeout bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldTimeout = shouldTimeout
}

// SetShouldFailConnect 设置是否连接失败
func (m *MockHTTPClient) SetShouldFailConnect(shouldFail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldFailConnect = shouldFail
}

// SetShouldReturn429 设置是否返回429限流错误
func (m *MockHTTPClient) SetShouldReturn429(shouldReturn bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldReturn429 = shouldReturn
}

// SetShouldReturn503 设置是否返回503服务不可用
func (m *MockHTTPClient) SetShouldReturn503(shouldReturn bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldReturn503 = shouldReturn
}

// SetShouldReturn502 设置是否返回502网关错误
func (m *MockHTTPClient) SetShouldReturn502(shouldReturn bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldReturn502 = shouldReturn
}

// SetFailCount 设置失败次数
func (m *MockHTTPClient) SetFailCount(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failCount = count
	m.currentFailCount = 0
}

// Do 执行HTTP请求（模拟）
func (m *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	m.mu.RLock()
	shouldTimeout := m.shouldTimeout
	shouldFailConnect := m.shouldFailConnect
	shouldReturn429 := m.shouldReturn429
	shouldReturn503 := m.shouldReturn503
	shouldReturn502 := m.shouldReturn502
	failCount := m.failCount
	currentFailCount := m.currentFailCount
	m.mu.RUnlock()

	// 检查失败次数
	if failCount > 0 && currentFailCount < failCount {
		m.mu.Lock()
		m.currentFailCount++
		m.mu.Unlock()
		return nil, errors.New(fmt.Sprintf("模拟网络故障：请求失败（第%d次）", m.currentFailCount+1))
	}

	// 模拟连接失败
	if shouldFailConnect {
		return nil, errors.New("模拟网络故障：连接被拒绝")
	}

	// 模拟超时
	if shouldTimeout {
		time.Sleep(m.timeoutDuration)
		return nil, errors.New("模拟网络故障：请求超时")
	}

	// 模拟限流
	if shouldReturn429 {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Status:     "429 Too Many Requests",
		}, nil
	}

	// 模拟服务不可用
	if shouldReturn503 {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Status:     "503 Service Unavailable",
		}, nil
	}

	// 模拟网关错误
	if shouldReturn502 {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Status:     "502 Bad Gateway",
		}, nil
	}

	// 正常响应
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
	}, nil
}

