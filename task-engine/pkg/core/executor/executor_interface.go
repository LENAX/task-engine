package executor

import (
	"github.com/stevelan1995/task-engine/pkg/core/task"
)

// Executor 任务执行器接口（对外导出）
type Executor interface {
	// Start 启动执行器
	Start()
	// Shutdown 关闭执行器
	Shutdown() error
	// SetPoolSize 动态调整Executor的全局并发池大小
	SetPoolSize(maxSize int) error
	// SetDomainPoolSize 动态调整指定业务域的子池大小
	SetDomainPoolSize(domain string, size int) error
	// GetDomainPoolStatus 查询指定业务域子池的状态
	GetDomainPoolStatus(domain string) (int, int, error)
	// SetRegistry 设置Job函数注册中心
	SetRegistry(registry task.FunctionRegistry)
	// SubmitTask 将待调度Task提交至Executor的任务队列
	SubmitTask(pendingTask *PendingTask) error
}
