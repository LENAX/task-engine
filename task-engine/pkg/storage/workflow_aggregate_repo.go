package storage

import (
	"context"

	"github.com/stevelan1995/task-engine/pkg/core/workflow"
)

// WorkflowAggregateRepository Workflow聚合根Repository（对外导出）
// 将Workflow作为聚合根，统一管理Workflow及其关联实体（Task定义、WorkflowInstance）的事务操作
// 所有操作都在单个事务中完成，保证数据一致性
//
// 幂等性保证：
//   - 所有写操作（Save/Update/Delete）均为幂等操作
//   - 重复执行相同操作不会产生副作用或错误
//   - 删除不存在的记录不会报错
//   - 更新不存在的记录不会报错
type WorkflowAggregateRepository interface {
	// ========== Workflow定义相关操作 ==========

	// SaveWorkflow 保存Workflow及其关联的Task定义（事务，幂等）
	// 如果Workflow已存在则更新，同时更新所有Task定义
	// 注意：此方法会删除旧的Task定义并插入新的Task定义（全量替换）
	SaveWorkflow(ctx context.Context, wf *workflow.Workflow) error

	// GetWorkflow 根据ID获取Workflow（不含Task定义）
	// 如果不存在返回 nil, nil
	GetWorkflow(ctx context.Context, id string) (*workflow.Workflow, error)

	// GetWorkflowWithTasks 根据ID获取Workflow及其所有Task定义
	// 返回的Workflow对象的Tasks字段会被填充
	// 如果不存在返回 nil, nil
	GetWorkflowWithTasks(ctx context.Context, id string) (*workflow.Workflow, error)

	// DeleteWorkflow 删除Workflow及其所有关联数据（事务，幂等）
	// 包括：Task定义、所有WorkflowInstance、所有TaskInstance
	// 如果Workflow不存在，不会报错（幂等）
	DeleteWorkflow(ctx context.Context, id string) error

	// ListWorkflows 列出所有Workflow（不含Task定义）
	ListWorkflows(ctx context.Context) ([]*workflow.Workflow, error)

	// ========== WorkflowInstance相关操作 ==========

	// StartWorkflow 启动Workflow，创建WorkflowInstance和关联的TaskInstance（事务）
	// 返回创建的WorkflowInstance
	// 注意：每次调用都会创建新的Instance，不是幂等操作
	StartWorkflow(ctx context.Context, wf *workflow.Workflow) (*workflow.WorkflowInstance, error)

	// GetWorkflowInstance 根据ID获取WorkflowInstance
	// 如果不存在返回 nil, nil
	GetWorkflowInstance(ctx context.Context, instanceID string) (*workflow.WorkflowInstance, error)

	// GetWorkflowInstanceWithTasks 根据ID获取WorkflowInstance及其所有TaskInstance
	// 如果不存在返回 nil, nil, nil
	GetWorkflowInstanceWithTasks(ctx context.Context, instanceID string) (*workflow.WorkflowInstance, []*TaskInstance, error)

	// UpdateWorkflowInstanceStatus 更新WorkflowInstance状态（幂等）
	// 如果Instance不存在，不会报错（幂等）
	UpdateWorkflowInstanceStatus(ctx context.Context, instanceID string, status string) error

	// DeleteWorkflowInstance 删除WorkflowInstance及其所有TaskInstance（事务，幂等）
	// 如果Instance不存在，不会报错（幂等）
	DeleteWorkflowInstance(ctx context.Context, instanceID string) error

	// ListWorkflowInstances 根据WorkflowID列出所有WorkflowInstance
	ListWorkflowInstances(ctx context.Context, workflowID string) ([]*workflow.WorkflowInstance, error)

	// ========== TaskInstance相关操作 ==========

	// GetTaskInstance 根据ID获取TaskInstance
	// 如果不存在返回 nil, nil
	GetTaskInstance(ctx context.Context, taskID string) (*TaskInstance, error)

	// UpdateTaskInstanceStatus 更新TaskInstance状态（幂等）
	// 如果TaskInstance不存在，不会报错（幂等）
	UpdateTaskInstanceStatus(ctx context.Context, taskID string, status string) error

	// UpdateTaskInstanceStatusWithError 更新TaskInstance状态和错误信息（幂等）
	// 如果TaskInstance不存在，不会报错（幂等）
	UpdateTaskInstanceStatusWithError(ctx context.Context, taskID string, status string, errorMsg string) error

	// SaveTaskInstance 保存TaskInstance（幂等）
	// 如果已存在则更新，不存在则创建
	SaveTaskInstance(ctx context.Context, task *TaskInstance) error

	// DeleteTaskInstance 删除TaskInstance（幂等）
	// 如果TaskInstance不存在，不会报错（幂等）
	DeleteTaskInstance(ctx context.Context, taskID string) error

	// GetTaskInstancesByWorkflowInstance 根据WorkflowInstance ID获取所有TaskInstance
	GetTaskInstancesByWorkflowInstance(ctx context.Context, instanceID string) ([]*TaskInstance, error)
}

// TaskDefinition Task定义结构（对外导出）
// 用于持久化Workflow中的Task定义
type TaskDefinition struct {
	ID                   string              // Task ID
	WorkflowID           string              // 所属Workflow ID
	Name                 string              // Task名称（在Workflow内唯一）
	Description          string              // Task描述
	JobFuncID            string              // Job函数ID
	JobFuncName          string              // Job函数名称
	CompensationFuncID   string              // 补偿函数ID
	CompensationFuncName string              // 补偿函数名称
	Params               map[string]any      // 执行参数
	TimeoutSeconds       int                 // 超时时间（秒）
	RetryCount           int                 // 重试次数
	Dependencies         []string            // 依赖的前置Task名称列表
	RequiredParams       []string            // 必需参数列表
	ResultMapping        map[string]string   // 结果映射规则
	StatusHandlers       map[string][]string // 状态处理器映射
	IsTemplate           bool                // 是否为模板任务
}

