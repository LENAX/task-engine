package storage

import (
	"context"
	"time"
)

// WorkflowInstanceRepository WorkflowInstance存储接口（对外导出）
type WorkflowInstanceRepository interface {
	// Save 保存WorkflowInstance
	Save(ctx context.Context, instance *WorkflowInstance) error
	// GetByID 根据ID查询WorkflowInstance
	GetByID(ctx context.Context, id string) (*WorkflowInstance, error)
	// UpdateStatus 更新WorkflowInstance状态
	UpdateStatus(ctx context.Context, id string, status string) error
	// UpdateBreakpoint 更新断点数据
	UpdateBreakpoint(ctx context.Context, id string, breakpoint *BreakpointData) error
	// ListByStatus 根据状态查询WorkflowInstance列表
	ListByStatus(ctx context.Context, status string) ([]*WorkflowInstance, error)
	// Delete 删除WorkflowInstance
	Delete(ctx context.Context, id string) error
}

// WorkflowInstance WorkflowInstance结构（对外导出）
type WorkflowInstance struct {
	ID           string          // 实例ID（系统自动生成的UUID）
	WorkflowID   string          // Workflow ID
	Status       string          // 状态（Ready/Running/Paused/Terminated/Success/Failed）
	Breakpoint   *BreakpointData // 断点数据（仅Paused/Terminated状态有值）
	StartTime    time.Time       // 启动时间
	EndTime      *time.Time      // 结束时间（成功/终止/失败时设置）
	ErrorMessage string          // 错误信息（失败时设置）
	CreateTime   time.Time       // 创建时间
}

// BreakpointData 断点数据（对外导出）
type BreakpointData struct {
	CompletedTaskNames []string               // 已完成的Task名称列表
	RunningTaskNames   []string               // 暂停时正在运行的Task名称
	DAGSnapshot        map[string]interface{} // DAG拓扑快照（JSON格式）
	ContextData        map[string]interface{} // 上下文数据（Task间传递的中间结果）
	LastUpdateTime     time.Time              // 最后更新时间
}
