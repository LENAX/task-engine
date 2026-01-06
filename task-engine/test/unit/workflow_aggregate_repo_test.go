package unit

import (
	"context"
	"os"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stevelan1995/task-engine/pkg/core/task"
	"github.com/stevelan1995/task-engine/pkg/core/workflow"
	"github.com/stevelan1995/task-engine/pkg/storage/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupAggregateTestDB 创建测试数据库
func setupAggregateTestDB(t *testing.T) *sqlx.DB {
	// 使用临时文件数据库
	dbFile := "test_aggregate_repo.db"
	os.Remove(dbFile)

	db, err := sqlx.Open("sqlite3", dbFile)
	require.NoError(t, err)

	t.Cleanup(func() {
		db.Close()
		os.Remove(dbFile)
	})

	return db
}

func TestWorkflowAggregateRepo_SaveAndGetWorkflow(t *testing.T) {
	db := setupAggregateTestDB(t)
	repo, err := sqlite.NewWorkflowAggregateRepo(db)
	require.NoError(t, err)

	ctx := context.Background()

	// 创建Workflow
	wf := workflow.NewWorkflow("test-workflow", "测试工作流")

	// 创建Task
	task1 := task.NewTask("task1", "任务1", "", map[string]any{"key1": "value1"}, nil)
	task2 := task.NewTask("task2", "任务2", "", map[string]any{"key2": "value2"}, nil)
	task2.SetDependencies([]string{"task1"})

	// 添加Task到Workflow
	err = wf.AddTask(task1)
	require.NoError(t, err)
	err = wf.AddTask(task2)
	require.NoError(t, err)

	// 保存Workflow
	err = repo.SaveWorkflow(ctx, wf)
	require.NoError(t, err)

	// 获取Workflow（不含Task）
	loadedWf, err := repo.GetWorkflow(ctx, wf.GetID())
	require.NoError(t, err)
	assert.NotNil(t, loadedWf)
	assert.Equal(t, wf.GetName(), loadedWf.GetName())
	assert.Empty(t, loadedWf.GetTasks()) // 不应包含Task

	// 获取Workflow（含Task）
	loadedWfWithTasks, err := repo.GetWorkflowWithTasks(ctx, wf.GetID())
	require.NoError(t, err)
	assert.NotNil(t, loadedWfWithTasks)
	assert.Equal(t, wf.GetName(), loadedWfWithTasks.GetName())

	tasks := loadedWfWithTasks.GetTasks()
	assert.Len(t, tasks, 2)

	// 验证Task内容
	task1Loaded, exists := loadedWfWithTasks.GetTaskByName("task1")
	assert.True(t, exists)
	assert.Equal(t, "任务1", task1Loaded.GetDescription())

	task2Loaded, exists := loadedWfWithTasks.GetTaskByName("task2")
	assert.True(t, exists)
	assert.Equal(t, "任务2", task2Loaded.GetDescription())
	assert.Contains(t, task2Loaded.GetDependencies(), "task1")
}

func TestWorkflowAggregateRepo_DeleteWorkflow(t *testing.T) {
	db := setupAggregateTestDB(t)
	repo, err := sqlite.NewWorkflowAggregateRepo(db)
	require.NoError(t, err)

	ctx := context.Background()

	// 创建并保存Workflow
	wf := workflow.NewWorkflow("test-workflow", "测试工作流")
	task1 := task.NewTask("task1", "任务1", "", nil, nil)
	err = wf.AddTask(task1)
	require.NoError(t, err)
	err = repo.SaveWorkflow(ctx, wf)
	require.NoError(t, err)

	// 启动Workflow（创建Instance）
	instance, err := repo.StartWorkflow(ctx, wf)
	require.NoError(t, err)
	assert.NotNil(t, instance)

	// 删除Workflow
	err = repo.DeleteWorkflow(ctx, wf.GetID())
	require.NoError(t, err)

	// 验证Workflow已删除
	loadedWf, err := repo.GetWorkflow(ctx, wf.GetID())
	require.NoError(t, err)
	assert.Nil(t, loadedWf)

	// 验证Instance已删除
	loadedInst, err := repo.GetWorkflowInstance(ctx, instance.ID)
	require.NoError(t, err)
	assert.Nil(t, loadedInst)
}

func TestWorkflowAggregateRepo_StartWorkflow(t *testing.T) {
	db := setupAggregateTestDB(t)
	repo, err := sqlite.NewWorkflowAggregateRepo(db)
	require.NoError(t, err)

	ctx := context.Background()

	// 创建并保存Workflow
	wf := workflow.NewWorkflow("test-workflow", "测试工作流")
	task1 := task.NewTask("task1", "任务1", "", map[string]any{"param1": "value1"}, nil)
	task2 := task.NewTask("task2", "任务2", "", nil, nil)
	task2.SetDependencies([]string{"task1"})

	err = wf.AddTask(task1)
	require.NoError(t, err)
	err = wf.AddTask(task2)
	require.NoError(t, err)
	err = repo.SaveWorkflow(ctx, wf)
	require.NoError(t, err)

	// 启动Workflow
	instance, err := repo.StartWorkflow(ctx, wf)
	require.NoError(t, err)
	assert.NotNil(t, instance)
	assert.Equal(t, wf.GetID(), instance.WorkflowID)
	assert.Equal(t, "Ready", instance.Status)

	// 获取Instance和TaskInstance
	loadedInst, taskInstances, err := repo.GetWorkflowInstanceWithTasks(ctx, instance.ID)
	require.NoError(t, err)
	assert.NotNil(t, loadedInst)
	assert.Len(t, taskInstances, 2)

	// 验证TaskInstance
	for _, ti := range taskInstances {
		assert.Equal(t, instance.ID, ti.WorkflowInstanceID)
		assert.Equal(t, "Pending", ti.Status)
	}
}

func TestWorkflowAggregateRepo_UpdateTaskInstanceStatus(t *testing.T) {
	db := setupAggregateTestDB(t)
	repo, err := sqlite.NewWorkflowAggregateRepo(db)
	require.NoError(t, err)

	ctx := context.Background()

	// 创建并保存Workflow
	wf := workflow.NewWorkflow("test-workflow", "测试工作流")
	task1 := task.NewTask("task1", "任务1", "", nil, nil)
	err = wf.AddTask(task1)
	require.NoError(t, err)
	err = repo.SaveWorkflow(ctx, wf)
	require.NoError(t, err)

	// 启动Workflow
	instance, err := repo.StartWorkflow(ctx, wf)
	require.NoError(t, err)

	// 获取TaskInstance
	taskInstances, err := repo.GetTaskInstancesByWorkflowInstance(ctx, instance.ID)
	require.NoError(t, err)
	require.Len(t, taskInstances, 1)

	taskInstID := taskInstances[0].ID

	// 更新状态
	err = repo.UpdateTaskInstanceStatus(ctx, taskInstID, "Running")
	require.NoError(t, err)

	// 验证状态更新
	ti, err := repo.GetTaskInstance(ctx, taskInstID)
	require.NoError(t, err)
	assert.Equal(t, "Running", ti.Status)

	// 更新状态和错误信息
	err = repo.UpdateTaskInstanceStatusWithError(ctx, taskInstID, "Failed", "执行失败")
	require.NoError(t, err)

	ti, err = repo.GetTaskInstance(ctx, taskInstID)
	require.NoError(t, err)
	assert.Equal(t, "Failed", ti.Status)
	assert.Equal(t, "执行失败", ti.ErrorMessage)
}

func TestWorkflowAggregateRepo_ListWorkflows(t *testing.T) {
	db := setupAggregateTestDB(t)
	repo, err := sqlite.NewWorkflowAggregateRepo(db)
	require.NoError(t, err)

	ctx := context.Background()

	// 创建多个Workflow
	wf1 := workflow.NewWorkflow("workflow-1", "工作流1")
	wf2 := workflow.NewWorkflow("workflow-2", "工作流2")

	err = repo.SaveWorkflow(ctx, wf1)
	require.NoError(t, err)
	err = repo.SaveWorkflow(ctx, wf2)
	require.NoError(t, err)

	// 列出所有Workflow
	workflows, err := repo.ListWorkflows(ctx)
	require.NoError(t, err)
	assert.Len(t, workflows, 2)
}

func TestWorkflowAggregateRepo_ListWorkflowInstances(t *testing.T) {
	db := setupAggregateTestDB(t)
	repo, err := sqlite.NewWorkflowAggregateRepo(db)
	require.NoError(t, err)

	ctx := context.Background()

	// 创建并保存Workflow
	wf := workflow.NewWorkflow("test-workflow", "测试工作流")
	task1 := task.NewTask("task1", "任务1", "", nil, nil)
	err = wf.AddTask(task1)
	require.NoError(t, err)
	err = repo.SaveWorkflow(ctx, wf)
	require.NoError(t, err)

	// 启动多个Instance
	_, err = repo.StartWorkflow(ctx, wf)
	require.NoError(t, err)
	_, err = repo.StartWorkflow(ctx, wf)
	require.NoError(t, err)

	// 列出所有Instance
	instances, err := repo.ListWorkflowInstances(ctx, wf.GetID())
	require.NoError(t, err)
	assert.Len(t, instances, 2)
}

func TestWorkflowAggregateRepo_SaveWorkflow_UpdateExisting(t *testing.T) {
	db := setupAggregateTestDB(t)
	repo, err := sqlite.NewWorkflowAggregateRepo(db)
	require.NoError(t, err)

	ctx := context.Background()

	// 创建并保存Workflow
	wf := workflow.NewWorkflow("test-workflow", "测试工作流")
	task1 := task.NewTask("task1", "任务1", "", nil, nil)
	err = wf.AddTask(task1)
	require.NoError(t, err)
	err = repo.SaveWorkflow(ctx, wf)
	require.NoError(t, err)

	// 重新加载并修改
	loadedWf, err := repo.GetWorkflowWithTasks(ctx, wf.GetID())
	require.NoError(t, err)

	// 添加新Task
	task2 := task.NewTask("task2", "任务2", "", nil, nil)
	task2.SetDependencies([]string{"task1"})
	err = loadedWf.AddTask(task2)
	require.NoError(t, err)

	// 保存修改
	err = repo.SaveWorkflow(ctx, loadedWf)
	require.NoError(t, err)

	// 验证修改
	finalWf, err := repo.GetWorkflowWithTasks(ctx, wf.GetID())
	require.NoError(t, err)
	assert.Len(t, finalWf.GetTasks(), 2)
}

// ========== 幂等性测试 ==========

func TestWorkflowAggregateRepo_Idempotency_DeleteWorkflow(t *testing.T) {
	db := setupAggregateTestDB(t)
	repo, err := sqlite.NewWorkflowAggregateRepo(db)
	require.NoError(t, err)

	ctx := context.Background()

	// 删除不存在的Workflow应该不报错（幂等）
	err = repo.DeleteWorkflow(ctx, "non-existent-id")
	require.NoError(t, err, "删除不存在的Workflow应该不报错")

	// 创建并删除Workflow
	wf := workflow.NewWorkflow("test-workflow", "测试工作流")
	task1 := task.NewTask("task1", "任务1", "", nil, nil)
	err = wf.AddTask(task1)
	require.NoError(t, err)
	err = repo.SaveWorkflow(ctx, wf)
	require.NoError(t, err)

	// 第一次删除
	err = repo.DeleteWorkflow(ctx, wf.GetID())
	require.NoError(t, err)

	// 第二次删除应该不报错（幂等）
	err = repo.DeleteWorkflow(ctx, wf.GetID())
	require.NoError(t, err, "重复删除Workflow应该不报错")
}

func TestWorkflowAggregateRepo_Idempotency_DeleteWorkflowInstance(t *testing.T) {
	db := setupAggregateTestDB(t)
	repo, err := sqlite.NewWorkflowAggregateRepo(db)
	require.NoError(t, err)

	ctx := context.Background()

	// 删除不存在的Instance应该不报错（幂等）
	err = repo.DeleteWorkflowInstance(ctx, "non-existent-id")
	require.NoError(t, err, "删除不存在的Instance应该不报错")

	// 创建Workflow和Instance
	wf := workflow.NewWorkflow("test-workflow", "测试工作流")
	task1 := task.NewTask("task1", "任务1", "", nil, nil)
	err = wf.AddTask(task1)
	require.NoError(t, err)
	err = repo.SaveWorkflow(ctx, wf)
	require.NoError(t, err)

	instance, err := repo.StartWorkflow(ctx, wf)
	require.NoError(t, err)

	// 第一次删除
	err = repo.DeleteWorkflowInstance(ctx, instance.ID)
	require.NoError(t, err)

	// 第二次删除应该不报错（幂等）
	err = repo.DeleteWorkflowInstance(ctx, instance.ID)
	require.NoError(t, err, "重复删除Instance应该不报错")

	// 验证Instance已删除
	loadedInst, err := repo.GetWorkflowInstance(ctx, instance.ID)
	require.NoError(t, err)
	assert.Nil(t, loadedInst)
}

func TestWorkflowAggregateRepo_Idempotency_DeleteTaskInstance(t *testing.T) {
	db := setupAggregateTestDB(t)
	repo, err := sqlite.NewWorkflowAggregateRepo(db)
	require.NoError(t, err)

	ctx := context.Background()

	// 删除不存在的TaskInstance应该不报错（幂等）
	err = repo.DeleteTaskInstance(ctx, "non-existent-id")
	require.NoError(t, err, "删除不存在的TaskInstance应该不报错")

	// 创建Workflow、Instance和TaskInstance
	wf := workflow.NewWorkflow("test-workflow", "测试工作流")
	task1 := task.NewTask("task1", "任务1", "", nil, nil)
	err = wf.AddTask(task1)
	require.NoError(t, err)
	err = repo.SaveWorkflow(ctx, wf)
	require.NoError(t, err)

	instance, err := repo.StartWorkflow(ctx, wf)
	require.NoError(t, err)

	taskInstances, err := repo.GetTaskInstancesByWorkflowInstance(ctx, instance.ID)
	require.NoError(t, err)
	require.Len(t, taskInstances, 1)

	taskInstID := taskInstances[0].ID

	// 第一次删除
	err = repo.DeleteTaskInstance(ctx, taskInstID)
	require.NoError(t, err)

	// 第二次删除应该不报错（幂等）
	err = repo.DeleteTaskInstance(ctx, taskInstID)
	require.NoError(t, err, "重复删除TaskInstance应该不报错")

	// 验证TaskInstance已删除
	loadedTask, err := repo.GetTaskInstance(ctx, taskInstID)
	require.NoError(t, err)
	assert.Nil(t, loadedTask)
}

func TestWorkflowAggregateRepo_Idempotency_UpdateStatus(t *testing.T) {
	db := setupAggregateTestDB(t)
	repo, err := sqlite.NewWorkflowAggregateRepo(db)
	require.NoError(t, err)

	ctx := context.Background()

	// 更新不存在的Instance状态应该不报错（幂等）
	err = repo.UpdateWorkflowInstanceStatus(ctx, "non-existent-id", "Running")
	require.NoError(t, err, "更新不存在的Instance状态应该不报错")

	// 更新不存在的TaskInstance状态应该不报错（幂等）
	err = repo.UpdateTaskInstanceStatus(ctx, "non-existent-id", "Running")
	require.NoError(t, err, "更新不存在的TaskInstance状态应该不报错")

	err = repo.UpdateTaskInstanceStatusWithError(ctx, "non-existent-id", "Failed", "error")
	require.NoError(t, err, "更新不存在的TaskInstance状态和错误应该不报错")
}

func TestWorkflowAggregateRepo_Idempotency_SaveWorkflow(t *testing.T) {
	db := setupAggregateTestDB(t)
	repo, err := sqlite.NewWorkflowAggregateRepo(db)
	require.NoError(t, err)

	ctx := context.Background()

	// 创建Workflow
	wf := workflow.NewWorkflow("test-workflow", "测试工作流")
	task1 := task.NewTask("task1", "任务1", "", nil, nil)
	err = wf.AddTask(task1)
	require.NoError(t, err)

	// 第一次保存
	err = repo.SaveWorkflow(ctx, wf)
	require.NoError(t, err)

	// 第二次保存相同的Workflow应该不报错（幂等）
	err = repo.SaveWorkflow(ctx, wf)
	require.NoError(t, err, "重复保存Workflow应该不报错")

	// 验证数据正确
	loadedWf, err := repo.GetWorkflowWithTasks(ctx, wf.GetID())
	require.NoError(t, err)
	assert.Equal(t, wf.GetName(), loadedWf.GetName())
	assert.Len(t, loadedWf.GetTasks(), 1)
}
