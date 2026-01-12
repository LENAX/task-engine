package unit

import (
	"context"
	"testing"

	"github.com/LENAX/task-engine/pkg/core/task"
	"github.com/LENAX/task-engine/pkg/storage"
)

// MockRepository 模拟的 Repository 接口
type MockRepository interface {
	Get(id string) (string, error)
	Save(id string, value string) error
}

// mockRepositoryImpl 实现 MockRepository
type mockRepositoryImpl struct {
	data map[string]string
}

func (m *mockRepositoryImpl) Get(id string) (string, error) {
	return m.data[id], nil
}

func (m *mockRepositoryImpl) Save(id string, value string) error {
	if m.data == nil {
		m.data = make(map[string]string)
	}
	m.data[id] = value
	return nil
}

// MockService 模拟的 Service 接口
type MockService interface {
	Process(data string) string
}

// mockServiceImpl 实现 MockService
type mockServiceImpl struct {
	prefix string
}

func (m *mockServiceImpl) Process(data string) string {
	return m.prefix + data
}

func TestDependencyInjection_RegisterAndGet(t *testing.T) {
	// 创建 registry
	registry := task.NewFunctionRegistry(nil, nil)

	// 注册依赖
	repo := &mockRepositoryImpl{}
	if err := registry.RegisterDependency(repo); err != nil {
		t.Fatalf("注册依赖失败: %v", err)
	}

	service := &mockServiceImpl{prefix: "processed: "}
	if err := registry.RegisterDependency(service); err != nil {
		t.Fatalf("注册依赖失败: %v", err)
	}

	// 创建 context 并注入依赖
	ctx := context.Background()
	ctx = registry.WithDependencies(ctx)

	// 从 context 中获取依赖
	repoFromCtx, ok := task.GetDependency[*mockRepositoryImpl](ctx)
	if !ok {
		t.Fatal("未能从 context 中获取 repository 依赖")
	}

	serviceFromCtx, ok := task.GetDependency[*mockServiceImpl](ctx)
	if !ok {
		t.Fatal("未能从 context 中获取 service 依赖")
	}

	// 验证依赖是否正确
	if repoFromCtx != repo {
		t.Error("获取的 repository 依赖不正确")
	}

	if serviceFromCtx != service {
		t.Error("获取的 service 依赖不正确")
	}

	// 测试依赖功能
	if err := repoFromCtx.Save("test", "value"); err != nil {
		t.Fatalf("保存数据失败: %v", err)
	}

	value, err := repoFromCtx.Get("test")
	if err != nil {
		t.Fatalf("获取数据失败: %v", err)
	}

	if value != "value" {
		t.Errorf("期望值 'value'，实际值 '%s'", value)
	}

	processed := serviceFromCtx.Process("data")
	if processed != "processed: data" {
		t.Errorf("期望值 'processed: data'，实际值 '%s'", processed)
	}
}

func TestDependencyInjection_FromTaskContext(t *testing.T) {
	// 创建 registry
	registry := task.NewFunctionRegistry(nil, nil)

	// 注册依赖
	repo := &mockRepositoryImpl{}
	if err := registry.RegisterDependency(repo); err != nil {
		t.Fatalf("注册依赖失败: %v", err)
	}

	// 创建 context 并注入依赖
	ctx := context.Background()
	ctx = registry.WithDependencies(ctx)

	// 创建 TaskContext
	taskCtx := task.NewTaskContext(
		ctx,
		"task-1",
		"test-task",
		"workflow-1",
		"instance-1",
		map[string]interface{}{"key": "value"},
	)

	// 从 TaskContext 中获取依赖
	repoFromCtx, ok := task.GetDependencyFromContext[*mockRepositoryImpl](taskCtx.Context())
	if !ok {
		t.Fatal("未能从 TaskContext 中获取 repository 依赖")
	}

	// 验证依赖功能
	if err := repoFromCtx.Save("test", "value"); err != nil {
		t.Fatalf("保存数据失败: %v", err)
	}

	value, err := repoFromCtx.Get("test")
	if err != nil {
		t.Fatalf("获取数据失败: %v", err)
	}

	if value != "value" {
		t.Errorf("期望值 'value'，实际值 '%s'", value)
	}
}

func TestDependencyInjection_DuplicateRegistration(t *testing.T) {
	// 创建 registry
	registry := task.NewFunctionRegistry(nil, nil)

	// 注册第一个依赖
	repo1 := &mockRepositoryImpl{}
	if err := registry.RegisterDependency(repo1); err != nil {
		t.Fatalf("注册依赖失败: %v", err)
	}

	// 尝试注册相同类型的依赖（现在允许更新，不返回错误）
	// 这是设计上的改变，允许重新注册依赖以支持更新场景
	repo2 := &mockRepositoryImpl{}
	err := registry.RegisterDependency(repo2)
	if err != nil {
		t.Errorf("注册重复依赖应该允许更新，但返回了错误: %v", err)
	}

	// 验证依赖已被更新（应该获取到repo2而不是repo1）
	ctx := context.Background()
	ctx = registry.WithDependencies(ctx)
	dep, ok := task.GetDependency[*mockRepositoryImpl](ctx)
	if !ok {
		t.Error("期望能够获取依赖，但未找到")
	}
	if dep != repo2 {
		t.Error("期望获取到更新后的依赖（repo2），但获取到了旧的依赖（repo1）")
	}
}

func TestDependencyInjection_NotRegistered(t *testing.T) {
	// 创建 context（未注入依赖）
	ctx := context.Background()

	// 尝试获取未注册的依赖
	_, ok := task.GetDependency[*mockRepositoryImpl](ctx)
	if ok {
		t.Error("期望未注册的依赖返回 false，但返回了 true")
	}
}

func TestDependencyInjection_WithStorageRepositories(t *testing.T) {
	// 创建 mock repositories
	var jobFuncRepo storage.JobFunctionRepository = nil
	var taskHandlerRepo storage.TaskHandlerRepository = nil

	// 创建 registry
	registry := task.NewFunctionRegistry(jobFuncRepo, taskHandlerRepo)

	// 注册一个 repository 依赖
	repo := &mockRepositoryImpl{}
	if err := registry.RegisterDependency(repo); err != nil {
		t.Fatalf("注册依赖失败: %v", err)
	}

	// 创建 context 并注入依赖
	ctx := context.Background()
	ctx = registry.WithDependencies(ctx)

	// 从 context 中获取依赖
	repoFromCtx, ok := task.GetDependency[*mockRepositoryImpl](ctx)
	if !ok {
		t.Fatal("未能从 context 中获取 repository 依赖")
	}

	// 验证依赖功能
	if err := repoFromCtx.Save("test", "value"); err != nil {
		t.Fatalf("保存数据失败: %v", err)
	}
}
