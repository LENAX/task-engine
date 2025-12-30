package builder

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/stevelan1995/task-engine/pkg/core/task"
)

// TaskBuilder Task构建器（对外导出）
type TaskBuilder struct {
	name           string
	description    string
	jobFuncName    string
	jobFuncID      string // Job函数ID（从registry获取）
	params         map[string]interface{}
	timeoutSeconds int
	retryCount     int
	dependencies   []string
	statusHandlers map[string]string // 状态处理函数映射（status -> handlerID）
	registry       *task.FunctionRegistry
}

// NewTaskBuilder 创建Task构建器（对外导出，必须包含registry）
// registry: 函数注册中心，用于验证JobFunction和TaskHandler是否存在（不能为nil）
func NewTaskBuilder(name, description string, registry *task.FunctionRegistry) *TaskBuilder {
	if registry == nil {
		panic("registry不能为nil，TaskBuilder必须使用registry")
	}
	return &TaskBuilder{
		name:           name,
		description:    description,
		timeoutSeconds: 30, // 默认30秒
		retryCount:     0,  // 默认0次，即不重试
		dependencies:   make([]string, 0),
		params:         make(map[string]interface{}),
		statusHandlers: make(map[string]string),
		registry:       registry,
	}
}

// WithJobFunction 绑定Task对应的业务执行函数及参数（链式构建，对外导出）
// fnName: 已注册的Job函数名称；params: 函数执行参数（键值对，需匹配函数参数类型）
// 注意：函数存在性校验延迟到Build()时进行
func (b *TaskBuilder) WithJobFunction(fnName string, params map[string]interface{}) *TaskBuilder {
	if fnName == "" {
		return b // 空名称，忽略
	}

	// 只保存函数名和参数，不进行校验
	b.jobFuncName = fnName
	b.jobFuncID = "" // 在Build()时再查找和验证

	if params != nil {
		b.params = params
	} else {
		b.params = make(map[string]interface{})
	}
	return b
}

// WithTimeout 设置Task的执行超时阈值（链式构建，对外导出）
// seconds: 超时时间（整数，单位：秒；默认30秒）
func (b *TaskBuilder) WithTimeout(seconds int) *TaskBuilder {
	if seconds < 0 {
		seconds = 30 // 无效值使用默认值
	}
	b.timeoutSeconds = seconds
	return b
}

// WithRetryCount 设置Task执行失败后的重试次数（链式构建，对外导出）
// count: 重试次数（整数，非负；默认0次，即不重试）
func (b *TaskBuilder) WithRetryCount(count int) *TaskBuilder {
	if count < 0 {
		count = 0 // 无效值使用默认值
	}
	b.retryCount = count
	return b
}

// WithDependency 为当前Task添加单个前置依赖Task（链式构建，对外导出）
// depTaskName: 依赖的前置Task名称（字符串，需为已构建的前置Task名称，且保证唯一）
func (b *TaskBuilder) WithDependency(depTaskName string) *TaskBuilder {
	if depTaskName == "" {
		return b // 忽略空字符串
	}
	// 检查是否已存在
	for _, dep := range b.dependencies {
		if dep == depTaskName {
			return b // 已存在，不重复添加
		}
	}
	b.dependencies = append(b.dependencies, depTaskName)
	return b
}

// WithDependencies 为当前Task批量添加前置依赖Task（链式构建，对外导出）
// depTaskNames: 依赖的前置Task名称列表（字符串切片，元素为已构建的前置Task名称，且均保证唯一）
func (b *TaskBuilder) WithDependencies(depTaskNames []string) *TaskBuilder {
	if len(depTaskNames) == 0 {
		return b
	}
	// 去重添加
	depMap := make(map[string]bool)
	for _, dep := range b.dependencies {
		depMap[dep] = true
	}
	for _, depName := range depTaskNames {
		if depName != "" && !depMap[depName] {
			b.dependencies = append(b.dependencies, depName)
			depMap[depName] = true
		}
	}
	return b
}

// WithTaskHandler 为Task添加状态处理函数（链式构建，对外导出）
// status: Task状态（如 task.TaskStatusSuccess, task.TaskStatusFailed 等）
// handlerName: 已注册的Task Handler名称或ID
// 如果TaskBuilder包含registry，会检查Handler是否存在
func (b *TaskBuilder) WithTaskHandler(status, handlerName string) *TaskBuilder {
	if status == "" || handlerName == "" {
		return b // 空值，忽略
	}

	// 初始化statusHandlers map
	if b.statusHandlers == nil {
		b.statusHandlers = make(map[string]string)
	}

	// 检查Handler是否存在
	// 先通过名称查找Handler ID
	handlerID := b.registry.GetTaskHandlerIDByName(handlerName)
	if handlerID == "" {
		// 如果通过名称找不到，尝试直接使用handlerName作为ID检查
		if !b.registry.TaskHandlerExists(handlerName) {
			// Handler不存在，但先保存名称，在Build()时统一报错
			b.statusHandlers[status] = handlerName
			return b
		}
		// 如果handlerName是ID，直接使用
		handlerID = handlerName
	}

	// 验证Handler确实存在
	if !b.registry.TaskHandlerExists(handlerID) {
		// Handler不存在，但先保存名称，在Build()时统一报错
		b.statusHandlers[status] = handlerName
		return b
	}

	// Handler存在，保存ID
	b.statusHandlers[status] = handlerID

	return b
}

// Build 完成Task构建（对外导出）
// 自动生成Task UUID作为唯一标识，校验Task名称唯一性
// 会验证所有引用的JobFunction和TaskHandler是否存在
func (b *TaskBuilder) Build() (*task.Task, error) {
	// 校验registry
	if b.registry == nil {
		return nil, fmt.Errorf("registry不能为nil，TaskBuilder必须使用registry")
	}

	// 校验名称
	if b.name == "" {
		return nil, fmt.Errorf("Task名称不能为空")
	}

	// 校验Job函数名称
	if b.jobFuncName == "" {
		return nil, fmt.Errorf("Job函数名称不能为空")
	}

	// 验证所有引用是否存在
	{
		// 验证JobFunction是否存在（延迟校验）
		// 尝试通过名称查找函数ID
		funcID := b.registry.GetIDByName(b.jobFuncName)
		if funcID == "" {
			// 如果通过名称找不到，尝试直接使用jobFuncName作为ID检查
			if !b.registry.Exists(b.jobFuncName) {
				return nil, fmt.Errorf("Job函数 %s 未在registry中注册", b.jobFuncName)
			}
			funcID = b.jobFuncName
		} else {
			// 验证函数确实存在
			if !b.registry.Exists(funcID) {
				return nil, fmt.Errorf("Job函数 %s (ID: %s) 未在registry中注册", b.jobFuncName, funcID)
			}
		}
		b.jobFuncID = funcID

		// 验证所有TaskHandler是否存在
		if len(b.statusHandlers) > 0 {
			for status, handlerRef := range b.statusHandlers {
				// 先通过名称查找Handler ID
				handlerID := b.registry.GetTaskHandlerIDByName(handlerRef)
				if handlerID == "" {
					// 如果通过名称找不到，尝试直接使用handlerRef作为ID检查
					if !b.registry.TaskHandlerExists(handlerRef) {
						return nil, fmt.Errorf("Task Handler %s (状态: %s) 未在registry中注册", handlerRef, status)
					}
					handlerID = handlerRef
				} else {
					// 验证Handler确实存在
					if !b.registry.TaskHandlerExists(handlerID) {
						return nil, fmt.Errorf("Task Handler %s (ID: %s, 状态: %s) 未在registry中注册", handlerRef, handlerID, status)
					}
				}
				// 更新为正确的Handler ID
				b.statusHandlers[status] = handlerID
			}
		}
	}

	// 创建Task实例
	t := &task.Task{
		ID:             uuid.NewString(),
		Name:           b.name,
		Description:    b.description,
		Status:         task.TaskStatusPending,
		JobFuncName:    b.jobFuncName,
		JobFuncID:      b.jobFuncID,
		TimeoutSeconds: b.timeoutSeconds,
		RetryCount:     b.retryCount,
		Dependencies:   make([]string, len(b.dependencies)),
		Params:         make(map[string]any),
	}

	// 设置StatusHandlers
	if len(b.statusHandlers) > 0 {
		t.StatusHandlers = make(map[string]string)
		for status, handlerID := range b.statusHandlers {
			t.StatusHandlers[status] = handlerID
		}
	}

	// 复制依赖列表
	copy(t.Dependencies, b.dependencies)

	// 转换参数：map[string]interface{} -> map[string]string
	for k, v := range b.params {
		var strValue string
		switch val := v.(type) {
		case string:
			strValue = val
		case nil:
			strValue = ""
		default:
			// 对于其他类型，使用fmt.Sprintf转换
			strValue = fmt.Sprintf("%v", val)
		}
		t.Params[k] = strValue
	}

	return t, nil
}
