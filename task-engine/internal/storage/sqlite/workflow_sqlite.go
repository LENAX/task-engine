package sqlite

import (
    "github.com/stevelan1995/task-engine/pkg/core/workflow"
    "github.com/stevelan1995/task-engine/pkg/storage"
    "context"
    "sync"
)

// workflowRepo SQLite实现（小写，不导出）
type workflowRepo struct {
    data map[string]*workflow.Workflow
    mu   sync.RWMutex
}

// NewWorkflowRepo 创建SQLite存储实例（内部工厂方法，不导出）
func NewWorkflowRepo() storage.WorkflowRepository {
    return &workflowRepo{
        data: make(map[string]*workflow.Workflow),
    }
}

// Save 实现存储接口（内部实现）
func (r *workflowRepo) Save(ctx context.Context, wf *workflow.Workflow) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.data[wf.ID] = wf
    return nil
}

// GetByID 实现存储接口（内部实现）
func (r *workflowRepo) GetByID(ctx context.Context, id string) (*workflow.Workflow, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    return r.data[id], nil
}

// Delete 实现存储接口（内部实现）
func (r *workflowRepo) Delete(ctx context.Context, id string) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    delete(r.data, id)
    return nil
}
