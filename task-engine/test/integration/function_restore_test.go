package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stevelan1995/task-engine/internal/storage/sqlite"
	"github.com/stevelan1995/task-engine/pkg/core/builder"
	"github.com/stevelan1995/task-engine/pkg/core/engine"
	"github.com/stevelan1995/task-engine/pkg/core/task"
	"github.com/stevelan1995/task-engine/pkg/core/workflow"
)

// TestFunctionRestore_CompleteFlow 测试完整的函数恢复流程
// 场景：注册函数 -> 持久化 -> 模拟重启 -> 恢复函数 -> 使用恢复的函数执行任务
func TestFunctionRestore_CompleteFlow(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test_function_restore_integration.db"

	// 定义测试函数
	testJobFunc1 := func(ctx *task.TaskContext) (interface{}, error) {
		return map[string]interface{}{
			"result":  "success from func1",
			"task_id": ctx.TaskID,
		}, nil
	}

	testJobFunc2 := func(ctx *task.TaskContext) (interface{}, error) {
		return map[string]interface{}{
			"result":  "success from func2",
			"task_id": ctx.TaskID,
		}, nil
	}

	// 创建函数映射表
	funcMap := map[string]interface{}{
		"testJobFunc1": testJobFunc1,
		"testJobFunc2": testJobFunc2,
	}

	ctx := context.Background()

	// ========== 第一阶段：注册函数并持久化 ==========
	t.Log("========== 第一阶段：注册函数并持久化 ==========")

	repos1, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("创建Repository失败: %v", err)
	}

	engine1, err := engine.NewEngineWithRepos(
		10, 30,
		repos1.Workflow,
		repos1.WorkflowInstance,
		repos1.Task,
		repos1.JobFunction,
		repos1.TaskHandler,
	)
	if err != nil {
		t.Fatalf("创建Engine失败: %v", err)
	}

	registry1 := engine1.GetRegistry()

	// 注册函数（第一次启动）
	func1ID, err := registry1.Register(ctx, "testJobFunc1", testJobFunc1, "测试Job函数1")
	if err != nil {
		t.Fatalf("注册函数1失败: %v", err)
	}

	func2ID, err := registry1.Register(ctx, "testJobFunc2", testJobFunc2, "测试Job函数2")
	if err != nil {
		t.Fatalf("注册函数2失败: %v", err)
	}

	t.Logf("函数已注册: func1ID=%s, func2ID=%s", func1ID, func2ID)

	// 启动引擎
	if err := engine1.Start(ctx); err != nil {
		t.Fatalf("启动Engine失败: %v", err)
	}

	// 等待一下，确保数据已持久化
	time.Sleep(200 * time.Millisecond)

	// 停止引擎（模拟重启）
	engine1.Stop()
	repos1.Close()

	// ========== 第二阶段：重启后恢复函数 ==========
	t.Log("========== 第二阶段：重启后恢复函数 ==========")

	repos2, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("重新创建Repository失败: %v", err)
	}
	defer repos2.Close()

	engine2, err := engine.NewEngineWithRepos(
		10, 30,
		repos2.Workflow,
		repos2.WorkflowInstance,
		repos2.Task,
		repos2.JobFunction,
		repos2.TaskHandler,
	)
	if err != nil {
		t.Fatalf("重新创建Engine失败: %v", err)
	}

	// 设置函数映射表并启用自动恢复
	engine2.SetFunctionMap(funcMap)
	engine2.EnableFunctionRestoreOnStart()

	// 启动引擎（会自动恢复函数）
	if err := engine2.Start(ctx); err != nil {
		t.Fatalf("重新启动Engine失败: %v", err)
	}
	defer engine2.Stop()

	registry2 := engine2.GetRegistry()

	// 验证函数已恢复
	restoredFunc1ID := registry2.GetIDByName("testJobFunc1")
	restoredFunc2ID := registry2.GetIDByName("testJobFunc2")

	if restoredFunc1ID == "" {
		t.Error("函数1未恢复")
	}
	if restoredFunc2ID == "" {
		t.Error("函数2未恢复")
	}

	// 验证函数ID一致（应该一致，因为是从数据库恢复的）
	if func1ID != restoredFunc1ID {
		t.Errorf("函数1 ID不一致，原ID=%s, 恢复ID=%s", func1ID, restoredFunc1ID)
	}
	if func2ID != restoredFunc2ID {
		t.Errorf("函数2 ID不一致，原ID=%s, 恢复ID=%s", func2ID, restoredFunc2ID)
	}

	t.Logf("函数已恢复: func1ID=%s, func2ID=%s", restoredFunc1ID, restoredFunc2ID)

	// ========== 第三阶段：使用恢复的函数执行任务 ==========
	t.Log("========== 第三阶段：使用恢复的函数执行任务 ==========")

	// 创建Workflow
	wf := workflow.NewWorkflow("test-restore-workflow", "测试函数恢复工作流")

	// 创建任务，使用恢复的函数
	task1, err := builder.NewTaskBuilder("task1", "任务1", registry2).
		WithJobFunction("testJobFunc1", nil).
		Build()
	if err != nil {
		t.Fatalf("创建任务1失败: %v", err)
	}

	task2, err := builder.NewTaskBuilder("task2", "任务2", registry2).
		WithJobFunction("testJobFunc2", nil).
		WithDependency("task1").
		Build()
	if err != nil {
		t.Fatalf("创建任务2失败: %v", err)
	}

	wf.AddTask(task1)
	wf.AddTask(task2)

	// 提交Workflow执行
	wfCtrl, err := engine2.SubmitWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("提交Workflow失败: %v", err)
	}

	t.Logf("Workflow已提交，InstanceID=%s", wfCtrl.InstanceID())

	// 等待Workflow完成
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatalf("Workflow执行超时，当前状态: %s", wfCtrl.Status())
		case <-ticker.C:
			status := wfCtrl.Status()
			if status == "Success" || status == "Failed" {
				if status != "Success" {
					t.Errorf("Workflow应该成功完成，实际状态: %s", status)
				} else {
					t.Logf("✅ Workflow完成，状态: %s", status)
				}
				return
			}
		}
	}
}

// TestFunctionRestore_WithEngineBuilder 测试使用EngineBuilder的函数恢复
func TestFunctionRestore_WithEngineBuilder(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test_function_restore_builder.db"

	// 定义测试函数
	testJobFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return map[string]interface{}{
			"result": "success",
		}, nil
	}

	// 创建函数映射表
	funcMap := map[string]interface{}{
		"testJobFunc": testJobFunc,
	}

	ctx := context.Background()

	// ========== 第一阶段：使用EngineBuilder注册函数 ==========
	t.Log("========== 第一阶段：使用EngineBuilder注册函数 ==========")

	// 创建临时配置文件（用于测试）
	configPath := tmpDir + "/test_config.yaml"
	createTestConfig(t, configPath, dbPath)

	engine1, err := engine.NewEngineBuilder(configPath).
		WithJobFunc("testJobFunc", testJobFunc).
		WithFunctionMap(funcMap).
		RestoreFunctionsOnStart().
		Build()
	if err != nil {
		t.Fatalf("构建Engine失败: %v", err)
	}

	// 启动引擎
	if err := engine1.Start(ctx); err != nil {
		t.Fatalf("启动Engine失败: %v", err)
	}

	registry1 := engine1.GetRegistry()
	funcID := registry1.GetIDByName("testJobFunc")
	t.Logf("函数已注册: funcID=%s", funcID)

	// 等待数据持久化
	time.Sleep(200 * time.Millisecond)

	// 停止引擎
	engine1.Stop()

	// ========== 第二阶段：使用EngineBuilder恢复函数 ==========
	t.Log("========== 第二阶段：使用EngineBuilder恢复函数 ==========")

	engine2, err := engine.NewEngineBuilder(configPath).
		WithFunctionMap(funcMap).
		RestoreFunctionsOnStart().
		Build()
	if err != nil {
		t.Fatalf("重新构建Engine失败: %v", err)
	}

	// 启动引擎（会自动恢复函数）
	if err := engine2.Start(ctx); err != nil {
		t.Fatalf("重新启动Engine失败: %v", err)
	}
	defer engine2.Stop()

	registry2 := engine2.GetRegistry()
	restoredFuncID := registry2.GetIDByName("testJobFunc")

	if restoredFuncID == "" {
		t.Error("函数未恢复")
	}

	if funcID != restoredFuncID {
		t.Errorf("函数ID不一致，原ID=%s, 恢复ID=%s", funcID, restoredFuncID)
	}

	// 验证恢复的函数可以正常调用
	fn := registry2.GetByName("testJobFunc")
	if fn == nil {
		t.Error("恢复的函数无法获取")
	}

	t.Logf("✅ 函数已恢复: funcID=%s", restoredFuncID)

	// 清理临时配置文件
	os.Remove(configPath)
}

// createTestConfig 创建测试配置文件
func createTestConfig(t *testing.T, configPath, dbPath string) {
	configContent := `task-engine:
  general:
    instance_name: "test-instance"
    log_level: "info"
  storage:
    database:
      type: "sqlite"
      dsn: "` + dbPath + `"
      max_open_conns: 10
      max_idle_conns: 5
  execution:
    worker_concurrency: 10
    default_task_timeout: 30s
    retry:
      enabled: false
      max_attempts: 3
      delay: 1s
      max_delay: 10s
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("创建测试配置文件失败: %v", err)
	}
}

// TestFunctionRestore_PartialRestore 测试部分函数恢复场景
func TestFunctionRestore_PartialRestore(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test_function_restore_partial.db"

	ctx := context.Background()

	// 定义多个测试函数
	testFunc1 := func(ctx *task.TaskContext) (interface{}, error) {
		return "result1", nil
	}

	testFunc2 := func(ctx *task.TaskContext) (interface{}, error) {
		return "result2", nil
	}

	testFunc3 := func(ctx *task.TaskContext) (interface{}, error) {
		return "result3", nil
	}

	// ========== 第一阶段：注册3个函数 ==========
	repos1, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("创建Repository失败: %v", err)
	}

	engine1, err := engine.NewEngineWithRepos(
		10, 30,
		repos1.Workflow,
		repos1.WorkflowInstance,
		repos1.Task,
		repos1.JobFunction,
		repos1.TaskHandler,
	)
	if err != nil {
		t.Fatalf("创建Engine失败: %v", err)
	}

	registry1 := engine1.GetRegistry()

	// 注册3个函数
	_, err = registry1.Register(ctx, "testFunc1", testFunc1, "测试函数1")
	if err != nil {
		t.Fatalf("注册函数1失败: %v", err)
	}

	_, err = registry1.Register(ctx, "testFunc2", testFunc2, "测试函数2")
	if err != nil {
		t.Fatalf("注册函数2失败: %v", err)
	}

	_, err = registry1.Register(ctx, "testFunc3", testFunc3, "测试函数3")
	if err != nil {
		t.Fatalf("注册函数3失败: %v", err)
	}

	engine1.Start(ctx)
	time.Sleep(200 * time.Millisecond)
	engine1.Stop()
	repos1.Close()

	// ========== 第二阶段：只恢复部分函数（funcMap中只有2个） ==========
	repos2, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("重新创建Repository失败: %v", err)
	}
	defer repos2.Close()

	engine2, err := engine.NewEngineWithRepos(
		10, 30,
		repos2.Workflow,
		repos2.WorkflowInstance,
		repos2.Task,
		repos2.JobFunction,
		repos2.TaskHandler,
	)
	if err != nil {
		t.Fatalf("重新创建Engine失败: %v", err)
	}

	// 只提供部分函数的映射表
	partialFuncMap := map[string]interface{}{
		"testFunc1": testFunc1,
		"testFunc2": testFunc2,
		// testFunc3 不在映射表中
	}

	engine2.SetFunctionMap(partialFuncMap)
	engine2.EnableFunctionRestoreOnStart()

	if err := engine2.Start(ctx); err != nil {
		t.Fatalf("重新启动Engine失败: %v", err)
	}
	defer engine2.Stop()

	registry2 := engine2.GetRegistry()

	// 验证testFunc1和testFunc2已恢复（有函数实例）
	fn1 := registry2.GetByName("testFunc1")
	fn2 := registry2.GetByName("testFunc2")

	if fn1 == nil {
		t.Error("函数1未恢复（应该有函数实例）")
	}
	if fn2 == nil {
		t.Error("函数2未恢复（应该有函数实例）")
	}

	// 验证testFunc3仅恢复元数据（无函数实例）
	func3ID := registry2.GetIDByName("testFunc3")
	if func3ID == "" {
		t.Error("函数3的元数据未恢复")
	}

	fn3 := registry2.GetByName("testFunc3")
	if fn3 != nil {
		t.Error("函数3不应该有函数实例（不在funcMap中）")
	}

	t.Log("✅ 部分函数恢复测试通过：func1和func2有实例，func3仅元数据")
}
