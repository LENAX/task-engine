package task

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/stevelan1995/task-engine/pkg/storage"
)

// JobFunctionRegistry Job函数注册中心（对外导出）
type JobFunctionRegistry struct {
	mu        sync.RWMutex
	functions map[string]JobFunctionType          // 函数ID -> 包装后的函数
	metaMap   map[string]*storage.JobFunctionMeta // 函数ID -> 元数据（用于快速查找）
	repo      storage.JobFunctionRepository
}

// NewJobFunctionRegistry 创建函数注册中心（对外导出）
func NewJobFunctionRegistry(repo storage.JobFunctionRepository) *JobFunctionRegistry {
	return &JobFunctionRegistry{
		functions: make(map[string]JobFunctionType),
		metaMap:   make(map[string]*storage.JobFunctionMeta),
		repo:      repo,
	}
}

// Register 注册Job函数（对外导出）
// name: 函数名称（唯一标识，如果为空则自动生成）
// fn: 用户自定义函数，首个参数必须是context.Context
// description: 函数描述（可选）
// 返回: 函数ID和错误
func (r *JobFunctionRegistry) Register(ctx context.Context, name string, fn interface{}, description string) (string, error) {
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
	if r.repo != nil {
		if err := r.repo.Save(ctx, meta); err != nil {
			return "", fmt.Errorf("保存函数元数据失败: %w", err)
		}
		// 从数据库重新加载以获取生成的ID
		loadedMeta, err := r.repo.GetByName(ctx, meta.Name)
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
func (r *JobFunctionRegistry) Get(funcID string) JobFunctionType {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.functions[funcID]
}

// GetByName 根据函数名获取包装后的函数（对外导出）
func (r *JobFunctionRegistry) GetByName(name string) JobFunctionType {
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
func (r *JobFunctionRegistry) GetMeta(funcID string) *storage.JobFunctionMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.metaMap[funcID]
}

// Exists 检查函数是否已注册（对外导出）
func (r *JobFunctionRegistry) Exists(funcID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.functions[funcID]
	return exists
}

// Unregister 注销函数（对外导出）
func (r *JobFunctionRegistry) Unregister(ctx context.Context, funcID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	meta, exists := r.metaMap[funcID]
	if !exists {
		return fmt.Errorf("函数 %s 未注册", funcID)
	}

	delete(r.functions, funcID)
	delete(r.metaMap, funcID)

	// 从数据库删除元数据
	if r.repo != nil && meta != nil {
		if err := r.repo.Delete(ctx, meta.Name); err != nil {
			return fmt.Errorf("删除函数元数据失败: %w", err)
		}
	}

	return nil
}

// ListAll 列出所有已注册的函数ID（对外导出）
func (r *JobFunctionRegistry) ListAll() []string {
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
func (r *JobFunctionRegistry) LoadFunction(ctx context.Context, funcID string, fn interface{}) error {
	if r.repo == nil {
		return fmt.Errorf("未配置存储仓库，无法加载")
	}

	// 从数据库加载元数据
	meta, err := r.repo.GetByID(ctx, funcID)
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
func (r *JobFunctionRegistry) RestoreFromDB(ctx context.Context) ([]*storage.JobFunctionMeta, error) {
	if r.repo == nil {
		return nil, fmt.Errorf("未配置存储仓库，无法恢复")
	}

	metas, err := r.repo.ListAll(ctx)
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
func (r *JobFunctionRegistry) RegisterBatch(ctx context.Context, functions []JobFunctionDef) error {
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
func (r *JobFunctionRegistry) RestoreFunctions(ctx context.Context, funcMap map[string]interface{}) error {
	if r.repo == nil {
		return fmt.Errorf("未配置存储仓库，无法恢复")
	}

	// 从数据库加载所有函数元数据
	metas, err := r.repo.ListAll(ctx)
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
func (r *JobFunctionRegistry) GetIDByName(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for id, meta := range r.metaMap {
		if meta.Name == name {
			return id
		}
	}
	return ""
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
