package unit

import (
	"context"
	"testing"
	"time"

	"github.com/LENAX/task-engine/pkg/core/cache"
	"github.com/LENAX/task-engine/pkg/core/task"
)

// TestDefaultLogSuccess 测试DefaultLogSuccess Handler
func TestDefaultLogSuccess(t *testing.T) {
	ctx := task.NewTaskContext(
		context.Background(),
		"test-task",
		"测试任务",
		"",
		"",
		map[string]interface{}{
			"_result_data": "test result",
		},
	)

	// 执行Handler（应该不会panic）
	task.DefaultLogSuccess(ctx)
}

// TestDefaultLogError 测试DefaultLogError Handler
func TestDefaultLogError(t *testing.T) {
	ctx := task.NewTaskContext(
		context.Background(),
		"test-task",
		"测试任务",
		"",
		"",
		map[string]interface{}{
			"_error_message": "test error",
		},
	)

	// 执行Handler（应该不会panic）
	task.DefaultLogError(ctx)
}

// MockSaveRepository 实现SaveRepository接口的Mock对象
type MockSaveRepository struct {
	savedData []map[string]interface{}
}

// Save 实现SaveRepository接口
func (r *MockSaveRepository) Save(data map[string]interface{}) error {
	r.savedData = append(r.savedData, data)
	return nil
}

// TestDefaultSaveResult 测试DefaultSaveResult Handler
func TestDefaultSaveResult(t *testing.T) {
	// 创建Mock Repository
	repo := &MockSaveRepository{
		savedData: make([]map[string]interface{}, 0),
	}

	// 注册依赖
	registry := task.NewFunctionRegistry(nil, nil)
	if err := registry.RegisterDependencyWithKey("DataRepository", repo); err != nil {
		t.Fatalf("注册依赖失败: %v", err)
	}

	ctx := context.Background()
	ctx = registry.WithDependencies(ctx)

	taskCtx := task.NewTaskContext(
		ctx,
		"test-task",
		"测试任务",
		"",
		"",
		map[string]interface{}{
			"_result_data": "test result",
		},
	)

	// 执行Handler
	task.DefaultSaveResult(taskCtx)

	// 验证数据已保存
	if len(repo.savedData) == 0 {
		t.Error("期望数据已保存，但savedData为空")
	}

	// 验证保存的数据
	savedData := repo.savedData[0]
	if savedData["task_id"] != "test-task" {
		t.Errorf("期望task_id为'test-task'，实际为%v", savedData["task_id"])
	}
	if savedData["result_data"] != "test result" {
		t.Errorf("期望result_data为'test result'，实际为%v", savedData["result_data"])
	}
}

// TestDefaultAggregateSubTaskResults 测试DefaultAggregateSubTaskResults Handler
func TestDefaultAggregateSubTaskResults(t *testing.T) {
	ctx := task.NewTaskContext(
		context.Background(),
		"test-task",
		"测试任务",
		"",
		"",
		map[string]interface{}{
			"sub_task_results_key":   "_sub_task_results",
			"success_rate_threshold": 80.0,
			"_sub_task_results": []map[string]interface{}{
				{"task_id": "sub1", "status": task.TaskStatusSuccess, "data": map[string]interface{}{"data_count": 10}},
				{"task_id": "sub2", "status": task.TaskStatusSuccess, "data": map[string]interface{}{"data_count": 20}},
				{"task_id": "sub3", "status": task.TaskStatusFailed, "error": "error"},
			},
		},
	)

	// 执行Handler
	task.DefaultAggregateSubTaskResults(ctx)

	// 验证统计信息
	stats := ctx.GetParam("_aggregation_stats")
	if stats == nil {
		t.Error("期望有聚合统计信息，但未找到")
	}
}

// TestDefaultSkipIfCached 测试DefaultSkipIfCached Handler
func TestDefaultSkipIfCached(t *testing.T) {
	// 创建缓存
	resultCache := cache.NewMemoryResultCache()
	ttl := 1 * time.Hour
	resultCache.Set("test-task", "cached result", ttl)

	// 注册缓存依赖
	registry := task.NewFunctionRegistry(nil, nil)
	if err := registry.RegisterDependencyWithKey("ResultCache", resultCache); err != nil {
		t.Fatalf("注册缓存依赖失败: %v", err)
	}

	ctx := context.Background()
	ctx = registry.WithDependencies(ctx)

	taskCtx := task.NewTaskContext(
		ctx,
		"test-task",
		"测试任务",
		"",
		"",
		map[string]interface{}{
			"cache_key": "test-task",
		},
	)

	// 执行Handler
	task.DefaultSkipIfCached(taskCtx)

	// 验证缓存命中
	if !taskCtx.HasParam("_skipped") {
		t.Error("期望缓存命中并跳过，但未设置_skipped标志")
	}

	cachedResult := taskCtx.GetParam("_cached_result")
	if cachedResult != "cached result" {
		t.Errorf("期望缓存结果为'cached result'，实际为 %v", cachedResult)
	}
}

// TestDefaultValidateParams 测试DefaultValidateParams Handler
func TestDefaultValidateParams(t *testing.T) {
	ctx := task.NewTaskContext(
		context.Background(),
		"test-task",
		"测试任务",
		"",
		"",
		map[string]interface{}{
			"required_params": []string{"param1", "param2"},
			"param1":          "value1",
			"param2":          "value2",
		},
	)

	// 执行Handler
	task.DefaultValidateParams(ctx)

	// 验证没有验证错误
	if ctx.HasParam("_validation_error") {
		t.Error("期望参数校验通过，但发现了验证错误")
	}

	// 测试缺少参数的情况
	ctx2 := task.NewTaskContext(
		context.Background(),
		"test-task-2",
		"测试任务2",
		"",
		"",
		map[string]interface{}{
			"required_params": []string{"param1", "param2"},
			"param1":          "value1",
			// param2缺失
		},
	)

	task.DefaultValidateParams(ctx2)

	// 验证有验证错误
	if !ctx2.HasParam("_validation_error") {
		t.Error("期望参数校验失败，但未发现验证错误")
	}
}

// TestDefaultRetryOnFailure 测试DefaultRetryOnFailure Handler
func TestDefaultRetryOnFailure(t *testing.T) {
	ctx := task.NewTaskContext(
		context.Background(),
		"test-task",
		"测试任务",
		"",
		"",
		map[string]interface{}{
			"max_retries":      3,
			"retry_delay":      1,
			"_current_retries": 1,
		},
	)

	// 执行Handler
	task.DefaultRetryOnFailure(ctx)

	// 验证重试信息
	if !ctx.HasParam("_should_retry") {
		t.Error("期望设置_should_retry标志")
	}

	retryDelay := ctx.GetParam("_retry_delay")
	if retryDelay == nil {
		t.Error("期望有重试延迟信息")
	}
}

// TestDefaultNotifyOnFailure 测试DefaultNotifyOnFailure Handler
func TestDefaultNotifyOnFailure(t *testing.T) {
	ctx := task.NewTaskContext(
		context.Background(),
		"test-task",
		"测试任务",
		"",
		"",
		map[string]interface{}{
			"notification_channels": []string{"email", "sms"},
			"_error_message":        "test error",
		},
	)

	// 执行Handler（应该不会panic）
	task.DefaultNotifyOnFailure(ctx)
}
