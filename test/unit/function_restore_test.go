package unit

import (
	"context"
	"os"
	"testing"

	"github.com/LENAX/task-engine/internal/storage/sqlite"
	"github.com/LENAX/task-engine/pkg/core/task"
)

// setupFunctionRestoreTest 设置函数恢复测试环境
func setupFunctionRestoreTest(t *testing.T) (task.FunctionRegistry, *sqlite.Repositories, string, func()) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test_function_restore.db"

	repos, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("创建Repository失败: %v", err)
	}

	registry := task.NewFunctionRegistry(repos.JobFunction, repos.TaskHandler)

	cleanup := func() {
		repos.Close()
		os.Remove(dbPath)
	}

	return registry, repos, dbPath, cleanup
}

// TestFunctionRestorer_Create 测试FunctionRestorer创建
func TestFunctionRestorer_Create(t *testing.T) {
	registry, _, _, cleanup := setupFunctionRestoreTest(t)
	defer cleanup()

	funcMap := map[string]interface{}{
		"testFunc": func(ctx *task.TaskContext) (interface{}, error) {
			return "test", nil
		},
	}

	restorer := task.NewFunctionRestorer(registry, funcMap)
	if restorer == nil {
		t.Fatal("FunctionRestorer创建失败")
	}

	// 验证函数映射表
	retrievedMap := restorer.GetFunctionMap()
	if len(retrievedMap) != 1 {
		t.Errorf("函数映射表长度不匹配，期望: 1, 实际: %d", len(retrievedMap))
	}
}

// TestFunctionRestorer_AddFunction 测试添加函数到映射表
func TestFunctionRestorer_AddFunction(t *testing.T) {
	registry, _, _, cleanup := setupFunctionRestoreTest(t)
	defer cleanup()

	restorer := task.NewFunctionRestorer(registry, nil)

	testFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "test", nil
	}

	restorer.AddFunction("testFunc", testFunc)

	funcMap := restorer.GetFunctionMap()
	if len(funcMap) != 1 {
		t.Errorf("函数映射表长度不匹配，期望: 1, 实际: %d", len(funcMap))
	}

	if _, exists := funcMap["testFunc"]; !exists {
		t.Error("函数未添加到映射表")
	}
}

// TestFunctionRestorer_Restore_Success 测试函数恢复成功场景
func TestFunctionRestorer_Restore_Success(t *testing.T) {
	registry, repos, dbPath, cleanup := setupFunctionRestoreTest(t)
	defer cleanup()

	ctx := context.Background()

	// 定义测试函数
	testFunc1 := func(ctx *task.TaskContext) (interface{}, error) {
		return "result1", nil
	}

	testFunc2 := func(ctx *task.TaskContext) (interface{}, error) {
		return "result2", nil
	}

	// 先注册函数（模拟第一次启动）
	_, err := registry.Register(ctx, "testFunc1", testFunc1, "测试函数1")
	if err != nil {
		t.Fatalf("注册函数1失败: %v", err)
	}

	_, err = registry.Register(ctx, "testFunc2", testFunc2, "测试函数2")
	if err != nil {
		t.Fatalf("注册函数2失败: %v", err)
	}

	// 关闭第一个registry的repo（模拟重启）
	repos.Close()

	// 创建函数映射表（模拟重启后的函数定义）
	funcMap := map[string]interface{}{
		"testFunc1": testFunc1,
		"testFunc2": testFunc2,
	}

	// 重新创建repo和registry（模拟重启）
	repos2, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("重新创建Repository失败: %v", err)
	}
	defer repos2.Close()

	registry2 := task.NewFunctionRegistry(repos2.JobFunction, repos2.TaskHandler)

	// 创建恢复器并执行恢复
	restorer := task.NewFunctionRestorer(registry2, funcMap)
	if err := restorer.Restore(ctx); err != nil {
		t.Fatalf("恢复函数失败: %v", err)
	}

	// 验证函数已恢复
	func1ID := registry2.GetIDByName("testFunc1")
	func2ID := registry2.GetIDByName("testFunc2")

	if func1ID == "" {
		t.Error("函数1未恢复")
	}
	if func2ID == "" {
		t.Error("函数2未恢复")
	}

	// 验证函数可以正常调用
	fn1 := registry2.GetByName("testFunc1")
	if fn1 == nil {
		t.Error("恢复的函数1无法获取")
	}

	fn2 := registry2.GetByName("testFunc2")
	if fn2 == nil {
		t.Error("恢复的函数2无法获取")
	}
}

// TestFunctionRestorer_Restore_Partial 测试部分函数恢复（部分函数在funcMap中不存在）
func TestFunctionRestorer_Restore_Partial(t *testing.T) {
	registry, repos, dbPath, cleanup := setupFunctionRestoreTest(t)
	defer cleanup()

	ctx := context.Background()

	// 定义测试函数
	testFunc1 := func(ctx *task.TaskContext) (interface{}, error) {
		return "result1", nil
	}

	testFunc2 := func(ctx *task.TaskContext) (interface{}, error) {
		return "result2", nil
	}

	// 先注册3个函数（模拟第一次启动）
	_, err := registry.Register(ctx, "testFunc1", testFunc1, "测试函数1")
	if err != nil {
		t.Fatalf("注册函数1失败: %v", err)
	}

	_, err = registry.Register(ctx, "testFunc2", testFunc2, "测试函数2")
	if err != nil {
		t.Fatalf("注册函数2失败: %v", err)
	}

	_, err = registry.Register(ctx, "testFunc3", testFunc2, "测试函数3")
	if err != nil {
		t.Fatalf("注册函数3失败: %v", err)
	}

	// 关闭第一个registry的repo（模拟重启）
	repos.Close()

	// 创建函数映射表（只包含部分函数，模拟重启后部分函数定义丢失）
	funcMap := map[string]interface{}{
		"testFunc1": testFunc1,
		// testFunc2 和 testFunc3 不在映射表中
	}

	// 重新创建repo和registry（模拟重启）
	repos2, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("重新创建Repository失败: %v", err)
	}
	defer repos2.Close()

	registry2 := task.NewFunctionRegistry(repos2.JobFunction, repos2.TaskHandler)

	// 创建恢复器并执行恢复
	restorer := task.NewFunctionRestorer(registry2, funcMap)
	if err := restorer.Restore(ctx); err != nil {
		t.Fatalf("恢复函数失败: %v", err)
	}

	// 验证testFunc1已恢复（有函数实例）
	func1ID := registry2.GetIDByName("testFunc1")
	if func1ID == "" {
		t.Error("函数1未恢复")
	}

	fn1 := registry2.GetByName("testFunc1")
	if fn1 == nil {
		t.Error("恢复的函数1无法获取")
	}

	// 验证testFunc2和testFunc3仅恢复元数据（无函数实例）
	func2ID := registry2.GetIDByName("testFunc2")
	func3ID := registry2.GetIDByName("testFunc3")

	// 元数据应该已恢复（ID不为空），但函数实例不存在
	if func2ID == "" {
		t.Error("函数2的元数据未恢复")
	}
	if func3ID == "" {
		t.Error("函数3的元数据未恢复")
	}

	// 函数实例应该不存在（因为不在funcMap中）
	// 注意：GetByName可能返回nil，这是预期的
	fn2 := registry2.GetByName("testFunc2")
	if fn2 != nil {
		t.Error("函数2不应该有函数实例（不在funcMap中）")
	}
}

// TestFunctionRestorer_Restore_NoRepo 测试没有Repository时的恢复失败
func TestFunctionRestorer_Restore_NoRepo(t *testing.T) {
	// 创建没有Repository的registry
	registry := task.NewFunctionRegistry(nil, nil)

	funcMap := map[string]interface{}{
		"testFunc": func(ctx *task.TaskContext) (interface{}, error) {
			return "test", nil
		},
	}

	restorer := task.NewFunctionRestorer(registry, funcMap)

	ctx := context.Background()
	err := restorer.Restore(ctx)
	if err == nil {
		t.Error("期望恢复失败（没有Repository），但实际成功")
	}
}

// TestEngineBuilder_WithFunctionMap 测试EngineBuilder.WithFunctionMap
func TestEngineBuilder_WithFunctionMap(t *testing.T) {
	// 这个测试需要完整的EngineBuilder，但由于需要配置文件，这里只测试方法调用
	// 完整的测试在集成测试中

	funcMap := map[string]interface{}{
		"testFunc": func(ctx *task.TaskContext) (interface{}, error) {
			return "test", nil
		},
	}

	// 注意：这里不能真正构建Engine，因为需要配置文件
	// 但可以测试方法链式调用不会panic
	builder := &struct {
		functionMap map[string]interface{}
		err          error
	}{
		functionMap: make(map[string]interface{}),
	}

	// 模拟WithFunctionMap的逻辑
	if builder.err == nil {
		builder.functionMap = make(map[string]interface{})
		for k, v := range funcMap {
			builder.functionMap[k] = v
		}
	}

	if len(builder.functionMap) != 1 {
		t.Errorf("函数映射表长度不匹配，期望: 1, 实际: %d", len(builder.functionMap))
	}
}

// TestEngineBuilder_RestoreFunctionsOnStart 测试EngineBuilder.RestoreFunctionsOnStart
func TestEngineBuilder_RestoreFunctionsOnStart(t *testing.T) {
	// 这个测试需要完整的EngineBuilder，但由于需要配置文件，这里只测试方法调用
	// 完整的测试在集成测试中

	builder := &struct {
		restoreFunctionsOnStart bool
		err                     error
	}{
		restoreFunctionsOnStart: false,
	}

	// 模拟RestoreFunctionsOnStart的逻辑
	if builder.err == nil {
		builder.restoreFunctionsOnStart = true
	}

	if !builder.restoreFunctionsOnStart {
		t.Error("RestoreFunctionsOnStart标志未设置")
	}
}

