package task

import (
	"context"
	"fmt"
	"reflect"
	"strings"
)

// TaskContext Task执行上下文，提供类型安全的API访问Task信息（对外导出）
type TaskContext struct {
	ctx                context.Context        // 底层context，用于超时、取消等
	TaskID             string                 // Task ID
	TaskName           string                 // Task名称
	Params             map[string]interface{} // Task参数（原始类型，不强制转换为string）
	WorkflowID         string                 // Workflow ID
	WorkflowInstanceID string                 // WorkflowInstance ID

	// 引擎组件引用（用于 Job Function 访问引擎能力）
	registry        FunctionRegistry // 函数注册中心
	instanceManager interface{}      // WorkflowInstanceManager 接口（避免循环依赖，使用 interface{}）
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

// SetRegistry 设置函数注册中心（对外导出）
func (tc *TaskContext) SetRegistry(registry FunctionRegistry) {
	tc.registry = registry
}

// GetRegistry 获取函数注册中心（对外导出）
func (tc *TaskContext) GetRegistry() FunctionRegistry {
	return tc.registry
}

// SetInstanceManager 设置 InstanceManager（对外导出）
// manager 需要实现 AtomicAddSubTasks 等方法
func (tc *TaskContext) SetInstanceManager(manager interface{}) {
	tc.instanceManager = manager
}

// GetInstanceManager 获取 InstanceManager（对外导出）
// 返回值需要类型断言为具体接口使用
// 示例:
//
//	type ManagerInterface interface {
//	    AtomicAddSubTasks(subTasks []types.Task, parentTaskID string) error
//	}
//	manager, ok := ctx.GetInstanceManager().(ManagerInterface)
func (tc *TaskContext) GetInstanceManager() interface{} {
	return tc.instanceManager
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

// ========== 上游任务结果 API ==========

// upstreamCachePrefix 上游任务结果缓存前缀
const upstreamCachePrefix = "_cached_"

// GetUpstreamResult 获取指定上游任务的结果
// taskID: 上游任务 ID
// 返回: 上游任务结果 map，如果不存在返回 nil
//
// 示例:
//
//	result := tc.GetUpstreamResult("FetchStockList")
//	if result != nil {
//	    stockCodes := result["stock_codes"]
//	}
func (tc *TaskContext) GetUpstreamResult(taskID string) map[string]interface{} {
	cacheKey := upstreamCachePrefix + taskID
	if val := tc.GetParam(cacheKey); val != nil {
		if result, ok := val.(map[string]interface{}); ok {
			return result
		}
	}
	return nil
}

// GetAllUpstreamResults 获取所有上游任务的结果
// 返回: map[taskID]result
//
// 示例:
//
//	results := tc.GetAllUpstreamResults()
//	for taskID, result := range results {
//	    fmt.Printf("上游任务 %s 的结果: %v\n", taskID, result)
//	}
func (tc *TaskContext) GetAllUpstreamResults() map[string]map[string]interface{} {
	results := make(map[string]map[string]interface{})
	if tc.Params == nil {
		return results
	}
	for key, val := range tc.Params {
		if strings.HasPrefix(key, upstreamCachePrefix) {
			taskID := strings.TrimPrefix(key, upstreamCachePrefix)
			if result, ok := val.(map[string]interface{}); ok {
				results[taskID] = result
			}
		}
	}
	return results
}

// GetUpstreamValue 从上游任务结果中获取指定字段
// taskID: 上游任务 ID
// field: 字段名
// 返回: 字段值，如果不存在返回 nil
//
// 示例:
//
//	stockCodes := tc.GetUpstreamValue("FetchStockList", "stock_codes")
func (tc *TaskContext) GetUpstreamValue(taskID, field string) interface{} {
	if result := tc.GetUpstreamResult(taskID); result != nil {
		return result[field]
	}
	return nil
}

// GetUpstreamString 从上游任务结果中获取字符串字段
// taskID: 上游任务 ID
// field: 字段名
// 返回: 字符串值，如果不存在或类型不匹配返回空字符串
func (tc *TaskContext) GetUpstreamString(taskID, field string) string {
	val := tc.GetUpstreamValue(taskID, field)
	if val == nil {
		return ""
	}
	if str, ok := val.(string); ok {
		return str
	}
	return fmt.Sprintf("%v", val)
}

// GetUpstreamInt 从上游任务结果中获取整数字段
// taskID: 上游任务 ID
// field: 字段名
// 返回: 整数值和错误
func (tc *TaskContext) GetUpstreamInt(taskID, field string) (int, error) {
	val := tc.GetUpstreamValue(taskID, field)
	if val == nil {
		return 0, fmt.Errorf("上游任务 %s 的字段 %s 不存在", taskID, field)
	}
	switch v := val.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("上游任务 %s 的字段 %s 类型不是整数，当前类型: %T", taskID, field, val)
	}
}

// GetUpstreamStringSlice 从上游任务结果中获取字符串切片字段
// taskID: 上游任务 ID
// field: 字段名
// 返回: 字符串切片，如果不存在返回空切片
func (tc *TaskContext) GetUpstreamStringSlice(taskID, field string) []string {
	val := tc.GetUpstreamValue(taskID, field)
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case []string:
		return v
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}

// ========== 子任务结果 API ==========

// SubTaskResult 子任务结果结构
type SubTaskResult struct {
	TaskID   string                 // 子任务 ID
	TaskName string                 // 子任务名称
	Status   string                 // 执行状态: "Success" | "Failed"
	Result   map[string]interface{} // 子任务返回结果
	Error    string                 // 错误信息（仅失败时有值）
}

// IsSuccess 判断子任务是否成功
func (r *SubTaskResult) IsSuccess() bool {
	return r.Status == "Success"
}

// GetResultValue 从子任务结果中获取指定字段
func (r *SubTaskResult) GetResultValue(field string) interface{} {
	if r.Result == nil {
		return nil
	}
	return r.Result[field]
}

// GetResultMap 从子任务结果中获取 map 类型字段
func (r *SubTaskResult) GetResultMap(field string) map[string]interface{} {
	val := r.GetResultValue(field)
	if val == nil {
		return nil
	}
	if m, ok := val.(map[string]interface{}); ok {
		return m
	}
	return nil
}

// GetSubTaskResults 获取所有子任务的结果（从模板任务的聚合结果中提取）
// 返回: 子任务结果列表
//
// 说明: 当模板任务的子任务全部完成后，引擎会将子任务结果聚合到模板任务的结果中，
// 结构为: { "subtask_results": [...], "subtask_count": N, "all_subtasks_succeeded": bool }
//
// 示例:
//
//	results := tc.GetSubTaskResults()
//	for _, r := range results {
//	    if r.IsSuccess() {
//	        fmt.Printf("子任务 %s 成功: %v\n", r.TaskID, r.Result)
//	    }
//	}
func (tc *TaskContext) GetSubTaskResults() []SubTaskResult {
	var results []SubTaskResult
	seen := make(map[string]bool) // 用于去重（按 taskID）

	if tc.Params == nil {
		return results
	}

	for key, val := range tc.Params {
		if !strings.HasPrefix(key, upstreamCachePrefix) {
			continue
		}
		resultMap, ok := val.(map[string]interface{})
		if !ok {
			continue
		}

		subtaskResultsRaw, ok := resultMap["subtask_results"]
		if !ok {
			continue
		}

		// 支持 []map[string]interface{} 和 []interface{} 两种类型
		switch subtaskResults := subtaskResultsRaw.(type) {
		case []map[string]interface{}:
			for _, sub := range subtaskResults {
				r := parseSubTaskResult(sub)
				if !seen[r.TaskID] {
					seen[r.TaskID] = true
					results = append(results, r)
				}
			}
		case []interface{}:
			for _, item := range subtaskResults {
				if sub, ok := item.(map[string]interface{}); ok {
					r := parseSubTaskResult(sub)
					if !seen[r.TaskID] {
						seen[r.TaskID] = true
						results = append(results, r)
					}
				}
			}
		}
	}
	return results
}

// parseSubTaskResult 解析单个子任务结果
func parseSubTaskResult(sub map[string]interface{}) SubTaskResult {
	r := SubTaskResult{}
	if id, ok := sub["task_id"].(string); ok {
		r.TaskID = id
	}
	if name, ok := sub["task_name"].(string); ok {
		r.TaskName = name
	}
	if status, ok := sub["status"].(string); ok {
		r.Status = status
	}
	if errStr, ok := sub["error"].(string); ok {
		r.Error = errStr
	}
	if result, ok := sub["result"].(map[string]interface{}); ok {
		r.Result = result
	}
	return r
}

// GetSuccessfulSubTaskResults 获取所有成功的子任务结果
//
// 示例:
//
//	successResults := tc.GetSuccessfulSubTaskResults()
//	for _, r := range successResults {
//	    data := r.Result["data"]
//	}
func (tc *TaskContext) GetSuccessfulSubTaskResults() []SubTaskResult {
	all := tc.GetSubTaskResults()
	successful := make([]SubTaskResult, 0, len(all))
	for _, r := range all {
		if r.IsSuccess() {
			successful = append(successful, r)
		}
	}
	return successful
}

// GetFailedSubTaskResults 获取所有失败的子任务结果
func (tc *TaskContext) GetFailedSubTaskResults() []SubTaskResult {
	all := tc.GetSubTaskResults()
	failed := make([]SubTaskResult, 0)
	for _, r := range all {
		if !r.IsSuccess() {
			failed = append(failed, r)
		}
	}
	return failed
}

// AllSubTasksSucceeded 检查是否所有子任务都成功
func (tc *TaskContext) AllSubTasksSucceeded() bool {
	if tc.Params == nil {
		return false
	}
	for key, val := range tc.Params {
		if !strings.HasPrefix(key, upstreamCachePrefix) {
			continue
		}
		resultMap, ok := val.(map[string]interface{})
		if !ok {
			continue
		}
		if allSucceeded, ok := resultMap["all_subtasks_succeeded"].(bool); ok {
			return allSucceeded
		}
	}
	return false
}

// GetSubTaskCount 获取子任务总数
func (tc *TaskContext) GetSubTaskCount() int {
	if tc.Params == nil {
		return 0
	}
	for key, val := range tc.Params {
		if !strings.HasPrefix(key, upstreamCachePrefix) {
			continue
		}
		resultMap, ok := val.(map[string]interface{})
		if !ok {
			continue
		}
		if count, ok := resultMap["subtask_count"].(int); ok {
			return count
		}
		// 兼容 float64（JSON 反序列化）
		if count, ok := resultMap["subtask_count"].(float64); ok {
			return int(count)
		}
	}
	return 0
}

// ExtractFromSubTasks 从成功的子任务结果中提取指定字段
// field: 要提取的字段名（如 "api_metadata"）
// 返回: 提取的字段值列表
//
// 示例:
//
//	apiMetadataList := tc.ExtractFromSubTasks("api_metadata")
//	for _, metadata := range apiMetadataList {
//	    // 处理每个 api_metadata
//	}
func (tc *TaskContext) ExtractFromSubTasks(field string) []interface{} {
	var values []interface{}
	for _, r := range tc.GetSuccessfulSubTaskResults() {
		if val := r.GetResultValue(field); val != nil {
			values = append(values, val)
		}
	}
	return values
}

// ExtractMapsFromSubTasks 从成功的子任务结果中提取 map 类型字段
// field: 要提取的字段名（如 "api_metadata"）
// 返回: 提取的 map 列表
//
// 示例:
//
//	apiMetadataMaps := tc.ExtractMapsFromSubTasks("api_metadata")
//	for _, m := range apiMetadataMaps {
//	    name := m["name"].(string)
//	}
func (tc *TaskContext) ExtractMapsFromSubTasks(field string) []map[string]interface{} {
	var maps []map[string]interface{}
	for _, val := range tc.ExtractFromSubTasks(field) {
		if m, ok := val.(map[string]interface{}); ok {
			maps = append(maps, m)
		}
	}
	return maps
}

// ExtractStringsFromSubTasks 从成功的子任务结果中提取字符串类型字段
// field: 要提取的字段名
// 返回: 提取的字符串列表
func (tc *TaskContext) ExtractStringsFromSubTasks(field string) []string {
	var strs []string
	for _, val := range tc.ExtractFromSubTasks(field) {
		if s, ok := val.(string); ok {
			strs = append(strs, s)
		}
	}
	return strs
}
