package builder

import (
	"fmt"

	"github.com/LENAX/task-engine/pkg/core/dag"
	"github.com/LENAX/task-engine/pkg/core/realtime"
	"github.com/LENAX/task-engine/pkg/core/task"
	"github.com/LENAX/task-engine/pkg/core/workflow"
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

// WithCronExpr 设置Cron表达式（链式构建，对外导出）
// cronExpr: Cron表达式，支持秒级精度（如 "0 0 0 * * *" 表示每天凌晨执行）
func (b *WorkflowBuilder) WithCronExpr(cronExpr string) *WorkflowBuilder {
	b.wf.SetCronExpr(cronExpr)
	b.wf.SetCronEnabled(true)
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

// WithRealtimeTask 向当前Workflow中添加实时任务（链式构建，对外导出）
// rtTask: RealtimeTask实例（需提前通过RealtimeTaskBuilder构建）
// 注意：添加实时任务时会自动将执行模式设置为 streaming
func (b *WorkflowBuilder) WithRealtimeTask(rtTask *realtime.RealtimeTask) *WorkflowBuilder {
	if rtTask != nil && rtTask.Task != nil {
		// 将实时任务配置存储到 Task.Params 中
		rtTask.Task.SetParam("execution_mode", string(rtTask.ExecutionMode))
		if rtTask.ContinuousConfig != nil {
			rtTask.Task.SetParam("continuous_config", rtTask.ContinuousConfig)
		}
		if len(rtTask.EventSubscriptions) > 0 {
			rtTask.Task.SetParam("event_subscriptions", rtTask.EventSubscriptions)
		}
		b.tasks = append(b.tasks, rtTask.Task)
	}
	return b
}

// WithStreamingMode 设置为流处理模式（链式构建，对外导出）
// 当调用此方法时，Workflow.ExecutionMode 会被设置为 "streaming"
// Engine 会根据此字段自动选择 RealtimeInstanceManager
func (b *WorkflowBuilder) WithStreamingMode() *WorkflowBuilder {
	b.wf.SetExecutionMode(workflow.ExecutionModeStreaming)
	return b
}

// WithBatchMode 设置为批处理模式（链式构建，对外导出）
// 默认就是批处理模式，但可以显式调用以明确意图
func (b *WorkflowBuilder) WithBatchMode() *WorkflowBuilder {
	b.wf.SetExecutionMode(workflow.ExecutionModeBatch)
	return b
}

// WithParams 设置自定义参数（链式构建，对外导出）
func (b *WorkflowBuilder) WithParams(params map[string]string) *WorkflowBuilder {
	b.wf.SetParams(params)
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

	// 2. 将Task添加到Workflow中（使用AddTask方法）
	for _, t := range b.tasks {
		if err := b.wf.AddTask(t); err != nil {
			return nil, fmt.Errorf("添加Task %s 失败: %w", t.Name, err)
		}
	}

	// 3. 根据各Task声明的前置Task名称匹配对应的Task ID，构建全局依赖关系
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
		b.wf.Dependencies.Store(t.ID, depIDs)
	}

	// 4. 构建DAG并检测循环依赖
	dagInstance, err := dag.BuildDAG(b.wf.GetTasks(), b.wf.GetDependencies())
	if err != nil {
		return nil, fmt.Errorf("构建DAG失败: %w", err)
	}

	// 检测循环依赖
	if err := dagInstance.DetectCycle(); err != nil {
		return nil, fmt.Errorf("检测到循环依赖: %w", err)
	}

	// 5. 校验Workflow合法性
	if err := b.wf.Validate(); err != nil {
		return nil, fmt.Errorf("Workflow校验失败: %w", err)
	}

	return b.wf, nil
}
