package unit

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/LENAX/task-engine/pkg/core/task"
)

// TestMultiHandlers_SequentialExecution 测试多Handler按顺序执行
func TestMultiHandlers_SequentialExecution(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	// 记录执行顺序
	executionOrder := make([]string, 0)
	var mu sync.Mutex

	// 创建3个Handler
	handler1 := func(ctx *task.TaskContext) {
		mu.Lock()
		executionOrder = append(executionOrder, "handler1")
		mu.Unlock()
		time.Sleep(10 * time.Millisecond) // 模拟处理时间
	}

	handler2 := func(ctx *task.TaskContext) {
		mu.Lock()
		executionOrder = append(executionOrder, "handler2")
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}

	handler3 := func(ctx *task.TaskContext) {
		mu.Lock()
		executionOrder = append(executionOrder, "handler3")
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}

	// 注册Handler
	handler1ID, err := registry.RegisterTaskHandler(ctx, "handler1", handler1, "Handler 1")
	if err != nil {
		t.Fatalf("注册handler1失败: %v", err)
	}

	handler2ID, err := registry.RegisterTaskHandler(ctx, "handler2", handler2, "Handler 2")
	if err != nil {
		t.Fatalf("注册handler2失败: %v", err)
	}

	handler3ID, err := registry.RegisterTaskHandler(ctx, "handler3", handler3, "Handler 3")
	if err != nil {
		t.Fatalf("注册handler3失败: %v", err)
	}

	// 创建Task，配置多个Handler
	taskObj := task.NewTask("test-task", "测试任务", "", make(map[string]any), map[string][]string{
		task.TaskStatusSuccess: {handler1ID, handler2ID, handler3ID},
	})
	taskObj.ID = "test-task" // 设置固定ID用于测试
	taskObj.SetStatus(task.TaskStatusPending)

	// 执行Handler（同步执行以验证顺序）
	err = task.ExecuteTaskHandlerSync(registry, taskObj, task.TaskStatusSuccess, "test result", "")
	if err != nil {
		t.Fatalf("执行Handler失败: %v", err)
	}

	// 等待Handler执行完成
	time.Sleep(100 * time.Millisecond)

	// 验证执行顺序
	mu.Lock()
	defer mu.Unlock()

	if len(executionOrder) != 3 {
		t.Errorf("期望执行3个Handler，实际执行了 %d 个", len(executionOrder))
	}

	if len(executionOrder) >= 3 {
		if executionOrder[0] != "handler1" {
			t.Errorf("期望第一个执行handler1，实际是 %s", executionOrder[0])
		}
		if executionOrder[1] != "handler2" {
			t.Errorf("期望第二个执行handler2，实际是 %s", executionOrder[1])
		}
		if executionOrder[2] != "handler3" {
			t.Errorf("期望第三个执行handler3，实际是 %s", executionOrder[2])
		}
	}
}

// TestMultiHandlers_FailureIsolation 测试Handler执行失败不影响后续Handler
func TestMultiHandlers_FailureIsolation(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	// 记录执行顺序
	executionOrder := make([]string, 0)
	var mu sync.Mutex

	// 创建3个Handler，第二个会panic
	handler1 := func(ctx *task.TaskContext) {
		mu.Lock()
		executionOrder = append(executionOrder, "handler1")
		mu.Unlock()
	}

	handler2 := func(ctx *task.TaskContext) {
		mu.Lock()
		executionOrder = append(executionOrder, "handler2")
		mu.Unlock()
		panic("handler2 panic")
	}

	handler3 := func(ctx *task.TaskContext) {
		mu.Lock()
		executionOrder = append(executionOrder, "handler3")
		mu.Unlock()
	}

	// 注册Handler
	handler1ID, _ := registry.RegisterTaskHandler(ctx, "handler1_fail", handler1, "Handler 1")
	handler2ID, _ := registry.RegisterTaskHandler(ctx, "handler2_fail", handler2, "Handler 2")
	handler3ID, _ := registry.RegisterTaskHandler(ctx, "handler3_fail", handler3, "Handler 3")

	// 创建Task
	taskObj := task.NewTask("test-task-fail", "测试任务失败", "", make(map[string]any), map[string][]string{
		task.TaskStatusSuccess: {handler1ID, handler2ID, handler3ID},
	})
	taskObj.ID = "test-task-fail" // 设置固定ID用于测试
	taskObj.SetStatus(task.TaskStatusPending)

	// 执行Handler（同步执行）
	err := task.ExecuteTaskHandlerSync(registry, taskObj, task.TaskStatusSuccess, "test result", "")
	if err != nil {
		t.Fatalf("执行Handler失败: %v", err)
	}

	// 等待Handler执行完成
	time.Sleep(100 * time.Millisecond)

	// 验证所有Handler都执行了（即使handler2 panic了）
	mu.Lock()
	defer mu.Unlock()

	if len(executionOrder) != 3 {
		t.Errorf("期望执行3个Handler（即使有panic），实际执行了 %d 个", len(executionOrder))
	}
}

// TestMultiHandlers_ParameterPassing 测试Handler参数传递
func TestMultiHandlers_ParameterPassing(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	// 记录接收到的参数
	receivedParams := make(map[string]interface{})
	var mu sync.Mutex

	handler := func(ctx *task.TaskContext) {
		mu.Lock()
		receivedParams["_result_data"] = ctx.GetParam("_result_data")
		receivedParams["_status"] = ctx.GetParam("_status")
		receivedParams["custom_param"] = ctx.GetParam("custom_param")
		mu.Unlock()
	}

	// 注册Handler
	handlerID, _ := registry.RegisterTaskHandler(ctx, "param_handler", handler, "参数Handler")

	// 创建Task
	taskObj := task.NewTask("test-task-param", "测试任务参数", "", map[string]any{
		"custom_param": "custom_value",
	}, map[string][]string{
		task.TaskStatusSuccess: {handlerID},
	})
	taskObj.ID = "test-task-param" // 设置固定ID用于测试
	taskObj.SetStatus(task.TaskStatusPending)

	// 执行Handler
	err := task.ExecuteTaskHandlerSync(registry, taskObj, task.TaskStatusSuccess, "test result", "")
	if err != nil {
		t.Fatalf("执行Handler失败: %v", err)
	}

	// 等待Handler执行完成
	time.Sleep(50 * time.Millisecond)

	// 验证参数传递
	mu.Lock()
	defer mu.Unlock()

	if receivedParams["_result_data"] != "test result" {
		t.Errorf("期望_result_data为'test result'，实际为 %v", receivedParams["_result_data"])
	}

	if receivedParams["_status"] != task.TaskStatusSuccess {
		t.Errorf("期望_status为'%s'，实际为 %v", task.TaskStatusSuccess, receivedParams["_status"])
	}

	if receivedParams["custom_param"] != "custom_value" {
		t.Errorf("期望custom_param为'custom_value'，实际为 %v", receivedParams["custom_param"])
	}
}

