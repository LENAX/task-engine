package builder

import (
	"fmt"

	"github.com/stevelan1995/task-engine/pkg/core/task"
	"github.com/stevelan1995/task-engine/pkg/core/workflow"
)

// WorkflowBuilder Workflow构建器（对外导出）
type WorkflowBuilder struct {
	wf    *workflow.Workflow
	tasks []*task.Task // 临时存储Task列表，Build时处理
}

// NewWorkflowBuilder 创建构建器（对外导出）
func NewWorkflowBuilder(name, desc string) *WorkflowBuilder {
	return &WorkflowBuilder{
		wf:    workflow.NewWorkflow(name, desc),
		tasks: make([]*task.Task, 0),
	}
}

// WithName 设置Workflow的业务名称（链式构建，对外导出）
// name: Workflow名称（字符串，用于标识业务含义）
func (b *WorkflowBuilder) WithName(name string) *WorkflowBuilder {
	b.wf.Name = name
	return b
}

// WithTask 向当前Workflow中添加任务（链式构建，对外导出）
// task: Task实例（需提前通过TaskBuilder构建）
func (b *WorkflowBuilder) WithTask(t *task.Task) *WorkflowBuilder {
	if t != nil {
		b.tasks = append(b.tasks, t)
	}
	return b
}

// WithParams 设置自定义参数（链式构建，对外导出）
func (b *WorkflowBuilder) WithParams(params map[string]string) *WorkflowBuilder {
	b.wf.Params = params
	return b
}

// Build 完成Workflow构建（对外导出）
// 自动生成Workflow UUID作为唯一标识，自动根据各Task声明的前置Task名称匹配对应的Task，提取依赖关系并构建全局依赖
// 若存在Task名称重复、依赖Task名称不存在等问题则返回错误
func (b *WorkflowBuilder) Build() (*workflow.Workflow, error) {
	if len(b.tasks) == 0 {
		// 空Workflow是合法的
		return b.wf, nil
	}

	// 1. 校验Task名称唯一性并建立名称到ID的映射
	nameToID := make(map[string]string) // Task名称 -> Task ID
	for _, t := range b.tasks {
		if t.Name == "" {
			return nil, fmt.Errorf("Task ID %s 名称为空", t.ID)
		}
		if existingID, exists := nameToID[t.Name]; exists {
			return nil, fmt.Errorf("Task名称 %s 重复（Task ID: %s 和 %s）", t.Name, existingID, t.ID)
		}
		nameToID[t.Name] = t.ID
	}

	// 2. 将Task添加到Workflow中
	if b.wf.Tasks == nil {
		b.wf.Tasks = make(map[string]workflow.Task)
	}
	for _, t := range b.tasks {
		b.wf.Tasks[t.ID] = t
	}

	// 3. 根据各Task声明的前置Task名称匹配对应的Task ID，构建全局依赖关系
	if b.wf.Dependencies == nil {
		b.wf.Dependencies = make(map[string][]string)
	}

	for _, t := range b.tasks {
		// 获取该Task的依赖列表（存储的是Task名称）
		deps := t.GetDependencies()
		if len(deps) == 0 {
			continue // 无依赖，跳过
		}

		// 为每个依赖名称查找对应的Task ID
		depIDs := make([]string, 0)
		for _, depName := range deps {
			depID, exists := nameToID[depName]
			if !exists {
				return nil, fmt.Errorf("Task %s 依赖的Task名称 %s 不存在", t.Name, depName)
			}
			// 检查是否形成自依赖
			if depID == t.ID {
				return nil, fmt.Errorf("Task %s 不能依赖自己", t.Name)
			}
			depIDs = append(depIDs, depID)
		}

		// 更新全局依赖关系（后置Task ID -> 前置Task ID列表）
		b.wf.Dependencies[t.ID] = depIDs
	}

	// 4. 校验Workflow合法性
	if err := b.wf.Validate(); err != nil {
		return nil, fmt.Errorf("Workflow校验失败: %w", err)
	}

	return b.wf, nil
}
