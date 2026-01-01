package unit

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stevelan1995/task-engine/internal/storage/sqlite"
	"github.com/stevelan1995/task-engine/pkg/core/workflow"
	"github.com/stevelan1995/task-engine/pkg/storage"
)

// 测试用的临时数据库文件
const testDBPath = "./test_repository.db"

func setupTestDB(t *testing.T) *sqlx.DB {
	// 删除旧的测试数据库
	os.Remove(testDBPath)

	// 创建新的数据库连接
	db, err := sqlx.Open("sqlite3", testDBPath)
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}

	// 测试连接
	if err := db.Ping(); err != nil {
		t.Fatalf("数据库连接失败: %v", err)
	}

	return db
}

func cleanupTestDB(t *testing.T) {
	os.Remove(testDBPath)
}

func TestWorkflowRepository_SaveAndGet(t *testing.T) {
	db := setupTestDB(t)
	defer cleanupTestDB(t)
	defer db.Close()

	repo, err := sqlite.NewWorkflowRepo(testDBPath)
	if err != nil {
		t.Fatalf("创建WorkflowRepository失败: %v", err)
	}

	ctx := context.Background()

	// 创建测试Workflow
	wf := workflow.NewWorkflow("test-workflow", "测试工作流")
	wf.SetParams(map[string]string{"key": "value"})

	// 保存
	err = repo.Save(ctx, wf)
	if err != nil {
		t.Fatalf("保存Workflow失败: %v", err)
	}

	// 查询
	retrieved, err := repo.GetByID(ctx, wf.GetID())
	if err != nil {
		t.Fatalf("查询Workflow失败: %v", err)
	}

	if retrieved == nil {
		t.Fatal("查询结果为空")
	}

	if retrieved.GetID() != wf.GetID() {
		t.Errorf("ID不匹配，期望: %s, 实际: %s", wf.GetID(), retrieved.GetID())
	}

	if retrieved.GetName() != wf.GetName() {
		t.Errorf("名称不匹配，期望: %s, 实际: %s", wf.GetName(), retrieved.GetName())
	}
}

func TestWorkflowInstanceRepository_CRUD(t *testing.T) {
	db := setupTestDB(t)
	defer cleanupTestDB(t)
	defer db.Close()

	repo := sqlite.NewWorkflowInstanceRepo(db)
	ctx := context.Background()

	// 创建测试实例
	instance := &storage.WorkflowInstance{
		ID:         uuid.NewString(),
		WorkflowID: uuid.NewString(),
		Status:     "Ready",
		StartTime:  time.Now(),
		CreateTime: time.Now(),
	}

	// 保存
	err := repo.Save(ctx, instance)
	if err != nil {
		t.Fatalf("保存WorkflowInstance失败: %v", err)
	}

	// 查询
	retrieved, err := repo.GetByID(ctx, instance.ID)
	if err != nil {
		t.Fatalf("查询WorkflowInstance失败: %v", err)
	}

	if retrieved == nil {
		t.Fatal("查询结果为空")
	}

	if retrieved.ID != instance.ID {
		t.Errorf("ID不匹配，期望: %s, 实际: %s", instance.ID, retrieved.ID)
	}

	// 更新状态
	err = repo.UpdateStatus(ctx, instance.ID, "Running")
	if err != nil {
		t.Fatalf("更新状态失败: %v", err)
	}

	// 验证状态更新
	retrieved, err = repo.GetByID(ctx, instance.ID)
	if err != nil {
		t.Fatalf("查询WorkflowInstance失败: %v", err)
	}

	if retrieved.Status != "Running" {
		t.Errorf("状态未更新，期望: Running, 实际: %s", retrieved.Status)
	}

	// 更新断点数据
	breakpoint := &storage.BreakpointData{
		CompletedTaskNames: []string{"task1", "task2"},
		RunningTaskNames:   []string{"task3"},
		DAGSnapshot:        make(map[string]interface{}),
		ContextData:        make(map[string]interface{}),
		LastUpdateTime:     time.Now(),
	}

	err = repo.UpdateBreakpoint(ctx, instance.ID, breakpoint)
	if err != nil {
		t.Fatalf("更新断点数据失败: %v", err)
	}

	// 验证断点数据
	retrieved, err = repo.GetByID(ctx, instance.ID)
	if err != nil {
		t.Fatalf("查询WorkflowInstance失败: %v", err)
	}

	if retrieved.Breakpoint == nil {
		t.Fatal("断点数据为空")
	}

	if len(retrieved.Breakpoint.CompletedTaskNames) != 2 {
		t.Errorf("已完成任务数量错误，期望: 2, 实际: %d", len(retrieved.Breakpoint.CompletedTaskNames))
	}

	// 按状态查询
	instances, err := repo.ListByStatus(ctx, "Running")
	if err != nil {
		t.Fatalf("按状态查询失败: %v", err)
	}

	if len(instances) == 0 {
		t.Fatal("未查询到Running状态的实例")
	}

	// 删除
	err = repo.Delete(ctx, instance.ID)
	if err != nil {
		t.Fatalf("删除WorkflowInstance失败: %v", err)
	}

	// 验证删除
	retrieved, err = repo.GetByID(ctx, instance.ID)
	if err != nil {
		t.Fatalf("查询WorkflowInstance失败: %v", err)
	}

	if retrieved != nil {
		t.Fatal("删除后仍能查询到实例")
	}
}

func TestTaskRepository_CRUD(t *testing.T) {
	db := setupTestDB(t)
	defer cleanupTestDB(t)
	defer db.Close()

	repo := sqlite.NewTaskRepo(db)
	ctx := context.Background()

	instanceID := uuid.NewString()

	// 创建测试Task
	task := &storage.TaskInstance{
		ID:                 uuid.NewString(),
		Name:               "test-task",
		WorkflowInstanceID: instanceID,
		JobFuncID:          uuid.NewString(),
		JobFuncName:        "test-func",
		Params:             map[string]interface{}{"key": "value"},
		Status:             "Pending",
		TimeoutSeconds:     30,
		RetryCount:         0,
		CreateTime:         time.Now(),
	}

	// 保存
	err := repo.Save(ctx, task)
	if err != nil {
		t.Fatalf("保存Task失败: %v", err)
	}

	// 查询
	retrieved, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("查询Task失败: %v", err)
	}

	if retrieved == nil {
		t.Fatal("查询结果为空")
	}

	if retrieved.ID != task.ID {
		t.Errorf("ID不匹配，期望: %s, 实际: %s", task.ID, retrieved.ID)
	}

	// 更新状态
	err = repo.UpdateStatus(ctx, task.ID, "Running")
	if err != nil {
		t.Fatalf("更新状态失败: %v", err)
	}

	// 更新状态和错误信息
	err = repo.UpdateStatusWithError(ctx, task.ID, "Failed", "测试错误")
	if err != nil {
		t.Fatalf("更新状态和错误信息失败: %v", err)
	}

	// 验证更新
	retrieved, err = repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("查询Task失败: %v", err)
	}

	if retrieved.Status != "Failed" {
		t.Errorf("状态未更新，期望: Failed, 实际: %s", retrieved.Status)
	}

	if retrieved.ErrorMessage != "测试错误" {
		t.Errorf("错误信息未更新，期望: 测试错误, 实际: %s", retrieved.ErrorMessage)
	}

	// 按WorkflowInstanceID查询
	tasks, err := repo.GetByWorkflowInstanceID(ctx, instanceID)
	if err != nil {
		t.Fatalf("按WorkflowInstanceID查询失败: %v", err)
	}

	if len(tasks) == 0 {
		t.Fatal("未查询到Task")
	}

	// 删除
	err = repo.Delete(ctx, task.ID)
	if err != nil {
		t.Fatalf("删除Task失败: %v", err)
	}

	// 验证删除
	retrieved, err = repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("查询Task失败: %v", err)
	}

	if retrieved != nil {
		t.Fatal("删除后仍能查询到Task")
	}
}

func TestJobFunctionRepository_CRUD(t *testing.T) {
	db := setupTestDB(t)
	defer cleanupTestDB(t)
	defer db.Close()

	repo := sqlite.NewJobFunctionRepo(db)
	ctx := context.Background()

	// 创建测试元数据
	meta := &storage.JobFunctionMeta{
		ID:          uuid.NewString(),
		Name:        "test-function",
		Description: "测试函数",
		CreateTime:  time.Now(),
		UpdateTime:  time.Now(),
	}

	// 保存
	err := repo.Save(ctx, meta)
	if err != nil {
		t.Fatalf("保存JobFunctionMeta失败: %v", err)
	}

	// 按名称查询
	retrieved, err := repo.GetByName(ctx, meta.Name)
	if err != nil {
		t.Fatalf("查询JobFunctionMeta失败: %v", err)
	}

	if retrieved == nil {
		t.Fatal("查询结果为空")
	}

	if retrieved.ID != meta.ID {
		t.Errorf("ID不匹配，期望: %s, 实际: %s", meta.ID, retrieved.ID)
	}

	// 按ID查询
	retrieved, err = repo.GetByID(ctx, meta.ID)
	if err != nil {
		t.Fatalf("查询JobFunctionMeta失败: %v", err)
	}

	if retrieved == nil {
		t.Fatal("查询结果为空")
	}

	// 查询所有
	all, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatalf("查询所有JobFunctionMeta失败: %v", err)
	}

	if len(all) == 0 {
		t.Fatal("未查询到任何元数据")
	}

	// 删除
	err = repo.Delete(ctx, meta.Name)
	if err != nil {
		t.Fatalf("删除JobFunctionMeta失败: %v", err)
	}

	// 验证删除
	retrieved, err = repo.GetByName(ctx, meta.Name)
	if err != nil {
		t.Fatalf("查询JobFunctionMeta失败: %v", err)
	}

	if retrieved != nil {
		t.Fatal("删除后仍能查询到元数据")
	}
}
