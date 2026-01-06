package unit

import (
	"testing"
	"time"

	"github.com/stevelan1995/task-engine/pkg/core/engine"
	"github.com/stevelan1995/task-engine/pkg/core/workflow"
)

// TestCronScheduler_Create 测试创建CronScheduler
func TestCronScheduler_Create(t *testing.T) {
	// 创建mock Engine
	eng := &engine.Engine{}

	// 创建CronScheduler
	scheduler := engine.NewCronScheduler(eng)
	if scheduler == nil {
		t.Fatal("创建CronScheduler失败")
	}

	// 验证初始状态
	workflows := scheduler.GetRegisteredWorkflows()
	if len(workflows) != 0 {
		t.Errorf("初始状态应该有0个已注册的Workflow，实际: %d", len(workflows))
	}
}

// TestCronScheduler_RegisterWorkflow 测试注册Workflow
func TestCronScheduler_RegisterWorkflow(t *testing.T) {
	// 创建mock Engine
	eng := &engine.Engine{}

	// 创建CronScheduler
	scheduler := engine.NewCronScheduler(eng)

	// 创建Workflow并启用Cron
	wf := workflow.NewWorkflow("test-workflow", "测试Workflow")
	wf.SetCronExpr("*/5 * * * * *") // 每5秒执行一次（6字段格式：秒 分 时 日 月 周）
	wf.SetCronEnabled(true)

	// 注册Workflow
	err := scheduler.RegisterWorkflow(wf)
	if err != nil {
		t.Fatalf("注册Workflow失败: %v", err)
	}

	// 验证已注册
	workflows := scheduler.GetRegisteredWorkflows()
	if len(workflows) != 1 {
		t.Errorf("应该有1个已注册的Workflow，实际: %d", len(workflows))
	}
	if workflows[0] != wf.GetID() {
		t.Errorf("已注册的Workflow ID不匹配，期望: %s, 实际: %s", wf.GetID(), workflows[0])
	}
}

// TestCronScheduler_RegisterWorkflow_InvalidCronExpr 测试注册无效Cron表达式的Workflow
func TestCronScheduler_RegisterWorkflow_InvalidCronExpr(t *testing.T) {
	// 创建mock Engine
	eng := &engine.Engine{}

	// 创建CronScheduler
	scheduler := engine.NewCronScheduler(eng)

	// 创建Workflow并设置无效的Cron表达式
	wf := workflow.NewWorkflow("test-workflow", "测试Workflow")
	wf.SetCronExpr("invalid cron expr")
	wf.SetCronEnabled(true)

	// 注册Workflow应该失败
	err := scheduler.RegisterWorkflow(wf)
	if err == nil {
		t.Error("注册无效Cron表达式的Workflow应该失败")
	}
}

// TestCronScheduler_RegisterWorkflow_NotEnabled 测试注册未启用Cron的Workflow
func TestCronScheduler_RegisterWorkflow_NotEnabled(t *testing.T) {
	// 创建mock Engine
	eng := &engine.Engine{}

	// 创建CronScheduler
	scheduler := engine.NewCronScheduler(eng)

	// 创建Workflow但未启用Cron
	wf := workflow.NewWorkflow("test-workflow", "测试Workflow")
	wf.SetCronExpr("*/5 * * * * *")
	wf.SetCronEnabled(false) // 未启用

	// 注册Workflow应该失败
	err := scheduler.RegisterWorkflow(wf)
	if err == nil {
		t.Error("注册未启用Cron的Workflow应该失败")
	}
}

// TestCronScheduler_UnregisterWorkflow 测试取消注册Workflow
func TestCronScheduler_UnregisterWorkflow(t *testing.T) {
	// 创建mock Engine
	eng := &engine.Engine{}

	// 创建CronScheduler
	scheduler := engine.NewCronScheduler(eng)

	// 创建并注册Workflow
	wf := workflow.NewWorkflow("test-workflow", "测试Workflow")
	wf.SetCronExpr("*/5 * * * * *")
	wf.SetCronEnabled(true)

	err := scheduler.RegisterWorkflow(wf)
	if err != nil {
		t.Fatalf("注册Workflow失败: %v", err)
	}

	// 取消注册
	err = scheduler.UnregisterWorkflow(wf.GetID())
	if err != nil {
		t.Fatalf("取消注册Workflow失败: %v", err)
	}

	// 验证已取消注册
	workflows := scheduler.GetRegisteredWorkflows()
	if len(workflows) != 0 {
		t.Errorf("取消注册后应该有0个已注册的Workflow，实际: %d", len(workflows))
	}
}

// TestCronScheduler_StartStop 测试启动和停止CronScheduler
func TestCronScheduler_StartStop(t *testing.T) {
	// 创建mock Engine
	eng := &engine.Engine{}

	// 创建CronScheduler
	scheduler := engine.NewCronScheduler(eng)

	// 启动
	scheduler.Start()

	// 等待一小段时间确保启动
	time.Sleep(100 * time.Millisecond)

	// 停止
	scheduler.Stop()

	// 等待一小段时间确保停止
	time.Sleep(100 * time.Millisecond)
}
