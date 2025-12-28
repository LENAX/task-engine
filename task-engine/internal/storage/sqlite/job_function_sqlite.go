package sqlite

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/stevelan1995/task-engine/pkg/storage"
)

// jobFunctionRepo SQLite实现（小写，不导出）
type jobFunctionRepo struct {
	data map[string]*storage.JobFunctionMeta
	mu   sync.RWMutex
}

// NewJobFunctionRepo 创建JobFunction存储实例（内部工厂方法，不导出）
func NewJobFunctionRepo() storage.JobFunctionRepository {
	return &jobFunctionRepo{
		data: make(map[string]*storage.JobFunctionMeta),
	}
}

// Save 实现存储接口（内部实现）
func (r *jobFunctionRepo) Save(ctx context.Context, meta *storage.JobFunctionMeta) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 如果ID为空，生成新ID
	if meta.ID == "" {
		meta.ID = uuid.NewString()
	}

	// 设置时间戳
	now := time.Now()
	if meta.CreateTime.IsZero() {
		meta.CreateTime = now
	}
	meta.UpdateTime = now

	// 保存到内存
	r.data[meta.Name] = meta
	return nil
}

// GetByName 实现存储接口（内部实现）
func (r *jobFunctionRepo) GetByName(ctx context.Context, name string) (*storage.JobFunctionMeta, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.data[name], nil
}

// GetByID 实现存储接口（内部实现）
func (r *jobFunctionRepo) GetByID(ctx context.Context, id string) (*storage.JobFunctionMeta, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, meta := range r.data {
		if meta.ID == id {
			return meta, nil
		}
	}
	return nil, nil
}

// ListAll 实现存储接口（内部实现）
func (r *jobFunctionRepo) ListAll(ctx context.Context) ([]*storage.JobFunctionMeta, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*storage.JobFunctionMeta, 0, len(r.data))
	for _, meta := range r.data {
		// 创建副本避免并发修改
		metaCopy := *meta
		result = append(result, &metaCopy)
	}
	return result, nil
}

// Delete 实现存储接口（内部实现）
func (r *jobFunctionRepo) Delete(ctx context.Context, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.data, name)
	return nil
}

// 辅助函数：序列化ParamTypes为JSON（用于真实数据库存储）
func serializeParamTypes(types map[string]string) (string, error) {
	data, err := json.Marshal(types)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// 辅助函数：反序列化ParamTypes（用于真实数据库加载）
func deserializeParamTypes(data string) (map[string]string, error) {
	var types map[string]string
	if err := json.Unmarshal([]byte(data), &types); err != nil {
		return nil, err
	}
	return types, nil
}

