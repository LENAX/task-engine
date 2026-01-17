package mocks

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// MockExternalAPI 模拟外部API，支持模拟各种异常响应
type MockExternalAPI struct {
	mu                sync.RWMutex
	shouldTimeout     bool
	shouldReturn429   bool
	shouldReturn503   bool
	shouldReturn502   bool
	shouldReturn401   bool
	shouldReturn403   bool
	shouldPartialFail bool
	timeoutDuration   time.Duration
	failCount         int
	currentFailCount  int
	successCount      int
}

// NewMockExternalAPI 创建MockExternalAPI
func NewMockExternalAPI() *MockExternalAPI {
	return &MockExternalAPI{
		timeoutDuration: 5 * time.Second,
	}
}

// SetShouldTimeout 设置是否超时
func (m *MockExternalAPI) SetShouldTimeout(shouldTimeout bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldTimeout = shouldTimeout
}

// SetShouldReturn429 设置是否返回429限流错误
func (m *MockExternalAPI) SetShouldReturn429(shouldReturn bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldReturn429 = shouldReturn
}

// SetShouldReturn503 设置是否返回503服务不可用
func (m *MockExternalAPI) SetShouldReturn503(shouldReturn bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldReturn503 = shouldReturn
}

// SetShouldReturn502 设置是否返回502网关错误
func (m *MockExternalAPI) SetShouldReturn502(shouldReturn bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldReturn502 = shouldReturn
}

// SetShouldReturn401 设置是否返回401认证错误
func (m *MockExternalAPI) SetShouldReturn401(shouldReturn bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldReturn401 = shouldReturn
}

// SetShouldReturn403 设置是否返回403权限错误
func (m *MockExternalAPI) SetShouldReturn403(shouldReturn bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldReturn403 = shouldReturn
}

// SetShouldPartialFail 设置是否部分失败
func (m *MockExternalAPI) SetShouldPartialFail(shouldFail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldPartialFail = shouldFail
}

// SetFailCount 设置失败次数
func (m *MockExternalAPI) SetFailCount(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failCount = count
	m.currentFailCount = 0
}

// Call 调用API（模拟）
func (m *MockExternalAPI) Call(apiName string, params map[string]interface{}) (interface{}, error) {
	m.mu.RLock()
	shouldTimeout := m.shouldTimeout
	shouldReturn429 := m.shouldReturn429
	shouldReturn503 := m.shouldReturn503
	shouldReturn502 := m.shouldReturn502
	shouldReturn401 := m.shouldReturn401
	shouldReturn403 := m.shouldReturn403
	shouldPartialFail := m.shouldPartialFail
	failCount := m.failCount
	currentFailCount := m.currentFailCount
	successCount := m.successCount
	m.mu.RUnlock()

	// 模拟部分失败
	if shouldPartialFail {
		m.mu.Lock()
		successCount++
		m.successCount = successCount
		m.mu.Unlock()

		if successCount%2 == 0 {
			return nil, errors.New("模拟API故障：部分请求失败")
		}
	}

	// 检查失败次数
	if failCount > 0 && currentFailCount < failCount {
		m.mu.Lock()
		m.currentFailCount++
		m.mu.Unlock()
		return nil, errors.New(fmt.Sprintf("模拟API故障：请求失败（第%d次）", m.currentFailCount+1))
	}

	// 模拟超时
	if shouldTimeout {
		time.Sleep(m.timeoutDuration)
		return nil, errors.New("模拟API故障：请求超时")
	}

	// 模拟限流
	if shouldReturn429 {
		return nil, errors.New("模拟API故障：429 Too Many Requests")
	}

	// 模拟服务不可用
	if shouldReturn503 {
		return nil, errors.New("模拟API故障：503 Service Unavailable")
	}

	// 模拟网关错误
	if shouldReturn502 {
		return nil, errors.New("模拟API故障：502 Bad Gateway")
	}

	// 模拟认证错误
	if shouldReturn401 {
		return nil, errors.New("模拟API故障：401 Unauthorized")
	}

	// 模拟权限错误
	if shouldReturn403 {
		return nil, errors.New("模拟API故障：403 Forbidden")
	}

	// 正常响应
	return map[string]interface{}{
		"status": "success",
		"data":   "mock data",
	}, nil
}

