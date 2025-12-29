package task

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/google/uuid"
	"github.com/stevelan1995/task-engine/pkg/storage"
)

// 统一函数注册中心（对外导出）
type FunctionRegistry struct {
	mu              sync.RWMutex
	functions       map[string]JobFunctionType          // Job函数ID -> 包装后的Job函数
	metaMap         map[string]*storage.JobFunctionMeta // Job函数ID -> 元数据（用于快速查找）
	taskHandlers    map[string]TaskHandlerType          // Task Handler ID -> Task Handler函数
	handlerMeta     map[string]*storage.TaskHandlerMeta // Task Handler ID -> 元数据
	dependencies    map[reflect.Type]interface{}        // 依赖类型 -> 依赖实例（用于依赖注入）
	jobFunctionRepo storage.JobFunctionRepository
	taskHandlerRepo storage.TaskHandlerRepository
}

// NewFunctionRegistry 创建函数注册中心（对外导出）
func NewFunctionRegistry(jobFunctionRepo storage.JobFunctionRepository, taskHandlerRepo storage.TaskHandlerRepository) *FunctionRegistry {
	return &FunctionRegistry{
		functions:       make(map[string]JobFunctionType),
		metaMap:         make(map[string]*storage.JobFunctionMeta),
		taskHandlers:    make(map[string]TaskHandlerType),
		handlerMeta:     make(map[string]*storage.TaskHandlerMeta),
		dependencies:    make(map[reflect.Type]interface{}),
		jobFunctionRepo: jobFunctionRepo,
		taskHandlerRepo: taskHandlerRepo,
	}
}

// generateFunctionName 自动生成函数名称（基于函数类型）
func generateFunctionName(fn interface{}) string {
	fnType := reflect.TypeOf(fn)
	if fnType.Kind() != reflect.Func {
		return "unknown"
	}
	// 使用函数类型的字符串表示作为名称（简化版）
	return fmt.Sprintf("func_%p", fn)
}

// Register 注册Job函数（对外导出）
// name: 函数名称（唯一标识，如果为空则自动生成）
// fn: 用户自定义函数，首个参数必须是context.Context
// description: 函数描述（可选）
// 返回: 函数ID和错误
func (r *FunctionRegistry) Register(ctx context.Context, name string, fn interface{}, description string) (string, error) {
	// 如果名称为空，自动生成
	if name == "" {
		name = generateFunctionName(fn)
	}

	// 包装函数
	wrappedFunc, err := WrapJobFunc(fn)
	if err != nil {
		return "", fmt.Errorf("包装函数失败: %w", err)
	}

	// 提取函数元数据
	meta, err := extractFunctionMeta(fn, name, description)
	if err != nil {
		return "", fmt.Errorf("提取函数元数据失败: %w", err)
	}

	// 持久化元数据到数据库（会自动生成ID）
	if r.jobFunctionRepo != nil {
		if err := r.jobFunctionRepo.Save(ctx, meta); err != nil {
			return "", fmt.Errorf("保存函数元数据失败: %w", err)
		}
		// 从数据库重新加载以获取生成的ID
		loadedMeta, err := r.jobFunctionRepo.GetByName(ctx, meta.Name)
		if err != nil {
			return "", fmt.Errorf("获取函数ID失败: %w", err)
		}
		if loadedMeta == nil {
			return "", fmt.Errorf("函数注册后未找到元数据")
		}
		meta = loadedMeta
	} else {
		// 如果没有repo，生成临时ID（仅内存使用）
		if meta.ID == "" {
			meta.ID = fmt.Sprintf("temp_%p", fn)
		}
	}

	// 保存到内存（使用ID作为key）
	r.mu.Lock()
	r.functions[meta.ID] = wrappedFunc
	r.metaMap[meta.ID] = meta
	r.mu.Unlock()

	return meta.ID, nil
}

// Get 根据函数ID获取包装后的函数（对外导出）
func (r *FunctionRegistry) Get(funcID string) JobFunctionType {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.functions[funcID]
}

// GetByName 根据函数名获取包装后的函数（对外导出）
func (r *FunctionRegistry) GetByName(name string) JobFunctionType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// 遍历metaMap查找匹配的函数名
	for id, meta := range r.metaMap {
		if meta.Name == name {
			return r.functions[id]
		}
	}
	return nil
}

// GetMeta 根据函数ID获取元数据（对外导出）
func (r *FunctionRegistry) GetMeta(funcID string) *storage.JobFunctionMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.metaMap[funcID]
}

// Exists 检查函数是否已注册（对外导出）
func (r *FunctionRegistry) Exists(funcID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.functions[funcID]
	return exists
}

// Unregister 注销函数（对外导出）
func (r *FunctionRegistry) Unregister(ctx context.Context, funcID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	meta, exists := r.metaMap[funcID]
	if !exists {
		return fmt.Errorf("函数 %s 未注册", funcID)
	}

	delete(r.functions, funcID)
	delete(r.metaMap, funcID)

	// 从数据库删除元数据
	if r.jobFunctionRepo != nil && meta != nil {
		if err := r.jobFunctionRepo.Delete(ctx, meta.Name); err != nil {
			return fmt.Errorf("删除函数元数据失败: %w", err)
		}
	}

	return nil
}

// ListAll 列出所有已注册的函数ID（对外导出）
func (r *FunctionRegistry) ListAll() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]string, 0, len(r.functions))
	for id := range r.functions {
		ids = append(ids, id)
	}
	return ids
}

// LoadFunction 从数据库加载函数元数据并注册函数实例（对外导出）
// 用于系统重启后恢复函数
func (r *FunctionRegistry) LoadFunction(ctx context.Context, funcID string, fn interface{}) error {
	if r.jobFunctionRepo == nil {
		return fmt.Errorf("未配置存储仓库，无法加载")
	}

	// 从数据库加载元数据
	meta, err := r.jobFunctionRepo.GetByID(ctx, funcID)
	if err != nil {
		return fmt.Errorf("加载函数元数据失败: %w", err)
	}
	if meta == nil {
		return fmt.Errorf("函数ID %s 不存在", funcID)
	}

	// 包装函数
	wrappedFunc, err := WrapJobFunc(fn)
	if err != nil {
		return fmt.Errorf("包装函数失败: %w", err)
	}

	// 保存到内存
	r.mu.Lock()
	r.functions[funcID] = wrappedFunc
	r.metaMap[funcID] = meta
	r.mu.Unlock()

	return nil
}

// RestoreFromDB 从数据库恢复函数元数据（对外导出）
// 注意：此方法只恢复元数据，函数实例需要用户通过LoadFunction重新注册
func (r *FunctionRegistry) RestoreFromDB(ctx context.Context) ([]*storage.JobFunctionMeta, error) {
	if r.jobFunctionRepo == nil {
		return nil, fmt.Errorf("未配置存储仓库，无法恢复")
	}

	metas, err := r.jobFunctionRepo.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("从数据库加载函数元数据失败: %w", err)
	}

	// 将元数据加载到内存（但不加载函数实例）
	r.mu.Lock()
	for _, meta := range metas {
		r.metaMap[meta.ID] = meta
	}
	r.mu.Unlock()

	return metas, nil
}

// JobFunctionDef 函数定义，用于批量注册
type JobFunctionDef struct {
	Name        string      // 函数名称
	Description string      // 函数描述
	Function    interface{} // 函数实例
}

// RegisterBatch 批量注册函数（对外导出）
func (r *FunctionRegistry) RegisterBatch(ctx context.Context, functions []JobFunctionDef) error {
	for _, def := range functions {
		_, err := r.Register(ctx, def.Name, def.Function, def.Description)
		if err != nil {
			return fmt.Errorf("注册函数 %s 失败: %w", def.Name, err)
		}
	}
	return nil
}

// RestoreFunctions 从数据库恢复函数元数据，并通过函数映射表恢复函数实例（对外导出）
// funcMap: 函数名称 -> 函数实例的映射
func (r *FunctionRegistry) RestoreFunctions(ctx context.Context, funcMap map[string]interface{}) error {
	if r.jobFunctionRepo == nil {
		return fmt.Errorf("未配置存储仓库，无法恢复")
	}

	// 从数据库加载所有函数元数据
	metas, err := r.jobFunctionRepo.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("从数据库加载函数元数据失败: %w", err)
	}

	// 恢复函数实例
	for _, meta := range metas {
		fn, exists := funcMap[meta.Name]
		if !exists {
			// 函数实例不存在，只加载元数据
			r.mu.Lock()
			r.metaMap[meta.ID] = meta
			r.mu.Unlock()
			continue
		}

		// 加载函数实例
		if err := r.LoadFunction(ctx, meta.ID, fn); err != nil {
			return fmt.Errorf("恢复函数 %s 失败: %w", meta.Name, err)
		}
	}

	return nil
}

// GetIDByName 根据函数名称获取函数ID（对外导出）
func (r *FunctionRegistry) GetIDByName(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for id, meta := range r.metaMap {
		if meta.Name == name {
			return id
		}
	}
	return ""
}

// RegisterTaskHandler 注册Task Handler（对外导出）
// name: Handler名称（唯一标识，如果为空则自动生成）
// handler: Task Handler函数
// description: Handler描述（可选）
// 返回: Handler ID和错误
func (r *FunctionRegistry) RegisterTaskHandler(ctx context.Context, name string, handler TaskHandlerType, description string) (string, error) {
	// 如果名称为空，自动生成
	if name == "" {
		name = generateHandlerName(handler)
	}

	// 检查名称是否已存在
	r.mu.RLock()
	for _, meta := range r.handlerMeta {
		if meta.Name == name {
			r.mu.RUnlock()
			return "", fmt.Errorf("Task Handler名称 %s 已存在", name)
		}
	}
	r.mu.RUnlock()

	// 生成Handler ID（使用UUID确保唯一性）
	handlerID := uuid.NewString()

	// 创建元数据
	meta := &storage.TaskHandlerMeta{
		ID:          handlerID,
		Name:        name,
		Description: description,
	}

	// 持久化元数据到数据库（如果配置了repo）
	if r.taskHandlerRepo != nil {
		if err := r.taskHandlerRepo.Save(ctx, meta); err != nil {
			return "", fmt.Errorf("保存TaskHandler元数据失败: %w", err)
		}
		// 从数据库重新加载以获取生成的时间戳
		loadedMeta, err := r.taskHandlerRepo.GetByID(ctx, meta.ID)
		if err != nil {
			return "", fmt.Errorf("获取TaskHandler ID失败: %w", err)
		}
		if loadedMeta == nil {
			return "", fmt.Errorf("TaskHandler注册后未找到元数据")
		}
		meta = loadedMeta
	}

	// 保存到内存
	r.mu.Lock()
	r.taskHandlers[handlerID] = handler
	r.handlerMeta[handlerID] = meta
	r.mu.Unlock()

	return handlerID, nil
}

// GetTaskHandler 根据Handler ID获取Task Handler（对外导出）
func (r *FunctionRegistry) GetTaskHandler(handlerID string) TaskHandlerType {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.taskHandlers[handlerID]
}

// GetTaskHandlerByName 根据Handler名称获取Task Handler（对外导出）
func (r *FunctionRegistry) GetTaskHandlerByName(name string) TaskHandlerType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// 遍历handlerMeta查找匹配的名称
	for id, meta := range r.handlerMeta {
		if meta.Name == name {
			return r.taskHandlers[id]
		}
	}
	return nil
}

// GetTaskHandlerIDByName 根据Handler名称获取Handler ID（对外导出）
func (r *FunctionRegistry) GetTaskHandlerIDByName(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for id, meta := range r.handlerMeta {
		if meta.Name == name {
			return id
		}
	}
	return ""
}

// UnregisterTaskHandler 注销Task Handler（对外导出）
func (r *FunctionRegistry) UnregisterTaskHandler(ctx context.Context, handlerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	meta, exists := r.handlerMeta[handlerID]
	if !exists {
		return fmt.Errorf("Task Handler %s 未注册", handlerID)
	}

	delete(r.taskHandlers, handlerID)
	delete(r.handlerMeta, handlerID)

	// 从数据库删除元数据
	if r.taskHandlerRepo != nil && meta != nil {
		if err := r.taskHandlerRepo.Delete(ctx, meta.Name); err != nil {
			return fmt.Errorf("删除TaskHandler元数据失败: %w", err)
		}
	}

	return nil
}

// ListAllTaskHandlers 列出所有已注册的Task Handler ID（对外导出）
func (r *FunctionRegistry) ListAllTaskHandlers() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]string, 0, len(r.taskHandlers))
	for id := range r.taskHandlers {
		ids = append(ids, id)
	}
	return ids
}

// TaskHandlerExists 检查Task Handler是否已注册（对外导出）
func (r *FunctionRegistry) TaskHandlerExists(handlerID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.taskHandlers[handlerID]
	return exists
}

// generateHandlerName 自动生成Handler名称（基于函数类型）
func generateHandlerName(handler TaskHandlerType) string {
	// 使用函数类型的字符串表示作为名称（简化版）
	return fmt.Sprintf("handler_%p", handler)
}

// extractFunctionMeta 提取函数元数据
// 注意：不再提取参数类型和返回值类型，这些信息在运行时从函数实例通过反射获取
func extractFunctionMeta(fn interface{}, name, description string) (*storage.JobFunctionMeta, error) {
	fnType := reflect.TypeOf(fn)
	if fnType.Kind() != reflect.Func {
		return nil, fmt.Errorf("参数必须是函数类型")
	}

	// 只验证函数签名，不提取参数类型信息
	// 参数类型信息在运行时通过反射从函数实例获取（在WrapJobFunc中）

	return &storage.JobFunctionMeta{
		Name:        name,
		Description: description,
		// 不再存储 ParamTypes 和 ReturnType
		// 运行时从函数实例通过反射获取类型信息
	}, nil
}

// RegisterDependency 注册依赖（对外导出）
// 支持注册任意类型的依赖，如 repository、service 等
// 依赖通过类型作为 key 进行存储，确保类型安全
// 示例: registry.RegisterDependency(userRepo)
func (r *FunctionRegistry) RegisterDependency(dep interface{}) error {
	if dep == nil {
		return fmt.Errorf("依赖不能为 nil")
	}

	depType := reflect.TypeOf(dep)
	// 如果是指针类型，使用指针指向的类型
	if depType.Kind() == reflect.Ptr {
		depType = depType.Elem()
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// 检查是否已注册相同类型的依赖
	if _, exists := r.dependencies[depType]; exists {
		return fmt.Errorf("类型 %s 的依赖已注册", depType.String())
	}

	r.dependencies[depType] = dep
	return nil
}

// GetDependency 从 context 中获取依赖（泛型方法，类型安全）（对外导出）
// 如果依赖未找到，返回零值和 false
// 示例: repo, ok := GetDependency[UserRepository](ctx)
func GetDependency[T any](ctx context.Context) (T, bool) {
	var zero T

	// 从 context 中获取依赖映射
	deps, ok := ctx.Value(dependenciesKey).(map[reflect.Type]interface{})
	if !ok || deps == nil {
		return zero, false
	}

	// 获取类型 T 的 reflect.Type
	var t T
	depType := reflect.TypeOf(t)
	// 如果是指针类型，使用指针指向的类型
	if depType.Kind() == reflect.Ptr {
		depType = depType.Elem()
	}

	// 查找依赖
	dep, exists := deps[depType]
	if !exists {
		return zero, false
	}

	// 类型断言
	if typedDep, ok := dep.(T); ok {
		return typedDep, true
	}

	// 如果直接类型断言失败，尝试通过反射转换
	depValue := reflect.ValueOf(dep)
	if depValue.Type().AssignableTo(reflect.TypeOf(zero)) {
		return depValue.Interface().(T), true
	}

	return zero, false
}

// WithDependencies 将依赖注入到 context 中（对外导出）
// 将 registry 中注册的所有依赖添加到 context 中
// 返回一个新的 context，包含所有依赖
func (r *FunctionRegistry) WithDependencies(ctx context.Context) context.Context {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// 创建依赖映射的副本
	deps := make(map[reflect.Type]interface{})
	for k, v := range r.dependencies {
		deps[k] = v
	}

	return context.WithValue(ctx, dependenciesKey, deps)
}

// dependenciesKey context key 类型，用于存储依赖映射
type dependenciesKeyType struct{}

var dependenciesKey = dependenciesKeyType{}
