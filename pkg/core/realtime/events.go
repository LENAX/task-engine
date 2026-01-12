// Package realtime 提供实时数据采集任务的事件驱动架构支持
package realtime

import (
	"time"

	"github.com/google/uuid"
)

// EventType 事件类型
type EventType string

const (
	// 数据事件
	EventDataArrived   EventType = "data.arrived"   // 数据到达
	EventDataProcessed EventType = "data.processed" // 数据处理完成
	EventDataBatched   EventType = "data.batched"   // 数据批量到达

	// 任务状态事件
	EventTaskStarted EventType = "task.started" // 任务启动
	EventTaskPaused  EventType = "task.paused"  // 任务暂停
	EventTaskResumed EventType = "task.resumed" // 任务恢复
	EventTaskStopped EventType = "task.stopped" // 任务停止

	// 连接事件
	EventConnected    EventType = "connection.established"  // 连接建立
	EventDisconnected EventType = "connection.lost"         // 连接断开
	EventReconnecting EventType = "connection.reconnecting" // 正在重连
	EventReconnected  EventType = "connection.restored"     // 重连成功

	// 错误事件
	EventError          EventType = "error.occurred"  // 错误发生
	EventErrorRecovered EventType = "error.recovered" // 错误恢复

	// 背压事件
	EventBackpressure         EventType = "backpressure.triggered" // 背压触发
	EventBackpressureRelieved EventType = "backpressure.relieved"  // 背压解除
)

// RealtimeEvent 实时事件基础结构
type RealtimeEvent struct {
	ID            string            `json:"id"`             // 事件ID（UUID）
	Type          EventType         `json:"type"`           // 事件类型
	TaskID        string            `json:"task_id"`        // 关联任务ID
	InstanceID    string            `json:"instance_id"`    // 关联实例ID
	Timestamp     time.Time         `json:"timestamp"`      // 事件时间
	Payload       interface{}       `json:"payload"`        // 事件负载
	Metadata      map[string]string `json:"metadata"`       // 元数据
	CorrelationID string            `json:"correlation_id"` // 关联ID（用于追踪）
}

// NewRealtimeEvent 创建实时事件
func NewRealtimeEvent(eventType EventType, taskID, instanceID string, payload interface{}) *RealtimeEvent {
	return &RealtimeEvent{
		ID:         uuid.NewString(),
		Type:       eventType,
		TaskID:     taskID,
		InstanceID: instanceID,
		Timestamp:  time.Now(),
		Payload:    payload,
		Metadata:   make(map[string]string),
	}
}

// WithMetadata 添加元数据
func (e *RealtimeEvent) WithMetadata(key, value string) *RealtimeEvent {
	if e.Metadata == nil {
		e.Metadata = make(map[string]string)
	}
	e.Metadata[key] = value
	return e
}

// WithCorrelationID 设置关联ID
func (e *RealtimeEvent) WithCorrelationID(correlationID string) *RealtimeEvent {
	e.CorrelationID = correlationID
	return e
}

// DataArrivedPayload 数据到达事件负载
type DataArrivedPayload struct {
	Data     interface{} `json:"data"`     // 原始数据
	Source   string      `json:"source"`   // 数据来源
	Size     int64       `json:"size"`     // 数据大小（字节）
	Sequence int64       `json:"sequence"` // 序列号（用于顺序保证）
	BatchID  string      `json:"batch_id"` // 批次ID（可选）
}

// ConnectionPayload 连接事件负载
type ConnectionPayload struct {
	Endpoint   string `json:"endpoint"`    // 连接端点
	Protocol   string `json:"protocol"`    // 协议类型
	RetryCount int    `json:"retry_count"` // 重试次数
	Error      string `json:"error"`       // 错误信息（断开时）
}

// ErrorPayload 错误事件负载
type ErrorPayload struct {
	ErrorCode   string `json:"error_code"`  // 错误码
	Message     string `json:"message"`     // 错误消息
	Stack       string `json:"stack"`       // 堆栈信息
	Recoverable bool   `json:"recoverable"` // 是否可恢复
}

// BackpressurePayload 背压事件负载
type BackpressurePayload struct {
	BufferUsage float64 `json:"buffer_usage"` // 缓冲区使用率
	QueueLength int     `json:"queue_length"` // 队列长度
	Threshold   float64 `json:"threshold"`    // 触发阈值
	Action      string  `json:"action"`       // 采取的动作
}

// TaskStatusPayload 任务状态事件负载
type TaskStatusPayload struct {
	TaskID    string `json:"task_id"`    // 任务ID
	TaskName  string `json:"task_name"`  // 任务名称
	OldStatus string `json:"old_status"` // 旧状态
	NewStatus string `json:"new_status"` // 新状态
	Reason    string `json:"reason"`     // 状态变化原因
}

// EventHandler 事件处理器函数类型
type EventHandler func(event *RealtimeEvent) error

// SubscriptionID 订阅ID类型
type SubscriptionID string

// EventSubscription 事件订阅配置
type EventSubscription struct {
	EventType   EventType `json:"event_type"`   // 事件类型
	HandlerName string    `json:"handler_name"` // 处理器名称
	Filter      string    `json:"filter"`       // 过滤条件（可选，CEL 表达式）
	Priority    int       `json:"priority"`     // 优先级
}
