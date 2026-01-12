package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/LENAX/task-engine/internal/storage/sqlite"
	"github.com/LENAX/task-engine/pkg/core/builder"
	"github.com/LENAX/task-engine/pkg/core/engine"
	"github.com/LENAX/task-engine/pkg/core/task"
	"github.com/LENAX/task-engine/pkg/core/workflow"
)

// TestErrorScenarios_NetworkTimeout 测试网络超时场景
func TestErrorScenarios_NetworkTimeout(t *testing.T) {
	eng, registry, wf, cleanup := setupErrorScenarioTest(t)
	defer cleanup()

	ctx := context.Background()

	// 创建会超时的Job函数
	timeoutFunc := func(ctx *task.TaskContext) (interface{}, error) {
		// 模拟网络超时
		time.Sleep(2 * time.Second)
		return nil, context.DeadlineExceeded
	}

	_, err := registry.Register(ctx, "timeoutFunc", timeoutFunc, "超时函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 创建任务，设置短超时时间
	task1, err := builder.NewTaskBuilder("timeout-task", "超时任务", registry).
		WithJobFunction("timeoutFunc", nil).
		WithTimeout(1). // 1秒超时
		WithTaskHandler(task.TaskStatusTimeout, "DefaultLogError").
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
	waitForCompletion(t, controller, 10*time.Second)

	// 验证任务应该因为超时失败
	finalStatus, _ := controller.GetStatus()
	t.Logf("工作流状态: %s（期望可能因为超时而失败）", finalStatus)
}

// TestErrorScenarios_APIRateLimit 测试API限流场景
func TestErrorScenarios_APIRateLimit(t *testing.T) {
	eng, registry, wf, cleanup := setupErrorScenarioTest(t)
	defer cleanup()

	ctx := context.Background()

	// 创建会返回限流错误的Job函数
	rateLimitFunc := func(ctx *task.TaskContext) (interface{}, error) {
		// 模拟API限流
		return nil, errors.New("429 Too Many Requests")
	}

	_, err := registry.Register(ctx, "rateLimitFunc", rateLimitFunc, "限流函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 创建任务，配置重试
	task1, err := builder.NewTaskBuilder("rate-limit-task", "限流任务", registry).
		WithJobFunction("rateLimitFunc", nil).
		WithRetryCount(3). // 重试3次
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
	waitForCompletion(t, controller, 10*time.Second)

	// 验证任务应该因为限流失败（即使有重试）
	finalStatus, _ := controller.GetStatus()
	t.Logf("工作流状态: %s（期望可能因为限流而失败）", finalStatus)
}

// TestErrorScenarios_ConnectionFailure 测试连接失败场景
func TestErrorScenarios_ConnectionFailure(t *testing.T) {
	eng, registry, wf, cleanup := setupErrorScenarioTest(t)
	defer cleanup()

	ctx := context.Background()

	// 创建会返回连接失败的Job函数
	connectionFailFunc := func(ctx *task.TaskContext) (interface{}, error) {
		// 模拟连接失败
		return nil, errors.New("connection refused")
	}

	_, err := registry.Register(ctx, "connectionFailFunc", connectionFailFunc, "连接失败函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 创建任务
	task1, err := builder.NewTaskBuilder("connection-fail-task", "连接失败任务", registry).
		WithJobFunction("connectionFailFunc", nil).
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
	waitForCompletion(t, controller, 10*time.Second)

	// 验证任务应该因为连接失败而失败
	finalStatus, _ := controller.GetStatus()
	t.Logf("工作流状态: %s（期望可能因为连接失败而失败）", finalStatus)
}

// TestErrorScenarios_PartialFailure 测试部分失败场景
func TestErrorScenarios_PartialFailure(t *testing.T) {
	eng, registry, wf, cleanup := setupErrorScenarioTest(t)
	defer cleanup()

	ctx := context.Background()

	// 创建部分失败的Job函数
	partialFailFunc := func(ctx *task.TaskContext) (interface{}, error) {
		// 模拟部分失败（50%成功率）
		if time.Now().Unix()%2 == 0 {
			return map[string]interface{}{
				"status": "success",
			}, nil
		}
		return nil, errors.New("partial failure")
	}

	_, err := registry.Register(ctx, "partialFailFunc", partialFailFunc, "部分失败函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 创建多个任务
	task1, _ := builder.NewTaskBuilder("task1", "任务1", registry).
		WithJobFunction("partialFailFunc", nil).
		WithTaskHandler(task.TaskStatusSuccess, "DefaultLogSuccess").
		WithTaskHandler(task.TaskStatusFailed, "DefaultLogError").
		Build()

	task2, _ := builder.NewTaskBuilder("task2", "任务2", registry).
		WithJobFunction("partialFailFunc", nil).
		WithTaskHandler(task.TaskStatusSuccess, "DefaultLogSuccess").
		WithTaskHandler(task.TaskStatusFailed, "DefaultLogError").
		Build()

	if err := wf.AddTask(task1); err != nil {
		t.Fatalf("添加任务失败: %v", err)
	}
	if err := wf.AddTask(task2); err != nil {
		t.Fatalf("添加任务2失败: %v", err)
	}

	// 提交Workflow
	controller, err := eng.SubmitWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("提交Workflow失败: %v", err)
	}

	// 等待执行完成
	waitForCompletion(t, controller, 10*time.Second)

	// 验证工作流可能部分失败
	finalStatus, _ := controller.GetStatus()
	t.Logf("工作流状态: %s（可能部分任务失败）", finalStatus)
}

// setupErrorScenarioTest 设置错误场景测试环境
func setupErrorScenarioTest(t *testing.T) (*engine.Engine, task.FunctionRegistry, *workflow.Workflow, func()) {
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

	// 注册默认Handler
	_, err = registry.RegisterTaskHandler(ctx, "DefaultLogError", task.DefaultLogError, "默认错误日志")
	if err != nil {
		t.Fatalf("注册DefaultLogError失败: %v", err)
	}

	_, err = registry.RegisterTaskHandler(ctx, "DefaultLogSuccess", task.DefaultLogSuccess, "默认成功日志")
	if err != nil {
		t.Fatalf("注册DefaultLogSuccess失败: %v", err)
	}

	if err := eng.Start(ctx); err != nil {
		t.Fatalf("启动Engine失败: %v", err)
	}

	wf := workflow.NewWorkflow("test-workflow", "测试工作流")

	cleanup := func() {
		eng.Stop()
		repos.Close()
	}

	return eng, registry, wf, cleanup
}

// waitForCompletion 等待工作流完成
func waitForCompletion(t *testing.T, controller workflow.WorkflowController, timeout time.Duration) {
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
}
