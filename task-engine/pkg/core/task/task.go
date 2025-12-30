package task

import (
	"encoding/json"
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
	ID             string
	Name           string
	Description    string
	Params         map[string]any
	CreateTime     time.Time
	Status         string
	StatusHandlers map[string]string // 状态处理函数映射（status -> handlerID）
	JobFuncID      string            // Job函数ID（通过Registry获取函数实例）
	JobFuncName    string            // Job函数名称（用于快速查找和依赖构建）
	TimeoutSeconds int               // 超时时间（秒，默认30秒）
	RetryCount     int               // 重试次数（默认0次，即不重试）
	Dependencies   []string          // 依赖的前置Task名称列表
}

// NewTask 创建Task实例（对外导出）
// jobFuncName: 已注册的Job函数名称（用于从数据库加载的场景）
func NewTask(name, desc, jobFuncID string, params map[string]any, statusHandlers map[string]string) *Task {
	return &Task{
		ID:             uuid.NewString(),
		Name:           name,
		Description:    desc,
		Status:         TaskStatusPending,
		StatusHandlers: statusHandlers,
		CreateTime:     time.Now(),
		Params:         params,
		JobFuncID:      jobFuncID,
		TimeoutSeconds: 30, // 默认30秒
		RetryCount:     0,  // 默认0次，即不重试
		Dependencies:   make([]string, 0),
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
// func NewTaskWithFunction(ctx context.Context, name, desc string, jobFunc interface{}, funcName, funcDesc string, registry *FunctionRegistry) (*Task, error) {
// 	if registry == nil {
// 		return nil, fmt.Errorf("registry不能为空")
// 	}

// 	// 如果函数名称为空，自动生成
// 	if funcName == "" {
// 		funcName = generateFunctionName(jobFunc)
// 	}

// 	// 检查函数是否已注册（通过名称）
// 	funcID := registry.GetIDByName(funcName)
// 	if funcID == "" {
// 		// 函数未注册，自动注册
// 		var err error
// 		funcID, err = registry.Register(ctx, funcName, jobFunc, funcDesc)
// 		if err != nil {
// 			return nil, fmt.Errorf("自动注册函数失败: %w", err)
// 		}
// 	}

// 	return &Task{
// 		ID:             uuid.NewString(),
// 		Name:           name,
// 		Description:    desc,
// 		Status:         TaskStatusPending,
// 		CreateTime:     time.Now(),
// 		Params:         make(map[string]any),
// 		JobFuncID:      funcID,
// 		JobFuncName:    funcName,
// 		TimeoutSeconds: 30, // 默认30秒
// 		RetryCount:     0,  // 默认0次，即不重试
// 		Dependencies:   make([]string, 0),
// 	}, nil
// }

// GetJobFunction 从Registry获取Job函数（对外导出）
// 如果函数未注册，返回nil
// func (t *Task) GetJobFunction(registry *FunctionRegistry) JobFunctionType {
// 	if registry == nil || t.JobFuncID == "" {
// 		return nil
// 	}
// 	return registry.Get(t.JobFuncID)
// }

// GetID 获取Task的唯一标识（对外导出）
func (t *Task) GetID() string {
	return t.ID
}

// GetName 获取Task的名称（对外导出）
func (t *Task) GetName() string {
	return t.Name
}

// GetJobFuncName 获取Task绑定的Job函数名称（对外导出）
func (t *Task) GetJobFuncName() string {
	return t.JobFuncName
}

// GetParams 获取Task的执行参数（对外导出）
// 返回map[string]interface{}以兼容设计文档要求
func (t *Task) GetParams() map[string]interface{} {
	if t.Params == nil {
		return make(map[string]interface{})
	}
	// 将map[string]any转换为map[string]interface{}
	result := make(map[string]interface{})
	for k, v := range t.Params {
		result[k] = v
	}
	return result
}

// UpdateParams 运行时更新Task的执行参数（对外导出）
// 接受map[string]interface{}，内部转换为map[string]any
func (t *Task) UpdateParams(newParams map[string]any) error {
	if newParams == nil {
		return fmt.Errorf("参数不能为空")
	}
	if t.Params == nil {
		t.Params = make(map[string]any)
	}
	// 将map[string]interface{}转换为map[string]any
	for k, v := range newParams {
		// 将值转换为字符串
		var strValue string
		switch val := v.(type) {
		case string:
			strValue = val
		case nil:
			strValue = ""
		default:
			// 对于其他类型，使用JSON序列化
			jsonBytes, err := json.Marshal(val)
			if err != nil {
				return fmt.Errorf("参数 %s 序列化失败: %w", k, err)
			}
			strValue = string(jsonBytes)
		}
		t.Params[k] = strValue
	}
	return nil
}

// GetStatus 获取Task当前的执行状态（对外导出）
func (t *Task) GetStatus() string {
	return t.Status
}

// GetDependencies 获取Task的依赖列表（对外导出）
// 返回依赖的前置Task名称列表
func (t *Task) GetDependencies() []string {
	if t.Dependencies == nil {
		return make([]string, 0)
	}
	// 返回副本，避免外部修改
	result := make([]string, len(t.Dependencies))
	copy(result, t.Dependencies)
	return result
}
