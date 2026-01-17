package executor

import (
	"time"

	"github.com/LENAX/task-engine/pkg/core/workflow"
)

// PendingTask 待调度的Task结构（对外导出）
type PendingTask struct {
	Task       workflow.Task         // Task实例（使用接口，避免循环依赖）
	WorkflowID string                // Workflow ID
	InstanceID string                // WorkflowInstance ID
	Domain     string                // 业务域名称
	RetryCount int                   // 当前重试次数
	MaxRetries int                   // 最大重试次数
	OnComplete func(*TaskResult)     // 完成回调（可选，与 StatusChan 二选一或同时使用）
	OnError    func(error)           // 错误回调（可选，与 StatusChan 二选一或同时使用）
	StatusChan chan *TaskStatusEvent // 任务状态事件 channel（可选，基于 channel 的监听模式）
}

// TaskResult Task执行结果（对外导出）
type TaskResult struct {
	TaskID   string
	Status   string // Success/Failed/TimeoutFailed
	Data     interface{}
	Error    error
	Duration int64 // 执行时长（毫秒）
}

// TaskStatusEvent 任务状态事件（通过 channel 传递，对外导出）
// 用于基于 channel 的监听模式，替代回调函数
type TaskStatusEvent struct {
	TaskID     string      // 任务ID
	Status     string      // Success, Failed, Timeout
	Result     interface{} // 任务结果（Success 时）
	Error      error       // 错误信息（Failed 时）
	IsTemplate bool        // 是否为模板任务
	IsSubTask  bool        // 是否为子任务
	ParentID   string      // 父任务ID（子任务特有）
	Timestamp  time.Time   // 事件时间戳
	Duration   int64       // 执行时长（毫秒）
}
