package unit

import (
	"context"
	"testing"
	"time"

	"github.com/stevelan1995/task-engine/internal/storage/sqlite"
	"github.com/stevelan1995/task-engine/pkg/core/saga"
	"github.com/stevelan1995/task-engine/pkg/core/task"
)

// setupSagaTest 设置SAGA测试环境
func setupSagaTest(t *testing.T) (task.FunctionRegistry, saga.Coordinator, func()) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test_saga.db"

	repos, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("创建Repository失败: %v", err)
	}

	registry := task.NewFunctionRegistry(repos.JobFunction, repos.TaskHandler)

	coordinator := saga.NewCoordinator("test-transaction", registry)

	cleanup := func() {
		repos.Close()
	}

	return registry, coordinator, cleanup
}

// TestSagaCoordinator_Create 测试SAGA协调器创建
func TestSagaCoordinator_Create(t *testing.T) {
	_, coordinator, cleanup := setupSagaTest(t)
	defer cleanup()

	if coordinator == nil {
		t.Fatal("SAGA协调器创建失败")
	}

	state := coordinator.GetState()
	if state != saga.TransactionStatePending {
		t.Errorf("初始状态不正确，期望: Pending, 实际: %s", state)
	}
}

// TestSagaCoordinator_AddStep 测试添加事务步骤
func TestSagaCoordinator_AddStep(t *testing.T) {
	_, coordinator, cleanup := setupSagaTest(t)
	defer cleanup()

	step := saga.NewTransactionStep("task1", "任务1", "Success", "compensateFunc1", "compensateFuncID1")
	coordinator.AddStep(step)

	steps := coordinator.GetSteps()
	if len(steps) != 1 {
		t.Errorf("步骤数量不正确，期望: 1, 实际: %d", len(steps))
	}

	if steps[0].TaskID != "task1" {
		t.Errorf("步骤TaskID不正确，期望: task1, 实际: %s", steps[0].TaskID)
	}
}

// TestSagaCoordinator_Commit 测试提交事务
func TestSagaCoordinator_Commit(t *testing.T) {
	_, coordinator, cleanup := setupSagaTest(t)
	defer cleanup()

	if err := coordinator.Commit(); err != nil {
		t.Fatalf("提交事务失败: %v", err)
	}

	state := coordinator.GetState()
	if state != saga.TransactionStateCommitted {
		t.Errorf("状态不正确，期望: Committed, 实际: %s", state)
	}

	// 已提交的事务不能再提交
	if err := coordinator.Commit(); err == nil {
		t.Error("期望提交失败（已提交状态），但实际成功")
	}
}

// TestSagaCoordinator_Compensate 测试补偿
func TestSagaCoordinator_Compensate(t *testing.T) {
	registry, coordinator, cleanup := setupSagaTest(t)
	defer cleanup()

	ctx := context.Background()

	// 注册补偿函数
	compensateFunc := func(ctx *task.TaskContext) {
		// 补偿逻辑
	}
	_, err := registry.RegisterTaskHandler(ctx, "compensateFunc1", compensateFunc, "补偿函数1")
	if err != nil {
		t.Fatalf("注册补偿函数失败: %v", err)
	}

	// 添加成功步骤
	step1 := saga.NewTransactionStep("task1", "任务1", "Success", "compensateFunc1", "")
	step1.ExecutedAt = time.Now().Unix()
	coordinator.AddStep(step1)

	step2 := saga.NewTransactionStep("task2", "任务2", "Success", "compensateFunc1", "")
	step2.ExecutedAt = time.Now().Unix()
	coordinator.AddStep(step2)

	// 执行补偿
	if err := coordinator.Compensate(ctx); err != nil {
		t.Fatalf("执行补偿失败: %v", err)
	}

	// 等待补偿执行完成（补偿是异步的）
	time.Sleep(100 * time.Millisecond)

	state := coordinator.GetState()
	if state != saga.TransactionStateCompensated {
		t.Errorf("状态不正确，期望: Compensated, 实际: %s", state)
	}
}

// TestSagaCoordinator_MarkStepSuccess 测试标记步骤成功
func TestSagaCoordinator_MarkStepSuccess(t *testing.T) {
	_, coordinator, cleanup := setupSagaTest(t)
	defer cleanup()

	step := saga.NewTransactionStep("task1", "任务1", "Pending", "compensateFunc1", "")
	coordinator.AddStep(step)

	coordinator.MarkStepSuccess("task1")

	steps := coordinator.GetSteps()
	if steps[0].Status != "Success" {
		t.Errorf("步骤状态不正确，期望: Success, 实际: %s", steps[0].Status)
	}
}

// TestSagaCoordinator_MarkStepFailed 测试标记步骤失败
func TestSagaCoordinator_MarkStepFailed(t *testing.T) {
	_, coordinator, cleanup := setupSagaTest(t)
	defer cleanup()

	step := saga.NewTransactionStep("task1", "任务1", "Pending", "compensateFunc1", "")
	coordinator.AddStep(step)

	coordinator.MarkStepFailed("task1")

	steps := coordinator.GetSteps()
	if steps[0].Status != "Failed" {
		t.Errorf("步骤状态不正确，期望: Failed, 实际: %s", steps[0].Status)
	}
}

// TestTransactionState_CanTransitionTo 测试状态转换
func TestTransactionState_CanTransitionTo(t *testing.T) {
	// Pending可以转换到Committed或Compensating
	if !saga.TransactionStatePending.CanTransitionTo(saga.TransactionStateCommitted) {
		t.Error("Pending应该可以转换到Committed")
	}
	if !saga.TransactionStatePending.CanTransitionTo(saga.TransactionStateCompensating) {
		t.Error("Pending应该可以转换到Compensating")
	}

	// Committed是终态，不能转换
	if saga.TransactionStateCommitted.CanTransitionTo(saga.TransactionStatePending) {
		t.Error("Committed不应该可以转换到其他状态")
	}

	// Compensating可以转换到Compensated或Failed
	if !saga.TransactionStateCompensating.CanTransitionTo(saga.TransactionStateCompensated) {
		t.Error("Compensating应该可以转换到Compensated")
	}
	if !saga.TransactionStateCompensating.CanTransitionTo(saga.TransactionStateFailed) {
		t.Error("Compensating应该可以转换到Failed")
	}
}
