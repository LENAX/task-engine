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
}
