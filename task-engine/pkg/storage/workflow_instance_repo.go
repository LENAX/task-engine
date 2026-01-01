package storage

import (
	"context"

	"github.com/stevelan1995/task-engine/pkg/core/workflow"
)

// WorkflowInstanceCRUDRepository WorkflowInstance通用CRUD接口（对外导出）
// 提供基础的增删改查操作
type WorkflowInstanceCRUDRepository interface {
	BaseRepository
	// Save 保存WorkflowInstance（创建或更新）
	Save(ctx context.Context, instance *workflow.WorkflowInstance) error
	// GetByID 根据ID查询WorkflowInstance
	GetByID(ctx context.Context, id string) (*workflow.WorkflowInstance, error)
	// Delete 删除WorkflowInstance
	Delete(ctx context.Context, id string) error
}

// WorkflowInstanceRepository WorkflowInstance业务存储接口（对外导出）
// 组合了通用CRUD接口，并添加了业务特定的方法
type WorkflowInstanceRepository interface {
	// 继承通用CRUD接口
	WorkflowInstanceCRUDRepository

	// UpdateStatus 更新WorkflowInstance状态（业务接口）
	UpdateStatus(ctx context.Context, id string, status string) error
	// UpdateBreakpoint 更新断点数据（业务接口）
	UpdateBreakpoint(ctx context.Context, id string, breakpoint *workflow.BreakpointData) error
	// ListByStatus 根据状态查询WorkflowInstance列表（业务接口）
	ListByStatus(ctx context.Context, status string) ([]*workflow.WorkflowInstance, error)
}

// WorkflowInstance 类型别名，保持向后兼容（对外导出）
// 实际使用 workflow.WorkflowInstance
type WorkflowInstance = workflow.WorkflowInstance

// BreakpointData 类型别名，保持向后兼容（对外导出）
// 实际使用 workflow.BreakpointData
type BreakpointData = workflow.BreakpointData
