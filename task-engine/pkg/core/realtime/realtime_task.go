// Package realtime 提供实时数据采集任务的扩展任务类型
package realtime

import (
	"github.com/stevelan1995/task-engine/pkg/core/task"
)

// TaskExecutionMode 任务执行模式
type TaskExecutionMode string

const (
	// ExecutionModeOneShot 一次性执行（默认，批处理任务）
	ExecutionModeOneShot TaskExecutionMode = "oneshot"
	// ExecutionModeContinuous 持续运行（实时任务）
	ExecutionModeContinuous TaskExecutionMode = "continuous"
	// ExecutionModeEventDriven 事件驱动（实时任务）
	ExecutionModeEventDriven TaskExecutionMode = "event"
)

// RealtimeTask 扩展的实时任务（组合现有 Task）
// 注意：仅在 Workflow.ExecutionMode == "streaming" 时使用
type RealtimeTask struct {
	// 嵌入现有 Task
	*task.Task

	// 实时扩展字段
	ExecutionMode TaskExecutionMode `json:"execution_mode"` // 任务执行模式

	// 持续任务配置（仅在 ExecutionMode 为 continuous 时使用）
	ContinuousConfig *ContinuousTaskConfig `json:"continuous_config,omitempty"`

	// 事件订阅配置
	EventSubscriptions []EventSubscription `json:"event_subscriptions,omitempty"`
}

// NewRealtimeTask 创建实时任务
func NewRealtimeTask(baseTask *task.Task, executionMode TaskExecutionMode) *RealtimeTask {
	return &RealtimeTask{
		Task:               baseTask,
		ExecutionMode:      executionMode,
		EventSubscriptions: make([]EventSubscription, 0),
	}
}

// NewContinuousRealtimeTask 创建持续运行的实时任务
func NewContinuousRealtimeTask(baseTask *task.Task, config *ContinuousTaskConfig) *RealtimeTask {
	return &RealtimeTask{
		Task:               baseTask,
		ExecutionMode:      ExecutionModeContinuous,
		ContinuousConfig:   config,
		EventSubscriptions: make([]EventSubscription, 0),
	}
}

// NewEventDrivenRealtimeTask 创建事件驱动的实时任务
func NewEventDrivenRealtimeTask(baseTask *task.Task, subscriptions []EventSubscription) *RealtimeTask {
	return &RealtimeTask{
		Task:               baseTask,
		ExecutionMode:      ExecutionModeEventDriven,
		EventSubscriptions: subscriptions,
	}
}

// WithContinuousConfig 设置持续任务配置
func (rt *RealtimeTask) WithContinuousConfig(config *ContinuousTaskConfig) *RealtimeTask {
	rt.ContinuousConfig = config
	return rt
}

// WithEventSubscriptions 设置事件订阅
func (rt *RealtimeTask) WithEventSubscriptions(subscriptions []EventSubscription) *RealtimeTask {
	rt.EventSubscriptions = subscriptions
	return rt
}

// AddEventSubscription 添加事件订阅
func (rt *RealtimeTask) AddEventSubscription(subscription EventSubscription) *RealtimeTask {
	rt.EventSubscriptions = append(rt.EventSubscriptions, subscription)
	return rt
}

// IsContinuous 判断是否为持续运行任务
func (rt *RealtimeTask) IsContinuous() bool {
	return rt.ExecutionMode == ExecutionModeContinuous
}

// IsEventDriven 判断是否为事件驱动任务
func (rt *RealtimeTask) IsEventDriven() bool {
	return rt.ExecutionMode == ExecutionModeEventDriven
}

// IsOneShot 判断是否为一次性任务
func (rt *RealtimeTask) IsOneShot() bool {
	return rt.ExecutionMode == ExecutionModeOneShot
}

// GetExecutionMode 获取执行模式
func (rt *RealtimeTask) GetExecutionMode() TaskExecutionMode {
	return rt.ExecutionMode
}

// GetContinuousConfig 获取持续任务配置
func (rt *RealtimeTask) GetContinuousConfig() *ContinuousTaskConfig {
	return rt.ContinuousConfig
}

// GetEventSubscriptions 获取事件订阅列表
func (rt *RealtimeTask) GetEventSubscriptions() []EventSubscription {
	if rt.EventSubscriptions == nil {
		return make([]EventSubscription, 0)
	}
	// 返回副本
	result := make([]EventSubscription, len(rt.EventSubscriptions))
	copy(result, rt.EventSubscriptions)
	return result
}

// ExtractRealtimeTask 从 Task 中提取实时任务信息
// 如果 Task 是 RealtimeTask 类型，直接返回
// 否则尝试从 Task.Params 中提取实时任务配置
func ExtractRealtimeTask(t interface{}) *RealtimeTask {
	// 方式1：检查 Task 类型
	if rtTask, ok := t.(*RealtimeTask); ok {
		return rtTask
	}

	// 方式2：检查是否是 *task.Task 并从 Params 中提取配置
	baseTask, ok := t.(*task.Task)
	if !ok {
		return nil
	}

	params := baseTask.GetParams()
	if params == nil {
		return nil
	}

	// 检查是否有 execution_mode 参数
	mode, ok := params["execution_mode"]
	if !ok {
		return nil
	}

	modeStr, ok := mode.(string)
	if !ok {
		return nil
	}

	// 如果是持续运行或事件驱动模式，创建 RealtimeTask
	if modeStr == "continuous" || modeStr == "event" {
		rtTask := &RealtimeTask{
			Task:          baseTask,
			ExecutionMode: TaskExecutionMode(modeStr),
		}

		// 尝试提取持续任务配置
		if configData, ok := params["continuous_config"]; ok {
			if config, ok := configData.(*ContinuousTaskConfig); ok {
				rtTask.ContinuousConfig = config
			}
		}

		return rtTask
	}

	return nil
}

// IsRealtimeTask 判断给定的 Task 是否为实时任务
func IsRealtimeTask(t interface{}) bool {
	return ExtractRealtimeTask(t) != nil
}
