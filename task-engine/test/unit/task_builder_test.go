package unit

import (
	"testing"

	"github.com/stevelan1995/task-engine/pkg/core/builder"
)

func TestTaskBuilder_WithDependency(t *testing.T) {
	builder := builder.NewTaskBuilder("task2", "任务2")

	task, err := builder.
		WithJobFunction("func2", nil).
		WithDependency("task1").
		Build()

	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	deps := task.GetDependencies()
	if len(deps) != 1 {
		t.Fatalf("依赖数量错误，期望: 1, 实际: %d", len(deps))
	}

	if deps[0] != "task1" {
		t.Errorf("依赖名称错误，期望: task1, 实际: %s", deps[0])
	}
}

func TestTaskBuilder_WithDependencies(t *testing.T) {
	builder := builder.NewTaskBuilder("task3", "任务3")

	task, err := builder.
		WithJobFunction("func3", nil).
		WithDependencies([]string{"task1", "task2"}).
		Build()

	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	deps := task.GetDependencies()
	if len(deps) != 2 {
		t.Fatalf("依赖数量错误，期望: 2, 实际: %d", len(deps))
	}
}

func TestTaskBuilder_EmptyName(t *testing.T) {
	builder := builder.NewTaskBuilder("", "描述")

	_, err := builder.
		WithJobFunction("func", nil).
		Build()

	if err == nil {
		t.Fatal("期望返回错误，但未返回")
	}
}

func TestTaskBuilder_EmptyJobFuncName(t *testing.T) {
	builder := builder.NewTaskBuilder("task", "描述")

	_, err := builder.Build()

	if err == nil {
		t.Fatal("期望返回错误，但未返回")
	}
}
