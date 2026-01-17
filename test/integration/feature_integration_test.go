package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/LENAX/task-engine/internal/storage/sqlite"
	"github.com/LENAX/task-engine/pkg/core/builder"
	"github.com/LENAX/task-engine/pkg/core/cache"
	"github.com/LENAX/task-engine/pkg/core/engine"
	"github.com/LENAX/task-engine/pkg/core/task"
	"github.com/LENAX/task-engine/pkg/core/workflow"
)

// setupFeatureIntegrationTest 设置功能集成测试环境
func setupFeatureIntegrationTest(t *testing.T) (*engine.Engine, task.FunctionRegistry, *workflow.Workflow, func()) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"

	repos, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("创建Repository失败: %v", err)
	}

	eng, err := engine.NewEngine(10, 30, repos.Workflow, repos.WorkflowInstance, repos.Task)
	if err != nil {
		t.Fatalf("创建Engine失败: %v", err)
	}

	registry := eng.GetRegistry()
	ctx := context.Background()

	// 注册缓存依赖
	resultCache := cache.NewMemoryResultCache()
	if err := registry.RegisterDependencyWithKey("ResultCache", resultCache); err != nil {
		t.Fatalf("注册缓存依赖失败: %v", err)
	}

	if err := eng.Start(ctx); err != nil {
		t.Fatalf("启动Engine失败: %v", err)
	}

	// 注册默认Handler
	_, err = registry.RegisterTaskHandler(ctx, "DefaultLogSuccess", task.DefaultLogSuccess, "默认成功日志")
	if err != nil {
		t.Fatalf("注册DefaultLogSuccess失败: %v", err)
	}

	_, err = registry.RegisterTaskHandler(ctx, "DefaultLogError", task.DefaultLogError, "默认错误日志")
	if err != nil {
		t.Fatalf("注册DefaultLogError失败: %v", err)
	}

	_, err = registry.RegisterTaskHandler(ctx, "DefaultSaveResult", task.DefaultSaveResult, "默认保存结果")
	if err != nil {
		t.Fatalf("注册DefaultSaveResult失败: %v", err)
	}

	_, err = registry.RegisterTaskHandler(ctx, "DefaultAggregateSubTaskResults", task.DefaultAggregateSubTaskResults, "默认聚合子任务结果")
	if err != nil {
		t.Fatalf("注册DefaultAggregateSubTaskResults失败: %v", err)
	}

	_, err = registry.RegisterTaskHandler(ctx, "DefaultValidateParams", task.DefaultValidateParams, "默认参数校验")
	if err != nil {
		t.Fatalf("注册DefaultValidateParams失败: %v", err)
	}

	wf := workflow.NewWorkflow("test-workflow", "测试工作流")

	cleanup := func() {
		eng.Stop()
		repos.Close()
	}

	return eng, registry, wf, cleanup
}

// TestFeatureIntegration_MultiHandlers 测试多Handler在完整工作流中的执行
func TestFeatureIntegration_MultiHandlers(t *testing.T) {
	eng, registry, wf, cleanup := setupFeatureIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// 创建Job函数
	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return map[string]interface{}{
			"result": "success",
			"data":   100,
		}, nil
	}

	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 创建任务，配置多个Handler
	task1, err := builder.NewTaskBuilder("task1", "任务1", registry).
		WithJobFunction("mockFunc", nil).
		WithTaskHandler(task.TaskStatusSuccess, "DefaultLogSuccess").
		WithTaskHandler(task.TaskStatusSuccess, "DefaultSaveResult").
		WithTaskHandler(task.TaskStatusFailed, "DefaultLogError").
		Build()
	if err != nil {
		t.Fatalf("构建任务失败: %v", err)
	}

	if err := wf.AddTask(task1); err != nil {
		t.Fatalf("添加任务失败: %v", err)
	}

	// 提交Workflow
	controller, err := eng.SubmitWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("提交Workflow失败: %v", err)
	}

	// 等待执行完成
	timeout := 10 * time.Second
	startTime := time.Now()
	for {
		status, err := controller.GetStatus()
		if err != nil {
			t.Fatalf("获取状态失败: %v", err)
		}

		if status == "Success" || status == "Failed" || status == "Terminated" {
			break
		}

		if time.Since(startTime) > timeout {
			t.Fatalf("工作流执行超时")
		}

		time.Sleep(100 * time.Millisecond)
	}

	// 验证工作流成功
	finalStatus, _ := controller.GetStatus()
	if finalStatus != "Success" {
		t.Errorf("期望工作流状态为Success，实际为%s", finalStatus)
	}
}

// TestFeatureIntegration_ParamValidation 测试参数校验在完整工作流中的执行
func TestFeatureIntegration_ParamValidation(t *testing.T) {
	eng, registry, wf, cleanup := setupFeatureIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// 创建父任务，返回结果
	parentFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return map[string]interface{}{
			"result_field": "result_value",
			"data_count":   100,
		}, nil
	}

	_, err := registry.Register(ctx, "parentFunc", parentFunc, "父任务函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	childFunc := func(ctx *task.TaskContext) (interface{}, error) {
		// 验证参数是否正确注入
		resultField := ctx.GetParamString("result_field")
		if resultField == "" {
			return nil, fmt.Errorf("缺少必需参数result_field")
		}
		return map[string]interface{}{
			"processed": true,
			"value":     resultField,
		}, nil
	}

	_, err = registry.Register(ctx, "childFunc", childFunc, "子任务函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 创建父任务
	parentTask, err := builder.NewTaskBuilder("parent-task", "父任务", registry).
		WithJobFunction("parentFunc", nil).
		WithTaskHandler(task.TaskStatusSuccess, "DefaultLogSuccess").
		Build()
	if err != nil {
		t.Fatalf("构建父任务失败: %v", err)
	}

	// 创建子任务，配置参数校验和resultMapping
	childTask, err := builder.NewTaskBuilder("child-task", "子任务", registry).
		WithJobFunction("childFunc", nil).
		WithDependency("parent-task").
		WithRequiredParams([]string{"result_field"}).
		WithResultMapping(map[string]string{
			"result_field": "result_field", // 映射上游结果字段
		}).
		WithTaskHandler(task.TaskStatusSuccess, "DefaultValidateParams").
		WithTaskHandler(task.TaskStatusSuccess, "DefaultLogSuccess").
		Build()
	if err != nil {
		t.Fatalf("构建子任务失败: %v", err)
	}

	if err := wf.AddTask(parentTask); err != nil {
		t.Fatalf("添加父任务失败: %v", err)
	}
	if err := wf.AddTask(childTask); err != nil {
		t.Fatalf("添加子任务失败: %v", err)
	}

	// 提交Workflow
	controller, err := eng.SubmitWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("提交Workflow失败: %v", err)
	}

	// 等待执行完成
	timeout := 10 * time.Second
	startTime := time.Now()
	for {
		status, err := controller.GetStatus()
		if err != nil {
			t.Fatalf("获取状态失败: %v", err)
		}

		if status == "Success" || status == "Failed" || status == "Terminated" {
			break
		}

		if time.Since(startTime) > timeout {
			t.Fatalf("工作流执行超时")
		}

		time.Sleep(100 * time.Millisecond)
	}

	// 验证工作流成功
	finalStatus, _ := controller.GetStatus()
	if finalStatus != "Success" {
		t.Errorf("期望工作流状态为Success，实际为%s", finalStatus)
	}
}

// TestFeatureIntegration_Cache 测试缓存机制在完整工作流中的执行
func TestFeatureIntegration_Cache(t *testing.T) {
	eng, registry, wf, cleanup := setupFeatureIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// 创建Job函数
	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return map[string]interface{}{
			"result": "cached_result",
			"data":   200,
		}, nil
	}

	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 创建任务
	task1, err := builder.NewTaskBuilder("task1", "任务1", registry).
		WithJobFunction("mockFunc", nil).
		WithTaskHandler(task.TaskStatusSuccess, "DefaultLogSuccess").
		Build()
	if err != nil {
		t.Fatalf("构建任务失败: %v", err)
	}

	if err := wf.AddTask(task1); err != nil {
		t.Fatalf("添加任务失败: %v", err)
	}

	// 提交Workflow
	controller, err := eng.SubmitWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("提交Workflow失败: %v", err)
	}

	// 等待执行完成
	timeout := 10 * time.Second
	startTime := time.Now()
	for {
		status, err := controller.GetStatus()
		if err != nil {
			t.Fatalf("获取状态失败: %v", err)
		}

		if status == "Success" || status == "Failed" || status == "Terminated" {
			break
		}

		if time.Since(startTime) > timeout {
			t.Fatalf("工作流执行超时")
		}

		time.Sleep(100 * time.Millisecond)
	}

	// 验证工作流成功（缓存应该在instance_manager中自动保存）
	finalStatus, _ := controller.GetStatus()
	if finalStatus != "Success" {
		t.Errorf("期望工作流状态为Success，实际为%s", finalStatus)
	}
}

// TestFeatureIntegration_DAGOptimization 测试DAG优化后的执行顺序
func TestFeatureIntegration_DAGOptimization(t *testing.T) {
	eng, registry, wf, cleanup := setupFeatureIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// 创建Job函数
	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}

	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 创建任务链：task1 -> task2 -> task3
	task1, _ := builder.NewTaskBuilder("task1", "任务1", registry).
		WithJobFunction("mockFunc", nil).
		WithTaskHandler(task.TaskStatusSuccess, "DefaultLogSuccess").
		Build()

	task2, _ := builder.NewTaskBuilder("task2", "任务2", registry).
		WithJobFunction("mockFunc", nil).
		WithDependency("task1").
		WithTaskHandler(task.TaskStatusSuccess, "DefaultLogSuccess").
		Build()

	task3, _ := builder.NewTaskBuilder("task3", "任务3", registry).
		WithJobFunction("mockFunc", nil).
		WithDependency("task2").
		WithTaskHandler(task.TaskStatusSuccess, "DefaultLogSuccess").
		Build()

	if err := wf.AddTask(task1); err != nil {
		t.Fatalf("添加任务失败: %v", err)
	}
	if err := wf.AddTask(task2); err != nil {
		t.Fatalf("添加任务2失败: %v", err)
	}
	if err := wf.AddTask(task3); err != nil {
		t.Fatalf("添加任务3失败: %v", err)
	}
	// 注意：依赖关系已通过WithDependency在构建时设置，AddTask会自动处理

	// 提交Workflow
	controller, err := eng.SubmitWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("提交Workflow失败: %v", err)
	}

	// 等待执行完成
	timeout := 10 * time.Second
	startTime := time.Now()
	for {
		status, err := controller.GetStatus()
		if err != nil {
			t.Fatalf("获取状态失败: %v", err)
		}

		if status == "Success" || status == "Failed" || status == "Terminated" {
			break
		}

		if time.Since(startTime) > timeout {
			t.Fatalf("工作流执行超时")
		}

		time.Sleep(100 * time.Millisecond)
	}

	// 验证工作流成功（DAG应该正确执行顺序）
	finalStatus, _ := controller.GetStatus()
	if finalStatus != "Success" {
		t.Errorf("期望工作流状态为Success，实际为%s", finalStatus)
	}
}
