package builder

import (
	"testing"
)

func TestWorkflowBuilder_Basic(t *testing.T) {
	builder := NewWorkflowBuilder("test-workflow", "测试工作流")
	
	task1, _ := NewTaskBuilder("task1", "任务1").
		WithJobFunction("func1", nil).
		Build()
	
	task2, _ := NewTaskBuilder("task2", "任务2").
		WithJobFunction("func2", nil).
		WithDependency("task1").
		Build()
	
	workflow, err := builder.
		WithName("updated-name").
		WithTask(task1).
		WithTask(task2).
		Build()
	
	if err != nil {
		t.Fatalf("构建Workflow失败: %v", err)
	}
	
	if workflow == nil {
		t.Fatal("Workflow为nil")
	}
	
	if workflow.GetName() != "updated-name" {
		t.Errorf("Workflow名称错误，期望: updated-name, 实际: %s", workflow.GetName())
	}
	
	tasks := workflow.GetTasks()
	if len(tasks) != 2 {
		t.Fatalf("Task数量错误，期望: 2, 实际: %d", len(tasks))
	}
	
	// 检查依赖关系
	deps := workflow.GetDependencies()
	if len(deps) == 0 {
		t.Fatal("依赖关系未构建")
	}
	
	// task2应该依赖task1
	task2ID := task2.GetID()
	if task2Deps, exists := deps[task2ID]; !exists || len(task2Deps) == 0 {
		t.Fatal("task2的依赖关系未正确构建")
	}
}

func TestWorkflowBuilder_DuplicateTaskName(t *testing.T) {
	builder := NewWorkflowBuilder("workflow", "描述")
	
	task1, _ := NewTaskBuilder("same-name", "任务1").
		WithJobFunction("func1", nil).
		Build()
	
	task2, _ := NewTaskBuilder("same-name", "任务2").
		WithJobFunction("func2", nil).
		Build()
	
	_, err := builder.
		WithTask(task1).
		WithTask(task2).
		Build()
	
	if err == nil {
		t.Fatal("期望返回错误（Task名称重复），但未返回")
	}
}

func TestWorkflowBuilder_MissingDependency(t *testing.T) {
	builder := NewWorkflowBuilder("workflow", "描述")
	
	task, _ := NewTaskBuilder("task1", "任务1").
		WithJobFunction("func1", nil).
		WithDependency("non-existent-task").
		Build()
	
	_, err := builder.
		WithTask(task).
		Build()
	
	if err == nil {
		t.Fatal("期望返回错误（依赖Task不存在），但未返回")
	}
}

func TestWorkflowBuilder_SelfDependency(t *testing.T) {
	builder := NewWorkflowBuilder("workflow", "描述")
	
	task, _ := NewTaskBuilder("task1", "任务1").
		WithJobFunction("func1", nil).
		WithDependency("task1"). // 依赖自己
		Build()
	
	_, err := builder.
		WithTask(task).
		Build()
	
	if err == nil {
		t.Fatal("期望返回错误（自依赖），但未返回")
	}
}

func TestWorkflowBuilder_EmptyWorkflow(t *testing.T) {
	builder := NewWorkflowBuilder("workflow", "描述")
	
	workflow, err := builder.Build()
	
	if err != nil {
		t.Fatalf("空Workflow应该合法，但返回错误: %v", err)
	}
	
	if workflow == nil {
		t.Fatal("Workflow为nil")
	}
}

func TestWorkflowBuilder_ComplexDependencies(t *testing.T) {
	builder := NewWorkflowBuilder("workflow", "描述")
	
	// 创建多个Task，形成依赖链：task1 -> task2 -> task3
	task1, _ := NewTaskBuilder("task1", "任务1").
		WithJobFunction("func1", nil).
		Build()
	
	task2, _ := NewTaskBuilder("task2", "任务2").
		WithJobFunction("func2", nil).
		WithDependency("task1").
		Build()
	
	task3, _ := NewTaskBuilder("task3", "任务3").
		WithJobFunction("func3", nil).
		WithDependencies([]string{"task1", "task2"}).
		Build()
	
	workflow, err := builder.
		WithTask(task1).
		WithTask(task2).
		WithTask(task3).
		Build()
	
	if err != nil {
		t.Fatalf("构建Workflow失败: %v", err)
	}
	
	deps := workflow.GetDependencies()
	
	// 检查task2的依赖
	task2ID := task2.GetID()
	if task2Deps, exists := deps[task2ID]; !exists || len(task2Deps) != 1 {
		t.Fatalf("task2的依赖关系错误，期望1个依赖，实际: %d", len(task2Deps))
	}
	
	// 检查task3的依赖
	task3ID := task3.GetID()
	if task3Deps, exists := deps[task3ID]; !exists || len(task3Deps) != 2 {
		t.Fatalf("task3的依赖关系错误，期望2个依赖，实际: %d", len(task3Deps))
	}
}

