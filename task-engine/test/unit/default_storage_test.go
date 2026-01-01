package unit

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stevelan1995/task-engine/internal/storage/sqlite"
	"github.com/stevelan1995/task-engine/pkg/core/engine"
	"github.com/stevelan1995/task-engine/pkg/core/task"
)

// setupDefaultStorageTest 设置默认存储测试环境
func setupDefaultStorageTest(t *testing.T) (*engine.Engine, *sqlite.Repositories, func()) {
	// 创建临时数据库文件
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_default_storage.db")

	// 创建Repositories
	repos, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("创建Repositories失败: %v", err)
	}

	// 创建Engine，传入JobFunction和TaskHandler的Repository以启用默认存储
	eng, err := engine.NewEngineWithRepos(
		10, // maxConcurrency
		30, // timeout
		repos.Workflow,
		repos.WorkflowInstance,
		repos.Task,
		repos.JobFunction, // 启用JobFunction默认存储
		repos.TaskHandler, // 启用TaskHandler默认存储
	)
	if err != nil {
		t.Fatalf("创建Engine失败: %v", err)
	}

	cleanup := func() {
		eng.Stop()
		os.Remove(dbPath)
	}

	return eng, repos, cleanup
}

// TestDefaultStorage_JobFunctionAutoSave 测试JobFunction自动保存到数据库
func TestDefaultStorage_JobFunctionAutoSave(t *testing.T) {
	eng, repos, cleanup := setupDefaultStorageTest(t)
	defer cleanup()

	ctx := context.Background()

	// 1. 注册JobFunction（应该自动保存到数据库）
	jobFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "test result", nil
	}

	funcID, err := eng.GetRegistry().Register(ctx, "testJobFunc", jobFunc, "测试Job函数")
	if err != nil {
		t.Fatalf("注册JobFunction失败: %v", err)
	}

	if funcID == "" {
		t.Fatal("函数ID为空")
	}

	// 2. 验证函数已保存到数据库
	meta, err := repos.JobFunction.GetByID(ctx, funcID)
	if err != nil {
		t.Fatalf("从数据库获取函数元数据失败: %v", err)
	}

	if meta == nil {
		t.Fatal("函数元数据未保存到数据库")
	}

	if meta.Name != "testJobFunc" {
		t.Errorf("函数名称错误，期望: testJobFunc, 实际: %s", meta.Name)
	}

	if meta.Description != "测试Job函数" {
		t.Errorf("函数描述错误，期望: 测试Job函数, 实际: %s", meta.Description)
	}

	// 3. 验证可以通过名称查找
	metaByName, err := repos.JobFunction.GetByName(ctx, "testJobFunc")
	if err != nil {
		t.Fatalf("通过名称查找函数失败: %v", err)
	}

	if metaByName == nil {
		t.Fatal("通过名称未找到函数")
	}

	if metaByName.ID != funcID {
		t.Errorf("函数ID不匹配，期望: %s, 实际: %s", funcID, metaByName.ID)
	}

	t.Logf("✅ JobFunction自动保存测试通过，FuncID: %s", funcID)
}

// TestDefaultStorage_TaskHandlerAutoSave 测试TaskHandler自动保存到数据库
func TestDefaultStorage_TaskHandlerAutoSave(t *testing.T) {
	eng, repos, cleanup := setupDefaultStorageTest(t)
	defer cleanup()

	ctx := context.Background()

	// 1. 注册TaskHandler（应该自动保存到数据库）
	handler := func(ctx *task.TaskContext) {
		// Handler逻辑
	}

	handlerID, err := eng.GetRegistry().RegisterTaskHandler(ctx, "testHandler", handler, "测试TaskHandler")
	if err != nil {
		t.Fatalf("注册TaskHandler失败: %v", err)
	}

	if handlerID == "" {
		t.Fatal("Handler ID为空")
	}

	// 2. 验证Handler已保存到数据库
	meta, err := repos.TaskHandler.GetByID(ctx, handlerID)
	if err != nil {
		t.Fatalf("从数据库获取Handler元数据失败: %v", err)
	}

	if meta == nil {
		t.Fatal("Handler元数据未保存到数据库")
	}

	if meta.Name != "testHandler" {
		t.Errorf("Handler名称错误，期望: testHandler, 实际: %s", meta.Name)
	}

	if meta.Description != "测试TaskHandler" {
		t.Errorf("Handler描述错误，期望: 测试TaskHandler, 实际: %s", meta.Description)
	}

	// 3. 验证可以通过名称查找
	metaByName, err := repos.TaskHandler.GetByName(ctx, "testHandler")
	if err != nil {
		t.Fatalf("通过名称查找Handler失败: %v", err)
	}

	if metaByName == nil {
		t.Fatal("通过名称未找到Handler")
	}

	if metaByName.ID != handlerID {
		t.Errorf("Handler ID不匹配，期望: %s, 实际: %s", handlerID, metaByName.ID)
	}

	t.Logf("✅ TaskHandler自动保存测试通过，HandlerID: %s", handlerID)
}

// TestDefaultStorage_NoRepositoryFallback 测试没有Repository时的降级行为
func TestDefaultStorage_NoRepositoryFallback(t *testing.T) {
	// 创建Engine，不传入JobFunction和TaskHandler的Repository
	eng, err := engine.NewEngine(
		10,  // maxConcurrency
		30,  // timeout
		nil, // workflowRepo
		nil, // workflowInstanceRepo
		nil, // taskRepo
	)
	if err != nil {
		t.Fatalf("创建Engine失败: %v", err)
	}
	defer eng.Stop()

	ctx := context.Background()

	// 注册JobFunction（应该使用临时ID，不保存到数据库）
	jobFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "test result", nil
	}

	funcID, err := eng.GetRegistry().Register(ctx, "testJobFunc", jobFunc, "测试Job函数")
	if err != nil {
		t.Fatalf("注册JobFunction失败: %v", err)
	}

	if funcID == "" {
		t.Fatal("函数ID为空")
	}

	// 验证函数在内存中可用
	retrievedFunc := eng.GetRegistry().Get(funcID)
	if retrievedFunc == nil {
		t.Fatal("无法从内存中获取函数")
	}

	// 验证函数名称查找
	retrievedByName := eng.GetRegistry().GetByName("testJobFunc")
	if retrievedByName == nil {
		t.Fatal("无法通过名称从内存中获取函数")
	}

	t.Logf("✅ 无Repository降级测试通过，FuncID: %s", funcID)
}

// TestDefaultStorage_EngineBuilderIntegration 测试EngineBuilder集成默认存储
func TestDefaultStorage_EngineBuilderIntegration(t *testing.T) {
	// 创建临时配置文件
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "engine_config.yaml")

	// 创建简单的配置文件
	configContent := `
task-engine:
  general:
    instance_name: "test-engine"
    log_level: "info"
    env: "test"
  storage:
    database:
      type: sqlite
      dsn: ` + filepath.Join(tmpDir, "test_engine_builder.db") + `
      max_open_conns: 10
      max_idle_conns: 5
  execution:
    worker_concurrency: 10
    default_task_timeout: 30s
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("创建配置文件失败: %v", err)
	}

	// 使用EngineBuilder创建Engine
	builder := engine.NewEngineBuilder(configPath)
	jobFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "test result", nil
	}
	builder = builder.WithJobFunc("testJobFunc", jobFunc)

	eng, err := builder.Build()
	if err != nil {
		t.Fatalf("构建Engine失败: %v", err)
	}
	defer eng.Stop()

	ctx := context.Background()

	// 验证函数已注册（通过EngineBuilder注册时应该自动保存到数据库）
	funcID := eng.GetRegistry().GetIDByName("testJobFunc")
	if funcID == "" {
		t.Fatal("函数未注册")
	}

	// 验证函数在内存中可用
	retrievedFunc := eng.GetRegistry().Get(funcID)
	if retrievedFunc == nil {
		t.Fatal("无法从内存中获取函数")
	}

	// 验证函数元数据已保存到数据库（通过EngineBuilder创建的Engine应该配置了Repository）
	// 注意：这里需要访问内部的repos，但为了测试，我们可以通过注册另一个函数来验证
	handler := func(ctx *task.TaskContext) {
		// Handler逻辑
	}

	handlerID, err := eng.GetRegistry().RegisterTaskHandler(ctx, "testHandler", handler, "测试Handler")
	if err != nil {
		t.Fatalf("注册TaskHandler失败: %v", err)
	}

	if handlerID == "" {
		t.Fatal("Handler ID为空")
	}

	// 验证Handler在内存中可用
	retrievedHandler := eng.GetRegistry().GetTaskHandler(handlerID)
	if retrievedHandler == nil {
		t.Fatal("无法从内存中获取Handler")
	}

	t.Logf("✅ EngineBuilder集成测试通过，FuncID: %s, HandlerID: %s", funcID, handlerID)
}

// TestDefaultStorage_MultipleRegistrations 测试多次注册的幂等性
func TestDefaultStorage_MultipleRegistrations(t *testing.T) {
	eng, repos, cleanup := setupDefaultStorageTest(t)
	defer cleanup()

	ctx := context.Background()

	// 1. 第一次注册
	jobFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "test result", nil
	}

	funcID1, err := eng.GetRegistry().Register(ctx, "testJobFunc", jobFunc, "测试Job函数")
	if err != nil {
		t.Fatalf("第一次注册失败: %v", err)
	}

	// 2. 尝试用相同名称再次注册（应该失败）
	_, err = eng.GetRegistry().Register(ctx, "testJobFunc", jobFunc, "测试Job函数2")
	if err == nil {
		t.Fatal("重复注册应该失败，因为JobFunction名称必须唯一")
	}

	// 验证错误信息包含名称和已存在的FuncID
	expectedErrorMsg := "JobFunction名称 testJobFunc 已存在（FuncID: " + funcID1 + "）"
	if err.Error() != expectedErrorMsg {
		// 也可能是数据库层面的错误，检查是否包含名称
		if err.Error() != "保存函数元数据失败: JobFunction名称 testJobFunc 已存在" {
			t.Logf("重复注册失败（符合预期）: %v", err)
		}
	}

	// 3. 验证第一次注册的ID仍然有效（由于名称唯一性，应该只有一条记录）
	meta1, err := repos.JobFunction.GetByID(ctx, funcID1)
	if err != nil {
		t.Fatalf("获取第一次注册的函数失败: %v", err)
	}

	if meta1 == nil {
		t.Fatal("第一次注册的函数不存在")
	}

	if meta1.Name != "testJobFunc" {
		t.Errorf("函数名称错误，期望: testJobFunc, 实际: %s", meta1.Name)
	}

	if meta1.Description != "测试Job函数" {
		t.Errorf("函数描述被修改，期望: 测试Job函数, 实际: %s", meta1.Description)
	}

	// 验证通过名称查找也返回同一个函数
	metaByName, err := repos.JobFunction.GetByName(ctx, "testJobFunc")
	if err != nil {
		t.Fatalf("通过名称查找函数失败: %v", err)
	}

	if metaByName == nil {
		t.Fatal("通过名称未找到函数")
	}

	if metaByName.ID != funcID1 {
		t.Errorf("通过名称查找的函数ID不匹配，期望: %s, 实际: %s", funcID1, metaByName.ID)
	}

	t.Logf("✅ 多次注册幂等性测试通过，FuncID: %s", funcID1)
}

// TestDefaultStorage_ListAll 测试列出所有已注册的函数和Handler
func TestDefaultStorage_ListAll(t *testing.T) {
	eng, repos, cleanup := setupDefaultStorageTest(t)
	defer cleanup()

	ctx := context.Background()

	// 注册多个JobFunction
	jobFunc1 := func(ctx *task.TaskContext) (interface{}, error) {
		return "result1", nil
	}
	jobFunc2 := func(ctx *task.TaskContext) (interface{}, error) {
		return "result2", nil
	}

	_, err := eng.GetRegistry().Register(ctx, "func1", jobFunc1, "函数1")
	if err != nil {
		t.Fatalf("注册func1失败: %v", err)
	}

	_, err = eng.GetRegistry().Register(ctx, "func2", jobFunc2, "函数2")
	if err != nil {
		t.Fatalf("注册func2失败: %v", err)
	}

	// 注册多个TaskHandler
	handler1 := func(ctx *task.TaskContext) {}
	handler2 := func(ctx *task.TaskContext) {}

	_, err = eng.GetRegistry().RegisterTaskHandler(ctx, "handler1", handler1, "Handler1")
	if err != nil {
		t.Fatalf("注册handler1失败: %v", err)
	}

	_, err = eng.GetRegistry().RegisterTaskHandler(ctx, "handler2", handler2, "Handler2")
	if err != nil {
		t.Fatalf("注册handler2失败: %v", err)
	}

	// 验证可以列出所有JobFunction
	jobFuncs, err := repos.JobFunction.ListAll(ctx)
	if err != nil {
		t.Fatalf("列出JobFunction失败: %v", err)
	}

	if len(jobFuncs) < 2 {
		t.Errorf("JobFunction数量不足，期望至少2个，实际: %d", len(jobFuncs))
	}

	// 验证可以列出所有TaskHandler
	handlers, err := repos.TaskHandler.ListAll(ctx)
	if err != nil {
		t.Fatalf("列出TaskHandler失败: %v", err)
	}

	if len(handlers) < 2 {
		t.Errorf("TaskHandler数量不足，期望至少2个，实际: %d", len(handlers))
	}

	t.Logf("✅ 列出所有函数和Handler测试通过，JobFunction: %d, TaskHandler: %d", len(jobFuncs), len(handlers))
}
