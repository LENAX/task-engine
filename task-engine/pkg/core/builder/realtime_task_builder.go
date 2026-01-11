// Package builder 提供实时任务构建器
package builder

import (
	"fmt"
	"time"

	"github.com/stevelan1995/task-engine/pkg/core/realtime"
	"github.com/stevelan1995/task-engine/pkg/core/task"
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
func (b *RealtimeTaskBuilder) WithTaskType(taskType realtime.ContinuousTaskType) *RealtimeTaskBuilder {
	if b.continuousConfig == nil {
		b.continuousConfig = realtime.NewContinuousTaskConfig("", "", taskType)
	} else {
		b.continuousConfig.Type = taskType
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

// WithJobFunction 设置 Job 函数（代理到基础构建器）
func (b *RealtimeTaskBuilder) WithJobFunction(jobFuncName string, params map[string]interface{}) *RealtimeTaskBuilder {
	b.baseBuilder.WithJobFunction(jobFuncName, params)
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

