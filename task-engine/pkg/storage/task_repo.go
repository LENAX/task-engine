package storage

import (
	"context"
	"time"
)

// TaskCRUDRepository Task通用CRUD接口（对外导出）
// 提供基础的增删改查操作
type TaskCRUDRepository interface {
	BaseRepository
	// Save 保存Task实例（创建或更新）
	Save(ctx context.Context, task *TaskInstance) error
	// GetByID 根据ID查询Task实例
	GetByID(ctx context.Context, id string) (*TaskInstance, error)
	// Delete 删除Task实例
	Delete(ctx context.Context, id string) error
}

// TaskRepository Task业务存储接口（对外导出）
// 组合了通用CRUD接口，并添加了业务特定的方法
type TaskRepository interface {
	// 继承通用CRUD接口
	TaskCRUDRepository

	// GetByWorkflowInstanceID 根据WorkflowInstance ID查询所有Task（业务接口）
	GetByWorkflowInstanceID(ctx context.Context, instanceID string) ([]*TaskInstance, error)
	// UpdateStatus 更新Task状态（业务接口）
	UpdateStatus(ctx context.Context, id string, status string) error
	// UpdateStatusWithError 更新Task状态和错误信息（业务接口）
	UpdateStatusWithError(ctx context.Context, id string, status string, errorMsg string) error
}

// TaskInstance Task实例结构（对外导出）
type TaskInstance struct {
	ID                   string                 // Task ID（系统自动生成的UUID）
	Name                 string                 // Task名称（唯一）
	WorkflowInstanceID   string                 // WorkflowInstance ID
	JobFuncID            string                 // Job函数ID
	JobFuncName          string                 // Job函数名称
	CompensationFuncID   string                 // 补偿函数ID（用于SAGA事务补偿）
	CompensationFuncName string                 // 补偿函数名称（用于SAGA事务补偿）
	Params               map[string]interface{} // 执行参数（JSON格式）
	Status               string                 // 状态（Pending/Running/Success/Failed/TimeoutFailed）
	TimeoutSeconds       int                    // 超时时间（秒）
	RetryCount           int                    // 重试次数
	StartTime            *time.Time             // 开始执行时间
	EndTime              *time.Time             // 结束时间
	ErrorMessage         string                 // 错误信息
	CreateTime           time.Time              // 创建时间
}
