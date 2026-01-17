package task

import "context"

// context key类型，用于类型安全的context.Value访问
type contextKey string

const (
	// TaskParamsKey Task参数在context中的key
	TaskParamsKey contextKey = "task.params"
	// TaskIDKey Task ID在context中的key
	TaskIDKey contextKey = "task.id"
	// TaskNameKey Task名称在context中的key
	TaskNameKey contextKey = "task.name"
	// WorkflowInstanceIDKey WorkflowInstance ID在context中的key
	WorkflowInstanceIDKey contextKey = "workflow.instance.id"
	// WorkflowIDKey Workflow ID在context中的key
	WorkflowIDKey contextKey = "workflow.id"
)

// WithTaskParams 将Task参数添加到context中（对外导出）
func WithTaskParams(ctx context.Context, params map[string]interface{}) context.Context {
	return context.WithValue(ctx, TaskParamsKey, params)
}

// GetTaskParams 从context中获取Task参数（对外导出）
func GetTaskParams(ctx context.Context) map[string]interface{} {
	if params, ok := ctx.Value(TaskParamsKey).(map[string]interface{}); ok {
		return params
	}
	return nil
}

// WithTaskID 将Task ID添加到context中（对外导出）
func WithTaskID(ctx context.Context, taskID string) context.Context {
	return context.WithValue(ctx, TaskIDKey, taskID)
}

// GetTaskID 从context中获取Task ID（对外导出）
func GetTaskID(ctx context.Context) string {
	if id, ok := ctx.Value(TaskIDKey).(string); ok {
		return id
	}
	return ""
}

// WithTaskName 将Task名称添加到context中（对外导出）
func WithTaskName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, TaskNameKey, name)
}

// GetTaskName 从context中获取Task名称（对外导出）
func GetTaskName(ctx context.Context) string {
	if name, ok := ctx.Value(TaskNameKey).(string); ok {
		return name
	}
	return ""
}

// WithWorkflowInstanceID 将WorkflowInstance ID添加到context中（对外导出）
func WithWorkflowInstanceID(ctx context.Context, instanceID string) context.Context {
	return context.WithValue(ctx, WorkflowInstanceIDKey, instanceID)
}

// GetWorkflowInstanceID 从context中获取WorkflowInstance ID（对外导出）
func GetWorkflowInstanceID(ctx context.Context) string {
	if id, ok := ctx.Value(WorkflowInstanceIDKey).(string); ok {
		return id
	}
	return ""
}

// WithWorkflowID 将Workflow ID添加到context中（对外导出）
func WithWorkflowID(ctx context.Context, workflowID string) context.Context {
	return context.WithValue(ctx, WorkflowIDKey, workflowID)
}

// GetWorkflowID 从context中获取Workflow ID（对外导出）
func GetWorkflowID(ctx context.Context) string {
	if id, ok := ctx.Value(WorkflowIDKey).(string); ok {
		return id
	}
	return ""
}
