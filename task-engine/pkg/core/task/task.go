package task

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	TaskStatusEnabled   = "ENABLED"
	TaskStatusDisabled  = "DISABLED"
	TaskStatusPending   = "PENDING"
	TaskStatusRunning   = "RUNNING"
	TaskStatusSuccess   = "SUCCESS"
	TaskStatusFailed    = "FAILED"
	TaskStatusTimeout   = "TIMEOUT"
	TaskStatusCancelled = "CANCELLED"
	TaskStatusPaused    = "PAUSED"
)

type Task struct {
	ID          string
	Name        string
	Description string
	Params      map[string]string
	CreateTime  time.Time
	Status      string
	JobFuncID   string // Job函数ID（通过Registry获取函数实例）
}

// NewTask 创建Task实例（对外导出）
// jobFuncName: 已注册的Job函数名称（用于从数据库加载的场景）
func NewTask(name, desc, jobFuncID string) *Task {
	return &Task{
		ID:          uuid.NewString(),
		Name:        name,
		Description: desc,
		Status:      TaskStatusEnabled,
		CreateTime:  time.Now(),
		Params:      make(map[string]string),
		JobFuncID:   jobFuncID,
	}
}

// NewTaskWithFunction 创建Task实例并自动注册函数（对外导出）
// name: Task名称
// desc: Task描述
// jobFunc: 用户自定义函数，首个参数必须是context.Context
// funcName: 函数名称（可选，为空则自动生成）
// funcDesc: 函数描述（可选）
// registry: 函数注册中心
// 返回: Task实例和错误
func NewTaskWithFunction(ctx context.Context, name, desc string, jobFunc interface{}, funcName, funcDesc string, registry *JobFunctionRegistry) (*Task, error) {
	if registry == nil {
		return nil, fmt.Errorf("registry不能为空")
	}

	// 自动注册函数
	funcID, err := registry.Register(ctx, funcName, jobFunc, funcDesc)
	if err != nil {
		return nil, fmt.Errorf("注册函数失败: %w", err)
	}

	return &Task{
		ID:          uuid.NewString(),
		Name:        name,
		Description: desc,
		Status:      TaskStatusEnabled,
		CreateTime:  time.Now(),
		Params:      make(map[string]string),
		JobFuncID:   funcID,
	}, nil
}

// GetJobFunction 从Registry获取Job函数（对外导出）
// 如果函数未注册，返回nil
func (t *Task) GetJobFunction(registry *JobFunctionRegistry) JobFunctionType {
	if registry == nil || t.JobFuncID == "" {
		return nil
	}
	return registry.Get(t.JobFuncID)
}
