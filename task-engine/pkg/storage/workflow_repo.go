package storage

import (
    "github.com/stevelan1995/task-engine/pkg/core/workflow"
    "context"
)

// WorkflowRepository Workflow存储接口（对外导出）
type WorkflowRepository interface {
    // Save 保存Workflow（对外接口）
    Save(ctx context.Context, wf *workflow.Workflow) error
    // GetByID 根据ID查询Workflow（对外接口）
    GetByID(ctx context.Context, id string) (*workflow.Workflow, error)
    // Delete 删除Workflow（对外接口）
    Delete(ctx context.Context, id string) error
}
