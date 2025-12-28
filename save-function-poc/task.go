package main

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
)

// Task 任务定义
type Task struct {
	ID          string            // 任务ID
	Name        string            // 任务名称
	Description string            // 任务描述
	FuncID      string            // 关联的函数ID
	Params      map[string]string // 任务参数
	CreateTime  time.Time         // 创建时间
}

// NewTask 创建新任务（通过函数ID）
func NewTask(name, description, funcID string) *Task {
	return &Task{
		ID:          uuid.NewString(),
		Name:        name,
		Description: description,
		FuncID:      funcID,
		Params:      make(map[string]string),
		CreateTime:  time.Now(),
	}
}

// NewTaskWithFunction 通过函数引用创建任务（自动注册函数）
// registry: 函数注册中心
// name: 任务名称
// description: 任务描述
// fn: 函数实例
// funcName: 函数名称（如果为空，使用函数类型名）
// funcDesc: 函数描述
func NewTaskWithFunction(ctx context.Context, registry *FunctionRegistry, name, description string, fn interface{}, funcName, funcDesc string) (*Task, error) {
	if registry == nil {
		return nil, fmt.Errorf("registry不能为空")
	}

	// 如果函数名称为空，尝试从函数类型获取
	if funcName == "" {
		funcName = getFunctionName(fn)
	}

	// 检查函数是否已注册（通过名称）
	funcID := registry.GetIDByName(funcName)
	if funcID == "" {
		// 函数未注册，自动注册
		var err error
		funcID, err = registry.Register(ctx, funcName, fn, funcDesc)
		if err != nil {
			return nil, fmt.Errorf("自动注册函数失败: %w", err)
		}
	}

	return &Task{
		ID:          uuid.NewString(),
		Name:        name,
		Description: description,
		FuncID:      funcID,
		Params:      make(map[string]string),
		CreateTime:  time.Now(),
	}, nil
}

// getFunctionName 从函数类型获取函数名称（简化版）
func getFunctionName(fn interface{}) string {
	fnType := reflect.TypeOf(fn)
	if fnType.Kind() != reflect.Func {
		return "unknown"
	}
	// 使用函数类型的字符串表示（简化版，实际可以使用更复杂的逻辑）
	return fmt.Sprintf("func_%p", fn)
}

// JobFunctionState 表示Job函数的执行状态和可选结果或错误
type JobFunctionState struct {
	Status string      // "Success" or "Failed"
	Data   interface{} // optional: any return value (on success)
	Error  error       // optional: error info (on failure)
}

// TaskFunction 任务函数类型，统一签名，返回channel用于异步获取执行状态
type TaskFunction func(ctx context.Context, params map[string]string) <-chan JobFunctionState
