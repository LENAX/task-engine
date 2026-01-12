package mocks

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/LENAX/task-engine/pkg/storage"
)

// MockTaskRepository 模拟TaskRepository，支持模拟各种故障场景
type MockTaskRepository struct {
	mu                sync.RWMutex
	tasks             map[string]*storage.TaskInstance
	shouldFailSave    bool
	shouldFailGet     bool
	shouldFailUpdate  bool
	shouldFailDelete  bool
	failCount         int
	currentFailCount  int
}

// NewMockTaskRepository 创建MockTaskRepository
func NewMockTaskRepository() *MockTaskRepository {
	return &MockTaskRepository{
		tasks: make(map[string]*storage.TaskInstance),
	}
}

// SetShouldFailSave 设置保存操作是否失败
func (m *MockTaskRepository) SetShouldFailSave(shouldFail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldFailSave = shouldFail
}

// SetShouldFailGet 设置获取操作是否失败
func (m *MockTaskRepository) SetShouldFailGet(shouldFail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldFailGet = shouldFail
}

// SetFailCount 设置失败次数（用于模拟部分失败）
func (m *MockTaskRepository) SetFailCount(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failCount = count
	m.currentFailCount = 0
}

// Save 保存Task实例
func (m *MockTaskRepository) Save(ctx context.Context, task *storage.TaskInstance) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.shouldFailSave {
		return errors.New("模拟存储故障：保存失败")
	}

	// 检查失败次数
	if m.failCount > 0 && m.currentFailCount < m.failCount {
		m.currentFailCount++
		return errors.New(fmt.Sprintf("模拟存储故障：保存失败（第%d次）", m.currentFailCount))
	}

	m.tasks[task.ID] = task
	return nil
}

// GetByID 根据ID获取Task实例
func (m *MockTaskRepository) GetByID(ctx context.Context, id string) (*storage.TaskInstance, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.shouldFailGet {
		return nil, errors.New("模拟存储故障：读取失败")
	}

	task, exists := m.tasks[id]
	if !exists {
		return nil, nil
	}

	return task, nil
}

// GetByWorkflowInstanceID 根据WorkflowInstanceID获取Task列表
func (m *MockTaskRepository) GetByWorkflowInstanceID(ctx context.Context, instanceID string) ([]*storage.TaskInstance, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.shouldFailGet {
		return nil, errors.New("模拟存储故障：读取失败")
	}

	var tasks []*storage.TaskInstance
	for _, task := range m.tasks {
		if task.WorkflowInstanceID == instanceID {
			tasks = append(tasks, task)
		}
	}

	return tasks, nil
}

// UpdateStatus 更新Task状态
func (m *MockTaskRepository) UpdateStatus(ctx context.Context, id string, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.shouldFailUpdate {
		return errors.New("模拟存储故障：更新失败")
	}

	task, exists := m.tasks[id]
	if !exists {
		return nil // 幂等性：不存在的记录不报错
	}

	task.Status = status
	return nil
}

// UpdateStatusWithError 更新Task状态和错误信息
func (m *MockTaskRepository) UpdateStatusWithError(ctx context.Context, id string, status string, errorMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.shouldFailUpdate {
		return errors.New("模拟存储故障：更新失败")
	}

	task, exists := m.tasks[id]
	if !exists {
		return nil
	}

	task.Status = status
	// 注意：TaskInstance可能没有ErrorMsg字段，这里只更新状态
	// 如果需要保存错误信息，应该通过其他方式（如Params字段）
	return nil
}

// Delete 删除Task实例
func (m *MockTaskRepository) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.shouldFailDelete {
		return errors.New("模拟存储故障：删除失败")
	}

	delete(m.tasks, id)
	return nil
}

// MockWorkflowInstanceRepository 模拟WorkflowInstanceRepository
type MockWorkflowInstanceRepository struct {
	mu                sync.RWMutex
	instances         map[string]*storage.WorkflowInstance
	shouldFailSave    bool
	shouldFailGet     bool
	shouldFailUpdate  bool
}

// NewMockWorkflowInstanceRepository 创建MockWorkflowInstanceRepository
func NewMockWorkflowInstanceRepository() *MockWorkflowInstanceRepository {
	return &MockWorkflowInstanceRepository{
		instances: make(map[string]*storage.WorkflowInstance),
	}
}

// SetShouldFailSave 设置保存操作是否失败
func (m *MockWorkflowInstanceRepository) SetShouldFailSave(shouldFail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldFailSave = shouldFail
}

// Save 保存WorkflowInstance
func (m *MockWorkflowInstanceRepository) Save(ctx context.Context, instance *storage.WorkflowInstance) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.shouldFailSave {
		return errors.New("模拟存储故障：保存失败")
	}

	m.instances[instance.ID] = instance
	return nil
}

// GetByID 根据ID获取WorkflowInstance
func (m *MockWorkflowInstanceRepository) GetByID(ctx context.Context, id string) (*storage.WorkflowInstance, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.shouldFailGet {
		return nil, errors.New("模拟存储故障：读取失败")
	}

	instance, exists := m.instances[id]
	if !exists {
		return nil, nil
	}

	return instance, nil
}

// UpdateStatus 更新WorkflowInstance状态
func (m *MockWorkflowInstanceRepository) UpdateStatus(ctx context.Context, id string, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.shouldFailUpdate {
		return errors.New("模拟存储故障：更新失败")
	}

	instance, exists := m.instances[id]
	if !exists {
		return nil
	}

	instance.Status = status
	return nil
}

// UpdateBreakpoint 更新断点数据
func (m *MockWorkflowInstanceRepository) UpdateBreakpoint(ctx context.Context, id string, breakpoint interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.shouldFailUpdate {
		return errors.New("模拟存储故障：更新失败")
	}

	return nil
}

// ListByStatus 根据状态查询WorkflowInstance列表
func (m *MockWorkflowInstanceRepository) ListByStatus(ctx context.Context, status string) ([]*storage.WorkflowInstance, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.shouldFailGet {
		return nil, errors.New("模拟存储故障：读取失败")
	}

	var instances []*storage.WorkflowInstance
	for _, instance := range m.instances {
		if instance.Status == status {
			instances = append(instances, instance)
		}
	}

	return instances, nil
}

// Delete 删除WorkflowInstance
func (m *MockWorkflowInstanceRepository) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.instances, id)
	return nil
}

