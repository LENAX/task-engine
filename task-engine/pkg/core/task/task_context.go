package task

import (
	"context"
	"fmt"
	"reflect"
)

// TaskContext Task执行上下文，提供类型安全的API访问Task信息（对外导出）
type TaskContext struct {
	ctx                context.Context        // 底层context，用于超时、取消等
	TaskID             string                 // Task ID
	TaskName           string                 // Task名称
	Params             map[string]interface{} // Task参数（原始类型，不强制转换为string）
	WorkflowID         string                 // Workflow ID
	WorkflowInstanceID string                 // WorkflowInstance ID
}

// NewTaskContext 创建TaskContext（对外导出）
func NewTaskContext(ctx context.Context, taskID, taskName, workflowID, workflowInstanceID string, params map[string]interface{}) *TaskContext {
	return &TaskContext{
		ctx:                ctx,
		TaskID:             taskID,
		TaskName:           taskName,
		Params:             params,
		WorkflowID:         workflowID,
		WorkflowInstanceID: workflowInstanceID,
	}
}

// Context 返回底层context.Context（对外导出）
// 用于超时、取消等标准context操作
func (tc *TaskContext) Context() context.Context {
	return tc.ctx
}

// GetParam 获取参数值（对外导出）
// key: 参数名
// 返回: 参数值，如果不存在返回nil
func (tc *TaskContext) GetParam(key string) interface{} {
	if tc.Params == nil {
		return nil
	}
	return tc.Params[key]
}

// GetParamString 获取字符串参数（对外导出）
func (tc *TaskContext) GetParamString(key string) string {
	val := tc.GetParam(key)
	if val == nil {
		return ""
	}
	if str, ok := val.(string); ok {
		return str
	}
	return fmt.Sprintf("%v", val)
}

// GetParamInt 获取整数参数（对外导出）
func (tc *TaskContext) GetParamInt(key string) (int, error) {
	val := tc.GetParam(key)
	if val == nil {
		return 0, fmt.Errorf("参数 %s 不存在", key)
	}

	switch v := val.(type) {
	case int:
		return v, nil
	case int8:
		return int(v), nil
	case int16:
		return int(v), nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	case string:
		var i int
		_, err := fmt.Sscanf(v, "%d", &i)
		return i, err
	default:
		return 0, fmt.Errorf("参数 %s 类型不是整数，当前类型: %T", key, val)
	}
}

// GetParamBool 获取布尔参数（对外导出）
func (tc *TaskContext) GetParamBool(key string) (bool, error) {
	val := tc.GetParam(key)
	if val == nil {
		return false, fmt.Errorf("参数 %s 不存在", key)
	}

	switch v := val.(type) {
	case bool:
		return v, nil
	case string:
		return v == "true" || v == "1" || v == "yes", nil
	default:
		return false, fmt.Errorf("参数 %s 类型不是布尔值，当前类型: %T", key, val)
	}
}

// GetParamFloat 获取浮点数参数（对外导出）
func (tc *TaskContext) GetParamFloat(key string) (float64, error) {
	val := tc.GetParam(key)
	if val == nil {
		return 0, fmt.Errorf("参数 %s 不存在", key)
	}

	switch v := val.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case string:
		var f float64
		_, err := fmt.Sscanf(v, "%f", &f)
		return f, err
	default:
		return 0, fmt.Errorf("参数 %s 类型不是浮点数，当前类型: %T", key, val)
	}
}

// MustGetParam 获取参数值，如果不存在则panic（对外导出）
// 用于确保参数存在的场景
func (tc *TaskContext) MustGetParam(key string) interface{} {
	val := tc.GetParam(key)
	if val == nil {
		panic(fmt.Sprintf("必需的参数 %s 不存在", key))
	}
	return val
}

// HasParam 检查参数是否存在（对外导出）
func (tc *TaskContext) HasParam(key string) bool {
	if tc.Params == nil {
		return false
	}
	_, exists := tc.Params[key]
	return exists
}

// Done 返回一个channel，当context被取消时该channel会被关闭（对外导出）
func (tc *TaskContext) Done() <-chan struct{} {
	return tc.ctx.Done()
}

// Err 返回context的错误（对外导出）
func (tc *TaskContext) Err() error {
	return tc.ctx.Err()
}

// GetDependency 从 context 中获取依赖（泛型函数，类型安全）（对外导出）
// 如果依赖未找到，返回零值和 false
// 示例: repo, ok := task.GetDependency[UserRepository](taskCtx.Context())
func GetDependencyFromContext[T any](ctx context.Context) (T, bool) {
	return GetDependency[T](ctx)
}

// GetDependency 通过字符串key获取依赖（对外导出）
// key: 依赖的字符串标识（如 "ExampleService"）
// 返回: 依赖实例和是否存在
// 示例: service, ok := ctx.GetDependency("ExampleService")
func (tc *TaskContext) GetDependency(key string) (interface{}, bool) {
	return GetDependencyByKey(tc.ctx, key)
}

// GetDependencyTyped 通过字符串key获取依赖并转换为指定类型（对外导出）
// key: 依赖的字符串标识
// 返回: 依赖实例（转换为指定类型）和是否存在
// 示例: service, ok := ctx.GetDependencyTyped[*ExampleService]("ExampleService")
func GetDependencyTyped[T any](ctx context.Context, key string) (T, bool) {
	var zero T
	dep, ok := GetDependencyByKey(ctx, key)
	if !ok {
		return zero, false
	}

	// 类型断言
	if typedDep, ok := dep.(T); ok {
		return typedDep, true
	}

	// 如果直接类型断言失败，尝试通过反射转换
	// 使用 reflect.TypeOf((*T)(nil)).Elem() 来获取类型信息，这样可以正确处理接口类型
	var typeT T
	targetType := reflect.TypeOf(&typeT).Elem()
	if targetType == nil {
		return zero, false
	}

	depValue := reflect.ValueOf(dep)
	if depValue.IsValid() && depValue.Type().AssignableTo(targetType) {
		return depValue.Interface().(T), true
	}

	return zero, false
}

// GetDependencyTypedFromContext 通过字符串key获取依赖并转换为指定类型（TaskContext辅助方法）
// key: 依赖的字符串标识
// 返回: 依赖实例（转换为指定类型）和是否存在
// 示例: service, ok := task.GetDependencyTyped[*ExampleService](ctx.Context(), "ExampleService")
func (tc *TaskContext) GetDependencyTypedFromContext(key string) (interface{}, bool) {
	return GetDependencyByKey(tc.ctx, key)
}
