package task

import (
	"fmt"
	"log"
	"time"

	"github.com/stevelan1995/task-engine/pkg/core/workflow"
)

// DefaultLogSuccess 默认成功日志Handler（对外导出）
// 记录任务成功执行的日志
func DefaultLogSuccess(ctx *TaskContext) {
	taskID := ctx.TaskID
	taskName := ctx.TaskName
	resultData := ctx.GetParam("_result_data")

	log.Printf("✅ [任务成功] TaskID=%s, TaskName=%s, 结果=%v", taskID, taskName, resultData)
}

// DefaultLogError 默认错误日志Handler（对外导出）
// 记录任务失败的日志
func DefaultLogError(ctx *TaskContext) {
	taskID := ctx.TaskID
	taskName := ctx.TaskName
	errorMsg := ctx.GetParamString("_error_message")

	log.Printf("❌ [任务失败] TaskID=%s, TaskName=%s, 错误=%s", taskID, taskName, errorMsg)
}

// DefaultSaveResult 默认保存结果Handler（对外导出）
// 将任务结果保存到Repository（需依赖注入DataRepository）
// 配置参数：repository_key (string, 默认: "DataRepository")
func DefaultSaveResult(ctx *TaskContext) {
	// 获取结果数据
	resultData := ctx.GetParam("_result_data")
	if resultData == nil {
		log.Printf("⚠️ [DefaultSaveResult] TaskID=%s, 未找到结果数据", ctx.TaskID)
		return
	}

	// 获取Repository（通过依赖注入）
	repoKey := ctx.GetParamString("repository_key")
	if repoKey == "" {
		repoKey = "DataRepository"
	}

	// 使用GetDependencyTyped获取类型安全的依赖
	type SaveRepository interface {
		Save(data map[string]interface{}) error
	}

	repo, ok := GetDependencyTyped[SaveRepository](ctx.Context(), repoKey)
	if !ok {
		log.Printf("⚠️ [DefaultSaveResult] TaskID=%s, 未找到Repository依赖 (key=%s)", ctx.TaskID, repoKey)
		return
	}

	// 构建保存数据
	dataToSave := map[string]interface{}{
		"task_id":     ctx.TaskID,
		"task_name":   ctx.TaskName,
		"workflow_id": ctx.WorkflowID,
		"instance_id": ctx.WorkflowInstanceID,
		"result_data": resultData,
		"timestamp":   time.Now(),
	}

	// 保存数据
	if err := repo.Save(dataToSave); err != nil {
		log.Printf("❌ [DefaultSaveResult] TaskID=%s, 保存结果失败: %v", ctx.TaskID, err)
	} else {
		log.Printf("✅ [DefaultSaveResult] TaskID=%s, 结果已保存", ctx.TaskID)
	}
}

// DefaultAggregateSubTaskResults 默认聚合子任务结果Handler（对外导出）
// 聚合所有子任务的结果，计算统计信息
// 配置参数：
//   - success_rate_threshold (float64, 默认: 80.0) - 成功率阈值
//   - sub_task_results_key (string, 默认: "_sub_task_results") - 子任务结果在参数中的key
func DefaultAggregateSubTaskResults(ctx *TaskContext) {
	// 获取子任务结果
	resultsKey := ctx.GetParamString("sub_task_results_key")
	if resultsKey == "" {
		resultsKey = "_sub_task_results"
	}

	subTaskResults := ctx.GetParam(resultsKey)
	if subTaskResults == nil {
		log.Printf("⚠️ [DefaultAggregateSubTaskResults] TaskID=%s, 未找到子任务结果", ctx.TaskID)
		return
	}

	// 尝试转换为结果列表
	type SubTaskResult struct {
		TaskID   string
		TaskName string
		Status   string
		Data     interface{}
		Error    string
	}

	var results []SubTaskResult
	switch v := subTaskResults.(type) {
	case []SubTaskResult:
		results = v
	case []interface{}:
		for _, item := range v {
			if result, ok := item.(SubTaskResult); ok {
				results = append(results, result)
			} else if resultMap, ok := item.(map[string]interface{}); ok {
				result := SubTaskResult{}
				if taskID, ok := resultMap["task_id"].(string); ok {
					result.TaskID = taskID
				}
				if taskName, ok := resultMap["task_name"].(string); ok {
					result.TaskName = taskName
				}
				if status, ok := resultMap["status"].(string); ok {
					result.Status = status
				}
				if data, ok := resultMap["data"]; ok {
					result.Data = data
				}
				if err, ok := resultMap["error"].(string); ok {
					result.Error = err
				}
				results = append(results, result)
			}
		}
	default:
		log.Printf("⚠️ [DefaultAggregateSubTaskResults] TaskID=%s, 子任务结果格式不正确", ctx.TaskID)
		return
	}

	// 计算统计信息
	total := len(results)
	successCount := 0
	failedCount := 0
	var totalData interface{}

	for _, result := range results {
		if result.Status == TaskStatusSuccess {
			successCount++
			// 尝试聚合数据量
			if result.Data != nil {
				if dataMap, ok := result.Data.(map[string]interface{}); ok {
					if count, ok := dataMap["data_count"].(float64); ok {
						if totalData == nil {
							totalData = float64(0)
						}
						totalData = totalData.(float64) + count
					}
				}
			}
		} else {
			failedCount++
		}
	}

	successRate := float64(0)
	if total > 0 {
		successRate = float64(successCount) / float64(total) * 100
	}

	// 获取成功率阈值
	threshold, err := ctx.GetParamFloat("success_rate_threshold")
	if err != nil || threshold == 0 {
		threshold = 80.0
	}

	meetsThreshold := successRate >= threshold

	// 输出统计信息
	log.Printf("📊 [DefaultAggregateSubTaskResults] TaskID=%s, 总数=%d, 成功=%d, 失败=%d, 成功率=%.2f%%, 阈值=%.2f%%, 达标=%v, 总数据量=%v",
		ctx.TaskID, total, successCount, failedCount, successRate, threshold, meetsThreshold, totalData)

	// 将统计信息保存到context中，供后续Handler使用
	ctx.Params["_aggregation_stats"] = map[string]interface{}{
		"total":           total,
		"success_count":   successCount,
		"failed_count":    failedCount,
		"success_rate":    successRate,
		"total_data":      totalData,
		"meets_threshold": meetsThreshold,
	}

	// 如果未达到阈值，可以触发失败处理
	if !meetsThreshold {
		log.Printf("⚠️ [DefaultAggregateSubTaskResults] TaskID=%s, 成功率未达到阈值，可能需要触发失败处理", ctx.TaskID)
	}
}

// DefaultBatchGenerateSubTasks 默认批量生成子任务Handler（对外导出）
// 限制一次性生成的子任务数量，分批生成
// 配置参数：
//   - batch_size (int, 默认: 10) - 每批生成数量
//   - sub_tasks_key (string, 默认: "_sub_tasks") - 子任务列表在参数中的key
//   - manager_key (string, 默认: "InstanceManager") - InstanceManager依赖的key
func DefaultBatchGenerateSubTasks(ctx *TaskContext) {
	// 获取子任务列表
	subTasksKey := ctx.GetParamString("sub_tasks_key")
	if subTasksKey == "" {
		subTasksKey = "_sub_tasks"
	}

	subTasks := ctx.GetParam(subTasksKey)
	if subTasks == nil {
		log.Printf("⚠️ [DefaultBatchGenerateSubTasks] TaskID=%s, 未找到子任务列表", ctx.TaskID)
		return
	}

	// 获取批量大小
	batchSize, err := ctx.GetParamInt("batch_size")
	if err != nil || batchSize == 0 {
		batchSize = 10
	}

	// 获取InstanceManager依赖（已由WorkflowInstanceManager注入）
	managerKey := ctx.GetParamString("manager_key")
	if managerKey == "" {
		managerKey = "InstanceManager"
	}

	// 使用GetDependencyTyped获取类型安全的依赖
	type ManagerAddSubTaskInterface interface {
		AddSubTask(subTask workflow.Task, parentTaskID string) error
	}

	manager, ok := GetDependencyTyped[ManagerAddSubTaskInterface](ctx.Context(), managerKey)
	if !ok {
		log.Printf("⚠️ [DefaultBatchGenerateSubTasks] TaskID=%s, 未找到InstanceManager依赖 (key=%s)", ctx.TaskID, managerKey)
		return
	}

	// 转换子任务列表
	var taskList []interface{}
	switch v := subTasks.(type) {
	case []interface{}:
		taskList = v
	default:
		log.Printf("⚠️ [DefaultBatchGenerateSubTasks] TaskID=%s, 子任务列表格式不正确", ctx.TaskID)
		return
	}

	// 获取父任务ID（当前任务ID作为父任务）
	parentTaskID := ctx.TaskID

	// 分批生成子任务
	totalTasks := len(taskList)
	generatedCount := 0

	for i := 0; i < totalTasks; i += batchSize {
		end := i + batchSize
		if end > totalTasks {
			end = totalTasks
		}

		batch := taskList[i:end]
		log.Printf("📦 [DefaultBatchGenerateSubTasks] TaskID=%s, 生成第 %d 批子任务 (共 %d 个)", ctx.TaskID, i/batchSize+1, len(batch))

		// 调用Manager的AddSubTask方法添加子任务
		for _, subTask := range batch {
			// 尝试转换为workflow.Task接口
			if task, ok := subTask.(workflow.Task); ok {
				// 直接调用Manager添加子任务，不需要Engine
				if err := manager.AddSubTask(task, parentTaskID); err != nil {
					log.Printf("❌ [DefaultBatchGenerateSubTasks] TaskID=%s, 添加子任务失败: %v", ctx.TaskID, err)
					continue
				}

				generatedCount++
				log.Printf("✅ [DefaultBatchGenerateSubTasks] TaskID=%s, 子任务已添加: %s", ctx.TaskID, task.GetID())
			} else {
				log.Printf("⚠️ [DefaultBatchGenerateSubTasks] TaskID=%s, 子任务类型不匹配，需要实现workflow.Task接口", ctx.TaskID)
			}
		}
	}

	log.Printf("✅ [DefaultBatchGenerateSubTasks] TaskID=%s, 共生成 %d 个子任务（分 %d 批）", ctx.TaskID, generatedCount, (totalTasks+batchSize-1)/batchSize)
}

// DefaultValidateParams 默认参数校验Handler（对外导出）
// 校验任务参数的完整性和合法性
// 配置参数：
//   - required_params ([]string) - 必需参数列表
//   - param_validators (map[string]func(interface{}) error) - 参数校验规则（通过参数传递，需要序列化）
func DefaultValidateParams(ctx *TaskContext) {
	// 获取必需参数列表
	requiredParams := ctx.GetParam("required_params")
	if requiredParams == nil {
		// 没有配置必需参数，跳过校验
		return
	}

	var requiredList []string
	switch v := requiredParams.(type) {
	case []string:
		requiredList = v
	case []interface{}:
		for _, item := range v {
			if param, ok := item.(string); ok {
				requiredList = append(requiredList, param)
			}
		}
	default:
		log.Printf("⚠️ [DefaultValidateParams] TaskID=%s, required_params格式不正确", ctx.TaskID)
		return
	}

	// 检查必需参数是否存在
	missingParams := make([]string, 0)
	for _, param := range requiredList {
		if !ctx.HasParam(param) {
			missingParams = append(missingParams, param)
		}
	}

	if len(missingParams) > 0 {
		log.Printf("❌ [DefaultValidateParams] TaskID=%s, 缺少必需参数: %v", ctx.TaskID, missingParams)
		// 将错误信息保存到context
		ctx.Params["_validation_error"] = fmt.Sprintf("缺少必需参数: %v", missingParams)
		return
	}

	log.Printf("✅ [DefaultValidateParams] TaskID=%s, 参数校验通过", ctx.TaskID)
}

// DefaultCompensate 默认补偿Handler（对外导出）
// 执行补偿逻辑，通过registry获取补偿函数并执行
// 配置参数：
//   - compensate_func_name (string) - 补偿函数名称（作为TaskHandler注册）
//   - compensate_func_id (string) - 补偿函数ID（可选，优先使用名称）
// 或者通过Task的CompensationFuncName字段获取
func DefaultCompensate(ctx *TaskContext) {
	// 尝试从参数获取补偿函数名称
	compensateFuncName := ctx.GetParamString("compensate_func_name")
	if compensateFuncName == "" {
		// 尝试从Task的CompensationFuncName获取（如果Task信息在context中）
		// 注意：这里需要从依赖注入获取registry，然后通过TaskID查找Task
		log.Printf("⚠️ [DefaultCompensate] TaskID=%s, 未找到补偿函数名称", ctx.TaskID)
		return
	}

	// 从依赖注入获取FunctionRegistry
	registry, ok := GetDependencyTyped[*FunctionRegistry](ctx.Context(), "FunctionRegistry")
	if !ok {
		// 尝试通过字符串key获取
		dep, ok := ctx.GetDependency("FunctionRegistry")
		if !ok {
			log.Printf("⚠️ [DefaultCompensate] TaskID=%s, 未找到FunctionRegistry依赖", ctx.TaskID)
			return
		}
		var ok2 bool
		registry, ok2 = dep.(*FunctionRegistry)
		if !ok2 {
			log.Printf("⚠️ [DefaultCompensate] TaskID=%s, FunctionRegistry类型不正确", ctx.TaskID)
			return
		}
	}

	// 从registry获取补偿函数（作为TaskHandler）
	compensateHandler := registry.GetTaskHandlerByName(compensateFuncName)
	if compensateHandler == nil {
		// 尝试通过ID获取
		compensateFuncID := ctx.GetParamString("compensate_func_id")
		if compensateFuncID != "" {
			compensateHandler = registry.GetTaskHandler(compensateFuncID)
		}
	}

	if compensateHandler == nil {
		log.Printf("⚠️ [DefaultCompensate] TaskID=%s, 补偿函数 %s 未找到", ctx.TaskID, compensateFuncName)
		return
	}

	// 执行补偿函数
	log.Printf("🔄 [DefaultCompensate] TaskID=%s, 开始执行补偿函数: %s", ctx.TaskID, compensateFuncName)
	
	// 在goroutine中执行，避免阻塞
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("❌ [DefaultCompensate] TaskID=%s, 补偿函数执行panic: %v", ctx.TaskID, r)
			}
		}()
		compensateHandler(ctx)
	}()

	log.Printf("✅ [DefaultCompensate] TaskID=%s, 补偿函数已启动", ctx.TaskID)
}

// DefaultSkipIfCached 默认缓存跳过Handler（对外导出）
// 检查缓存，如果命中则跳过任务执行
// 配置参数：
//   - cache_key (string, 默认: 使用任务ID) - 缓存键
//   - cache_ttl (int, 默认: 0) - 缓存有效期（秒）
func DefaultSkipIfCached(ctx *TaskContext) {
	// 获取缓存键
	cacheKey := ctx.GetParamString("cache_key")
	if cacheKey == "" {
		cacheKey = ctx.TaskID
	}

	// 使用GetDependencyTyped获取类型安全的依赖
	type CacheInterface interface {
		Get(key string) (interface{}, bool)
	}

	cache, ok := GetDependencyTyped[CacheInterface](ctx.Context(), "ResultCache")
	if !ok {
		// 没有缓存依赖，不跳过
		return
	}

	// 检查缓存
	if cached, found := cache.Get(cacheKey); found {
		log.Printf("✅ [DefaultSkipIfCached] TaskID=%s, 缓存命中，跳过任务执行，缓存值=%v", ctx.TaskID, cached)
		// 将缓存值保存到结果中
		ctx.Params["_cached_result"] = cached
		ctx.Params["_skipped"] = true
	} else {
		log.Printf("ℹ️ [DefaultSkipIfCached] TaskID=%s, 缓存未命中，继续执行", ctx.TaskID)
	}
}

// DefaultRetryOnFailure 默认重试Handler（对外导出）
// 失败时自动重试（增强版，支持自定义重试策略）
// 配置参数：
//   - max_retries (int, 默认: 3) - 最大重试次数
//   - retry_delay (int, 默认: 1) - 重试延迟（秒，支持指数退避）
func DefaultRetryOnFailure(ctx *TaskContext) {
	// 获取重试配置
	maxRetries, err := ctx.GetParamInt("max_retries")
	if err != nil || maxRetries == 0 {
		maxRetries = 3
	}

	retryDelay, err := ctx.GetParamInt("retry_delay")
	if err != nil || retryDelay == 0 {
		retryDelay = 1
	}

	// 获取当前重试次数
	currentRetries, _ := ctx.GetParamInt("_current_retries")
	if currentRetries < 0 {
		currentRetries = 0
	}

	if currentRetries >= maxRetries {
		log.Printf("❌ [DefaultRetryOnFailure] TaskID=%s, 已达到最大重试次数 %d，停止重试", ctx.TaskID, maxRetries)
		return
	}

	// 计算重试延迟（指数退避）
	delay := retryDelay * (1 << uint(currentRetries))
	log.Printf("🔄 [DefaultRetryOnFailure] TaskID=%s, 准备重试，当前重试次数=%d/%d, 延迟=%d秒", ctx.TaskID, currentRetries+1, maxRetries, delay)

	// 将重试信息保存到context
	ctx.Params["_should_retry"] = true
	ctx.Params["_retry_delay"] = delay
	ctx.Params["_current_retries"] = currentRetries + 1
}

// DefaultNotifyOnFailure 默认通知Handler（对外导出）
// 任务失败时发送通知
// 配置参数：
//   - notification_channels ([]string) - 通知渠道列表（如: ["email", "sms", "webhook"]）
func DefaultNotifyOnFailure(ctx *TaskContext) {
	// 获取通知渠道
	channels := ctx.GetParam("notification_channels")
	if channels == nil {
		log.Printf("⚠️ [DefaultNotifyOnFailure] TaskID=%s, 未配置通知渠道", ctx.TaskID)
		return
	}

	var channelList []string
	switch v := channels.(type) {
	case []string:
		channelList = v
	case []interface{}:
		for _, item := range v {
			if channel, ok := item.(string); ok {
				channelList = append(channelList, channel)
			}
		}
	default:
		log.Printf("⚠️ [DefaultNotifyOnFailure] TaskID=%s, 通知渠道格式不正确", ctx.TaskID)
		return
	}

	// 获取错误信息
	errorMsg := ctx.GetParamString("_error_message")
	taskName := ctx.TaskName

	// 发送通知（这里只记录日志，实际实现需要调用通知服务）
	for _, channel := range channelList {
		log.Printf("📢 [DefaultNotifyOnFailure] TaskID=%s, 通过 %s 发送通知: 任务 %s 失败，错误=%s", ctx.TaskID, channel, taskName, errorMsg)
	}

	log.Printf("✅ [DefaultNotifyOnFailure] TaskID=%s, 通知已发送到 %d 个渠道", ctx.TaskID, len(channelList))
}
