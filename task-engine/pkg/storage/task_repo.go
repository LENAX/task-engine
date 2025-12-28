package storage

import (
	"context"
	"time"
)

// TaskRepository Task存储接口（对外导出）
type TaskRepository interface {
	// Save 保存Task实例
	Save(ctx context.Context, task *TaskInstance) error
	// GetByID 根据ID查询Task实例
	GetByID(ctx context.Context, id string) (*TaskInstance, error)
	// GetByWorkflowInstanceID 根据WorkflowInstance ID查询所有Task
	GetByWorkflowInstanceID(ctx context.Context, instanceID string) ([]*TaskInstance, error)
	// UpdateStatus 更新Task状态
	UpdateStatus(ctx context.Context, id string, status string) error
	// UpdateStatusWithError 更新Task状态和错误信息
	UpdateStatusWithError(ctx context.Context, id string, status string, errorMsg string) error
	// Delete 删除Task实例
	Delete(ctx context.Context, id string) error
}

// TaskInstance Task实例结构（对外导出）
type TaskInstance struct {
	ID                 string                 // Task ID（系统自动生成的UUID）
	Name               string                 // Task名称（唯一）
	WorkflowInstanceID string                 // WorkflowInstance ID
	JobFuncID          string                 // Job函数ID
	JobFuncName        string                 // Job函数名称
	Params             map[string]interface{} // 执行参数（JSON格式）
	Status             string                 // 状态（Pending/Running/Success/Failed/TimeoutFailed）
	TimeoutSeconds     int                    // 超时时间（秒）
	RetryCount         int                    // 重试次数
	StartTime          *time.Time             // 开始执行时间
	EndTime            *time.Time             // 结束时间
	ErrorMessage       string                 // 错误信息
	CreateTime         time.Time              // 创建时间
}
