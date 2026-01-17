// Package realtime 提供实时数据采集任务的持续任务管理
package realtime

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// ContinuousTaskType 持续任务类型
type ContinuousTaskType string

const (
	TaskTypeDataCollector   ContinuousTaskType = "data_collector"   // 数据采集器
	TaskTypeStreamProcessor ContinuousTaskType = "stream_processor" // 流处理器
	TaskTypeEventListener   ContinuousTaskType = "event_listener"   // 事件监听器
	TaskTypeScheduledPoller ContinuousTaskType = "scheduled_poller" // 定时轮询器
)

// ContinuousTaskState 持续任务状态
type ContinuousTaskState string

const (
	StateInitializing ContinuousTaskState = "initializing" // 初始化中
	StateRunning      ContinuousTaskState = "running"      // 运行中
	StatePaused       ContinuousTaskState = "paused"       // 已暂停
	StateReconnecting ContinuousTaskState = "reconnecting" // 重连中
	StateStopping     ContinuousTaskState = "stopping"     // 停止中
	StateStopped      ContinuousTaskState = "stopped"      // 已停止
	StateError        ContinuousTaskState = "error"        // 错误状态
)

// ReconnectBackoffConfig 重连退避配置
type ReconnectBackoffConfig struct {
	InitialInterval time.Duration `json:"initial_interval"` // 初始间隔（默认1s）
	MaxInterval     time.Duration `json:"max_interval"`     // 最大间隔（默认30s）
	Multiplier      float64       `json:"multiplier"`       // 倍增因子（默认2.0）
	Jitter          float64       `json:"jitter"`           // 抖动因子（默认0.1）
}

// DefaultReconnectBackoffConfig 返回默认重连退避配置
func DefaultReconnectBackoffConfig() ReconnectBackoffConfig {
	return ReconnectBackoffConfig{
		InitialInterval: time.Second,
		MaxInterval:     30 * time.Second,
		Multiplier:      2.0,
		Jitter:          0.1,
	}
}

// ContinuousTaskConfig 持续任务配置
type ContinuousTaskConfig struct {
	// 基础配置
	ID   string             `json:"id"`
	Name string             `json:"name"`
	Type ContinuousTaskType `json:"type"`

	// 连接配置
	Endpoint string `json:"endpoint"` // 连接端点
	Protocol string `json:"protocol"` // 协议（ws/wss/tcp/http）

	// 重连配置
	ReconnectEnabled     bool                   `json:"reconnect_enabled"`      // 是否自动重连
	ReconnectBackoff     ReconnectBackoffConfig `json:"reconnect_backoff"`      // 重连退避配置
	MaxReconnectAttempts int                    `json:"max_reconnect_attempts"` // 最大重连次数（0=无限）

	// 缓冲配置
	BufferSize    int           `json:"buffer_size"`    // 缓冲区大小
	BatchSize     int           `json:"batch_size"`     // 批处理大小
	FlushInterval time.Duration `json:"flush_interval"` // 刷新间隔

	// 背压配置
	BackpressureThreshold float64 `json:"backpressure_threshold"` // 背压阈值（0.0-1.0）
	BackpressureAction    string  `json:"backpressure_action"`    // 背压动作（drop/block/throttle）

	// 事件订阅
	SubscribedEvents []EventType `json:"subscribed_events"` // 订阅的事件类型

	// 处理函数
	DataHandler  string `json:"data_handler"`  // 数据处理函数名
	ErrorHandler string `json:"error_handler"` // 错误处理函数名

	// 自定义参数
	Params map[string]interface{} `json:"params"`
}

// NewContinuousTaskConfig 创建默认的持续任务配置
func NewContinuousTaskConfig(id, name string, taskType ContinuousTaskType) *ContinuousTaskConfig {
	return &ContinuousTaskConfig{
		ID:                    id,
		Name:                  name,
		Type:                  taskType,
		ReconnectEnabled:      true,
		ReconnectBackoff:      DefaultReconnectBackoffConfig(),
		MaxReconnectAttempts:  0, // 无限重连
		BufferSize:            10000,
		BatchSize:             100,
		FlushInterval:         time.Second,
		BackpressureThreshold: 0.8,
		BackpressureAction:    "throttle",
		SubscribedEvents:      make([]EventType, 0),
		Params:                make(map[string]interface{}),
	}
}

// ContinuousTask 持续运行任务
type ContinuousTask struct {
	Config ContinuousTaskConfig `json:"config"`
	State  ContinuousTaskState  `json:"state"`

	// 运行时状态
	StartTime    time.Time `json:"start_time"`
	LastDataTime time.Time `json:"last_data_time"` // 最后数据时间
	DataCount    int64     `json:"data_count"`     // 处理数据计数
	ErrorCount   int64     `json:"error_count"`    // 错误计数

	// 连接状态
	Connected      bool `json:"connected"`
	ReconnectCount int  `json:"reconnect_count"`

	// 内部组件（不序列化）
	ctx    context.Context    `json:"-"`
	cancel context.CancelFunc `json:"-"`
	mu     sync.RWMutex       `json:"-"`
}

// NewContinuousTask 创建持续任务
func NewContinuousTask(config ContinuousTaskConfig) *ContinuousTask {
	return &ContinuousTask{
		Config:    config,
		State:     StateInitializing,
		StartTime: time.Now(),
	}
}

// Start 启动任务
func (ct *ContinuousTask) Start(parentCtx context.Context) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	ct.ctx, ct.cancel = context.WithCancel(parentCtx)
	ct.State = StateRunning
	ct.StartTime = time.Now()
}

// Stop 停止任务
func (ct *ContinuousTask) Stop() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	ct.State = StateStopping
	if ct.cancel != nil {
		ct.cancel()
	}
	ct.State = StateStopped
}

// Pause 暂停任务
func (ct *ContinuousTask) Pause() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if ct.State == StateRunning {
		ct.State = StatePaused
	}
}

// Resume 恢复任务
func (ct *ContinuousTask) Resume() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if ct.State == StatePaused {
		ct.State = StateRunning
	}
}

// GetState 获取任务状态
func (ct *ContinuousTask) GetState() ContinuousTaskState {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return ct.State
}

// SetState 设置任务状态
func (ct *ContinuousTask) SetState(state ContinuousTaskState) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.State = state
}

// IsConnected 检查是否已连接
func (ct *ContinuousTask) IsConnected() bool {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return ct.Connected
}

// SetConnected 设置连接状态
func (ct *ContinuousTask) SetConnected(connected bool) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.Connected = connected
}

// IncrementDataCount 增加数据计数
func (ct *ContinuousTask) IncrementDataCount() int64 {
	return atomic.AddInt64(&ct.DataCount, 1)
}

// IncrementErrorCount 增加错误计数
func (ct *ContinuousTask) IncrementErrorCount() int64 {
	return atomic.AddInt64(&ct.ErrorCount, 1)
}

// GetDataCount 获取数据计数
func (ct *ContinuousTask) GetDataCount() int64 {
	return atomic.LoadInt64(&ct.DataCount)
}

// GetErrorCount 获取错误计数
func (ct *ContinuousTask) GetErrorCount() int64 {
	return atomic.LoadInt64(&ct.ErrorCount)
}

// UpdateLastDataTime 更新最后数据时间
func (ct *ContinuousTask) UpdateLastDataTime() {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.LastDataTime = time.Now()
}

// GetLastDataTime 获取最后数据时间
func (ct *ContinuousTask) GetLastDataTime() time.Time {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return ct.LastDataTime
}

// IncrementReconnectCount 增加重连计数
func (ct *ContinuousTask) IncrementReconnectCount() int {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.ReconnectCount++
	return ct.ReconnectCount
}

// GetReconnectCount 获取重连计数
func (ct *ContinuousTask) GetReconnectCount() int {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return ct.ReconnectCount
}

// Context 获取任务上下文
func (ct *ContinuousTask) Context() context.Context {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return ct.ctx
}

// GetSnapshot 获取任务快照（用于断点）
func (ct *ContinuousTask) GetSnapshot() map[string]interface{} {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	return map[string]interface{}{
		"state":           string(ct.State),
		"connected":       ct.Connected,
		"data_count":      atomic.LoadInt64(&ct.DataCount),
		"error_count":     atomic.LoadInt64(&ct.ErrorCount),
		"reconnect_count": ct.ReconnectCount,
		"last_data_time":  ct.LastDataTime,
		"start_time":      ct.StartTime,
	}
}
