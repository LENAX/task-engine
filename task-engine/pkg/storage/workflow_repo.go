package storage

import (
	"context"

	"github.com/stevelan1995/task-engine/pkg/core/workflow"
)

// WorkflowCRUDRepository Workflow通用CRUD接口（对外导出）
// 提供基础的增删改查操作
type WorkflowCRUDRepository interface {
	BaseRepository
	// Save 保存Workflow（创建或更新）
	Save(ctx context.Context, wf *workflow.Workflow) error
	// GetByID 根据ID查询Workflow
	GetByID(ctx context.Context, id string) (*workflow.Workflow, error)
	// Delete 删除Workflow
	Delete(ctx context.Context, id string) error
}

// WorkflowRepository Workflow业务存储接口（对外导出）
// 组合了通用CRUD接口，并添加了业务特定的方法
type WorkflowRepository interface {
	// 继承通用CRUD接口
	WorkflowCRUDRepository

	// SaveWithTasks 在事务中同时保存Workflow和所有预定义的Task（业务接口）
	// workflowInstanceID: WorkflowInstance ID，用于关联Task
	// taskRepo: TaskRepository，用于保存Task
	// registry: FunctionRegistry，用于获取JobFuncID
	SaveWithTasks(ctx context.Context, wf *workflow.Workflow, workflowInstanceID string, taskRepo TaskRepository, registry interface{}) error
}
