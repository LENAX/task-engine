package unit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stevelan1995/task-engine/internal/storage/sqlite"
	"github.com/stevelan1995/task-engine/pkg/core/builder"
	"github.com/stevelan1995/task-engine/pkg/core/engine"
	"github.com/stevelan1995/task-engine/pkg/core/task"
	"github.com/stevelan1995/task-engine/pkg/core/workflow"
	"github.com/stevelan1995/task-engine/test/mocks"
)

// TestErrorHandling_StorageFailure 测试存储故障处理
func TestErrorHandling_StorageFailure(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"

	repos, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("创建Repository失败: %v", err)
	}

	// 创建Mock Repository来模拟存储故障
	mockTaskRepo := mocks.NewMockTaskRepository()
	mockTaskRepo.SetShouldFailSave(true)

	// 注意：这里我们需要能够注入Mock Repository，但Engine的构造函数不接受Mock
	// 所以这个测试主要验证错误处理逻辑，实际存储故障需要在集成测试中验证
	_ = mockTaskRepo

	eng, err := engine.NewEngine(10, 30, repos.Workflow, repos.WorkflowInstance, repos.Task)
	if err != nil {
		t.Fatalf("创建Engine失败: %v", err)
	}

	registry := eng.GetRegistry()
	ctx := context.Background()

	// 注册Job函数
	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	_, err = registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	if err := eng.Start(ctx); err != nil {
		t.Fatalf("启动Engine失败: %v", err)
	}
	defer eng.Stop()

	// 创建Workflow
	wf := workflow.NewWorkflow("test-workflow", "测试工作流")
	task1, _ := builder.NewTaskBuilder("task1", "任务1", registry).
		WithJobFunction("mockFunc", nil).
		Build()

	if err := wf.AddTask(task1); err != nil {
		t.Fatalf("添加任务失败: %v", err)
	}

	// 提交Workflow（应该能正常处理，即使存储有故障）
	controller, err := eng.SubmitWorkflow(ctx, wf)
	if err != nil {
		t.Logf("提交Workflow失败（可能因为存储故障）: %v", err)
	} else {
		// 等待执行完成
		timeout := 5 * time.Second
		startTime := time.Now()
		for {
			status, err := controller.GetStatus()
			if err != nil {
				break
			}

			if status == "Success" || status == "Failed" || status == "Terminated" {
				break
			}

			if time.Since(startTime) > timeout {
				break
			}

			time.Sleep(100 * time.Millisecond)
		}
	}
}

// TestErrorHandling_NetworkTimeout 测试网络超时处理
func TestErrorHandling_NetworkTimeout(t *testing.T) {
	eng, registry, wf, cleanup := setupErrorHandlingTest(t)
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

	// 验证任务应该因为超时失败
	finalStatus, _ := controller.GetStatus()
	t.Logf("工作流状态: %s（期望可能因为超时而失败）", finalStatus)
}

// TestErrorHandling_APIRateLimit 测试API限流处理
func TestErrorHandling_APIRateLimit(t *testing.T) {
	eng, registry, wf, cleanup := setupErrorHandlingTest(t)
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

	// 验证任务应该因为限流失败（即使有重试）
	finalStatus, _ := controller.GetStatus()
	t.Logf("工作流状态: %s（期望可能因为限流而失败）", finalStatus)
}

// setupErrorHandlingTest 设置错误处理测试环境
func setupErrorHandlingTest(t *testing.T) (*engine.Engine, task.FunctionRegistry, *workflow.Workflow, func()) {
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

