// Package builder 提供实时任务构建器
package builder

import (
	"fmt"
	"time"

	"github.com/LENAX/task-engine/pkg/core/realtime"
	"github.com/LENAX/task-engine/pkg/core/task"
)

// RealtimeTaskBuilder 实时任务构建器
type RealtimeTaskBuilder struct {
	baseBuilder      *TaskBuilder
	executionMode    realtime.TaskExecutionMode
	continuousConfig *realtime.ContinuousTaskConfig
	subscriptions    []realtime.EventSubscription
}

// NewRealtimeTaskBuilder 创建实时任务构建器
func NewRealtimeTaskBuilder(name, desc string, registry task.FunctionRegistry) *RealtimeTaskBuilder {
	return &RealtimeTaskBuilder{
		baseBuilder:   NewTaskBuilder(name, desc, registry),
		executionMode: realtime.ExecutionModeOneShot,
		subscriptions: make([]realtime.EventSubscription, 0),
	}
}

// WithContinuousMode 设置为持续运行模式
func (b *RealtimeTaskBuilder) WithContinuousMode() *RealtimeTaskBuilder {
	b.executionMode = realtime.ExecutionModeContinuous
	return b
}

// WithEventDrivenMode 设置为事件驱动模式
func (b *RealtimeTaskBuilder) WithEventDrivenMode() *RealtimeTaskBuilder {
	b.executionMode = realtime.ExecutionModeEventDriven
	return b
}

// WithEndpoint 设置连接端点
func (b *RealtimeTaskBuilder) WithEndpoint(endpoint, protocol string) *RealtimeTaskBuilder {
	if b.continuousConfig == nil {
		b.continuousConfig = realtime.NewContinuousTaskConfig("", "", realtime.TaskTypeDataCollector)
	}
	b.continuousConfig.Endpoint = endpoint
	b.continuousConfig.Protocol = protocol
	return b
}

// WithTaskType 设置持续任务类型
// 当设为 TaskTypeScheduledPoller 且 Mode 未显式设置时，自动设为 CollectorModePull
func (b *RealtimeTaskBuilder) WithTaskType(taskType realtime.ContinuousTaskType) *RealtimeTaskBuilder {
	if b.continuousConfig == nil {
		b.continuousConfig = realtime.NewContinuousTaskConfig("", "", taskType)
	} else {
		b.continuousConfig.Type = taskType
	}
	if taskType == realtime.TaskTypeScheduledPoller && b.continuousConfig.Mode == realtime.CollectorModePush {
		b.continuousConfig.Mode = realtime.CollectorModePull
	}
	return b
}

// WithCollector 设置采集器名称（对应 WorkflowBuilder.WithDataCollector 注册的名称）
func (b *RealtimeTaskBuilder) WithCollector(name string) *RealtimeTaskBuilder {
	if b.continuousConfig == nil {
		b.continuousConfig = realtime.NewContinuousTaskConfig("", "", realtime.TaskTypeDataCollector)
	}
	b.continuousConfig.CollectorName = name
	return b
}

// WithMode 设置采集器 Mode：push（推送/长连接）或 pull（拉取/定时轮询）
// 仅当 mode 为 CollectorModePush 或 CollectorModePull 时写入，否则保持原值
func (b *RealtimeTaskBuilder) WithMode(mode string) *RealtimeTaskBuilder {
	if b.continuousConfig == nil {
		b.continuousConfig = realtime.NewContinuousTaskConfig("", "", realtime.TaskTypeDataCollector)
	}
	if mode == realtime.CollectorModePush || mode == realtime.CollectorModePull {
		b.continuousConfig.Mode = mode
	}
	return b
}

// WithReconnect 配置重连策略
func (b *RealtimeTaskBuilder) WithReconnect(enabled bool, maxAttempts int) *RealtimeTaskBuilder {
	if b.continuousConfig == nil {
		b.continuousConfig = realtime.NewContinuousTaskConfig("", "", realtime.TaskTypeDataCollector)
	}
	b.continuousConfig.ReconnectEnabled = enabled
	b.continuousConfig.MaxReconnectAttempts = maxAttempts
	return b
}

// WithReconnectBackoff 配置重连退避策略
func (b *RealtimeTaskBuilder) WithReconnectBackoff(initialInterval, maxInterval time.Duration, multiplier float64) *RealtimeTaskBuilder {
	if b.continuousConfig == nil {
		b.continuousConfig = realtime.NewContinuousTaskConfig("", "", realtime.TaskTypeDataCollector)
	}
	b.continuousConfig.ReconnectBackoff = realtime.ReconnectBackoffConfig{
		InitialInterval: initialInterval,
		MaxInterval:     maxInterval,
		Multiplier:      multiplier,
		Jitter:          0.1,
	}
	return b
}

// WithBackpressure 配置背压策略
func (b *RealtimeTaskBuilder) WithBackpressure(threshold float64, action string) *RealtimeTaskBuilder {
	if b.continuousConfig == nil {
		b.continuousConfig = realtime.NewContinuousTaskConfig("", "", realtime.TaskTypeDataCollector)
	}
	b.continuousConfig.BackpressureThreshold = threshold
	b.continuousConfig.BackpressureAction = action
	return b
}

// WithBuffer 配置缓冲区
func (b *RealtimeTaskBuilder) WithBuffer(size int, batchSize int) *RealtimeTaskBuilder {
	if b.continuousConfig == nil {
		b.continuousConfig = realtime.NewContinuousTaskConfig("", "", realtime.TaskTypeDataCollector)
	}
	b.continuousConfig.BufferSize = size
	b.continuousConfig.BatchSize = batchSize
	return b
}

// WithFlushInterval 配置刷新间隔
func (b *RealtimeTaskBuilder) WithFlushInterval(interval time.Duration) *RealtimeTaskBuilder {
	if b.continuousConfig == nil {
		b.continuousConfig = realtime.NewContinuousTaskConfig("", "", realtime.TaskTypeDataCollector)
	}
	b.continuousConfig.FlushInterval = interval
	return b
}

// WithDataHandler 设置数据处理函数名
func (b *RealtimeTaskBuilder) WithDataHandler(handlerName string) *RealtimeTaskBuilder {
	if b.continuousConfig == nil {
		b.continuousConfig = realtime.NewContinuousTaskConfig("", "", realtime.TaskTypeDataCollector)
	}
	b.continuousConfig.DataHandler = handlerName
	return b
}

// WithDataHandlerMaxRetries 设置 DataHandler 失败时最大重试次数，0 表示不重试（失败即丢弃）
func (b *RealtimeTaskBuilder) WithDataHandlerMaxRetries(maxRetries int) *RealtimeTaskBuilder {
	if b.continuousConfig == nil {
		b.continuousConfig = realtime.NewContinuousTaskConfig("", "", realtime.TaskTypeDataCollector)
	}
	if maxRetries < 0 {
		maxRetries = 0
	}
	b.continuousConfig.DataHandlerMaxRetries = maxRetries
	return b
}

// WithErrorHandler 设置错误处理函数名
func (b *RealtimeTaskBuilder) WithErrorHandler(handlerName string) *RealtimeTaskBuilder {
	if b.continuousConfig == nil {
		b.continuousConfig = realtime.NewContinuousTaskConfig("", "", realtime.TaskTypeDataCollector)
	}
	b.continuousConfig.ErrorHandler = handlerName
	return b
}

// SubscribeEvent 订阅事件
func (b *RealtimeTaskBuilder) SubscribeEvent(eventType realtime.EventType, handlerName string) *RealtimeTaskBuilder {
	b.subscriptions = append(b.subscriptions, realtime.EventSubscription{
		EventType:   eventType,
		HandlerName: handlerName,
	})
	return b
}

// SubscribeEventWithPriority 订阅事件（带优先级）
func (b *RealtimeTaskBuilder) SubscribeEventWithPriority(eventType realtime.EventType, handlerName string, priority int) *RealtimeTaskBuilder {
	b.subscriptions = append(b.subscriptions, realtime.EventSubscription{
		EventType:   eventType,
		HandlerName: handlerName,
		Priority:    priority,
	})
	return b
}

// SubscribeEventWithFilter 订阅事件（带过滤器）
func (b *RealtimeTaskBuilder) SubscribeEventWithFilter(eventType realtime.EventType, handlerName, filter string) *RealtimeTaskBuilder {
	b.subscriptions = append(b.subscriptions, realtime.EventSubscription{
		EventType:   eventType,
		HandlerName: handlerName,
		Filter:      filter,
	})
	return b
}

// WithSubscriberName 设置订阅者名（多订阅者广播时，该 StreamProcessor 绑定到此订阅者）
func (b *RealtimeTaskBuilder) WithSubscriberName(name string) *RealtimeTaskBuilder {
	if b.continuousConfig == nil {
		b.continuousConfig = realtime.NewContinuousTaskConfig("", "", realtime.TaskTypeStreamProcessor)
	}
	b.continuousConfig.SubscriberName = name
	return b
}

// WithSubscriberFilter 设置订阅者过滤：按 data[field] 过滤，仅接收值在 values 内的数据；field 为空用 "code"，values 为空表示全量
func (b *RealtimeTaskBuilder) WithSubscriberFilter(field string, values []string) *RealtimeTaskBuilder {
	if b.continuousConfig == nil {
		b.continuousConfig = realtime.NewContinuousTaskConfig("", "", realtime.TaskTypeStreamProcessor)
	}
	b.continuousConfig.SubscriberFilterField = field
	b.continuousConfig.SubscriberFilterCodes = values
	return b
}

// WithSubscriberFilterCodes 设置订阅者仅接收的代码列表（按 data.code 过滤，空表示全量），等价于 WithSubscriberFilter("code", codes)
func (b *RealtimeTaskBuilder) WithSubscriberFilterCodes(codes []string) *RealtimeTaskBuilder {
	return b.WithSubscriberFilter("code", codes)
}

// WithBufferPolicyBlocking 设置该订阅者缓冲策略为阻塞 + 容量（关键下游如 DB）
func (b *RealtimeTaskBuilder) WithBufferPolicyBlocking(capacity int) *RealtimeTaskBuilder {
	if b.continuousConfig == nil {
		b.continuousConfig = realtime.NewContinuousTaskConfig("", "", realtime.TaskTypeStreamProcessor)
	}
	b.continuousConfig.BufferPolicy = &realtime.BufferPolicy{
		Mode:     realtime.BufferModeBlocking,
		Capacity: capacity,
	}
	return b
}

// WithBufferPolicyNonBlockingDrop 设置该订阅者缓冲策略为非阻塞满则丢弃（如前端推送）
func (b *RealtimeTaskBuilder) WithBufferPolicyNonBlockingDrop(capacity int) *RealtimeTaskBuilder {
	if b.continuousConfig == nil {
		b.continuousConfig = realtime.NewContinuousTaskConfig("", "", realtime.TaskTypeStreamProcessor)
	}
	b.continuousConfig.BufferPolicy = &realtime.BufferPolicy{
		Mode:     realtime.BufferModeNonBlockingDrop,
		Capacity: capacity,
	}
	return b
}

// WithJobFunction 设置 Job 函数（代理到基础构建器，并同步到 DataHandler 供 runStreamProcessor 调用）
func (b *RealtimeTaskBuilder) WithJobFunction(jobFuncName string, params map[string]interface{}) *RealtimeTaskBuilder {
	b.baseBuilder.WithJobFunction(jobFuncName, params)
	if jobFuncName != "" {
		if b.continuousConfig == nil {
			b.continuousConfig = realtime.NewContinuousTaskConfig("", "", realtime.TaskTypeDataCollector)
		}
		b.continuousConfig.DataHandler = jobFuncName
	}
	return b
}

// WithDependency 添加依赖（代理到基础构建器）
func (b *RealtimeTaskBuilder) WithDependency(taskName string) *RealtimeTaskBuilder {
	b.baseBuilder.WithDependency(taskName)
	return b
}

// WithDependencies 添加多个依赖（代理到基础构建器）
func (b *RealtimeTaskBuilder) WithDependencies(taskNames ...string) *RealtimeTaskBuilder {
	b.baseBuilder.WithDependencies(taskNames)
	return b
}

// WithTimeout 设置超时时间（代理到基础构建器）
func (b *RealtimeTaskBuilder) WithTimeout(seconds int) *RealtimeTaskBuilder {
	b.baseBuilder.WithTimeout(seconds)
	return b
}

// WithRetryCount 设置重试次数（代理到基础构建器）
func (b *RealtimeTaskBuilder) WithRetryCount(count int) *RealtimeTaskBuilder {
	b.baseBuilder.WithRetryCount(count)
	return b
}

// Build 构建实时任务
func (b *RealtimeTaskBuilder) Build() (*realtime.RealtimeTask, error) {
	// 构建基础任务
	baseTask, err := b.baseBuilder.Build()
	if err != nil {
		return nil, fmt.Errorf("构建基础任务失败: %w", err)
	}

	// 设置持续配置的 ID 和 Name
	if b.continuousConfig != nil {
		b.continuousConfig.ID = baseTask.GetID()
		b.continuousConfig.Name = baseTask.GetName()
	}

	// 创建实时任务
	rtTask := &realtime.RealtimeTask{
		Task:               baseTask,
		ExecutionMode:      b.executionMode,
		ContinuousConfig:   b.continuousConfig,
		EventSubscriptions: b.subscriptions,
	}

	return rtTask, nil
}

// BuildAsTask 构建为普通 Task（用于添加到 Workflow）
// 将实时任务的配置存储到 Task.Params 中
func (b *RealtimeTaskBuilder) BuildAsTask() (*task.Task, error) {
	rtTask, err := b.Build()
	if err != nil {
		return nil, err
	}

	// 将实时任务配置存储到 Params 中
	rtTask.Task.SetParam("execution_mode", string(rtTask.ExecutionMode))
	if rtTask.ContinuousConfig != nil {
		rtTask.Task.SetParam("continuous_config", rtTask.ContinuousConfig)
	}
	if len(rtTask.EventSubscriptions) > 0 {
		rtTask.Task.SetParam("event_subscriptions", rtTask.EventSubscriptions)
	}

	return rtTask.Task, nil
}

