package workflow

import (
    "time"
    "github.com/google/uuid"
)

// Workflow Workflow核心结构体（对外导出）
type Workflow struct {
    ID          string            `json:"id"`
    Name        string            `json:"name"`
    Description string            `json:"description"`
    Params      map[string]string `json:"params"`
    CreateTime  time.Time         `json:"create_time"`
    Status      string            `json:"status"` // ENABLED/DISABLED
}

// WorkflowInstance Workflow实例（对外导出）
type WorkflowInstance struct {
    ID         string    `json:"instance_id"`
    WorkflowID string    `json:"workflow_id"`
    Status     string    `json:"status"` // RUNNING/SUCCESS/FAILED
    StartTime  time.Time `json:"start_time"`
    EndTime    time.Time `json:"end_time"`
}

// NewWorkflow 创建Workflow实例（对外导出）
func NewWorkflow(name, desc string) *Workflow {
    return &Workflow{
        ID:          uuid.NewString(),
        Name:        name,
        Description: desc,
        Status:      "ENABLED",
        CreateTime:  time.Now(),
    }
}

// Run 运行Workflow（对外导出）
func (w *Workflow) Run() (*WorkflowInstance, error) {
    instance := &WorkflowInstance{
        ID:         uuid.NewString(),
        WorkflowID: w.ID,
        Status:     "RUNNING",
        StartTime:  time.Now(),
    }
    return instance, nil
}
