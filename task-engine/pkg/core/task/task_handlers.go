package task

import (
	"context"
	"fmt"
	"log"
)

// TaskHandlerType Task Handler函数签名（对外导出）
// 参数通过TaskContext传递，提供类型安全的API访问Task信息
type TaskHandlerType func(ctx *TaskContext)

// ExecuteTaskHandler 执行Task的状态Handler（对外导出）
// registry: 函数注册中心
// task: Task实例
// status: 当前状态
// resultData: 任务执行结果数据（可选，用于Success状态）
// errorMsg: 错误信息（可选，用于Failed/Timeout状态）
func ExecuteTaskHandler(registry *FunctionRegistry, task *Task, status string, resultData interface{}, errorMsg string) error {
	if registry == nil {
		return fmt.Errorf("函数注册中心未配置")
	}

	if task == nil {
		return fmt.Errorf("Task实例为空")
	}

	// 检查是否有配置该状态的Handler
	if len(task.StatusHandlers) == 0 {
		return nil // 没有配置Handler，直接返回
	}

	// 获取该状态对应的Handler ID
	handlerID, exists := task.StatusHandlers[status]
	if !exists {
		return nil // 该状态没有配置Handler，直接返回
	}

	// 从registry获取Handler
	handler := registry.GetTaskHandler(handlerID)
	if handler == nil {
		// 尝试通过名称获取
		handler = registry.GetTaskHandlerByName(handlerID)
	}

	if handler == nil {
		return fmt.Errorf("Task Handler %s 未找到", handlerID)
	}

	// 创建TaskContext
	// 注意：这里需要创建一个基础的context，因为handler可能只需要访问Task信息
	ctx := context.Background()

	// 准备参数，包含结果数据或错误信息
	params := make(map[string]interface{})
	if task.Params != nil {
		// 复制原有参数
		for k, v := range task.Params {
			params[k] = v
		}
	}

	// 根据状态添加特定数据
	switch status {
	case TaskStatusSuccess:
		if resultData != nil {
			params["result"] = resultData
			params["_result_data"] = resultData
		}
	case TaskStatusFailed, TaskStatusTimeout:
		if errorMsg != "" {
			params["error"] = errorMsg
			params["_error_message"] = errorMsg
		}
	}

	// 添加状态信息
	params["_status"] = status
	params["_previous_status"] = task.Status

	taskCtx := NewTaskContext(
		ctx,
		task.ID,
		task.Name,
		"", // WorkflowID，如果需要在handler中使用，应该从外部传入
		"", // WorkflowInstanceID，如果需要在handler中使用，应该从外部传入
		params,
	)

	// 执行Handler（在goroutine中执行，避免阻塞）
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Task Handler执行panic: Task=%s, Status=%s, HandlerID=%s, Error=%v",
					task.ID, status, handlerID, r)
			}
		}()

		handler(taskCtx)
	}()

	return nil
}

// ExecuteTaskHandlerSync 同步执行Task的状态Handler（对外导出）
// 与ExecuteTaskHandler的区别是：同步执行，会等待handler完成
// 适用于需要确保handler执行完成后再继续的场景
func ExecuteTaskHandlerSync(registry *FunctionRegistry, task *Task, status string, resultData interface{}, errorMsg string) error {
	if registry == nil {
		return fmt.Errorf("函数注册中心未配置")
	}

	if task == nil {
		return fmt.Errorf("Task实例为空")
	}

	// 检查是否有配置该状态的Handler
	if len(task.StatusHandlers) == 0 {
		return nil
	}

	// 获取该状态对应的Handler ID
	handlerID, exists := task.StatusHandlers[status]
	if !exists {
		return nil
	}

	// 从registry获取Handler
	handler := registry.GetTaskHandler(handlerID)
	if handler == nil {
		handler = registry.GetTaskHandlerByName(handlerID)
	}

	if handler == nil {
		return fmt.Errorf("Task Handler %s 未找到", handlerID)
	}

	// 创建TaskContext
	ctx := context.Background()

	params := make(map[string]interface{})
	if task.Params != nil {
		for k, v := range task.Params {
			params[k] = v
		}
	}

	// 根据状态添加特定数据
	switch status {
	case TaskStatusSuccess:
		if resultData != nil {
			params["result"] = resultData
			params["_result_data"] = resultData
		}
	case TaskStatusFailed, TaskStatusTimeout:
		if errorMsg != "" {
			params["error"] = errorMsg
			params["_error_message"] = errorMsg
		}
	}

	params["_status"] = status
	params["_previous_status"] = task.Status

	taskCtx := NewTaskContext(
		ctx,
		task.ID,
		task.Name,
		"",
		"",
		params,
	)

	// 同步执行Handler
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Task Handler执行panic: Task=%s, Status=%s, HandlerID=%s, Error=%v",
				task.ID, status, handlerID, r)
		}
	}()

	handler(taskCtx)

	return nil
}

// ExecuteTaskHandlerWithContext 执行Task的状态Handler，并传入完整的上下文信息（对外导出）
// 与ExecuteTaskHandler的区别是：可以传入WorkflowID和WorkflowInstanceID
func ExecuteTaskHandlerWithContext(
	registry *FunctionRegistry,
	task *Task,
	status string,
	workflowID string,
	workflowInstanceID string,
	resultData interface{},
	errorMsg string,
) error {
	if registry == nil {
		return fmt.Errorf("函数注册中心未配置")
	}

	if task == nil {
		return fmt.Errorf("Task实例为空")
	}

	// 检查是否有配置该状态的Handler
	if len(task.StatusHandlers) == 0 {
		return nil
	}

	// 获取该状态对应的Handler ID
	handlerID, exists := task.StatusHandlers[status]
	if !exists {
		return nil
	}

	// 从registry获取Handler
	handler := registry.GetTaskHandler(handlerID)
	if handler == nil {
		handler = registry.GetTaskHandlerByName(handlerID)
	}

	if handler == nil {
		return fmt.Errorf("Task Handler %s 未找到", handlerID)
	}

	// 创建TaskContext
	ctx := context.Background()

	// 注入依赖到 context（如果 registry 支持依赖注入）
	if registry != nil {
		ctx = registry.WithDependencies(ctx)
	}

	params := make(map[string]interface{})
	if task.Params != nil {
		for k, v := range task.Params {
			params[k] = v
		}
	}

	// 根据状态添加特定数据
	switch status {
	case TaskStatusSuccess:
		if resultData != nil {
			params["result"] = resultData
			params["_result_data"] = resultData
		}
	case TaskStatusFailed, TaskStatusTimeout:
		if errorMsg != "" {
			params["error"] = errorMsg
			params["_error_message"] = errorMsg
		}
	}

	params["_status"] = status
	params["_previous_status"] = task.Status

	taskCtx := NewTaskContext(
		ctx,
		task.ID,
		task.Name,
		workflowID,
		workflowInstanceID,
		params,
	)

	// 执行Handler（异步执行）
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Task Handler执行panic: Task=%s, Status=%s, HandlerID=%s, Error=%v",
					task.ID, status, handlerID, r)
			}
		}()

		handler(taskCtx)
	}()

	return nil
}
