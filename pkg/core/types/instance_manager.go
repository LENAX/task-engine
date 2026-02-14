package types

import (
	"context"
)

// WorkflowInstanceManager 定义WorkflowInstanceManager接口（对外导出）
// 用于解耦Engine和WorkflowInstanceManager的具体实现
// 注意：接口方法中使用 interface{} 来避免循环依赖，具体实现需要进行类型转换
type WorkflowInstanceManager interface {
	// Start 启动WorkflowInstance执行
	Start()

	// Shutdown 优雅关闭WorkflowInstanceManager
	Shutdown()

	// GetControlSignalChannel 获取控制信号通道
	// 返回类型为 interface{}，实际类型为 chan<- workflow.ControlSignal
	GetControlSignalChannel() interface{}

	// GetStatusUpdateChannel 获取状态更新通道
	GetStatusUpdateChannel() <-chan string

	// AddSubTask 动态添加子任务到WorkflowInstance
	// subTask 类型为 Task（即 types.Task，workflow.Task 是它的别名）
	AddSubTask(subTask Task, parentTaskID string) error

	// AtomicAddSubTasks 原子性地添加多个子任务到WorkflowInstance
	// 保证要么全部成功，要么全部失败（回滚）
	// subTasks 类型为 []Task（即 []types.Task，workflow.Task 是它的别名）
	// parentTaskID 父任务ID
	// 返回错误时，所有已添加的子任务都会被回滚
	AtomicAddSubTasks(subTasks []Task, parentTaskID string) error

	// RestoreFromBreakpoint 从断点数据恢复WorkflowInstance状态
	// breakpoint 类型为 *workflow.BreakpointData
	RestoreFromBreakpoint(breakpoint interface{}) error

	// CreateBreakpoint 创建断点数据
	// 返回类型为 *workflow.BreakpointData
	CreateBreakpoint() interface{}

	// GetInstanceID 获取WorkflowInstance ID
	GetInstanceID() string

	// GetStatus 获取WorkflowInstance状态
	GetStatus() string

	// Context 获取context（用于监听取消信号）
	Context() context.Context

	// GetProgress 获取当前实例的内存中任务进度（总任务数、已完成、运行中、失败、待执行）
	// 用于运行中实例的实时进度展示，包含动态子任务
	GetProgress() ProgressSnapshot
}

// ProgressSnapshot 内存中的任务进度快照（与入库数据无关）
// Running = len(RunningTaskIDs)，Pending = 各层级队列中待运行任务总数，PendingTaskIDs 为其 ID 列表（可能截断）
type ProgressSnapshot struct {
	Total          int      // 总任务数（含动态子任务）
	Completed      int      // 成功完成数
	Running        int      // 正在执行数
	Failed         int      // 失败数
	Pending        int      // 各层级待运行任务总数
	RunningTaskIDs []string // 正在执行的任务 ID 列表
	PendingTaskIDs []string // 待运行的任务 ID 列表（当前层优先，可能截断）
}
