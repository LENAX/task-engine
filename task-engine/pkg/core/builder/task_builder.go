package builder

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/stevelan1995/task-engine/pkg/core/task"
)

// TaskBuilder Task构建器（对外导出）
type TaskBuilder struct {
	name          string
	description   string
	jobFuncName   string
	params        map[string]interface{}
	timeoutSeconds int
	retryCount     int
	dependencies   []string
}

// NewTaskBuilder 创建Task构建器（对外导出）
func NewTaskBuilder(name, description string) *TaskBuilder {
	return &TaskBuilder{
		name:           name,
		description:    description,
		timeoutSeconds: 30, // 默认30秒
		retryCount:     0,   // 默认0次，即不重试
		dependencies:   make([]string, 0),
		params:         make(map[string]interface{}),
	}
}

// WithJobFunction 绑定Task对应的业务执行函数及参数（链式构建，对外导出）
// fnName: 已注册的Job函数名称；params: 函数执行参数（键值对，需匹配函数参数类型）
func (b *TaskBuilder) WithJobFunction(fnName string, params map[string]interface{}) *TaskBuilder {
	b.jobFuncName = fnName
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
	if depTaskNames == nil || len(depTaskNames) == 0 {
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

// Build 完成Task构建（对外导出）
// 自动生成Task UUID作为唯一标识，校验Task名称唯一性
// 注意：函数未注册的校验在WorkflowBuilder.Build()时进行
func (b *TaskBuilder) Build() (*task.Task, error) {
	// 校验名称
	if b.name == "" {
		return nil, fmt.Errorf("Task名称不能为空")
	}

	// 校验Job函数名称
	if b.jobFuncName == "" {
		return nil, fmt.Errorf("Job函数名称不能为空")
	}

	// 创建Task实例
	t := &task.Task{
		ID:            uuid.NewString(),
		Name:          b.name,
		Description:   b.description,
		Status:        task.TaskStatusPending,
		JobFuncName:   b.jobFuncName,
		TimeoutSeconds: b.timeoutSeconds,
		RetryCount:     b.retryCount,
		Dependencies:   make([]string, len(b.dependencies)),
		Params:         make(map[string]string),
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

