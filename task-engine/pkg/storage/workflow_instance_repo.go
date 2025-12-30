package storage

import (
	"context"

	"github.com/stevelan1995/task-engine/pkg/core/workflow"
)

// WorkflowInstanceRepository WorkflowInstance存储接口（对外导出）
type WorkflowInstanceRepository interface {
	// Save 保存WorkflowInstance
	Save(ctx context.Context, instance *workflow.WorkflowInstance) error
	// GetByID 根据ID查询WorkflowInstance
	GetByID(ctx context.Context, id string) (*workflow.WorkflowInstance, error)
	// UpdateStatus 更新WorkflowInstance状态
	UpdateStatus(ctx context.Context, id string, status string) error
	// UpdateBreakpoint 更新断点数据
	UpdateBreakpoint(ctx context.Context, id string, breakpoint *workflow.BreakpointData) error
	// ListByStatus 根据状态查询WorkflowInstance列表
	ListByStatus(ctx context.Context, status string) ([]*workflow.WorkflowInstance, error)
	// Delete 删除WorkflowInstance
	Delete(ctx context.Context, id string) error
}

// WorkflowInstance 类型别名，保持向后兼容（对外导出）
// 实际使用 workflow.WorkflowInstance
type WorkflowInstance = workflow.WorkflowInstance

// BreakpointData 类型别名，保持向后兼容（对外导出）
// 实际使用 workflow.BreakpointData
type BreakpointData = workflow.BreakpointData
