package executor

import (
	"github.com/stevelan1995/task-engine/pkg/core/task"
)

// PendingTask 待调度的Task结构（对外导出）
type PendingTask struct {
	Task        *task.Task      // Task实例
	WorkflowID  string          // Workflow ID
	InstanceID  string          // WorkflowInstance ID
	Domain      string          // 业务域名称
	RetryCount  int             // 当前重试次数
	MaxRetries  int             // 最大重试次数
	OnComplete  func(*TaskResult) // 完成回调
	OnError     func(error)     // 错误回调
}

// TaskResult Task执行结果（对外导出）
type TaskResult struct {
	TaskID    string
	Status    string // Success/Failed/TimeoutFailed
	Data      interface{}
	Error     error
	Duration  int64 // 执行时长（毫秒）
}

