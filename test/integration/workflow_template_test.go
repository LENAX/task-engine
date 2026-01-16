package integration

import (
	"context"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/LENAX/task-engine/internal/storage/sqlite"
	"github.com/LENAX/task-engine/pkg/core/builder"
	"github.com/LENAX/task-engine/pkg/core/engine"
	"github.com/LENAX/task-engine/pkg/core/task"
)

// TestWorkflow_ParamTemplate_BuildReplaceSubmit 测试完整的Build->Replace->Submit流程
func TestWorkflow_ParamTemplate_BuildReplaceSubmit(t *testing.T) {
	// 设置测试环境
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"

	repos, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("创建Repository失败: %v", err)
	}
	defer repos.Close()

	eng, err := engine.NewEngine(10, 30, repos.Workflow, repos.WorkflowInstance, repos.Task)
	if err != nil {
		t.Fatalf("创建Engine失败: %v", err)
	}

	ctx := context.Background()
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("启动Engine失败: %v", err)
	}
	defer eng.Stop()

	registry := eng.GetRegistry()

	// 注册测试函数
	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		// 验证参数是否被正确替换
		dataSourceID := ctx.GetParamString("data_source_id")
		dateRange := ctx.GetParamString("date_range")

		if dataSourceID != "ds_001" {
			t.Errorf("期望 data_source_id = 'ds_001', 实际 = '%s'", dataSourceID)
		}
		if dateRange != "2024-01-01,2024-01-31" {
			t.Errorf("期望 date_range = '2024-01-01,2024-01-31', 实际 = '%s'", dateRange)
		}

		return map[string]interface{}{
			"data_source_id": dataSourceID,
			"date_range":     dateRange,
			"status":         "success",
		}, nil
	}

	_, err = registry.Register(ctx, "FetchDataFunc", mockFunc, "获取数据函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 1. Build时使用占位符
	taskBuilder := builder.NewTaskBuilder("FetchData", "获取数据", registry)
	taskBuilder = taskBuilder.WithJobFunction("FetchDataFunc", map[string]interface{}{
		"data_source_id": "${data_source_id}",
		"date_range":     "${date_range}",
	})

	taskObj, err := taskBuilder.Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	// 创建Workflow
	wfBuilder := builder.NewWorkflowBuilder("DataFetchWorkflow", "数据获取工作流")
	wfBuilder = wfBuilder.WithTask(taskObj)
	wfBuilder = wfBuilder.WithParams(map[string]string{
		"workflow_id": "${workflow_id}",
	})

	wf, err := wfBuilder.Build()
	if err != nil {
		t.Fatalf("构建Workflow失败: %v", err)
	}

	// 2. 执行前替换参数
	params := map[string]interface{}{
		"data_source_id": "ds_001",
		"date_range":     "2024-01-01,2024-01-31",
		"workflow_id":    "wf_001",
	}

	err = wf.ReplaceParams(params)
	if err != nil {
		t.Fatalf("替换参数失败: %v", err)
	}

	// 验证Workflow参数是否被替换
	workflowID, exists := wf.GetParam("workflow_id")
	if !exists || workflowID != "wf_001" {
		t.Errorf("期望 workflow_id = 'wf_001', 实际 = '%v', exists = %v", workflowID, exists)
	}

	// 验证Task参数是否被替换
	taskActual, _ := wf.GetTaskByName("FetchData")
	taskParams := taskActual.GetParams()
	if taskParams["data_source_id"] != "ds_001" {
		t.Errorf("期望 data_source_id = 'ds_001', 实际 = '%v'", taskParams["data_source_id"])
	}
	if taskParams["date_range"] != "2024-01-01,2024-01-31" {
		t.Errorf("期望 date_range = '2024-01-01,2024-01-31', 实际 = '%v'", taskParams["date_range"])
	}

	// 3. 提交执行
	controller, err := eng.SubmitWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("提交Workflow失败: %v", err)
	}

	// 等待Workflow完成
	err = controller.Wait(5 * time.Second)
	if err != nil {
		t.Fatalf("等待Workflow完成失败: %v", err)
	}

	// 验证Workflow状态
	status := controller.Status()
	if status != "Success" {
		t.Errorf("期望Workflow状态为Success，实际为%s", status)
	}
}

// TestWorkflow_ParamTemplate_ReplaceTaskParams 测试通过Task名称替换参数
func TestWorkflow_ParamTemplate_ReplaceTaskParams(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"

	repos, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("创建Repository失败: %v", err)
	}
	defer repos.Close()

	eng, err := engine.NewEngine(10, 30, repos.Workflow, repos.WorkflowInstance, repos.Task)
	if err != nil {
		t.Fatalf("创建Engine失败: %v", err)
	}

	ctx := context.Background()
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("启动Engine失败: %v", err)
	}
	defer eng.Stop()

	registry := eng.GetRegistry()

	// 注册测试函数
	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	_, err = registry.Register(ctx, "TestFunc", mockFunc, "测试函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 创建多个Task，使用占位符
	task1Builder := builder.NewTaskBuilder("Task1", "任务1", registry)
	task1Builder = task1Builder.WithJobFunction("TestFunc", map[string]interface{}{
		"param1": "${param1}",
	})
	task1, _ := task1Builder.Build()

	task2Builder := builder.NewTaskBuilder("Task2", "任务2", registry)
	task2Builder = task2Builder.WithJobFunction("TestFunc", map[string]interface{}{
		"param2": "${param2}",
	})
	task2, _ := task2Builder.Build()

	// 创建Workflow
	wfBuilder := builder.NewWorkflowBuilder("TestWorkflow", "测试工作流")
	wfBuilder = wfBuilder.WithTask(task1)
	wfBuilder = wfBuilder.WithTask(task2)

	wf, err := wfBuilder.Build()
	if err != nil {
		t.Fatalf("构建Workflow失败: %v", err)
	}

	// 仅替换Task1的参数
	task1Params := map[string]interface{}{
		"param1": "value1",
	}

	err = wf.ReplaceTaskParams("Task1", task1Params)
	if err != nil {
		t.Fatalf("替换Task1参数失败: %v", err)
	}

	// 验证Task1的参数被替换，Task2的参数保持原样
	task1Actual, _ := wf.GetTaskByName("Task1")
	task1ParamsActual := task1Actual.GetParams()
	if task1ParamsActual["param1"] != "value1" {
		t.Errorf("期望 Task1.param1 = 'value1', 实际 = '%v'", task1ParamsActual["param1"])
	}

	task2Actual, _ := wf.GetTaskByName("Task2")
	task2ParamsActual := task2Actual.GetParams()
	if task2ParamsActual["param2"] != "${param2}" {
		t.Errorf("期望 Task2.param2 = '${param2}', 实际 = '%v'", task2ParamsActual["param2"])
	}
}

// TestWorkflow_ParamTemplate_ReplaceWorkflowParams 测试仅替换Workflow参数
func TestWorkflow_ParamTemplate_ReplaceWorkflowParams(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"

	repos, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("创建Repository失败: %v", err)
	}
	defer repos.Close()

	eng, err := engine.NewEngine(10, 30, repos.Workflow, repos.WorkflowInstance, repos.Task)
	if err != nil {
		t.Fatalf("创建Engine失败: %v", err)
	}

	ctx := context.Background()
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("启动Engine失败: %v", err)
	}
	defer eng.Stop()

	registry := eng.GetRegistry()

	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	_, err = registry.Register(ctx, "TestFunc", mockFunc, "测试函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 创建Task（不使用占位符）
	taskBuilder := builder.NewTaskBuilder("TestTask", "测试任务", registry)
	taskBuilder = taskBuilder.WithJobFunction("TestFunc", map[string]interface{}{
		"static_param": "static_value",
	})
	taskObj, _ := taskBuilder.Build()

	// 创建Workflow，使用占位符
	wfBuilder := builder.NewWorkflowBuilder("TestWorkflow", "测试工作流")
	wfBuilder = wfBuilder.WithTask(taskObj)
	wfBuilder = wfBuilder.WithParams(map[string]string{
		"workflow_id": "${workflow_id}",
	})

	wf, err := wfBuilder.Build()
	if err != nil {
		t.Fatalf("构建Workflow失败: %v", err)
	}

	// 仅替换Workflow参数
	workflowParams := map[string]interface{}{
		"workflow_id": "wf_001",
	}

	err = wf.ReplaceWorkflowParams(workflowParams)
	if err != nil {
		t.Fatalf("替换Workflow参数失败: %v", err)
	}

	// 验证Workflow参数被替换
	workflowID, exists := wf.GetParam("workflow_id")
	if !exists || workflowID != "wf_001" {
		t.Errorf("期望 workflow_id = 'wf_001', 实际 = '%v', exists = %v", workflowID, exists)
	}

	// 验证Task参数未被影响
	taskActual, _ := wf.GetTaskByName("TestTask")
	taskParams := taskActual.GetParams()
	if taskParams["static_param"] != "static_value" {
		t.Errorf("期望 static_param = 'static_value', 实际 = '%v'", taskParams["static_param"])
	}
}

// TestWorkflow_ParamTemplate_ReplaceAllTaskParams 测试替换所有Task参数
func TestWorkflow_ParamTemplate_ReplaceAllTaskParams(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"

	repos, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("创建Repository失败: %v", err)
	}
	defer repos.Close()

	eng, err := engine.NewEngine(10, 30, repos.Workflow, repos.WorkflowInstance, repos.Task)
	if err != nil {
		t.Fatalf("创建Engine失败: %v", err)
	}

	ctx := context.Background()
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("启动Engine失败: %v", err)
	}
	defer eng.Stop()

	registry := eng.GetRegistry()

	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	_, err = registry.Register(ctx, "TestFunc", mockFunc, "测试函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 创建多个Task，使用占位符
	task1Builder := builder.NewTaskBuilder("Task1", "任务1", registry)
	task1Builder = task1Builder.WithJobFunction("TestFunc", map[string]interface{}{
		"common_param": "${common_param}",
	})
	task1, _ := task1Builder.Build()

	task2Builder := builder.NewTaskBuilder("Task2", "任务2", registry)
	task2Builder = task2Builder.WithJobFunction("TestFunc", map[string]interface{}{
		"common_param": "${common_param}",
	})
	task2, _ := task2Builder.Build()

	// 创建Workflow
	wfBuilder := builder.NewWorkflowBuilder("TestWorkflow", "测试工作流")
	wfBuilder = wfBuilder.WithTask(task1)
	wfBuilder = wfBuilder.WithTask(task2)

	wf, err := wfBuilder.Build()
	if err != nil {
		t.Fatalf("构建Workflow失败: %v", err)
	}

	// 替换所有Task的参数
	params := map[string]interface{}{
		"common_param": "common_value",
	}

	err = wf.ReplaceAllTaskParams(params)
	if err != nil {
		t.Fatalf("替换所有Task参数失败: %v", err)
	}

	// 验证所有Task的参数都被替换
	task1Actual, _ := wf.GetTaskByName("Task1")
	task1Params := task1Actual.GetParams()
	if task1Params["common_param"] != "common_value" {
		t.Errorf("期望 Task1.common_param = 'common_value', 实际 = '%v'", task1Params["common_param"])
	}

	task2Actual, _ := wf.GetTaskByName("Task2")
	task2Params := task2Actual.GetParams()
	if task2Params["common_param"] != "common_value" {
		t.Errorf("期望 Task2.common_param = 'common_value', 实际 = '%v'", task2Params["common_param"])
	}
}
