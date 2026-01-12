package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/LENAX/task-engine/internal/storage/sqlite"
	"github.com/LENAX/task-engine/pkg/core/builder"
	"github.com/LENAX/task-engine/pkg/core/engine"
	"github.com/LENAX/task-engine/pkg/core/task"
)

// TestCronScheduling_Basic 测试基本的Cron调度功能
func TestCronScheduling_Basic(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test_cron.db"

	repos, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("创建Repository失败: %v", err)
	}
	defer repos.Close()

	eng, err := engine.NewEngineWithRepos(
		10, 30,
		repos.Workflow,
		repos.WorkflowInstance,
		repos.Task,
		repos.JobFunction,
		repos.TaskHandler,
	)
	if err != nil {
		t.Fatalf("创建Engine失败: %v", err)
	}

	registry := eng.GetRegistry()
	ctx := context.Background()

	// 用于跟踪执行次数的变量
	var executionCount int
	var mu sync.Mutex

	// 定义Job函数
	jobFunc := func(ctx *task.TaskContext) (interface{}, error) {
		mu.Lock()
		executionCount++
		mu.Unlock()
		return map[string]interface{}{
			"result": "success",
		}, nil
	}

	// 注册Job函数
	_, err = registry.Register(ctx, "jobFunc", jobFunc, "Job函数")
	if err != nil {
		t.Fatalf("注册Job函数失败: %v", err)
	}

	// 启动引擎
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("启动Engine失败: %v", err)
	}
	defer eng.Stop()

	// 创建Workflow并启用Cron（每2秒执行一次）
	task1, err := builder.NewTaskBuilder("task1", "任务1", registry).
		WithJobFunction("jobFunc", nil).
		Build()
	if err != nil {
		t.Fatalf("创建任务1失败: %v", err)
	}

	wf, err := builder.NewWorkflowBuilder("test-cron-workflow", "测试Cron工作流").
		WithTask(task1).
		WithCronExpr("*/2 * * * * *"). // 每2秒执行一次
		Build()
	if err != nil {
		t.Fatalf("创建Workflow失败: %v", err)
	}

	// 提交Workflow（会自动注册到CronScheduler）
	_, err = eng.SubmitWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("提交Workflow失败: %v", err)
	}

	// 等待一段时间，观察Cron触发
	time.Sleep(7 * time.Second) // 等待7秒，应该触发至少3次

	// 验证执行次数
	mu.Lock()
	count := executionCount
	mu.Unlock()

	if count < 3 {
		t.Errorf("Cron调度应该触发至少3次，实际触发: %d", count)
	} else {
		t.Logf("✅ Cron调度已触发 %d 次", count)
	}
}

// TestCronScheduling_WorkflowBuilder 测试使用WorkflowBuilder设置Cron
func TestCronScheduling_WorkflowBuilder(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test_cron_builder.db"

	repos, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("创建Repository失败: %v", err)
	}
	defer repos.Close()

	eng, err := engine.NewEngineWithRepos(
		10, 30,
		repos.Workflow,
		repos.WorkflowInstance,
		repos.Task,
		repos.JobFunction,
		repos.TaskHandler,
	)
	if err != nil {
		t.Fatalf("创建Engine失败: %v", err)
	}

	registry := eng.GetRegistry()
	ctx := context.Background()

	// 定义Job函数
	jobFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return map[string]interface{}{
			"result": "success",
		}, nil
	}

	// 注册Job函数
	_, err = registry.Register(ctx, "jobFunc", jobFunc, "Job函数")
	if err != nil {
		t.Fatalf("注册Job函数失败: %v", err)
	}

	// 启动引擎
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("启动Engine失败: %v", err)
	}
	defer eng.Stop()

	// 使用WorkflowBuilder创建Workflow并设置Cron
	task1, err := builder.NewTaskBuilder("task1", "任务1", registry).
		WithJobFunction("jobFunc", nil).
		Build()
	if err != nil {
		t.Fatalf("创建任务1失败: %v", err)
	}

	wf, err := builder.NewWorkflowBuilder("test-cron-builder", "测试Cron Builder").
		WithTask(task1).
		WithCronExpr("*/3 * * * * *"). // 每3秒执行一次
		Build()
	if err != nil {
		t.Fatalf("创建Workflow失败: %v", err)
	}

	// 验证Cron设置
	if !wf.IsCronEnabled() {
		t.Error("Workflow应该启用Cron调度")
	}
	if wf.GetCronExpr() != "*/3 * * * * *" {
		t.Errorf("Cron表达式不匹配，期望: */3 * * * * *, 实际: %s", wf.GetCronExpr())
	}

	// 提交Workflow
	_, err = eng.SubmitWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("提交Workflow失败: %v", err)
	}

	t.Logf("✅ Workflow已提交，Cron表达式: %s", wf.GetCronExpr())
}

