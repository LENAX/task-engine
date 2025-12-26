package builder

import (
    "github.com/stevelan1995/task-engine/pkg/core/workflow"
)

// WorkflowBuilder Workflow构建器（对外导出）
type WorkflowBuilder struct {
    wf *workflow.Workflow
}

// NewWorkflowBuilder 创建构建器（对外导出）
func NewWorkflowBuilder(name, desc string) *WorkflowBuilder {
    return &WorkflowBuilder{
        wf: workflow.NewWorkflow(name, desc),
    }
}

// WithParams 设置自定义参数（链式构建，对外导出）
func (b *WorkflowBuilder) WithParams(params map[string]string) *WorkflowBuilder {
    b.wf.Params = params
    return b
}

// Build 构建Workflow实例（对外导出）
func (b *WorkflowBuilder) Build() *workflow.Workflow {
    return b.wf
}
