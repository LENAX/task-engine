package integration

import (
	"context"
	"testing"

	"github.com/stevelan1995/task-engine/internal/storage/sqlite"
	"github.com/stevelan1995/task-engine/pkg/core/builder"
	"github.com/stevelan1995/task-engine/pkg/core/engine"
	"github.com/stevelan1995/task-engine/pkg/core/task"
)

// TestCronPersistence_SaveAndLoad 测试Cron字段的持久化和恢复
func TestCronPersistence_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test_cron_persistence.db"

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

	// 创建Workflow并设置Cron
	task1, err := builder.NewTaskBuilder("task1", "任务1", registry).
		WithJobFunction("jobFunc", nil).
		Build()
	if err != nil {
		t.Fatalf("创建任务1失败: %v", err)
	}

	wf, err := builder.NewWorkflowBuilder("test-cron-persistence", "测试Cron持久化").
		WithTask(task1).
		WithCronExpr("0 0 0 * * *"). // 每天凌晨执行
		Build()
	if err != nil {
		t.Fatalf("创建Workflow失败: %v", err)
	}

	// 验证Cron设置
	if !wf.IsCronEnabled() {
		t.Error("Workflow应该启用Cron调度")
	}
	if wf.GetCronExpr() != "0 0 0 * * *" {
		t.Errorf("Cron表达式不匹配，期望: 0 0 0 * * *, 实际: %s", wf.GetCronExpr())
	}

	// 保存Workflow到数据库
	err = repos.Workflow.Save(ctx, wf)
	if err != nil {
		t.Fatalf("保存Workflow失败: %v", err)
	}

	// 从数据库读取Workflow
	loadedWf, err := repos.Workflow.GetByID(ctx, wf.GetID())
	if err != nil {
		t.Fatalf("读取Workflow失败: %v", err)
	}
	if loadedWf == nil {
		t.Fatal("读取的Workflow为空")
	}

	// 验证Cron字段是否正确恢复
	if !loadedWf.IsCronEnabled() {
		t.Error("从数据库读取的Workflow应该启用Cron调度")
	}
	if loadedWf.GetCronExpr() != "0 0 0 * * *" {
		t.Errorf("从数据库读取的Cron表达式不匹配，期望: 0 0 0 * * *, 实际: %s", loadedWf.GetCronExpr())
	}

	t.Logf("✅ Cron字段持久化验证通过: CronExpr=%s, CronEnabled=%v", loadedWf.GetCronExpr(), loadedWf.IsCronEnabled())
}

// TestCronPersistence_WithoutCron 测试没有Cron设置的Workflow持久化
func TestCronPersistence_WithoutCron(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test_cron_persistence_no_cron.db"

	repos, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("创建Repository失败: %v", err)
	}
	defer repos.Close()

	// 创建Workflow但不设置Cron
	wf := builder.NewWorkflowBuilder("test-no-cron", "测试无Cron Workflow").Build()
	if wf == nil {
		t.Fatal("创建Workflow失败")
	}

	// 验证默认值
	if wf.IsCronEnabled() {
		t.Error("Workflow默认不应该启用Cron调度")
	}
	if wf.GetCronExpr() != "" {
		t.Errorf("Workflow默认Cron表达式应该为空，实际: %s", wf.GetCronExpr())
	}

	// 保存Workflow到数据库
	ctx := context.Background()
	err = repos.Workflow.Save(ctx, wf)
	if err != nil {
		t.Fatalf("保存Workflow失败: %v", err)
	}

	// 从数据库读取Workflow
	loadedWf, err := repos.Workflow.GetByID(ctx, wf.GetID())
	if err != nil {
		t.Fatalf("读取Workflow失败: %v", err)
	}
	if loadedWf == nil {
		t.Fatal("读取的Workflow为空")
	}

	// 验证默认值是否正确恢复
	if loadedWf.IsCronEnabled() {
		t.Error("从数据库读取的Workflow默认不应该启用Cron调度")
	}
	if loadedWf.GetCronExpr() != "" {
		t.Errorf("从数据库读取的Workflow默认Cron表达式应该为空，实际: %s", loadedWf.GetCronExpr())
	}

	t.Logf("✅ 无Cron Workflow持久化验证通过: CronExpr=%s, CronEnabled=%v", loadedWf.GetCronExpr(), loadedWf.IsCronEnabled())
}

