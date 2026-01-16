package unit

import (
	"context"
	"testing"

	"github.com/LENAX/task-engine/pkg/core/builder"
	"github.com/LENAX/task-engine/pkg/core/task"
)

func TestWorkflow_ReplaceWorkflowParams(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()
	testFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	registry.Register(ctx, "TestFunc", testFunc, "测试函数")

	// 创建Workflow
	wfBuilder := builder.NewWorkflowBuilder("TestWorkflow", "测试工作流")
	wfBuilder = wfBuilder.WithParams(map[string]string{
		"workflow_id": "${workflow_id}",
		"static_val":  "static",
	})

	wf, err := wfBuilder.Build()
	if err != nil {
		t.Fatalf("构建Workflow失败: %v", err)
	}

	// 替换Workflow参数
	params := map[string]interface{}{
		"workflow_id": "wf_001",
	}

	err = wf.ReplaceWorkflowParams(params)
	if err != nil {
		t.Fatalf("替换Workflow参数失败: %v", err)
	}

	// 验证参数是否被正确替换
	workflowID, exists := wf.GetParam("workflow_id")
	if !exists || workflowID != "wf_001" {
		t.Errorf("期望 workflow_id = 'wf_001', 实际 = '%v', exists = %v", workflowID, exists)
	}

	staticVal, exists := wf.GetParam("static_val")
	if !exists || staticVal != "static" {
		t.Errorf("期望 static_val = 'static', 实际 = '%v', exists = %v", staticVal, exists)
	}
}

func TestWorkflow_ReplaceTaskParams(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()
	testFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	registry.Register(ctx, "TestFunc", testFunc, "测试函数")

	// 创建Task
	taskBuilder := builder.NewTaskBuilder("TestTask", "测试任务", registry)
	taskBuilder = taskBuilder.WithJobFunction("TestFunc", map[string]interface{}{
		"data_source_id": "${data_source_id}",
	})

	taskObj, err := taskBuilder.Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	// 创建Workflow并添加Task
	wfBuilder := builder.NewWorkflowBuilder("TestWorkflow", "测试工作流")
	wfBuilder = wfBuilder.WithTask(taskObj)

	wf, err := wfBuilder.Build()
	if err != nil {
		t.Fatalf("构建Workflow失败: %v", err)
	}

	// 通过Task名称替换参数
	params := map[string]interface{}{
		"data_source_id": "ds_001",
	}

	err = wf.ReplaceTaskParams("TestTask", params)
	if err != nil {
		t.Fatalf("替换Task参数失败: %v", err)
	}

	// 验证参数是否被正确替换
	task, _ := wf.GetTaskByName("TestTask")
	actualParams := task.GetParams()
	if actualParams["data_source_id"] != "ds_001" {
		t.Errorf("期望 data_source_id = 'ds_001', 实际 = '%v'", actualParams["data_source_id"])
	}
}

func TestWorkflow_ReplaceTaskParams_ByID(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()
	testFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	registry.Register(ctx, "TestFunc", testFunc, "测试函数")

	taskBuilder := builder.NewTaskBuilder("TestTask", "测试任务", registry)
	taskBuilder = taskBuilder.WithJobFunction("TestFunc", map[string]interface{}{
		"param1": "${param1}",
	})

	taskObj, err := taskBuilder.Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	taskID := taskObj.GetID()

	wfBuilder := builder.NewWorkflowBuilder("TestWorkflow", "测试工作流")
	wfBuilder = wfBuilder.WithTask(taskObj)

	wf, err := wfBuilder.Build()
	if err != nil {
		t.Fatalf("构建Workflow失败: %v", err)
	}

	// 通过Task ID替换参数
	params := map[string]interface{}{
		"param1": "value1",
	}

	err = wf.ReplaceTaskParams(taskID, params)
	if err != nil {
		t.Fatalf("替换Task参数失败: %v", err)
	}

	// 验证参数是否被正确替换
	task, _ := wf.GetTask(taskID)
	actualParams := task.GetParams()
	if actualParams["param1"] != "value1" {
		t.Errorf("期望 param1 = 'value1', 实际 = '%v'", actualParams["param1"])
	}
}

func TestWorkflow_ReplaceTaskParams_NotFound(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	registry.Register(nil, "TestFunc", func() {}, "测试函数")

	wfBuilder := builder.NewWorkflowBuilder("TestWorkflow", "测试工作流")
	wf, err := wfBuilder.Build()
	if err != nil {
		t.Fatalf("构建Workflow失败: %v", err)
	}

	params := map[string]interface{}{
		"param1": "value1",
	}

	// 尝试替换不存在的Task
	err = wf.ReplaceTaskParams("NonExistentTask", params)
	if err == nil {
		t.Error("期望返回错误，因为Task不存在")
	}
}

func TestWorkflow_ReplaceAllTaskParams(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()
	testFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	registry.Register(ctx, "TestFunc", testFunc, "测试函数")

	// 创建多个Task
	task1Builder := builder.NewTaskBuilder("Task1", "任务1", registry)
	task1Builder = task1Builder.WithJobFunction("TestFunc", map[string]interface{}{
		"param1": "${param1}",
	})
	task1, err := task1Builder.Build()
	if err != nil {
		t.Fatalf("构建Task1失败: %v", err)
	}

	task2Builder := builder.NewTaskBuilder("Task2", "任务2", registry)
	task2Builder = task2Builder.WithJobFunction("TestFunc", map[string]interface{}{
		"param2": "${param2}",
	})
	task2, err := task2Builder.Build()
	if err != nil {
		t.Fatalf("构建Task2失败: %v", err)
	}

	// 创建Workflow并添加Task
	wfBuilder := builder.NewWorkflowBuilder("TestWorkflow", "测试工作流")
	wfBuilder = wfBuilder.WithTask(task1)
	wfBuilder = wfBuilder.WithTask(task2)

	wf, err := wfBuilder.Build()
	if err != nil {
		t.Fatalf("构建Workflow失败: %v", err)
	}

	// 替换所有Task的参数
	params := map[string]interface{}{
		"param1": "value1",
		"param2": "value2",
	}

	err = wf.ReplaceAllTaskParams(params)
	if err != nil {
		t.Fatalf("替换所有Task参数失败: %v", err)
	}

	// 验证所有Task的参数都被替换
	task1Actual, _ := wf.GetTaskByName("Task1")
	params1 := task1Actual.GetParams()
	if params1["param1"] != "value1" {
		t.Errorf("期望 Task1.param1 = 'value1', 实际 = '%v'", params1["param1"])
	}

	task2Actual, _ := wf.GetTaskByName("Task2")
	params2 := task2Actual.GetParams()
	if params2["param2"] != "value2" {
		t.Errorf("期望 Task2.param2 = 'value2', 实际 = '%v'", params2["param2"])
	}
}

func TestWorkflow_ReplaceParams(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()
	testFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	registry.Register(ctx, "TestFunc", testFunc, "测试函数")

	// 创建Task
	taskBuilder := builder.NewTaskBuilder("TestTask", "测试任务", registry)
	taskBuilder = taskBuilder.WithJobFunction("TestFunc", map[string]interface{}{
		"task_param": "${task_param}",
	})
	taskObj, err := taskBuilder.Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	// 创建Workflow
	wfBuilder := builder.NewWorkflowBuilder("TestWorkflow", "测试工作流")
	wfBuilder = wfBuilder.WithTask(taskObj)
	wfBuilder = wfBuilder.WithParams(map[string]string{
		"workflow_param": "${workflow_param}",
	})

	wf, err := wfBuilder.Build()
	if err != nil {
		t.Fatalf("构建Workflow失败: %v", err)
	}

	// 同时替换Workflow和所有Task的参数
	params := map[string]interface{}{
		"task_param":     "task_value",
		"workflow_param": "workflow_value",
	}

	err = wf.ReplaceParams(params)
	if err != nil {
		t.Fatalf("替换参数失败: %v", err)
	}

	// 验证Workflow参数
	workflowParam, exists := wf.GetParam("workflow_param")
	if !exists || workflowParam != "workflow_value" {
		t.Errorf("期望 workflow_param = 'workflow_value', 实际 = '%v', exists = %v", workflowParam, exists)
	}

	// 验证Task参数
	task, _ := wf.GetTaskByName("TestTask")
	taskParams := task.GetParams()
	if taskParams["task_param"] != "task_value" {
		t.Errorf("期望 task_param = 'task_value', 实际 = '%v'", taskParams["task_param"])
	}
}

func TestWorkflow_ReplaceParams_EmptyParams(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()
	testFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	registry.Register(ctx, "TestFunc", testFunc, "测试函数")

	taskBuilder := builder.NewTaskBuilder("TestTask", "测试任务", registry)
	taskBuilder = taskBuilder.WithJobFunction("TestFunc", map[string]interface{}{
		"param1": "${param1}",
	})
	taskObj, _ := taskBuilder.Build()

	wfBuilder := builder.NewWorkflowBuilder("TestWorkflow", "测试工作流")
	wfBuilder = wfBuilder.WithTask(taskObj)
	wf, _ := wfBuilder.Build()

	// 空参数应该不报错
	err := wf.ReplaceParams(nil)
	if err != nil {
		t.Errorf("空参数不应该返回错误: %v", err)
	}

	err = wf.ReplaceParams(map[string]interface{}{})
	if err != nil {
		t.Errorf("空参数map不应该返回错误: %v", err)
	}
}
