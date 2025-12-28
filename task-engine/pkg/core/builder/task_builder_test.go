package builder

import (
	"testing"
)

func TestTaskBuilder_Basic(t *testing.T) {
	builder := NewTaskBuilder("test-task", "测试任务")

	task, err := builder.
		WithJobFunction("testFunc", map[string]interface{}{"arg0": "value1"}).
		WithTimeout(60).
		WithRetryCount(3).
		Build()

	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	if task == nil {
		t.Fatal("Task为nil")
	}

	if task.Name != "test-task" {
		t.Errorf("Task名称错误，期望: test-task, 实际: %s", task.Name)
	}

	if task.JobFuncName != "testFunc" {
		t.Errorf("Job函数名称错误，期望: testFunc, 实际: %s", task.JobFuncName)
	}

	if task.TimeoutSeconds != 60 {
		t.Errorf("超时时间错误，期望: 60, 实际: %d", task.TimeoutSeconds)
	}

	if task.RetryCount != 3 {
		t.Errorf("重试次数错误，期望: 3, 实际: %d", task.RetryCount)
	}

	if task.Params["arg0"] != "value1" {
		t.Errorf("参数错误，期望: value1, 实际: %s", task.Params["arg0"])
	}
}

func TestTaskBuilder_WithDependency(t *testing.T) {
	builder := NewTaskBuilder("task2", "任务2")

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
	builder := NewTaskBuilder("task3", "任务3")

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
	builder := NewTaskBuilder("", "描述")

	_, err := builder.
		WithJobFunction("func", nil).
		Build()

	if err == nil {
		t.Fatal("期望返回错误，但未返回")
	}
}

func TestTaskBuilder_EmptyJobFuncName(t *testing.T) {
	builder := NewTaskBuilder("task", "描述")

	_, err := builder.Build()

	if err == nil {
		t.Fatal("期望返回错误，但未返回")
	}
}

func TestTaskBuilder_DefaultValues(t *testing.T) {
	builder := NewTaskBuilder("task", "描述")

	task, err := builder.
		WithJobFunction("func", nil).
		Build()

	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	if task.TimeoutSeconds != 30 {
		t.Errorf("默认超时时间错误，期望: 30, 实际: %d", task.TimeoutSeconds)
	}

	if task.RetryCount != 0 {
		t.Errorf("默认重试次数错误，期望: 0, 实际: %d", task.RetryCount)
	}
}
