package integration

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/LENAX/task-engine/internal/storage/sqlite"
	"github.com/LENAX/task-engine/pkg/core/builder"
	"github.com/LENAX/task-engine/pkg/core/engine"
	"github.com/LENAX/task-engine/pkg/core/task"
	"github.com/LENAX/task-engine/pkg/core/types"
	"github.com/LENAX/task-engine/pkg/core/workflow"
)

// TestUpstreamResultPassing_Basic 测试基础上下游参数传递
// 场景：Task1 返回 stock_codes，Task2 通过新 API 获取
func TestUpstreamResultPassing_Basic(t *testing.T) {
	eng, registry, wf, cleanup := setupUpstreamTestEnv(t)
	defer cleanup()

	ctx := context.Background()

	// 捕获下游任务接收到的数据
	var capturedStockCodes []string
	var capturedCount int
	capturedMutex := sync.Mutex{}

	// Task1: 上游任务，返回股票代码列表
	fetchStockListFunc := func(tc *task.TaskContext) (interface{}, error) {
		return map[string]interface{}{
			"stock_codes": []string{"000001", "000002", "000003"},
			"count":       3,
			"source":      "tushare",
		}, nil
	}
	_, err := registry.Register(ctx, "fetchStockListFunc", fetchStockListFunc, "获取股票列表")
	if err != nil {
		t.Fatalf("注册 fetchStockListFunc 失败: %v", err)
	}

	// Task2: 下游任务，使用新 API 获取上游结果
	processStocksFunc := func(tc *task.TaskContext) (interface{}, error) {
		// 使用新 API 获取上游结果
		stockCodes := tc.GetUpstreamStringSlice("FetchStockList", "stock_codes")
		count, _ := tc.GetUpstreamInt("FetchStockList", "count")
		source := tc.GetUpstreamString("FetchStockList", "source")

		capturedMutex.Lock()
		capturedStockCodes = stockCodes
		capturedCount = count
		capturedMutex.Unlock()

		t.Logf("✅ 下游任务收到: stock_codes=%v, count=%d, source=%s", stockCodes, count, source)

		return map[string]interface{}{
			"processed_count": len(stockCodes),
			"source":          source,
		}, nil
	}
	_, err = registry.Register(ctx, "processStocksFunc", processStocksFunc, "处理股票数据")
	if err != nil {
		t.Fatalf("注册 processStocksFunc 失败: %v", err)
	}

	// 创建任务
	task1, _ := builder.NewTaskBuilder("FetchStockList", "获取股票列表", registry).
		WithJobFunction("fetchStockListFunc", nil).
		Build()

	task2, _ := builder.NewTaskBuilder("ProcessStocks", "处理股票数据", registry).
		WithJobFunction("processStocksFunc", nil).
		WithDependency("FetchStockList").
		Build()

	wf.AddTask(task1)
	wf.AddTask(task2)

	// 执行 Workflow
	controller, err := eng.SubmitWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("提交 Workflow 失败: %v", err)
	}

	// 等待完成
	waitForWorkflowCompleteByController(t, controller, 10*time.Second)

	// 验证结果
	capturedMutex.Lock()
	defer capturedMutex.Unlock()

	if len(capturedStockCodes) != 3 {
		t.Errorf("期望收到 3 个股票代码，实际收到 %d 个", len(capturedStockCodes))
	}
	if capturedCount != 3 {
		t.Errorf("期望 count=3，实际 count=%d", capturedCount)
	}
	if capturedStockCodes[0] != "000001" {
		t.Errorf("期望第一个股票代码为 '000001'，实际为 %s", capturedStockCodes[0])
	}

	t.Log("✅ 上下游参数传递测试通过")
}

// TestUpstreamResultPassing_GetAllUpstreamResults 测试获取所有上游结果
// 场景：Task3 依赖 Task1 和 Task2，获取所有上游结果
func TestUpstreamResultPassing_GetAllUpstreamResults(t *testing.T) {
	eng, registry, wf, cleanup := setupUpstreamTestEnv(t)
	defer cleanup()

	ctx := context.Background()

	// 捕获下游任务接收到的数据
	var capturedUpstreamCount int
	capturedMutex := sync.Mutex{}

	// Task1
	task1Func := func(tc *task.TaskContext) (interface{}, error) {
		return map[string]interface{}{"data": "from_task1", "value": 100}, nil
	}
	registry.Register(ctx, "task1Func", task1Func, "任务1")

	// Task2
	task2Func := func(tc *task.TaskContext) (interface{}, error) {
		return map[string]interface{}{"data": "from_task2", "value": 200}, nil
	}
	registry.Register(ctx, "task2Func", task2Func, "任务2")

	// Task3: 依赖 Task1 和 Task2
	task3Func := func(tc *task.TaskContext) (interface{}, error) {
		allUpstream := tc.GetAllUpstreamResults()

		capturedMutex.Lock()
		capturedUpstreamCount = len(allUpstream)
		capturedMutex.Unlock()

		t.Logf("✅ Task3 收到 %d 个上游任务结果", len(allUpstream))
		for taskID, result := range allUpstream {
			t.Logf("   - %s: %v", taskID, result)
		}

		return map[string]interface{}{"upstream_count": len(allUpstream)}, nil
	}
	registry.Register(ctx, "task3Func", task3Func, "任务3")

	// 创建任务
	t1, _ := builder.NewTaskBuilder("Task1", "任务1", registry).
		WithJobFunction("task1Func", nil).Build()
	t2, _ := builder.NewTaskBuilder("Task2", "任务2", registry).
		WithJobFunction("task2Func", nil).Build()
	t3, _ := builder.NewTaskBuilder("Task3", "任务3", registry).
		WithJobFunction("task3Func", nil).
		WithDependency("Task1").
		WithDependency("Task2").
		Build()

	wf.AddTask(t1)
	wf.AddTask(t2)
	wf.AddTask(t3)

	// 执行
	controller, _ := eng.SubmitWorkflow(ctx, wf)
	waitForWorkflowCompleteByController(t, controller, 10*time.Second)

	// 验证
	capturedMutex.Lock()
	defer capturedMutex.Unlock()

	// 注意：由于现在同时使用 taskID 和 taskName 作为 key，
	// 2 个上游任务会产生 4 个 key（每个任务 2 个：taskID 和 taskName）
	// 这是一个功能增强，允许用户通过 taskID 或 taskName 访问上游结果
	if capturedUpstreamCount < 2 {
		t.Errorf("期望收到至少 2 个上游结果，实际收到 %d 个", capturedUpstreamCount)
	}

	t.Log("✅ GetAllUpstreamResults 测试通过")
}

// TestUpstreamResultPassing_DynamicSubTasks 测试下游任务获取上游动态子任务结果
// 场景：模板任务生成 3 个子任务，下游任务使用新 API 提取子任务结果
func TestUpstreamResultPassing_DynamicSubTasks(t *testing.T) {
	eng, registry, wf, cleanup := setupUpstreamTestEnv(t)
	defer cleanup()

	ctx := context.Background()

	// 捕获下游任务接收到的数据
	var capturedAPIMetadataCount int
	var capturedAllSucceeded bool
	var capturedAPINames []string
	capturedMutex := sync.Mutex{}

	// 子任务函数：模拟获取 API 详情
	fetchAPIDetailFunc := func(tc *task.TaskContext) (interface{}, error) {
		apiName := tc.GetParamString("api_name")
		index, _ := tc.GetParamInt("index")

		t.Logf("📝 子任务执行: api_name=%s, index=%d", apiName, index)

		return map[string]interface{}{
			"api_metadata": map[string]interface{}{
				"id":          fmt.Sprintf("api-%03d", index),
				"name":        apiName,
				"endpoint":    fmt.Sprintf("/%s", apiName),
				"description": fmt.Sprintf("%s API 详情", apiName),
			},
		}, nil
	}
	registry.Register(ctx, "fetchAPIDetailFunc", fetchAPIDetailFunc, "获取API详情")

	// 模板任务函数：动态生成子任务
	templateFunc := func(tc *task.TaskContext) (interface{}, error) {
		type ManagerInterface interface {
			AtomicAddSubTasks(subTasks []types.Task, parentTaskID string) error
		}

		managerRaw := tc.GetInstanceManager()
		if managerRaw == nil {
			return nil, fmt.Errorf("无法获取 InstanceManager")
		}
		manager, ok := managerRaw.(ManagerInterface)
		if !ok {
			return nil, fmt.Errorf("InstanceManager 类型断言失败")
		}

		// 模拟获取 API 列表
		apiNames := []string{"stock_basic", "daily", "income"}

		// 生成子任务
		subTasks := make([]types.Task, 0, len(apiNames))
		for i, apiName := range apiNames {
			subTask, err := builder.NewTaskBuilder(
				fmt.Sprintf("fetch-api-%d", i),
				fmt.Sprintf("获取 %s 详情", apiName),
				registry,
			).
				WithJobFunction("fetchAPIDetailFunc", map[string]interface{}{
					"api_name": apiName,
					"index":    i + 1,
				}).
				Build()
			if err != nil {
				return nil, fmt.Errorf("构建子任务失败: %v", err)
			}
			subTasks = append(subTasks, subTask)
		}

		if err := manager.AtomicAddSubTasks(subTasks, tc.TaskID); err != nil {
			return nil, fmt.Errorf("添加子任务失败: %v", err)
		}

		t.Logf("📝 模板任务生成 %d 个子任务", len(subTasks))

		return map[string]interface{}{
			"api_count": len(apiNames),
		}, nil
	}
	registry.Register(ctx, "templateFunc", templateFunc, "模板任务")

	// 下游任务：使用新 API 提取子任务结果
	saveAPIMetadataFunc := func(tc *task.TaskContext) (interface{}, error) {
		// 使用新 API
		apiMetadataMaps := tc.ExtractMapsFromSubTasks("api_metadata")
		allSucceeded := tc.AllSubTasksSucceeded()
		subtaskCount := tc.GetSubTaskCount()

		t.Logf("📊 下游任务收到: %d 个 api_metadata, 子任务总数=%d, 全部成功=%v",
			len(apiMetadataMaps), subtaskCount, allSucceeded)

		apiNames := make([]string, 0, len(apiMetadataMaps))
		for _, m := range apiMetadataMaps {
			if name, ok := m["name"].(string); ok {
				apiNames = append(apiNames, name)
				t.Logf("   - API: %s, endpoint: %v", name, m["endpoint"])
			}
		}

		capturedMutex.Lock()
		capturedAPIMetadataCount = len(apiMetadataMaps)
		capturedAllSucceeded = allSucceeded
		capturedAPINames = apiNames
		capturedMutex.Unlock()

		return map[string]interface{}{
			"saved_count": len(apiMetadataMaps),
		}, nil
	}
	registry.Register(ctx, "saveAPIMetadataFunc", saveAPIMetadataFunc, "保存API元数据")

	// 创建任务
	templateTask, _ := builder.NewTaskBuilder("FetchAllAPIDetails", "获取所有API详情", registry).
		WithJobFunction("templateFunc", nil).
		WithTemplate(true).
		Build()

	downstreamTask, _ := builder.NewTaskBuilder("SaveAPIMetadata", "保存API元数据", registry).
		WithJobFunction("saveAPIMetadataFunc", nil).
		WithDependency("FetchAllAPIDetails").
		Build()

	wf.AddTask(templateTask)
	wf.AddTask(downstreamTask)

	// 执行
	controller, err := eng.SubmitWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("提交 Workflow 失败: %v", err)
	}

	// 等待完成（子任务需要更长时间）
	waitForWorkflowCompleteByController(t, controller, 30*time.Second)

	// 验证结果
	capturedMutex.Lock()
	defer capturedMutex.Unlock()

	if capturedAPIMetadataCount != 3 {
		t.Errorf("期望提取 3 个 api_metadata，实际提取 %d 个", capturedAPIMetadataCount)
	}
	if !capturedAllSucceeded {
		t.Errorf("期望 AllSubTasksSucceeded=true，实际为 false")
	}
	if len(capturedAPINames) != 3 {
		t.Errorf("期望 3 个 API 名称，实际 %d 个", len(capturedAPINames))
	}

	// 验证 API 名称
	expectedNames := map[string]bool{"stock_basic": true, "daily": true, "income": true}
	for _, name := range capturedAPINames {
		if !expectedNames[name] {
			t.Errorf("意外的 API 名称: %s", name)
		}
	}

	t.Log("✅ 动态子任务结果传递测试通过")
}

// TestUpstreamResultPassing_PartialSubTaskFailure 测试部分子任务失败时的结果传递
// 场景：3 个子任务中 1 个失败，验证下游能正确获取成功的子任务结果
func TestUpstreamResultPassing_PartialSubTaskFailure(t *testing.T) {
	eng, registry, wf, cleanup := setupUpstreamTestEnv(t)
	defer cleanup()

	ctx := context.Background()

	// 捕获结果
	var capturedSuccessCount int
	var capturedFailedCount int
	var capturedAllSucceeded bool
	capturedMutex := sync.Mutex{}

	// 子任务函数：index=1 会失败
	subTaskFunc := func(tc *task.TaskContext) (interface{}, error) {
		index, _ := tc.GetParamInt("index")

		if index == 1 {
			return nil, fmt.Errorf("子任务 %d 执行失败（模拟错误）", index)
		}

		return map[string]interface{}{
			"data": map[string]interface{}{
				"index":  index,
				"status": "success",
			},
		}, nil
	}
	registry.Register(ctx, "subTaskFunc", subTaskFunc, "子任务函数")

	// 模板任务
	templateFunc := func(tc *task.TaskContext) (interface{}, error) {
		type ManagerInterface interface {
			AtomicAddSubTasks(subTasks []types.Task, parentTaskID string) error
		}
		manager := tc.GetInstanceManager().(ManagerInterface)

		subTasks := make([]types.Task, 0, 3)
		for i := 0; i < 3; i++ {
			subTask, _ := builder.NewTaskBuilder(
				fmt.Sprintf("subtask-%d", i),
				fmt.Sprintf("子任务 %d", i),
				registry,
			).
				WithJobFunction("subTaskFunc", map[string]interface{}{"index": i}).
				Build()
			subTasks = append(subTasks, subTask)
		}

		manager.AtomicAddSubTasks(subTasks, tc.TaskID)
		return map[string]interface{}{"generated": 3}, nil
	}
	registry.Register(ctx, "templateFunc", templateFunc, "模板任务")

	// 下游任务
	downstreamFunc := func(tc *task.TaskContext) (interface{}, error) {
		successResults := tc.GetSuccessfulSubTaskResults()
		failedResults := tc.GetFailedSubTaskResults()
		allSucceeded := tc.AllSubTasksSucceeded()

		t.Logf("📊 下游任务: 成功=%d, 失败=%d, 全部成功=%v",
			len(successResults), len(failedResults), allSucceeded)

		// 提取成功子任务的数据
		dataMaps := tc.ExtractMapsFromSubTasks("data")
		t.Logf("📊 提取到 %d 个 data", len(dataMaps))

		capturedMutex.Lock()
		capturedSuccessCount = len(successResults)
		capturedFailedCount = len(failedResults)
		capturedAllSucceeded = allSucceeded
		capturedMutex.Unlock()

		return map[string]interface{}{
			"success_count": len(successResults),
			"failed_count":  len(failedResults),
		}, nil
	}
	registry.Register(ctx, "downstreamFunc", downstreamFunc, "下游任务")

	// 创建任务
	templateTask, _ := builder.NewTaskBuilder("TemplateTask", "模板任务", registry).
		WithJobFunction("templateFunc", nil).
		WithTemplate(true).
		Build()

	downstreamTask, _ := builder.NewTaskBuilder("DownstreamTask", "下游任务", registry).
		WithJobFunction("downstreamFunc", nil).
		WithDependency("TemplateTask").
		Build()

	wf.AddTask(templateTask)
	wf.AddTask(downstreamTask)

	// 执行
	controller, _ := eng.SubmitWorkflow(ctx, wf)
	waitForWorkflowCompleteByController(t, controller, 30*time.Second)

	// 验证
	capturedMutex.Lock()
	defer capturedMutex.Unlock()

	if capturedSuccessCount != 2 {
		t.Errorf("期望 2 个成功的子任务，实际 %d 个", capturedSuccessCount)
	}
	if capturedFailedCount != 1 {
		t.Errorf("期望 1 个失败的子任务，实际 %d 个", capturedFailedCount)
	}
	if capturedAllSucceeded {
		t.Errorf("期望 AllSubTasksSucceeded=false，实际为 true")
	}

	t.Log("✅ 部分子任务失败场景测试通过")
}

// TestUpstreamResultPassing_SubTaskResultDetails 测试子任务结果详情获取
// 验证 SubTaskResult 结构的各个字段能正确获取
func TestUpstreamResultPassing_SubTaskResultDetails(t *testing.T) {
	eng, registry, wf, cleanup := setupUpstreamTestEnv(t)
	defer cleanup()

	ctx := context.Background()

	// 捕获结果
	var capturedResults []task.SubTaskResult
	capturedMutex := sync.Mutex{}

	// 子任务函数
	subTaskFunc := func(tc *task.TaskContext) (interface{}, error) {
		name := tc.GetParamString("name")
		return map[string]interface{}{
			"processed_name": name,
			"timestamp":      time.Now().Unix(),
		}, nil
	}
	registry.Register(ctx, "subTaskFunc", subTaskFunc, "子任务")

	// 模板任务
	templateFunc := func(tc *task.TaskContext) (interface{}, error) {
		type ManagerInterface interface {
			AtomicAddSubTasks(subTasks []types.Task, parentTaskID string) error
		}
		manager := tc.GetInstanceManager().(ManagerInterface)

		names := []string{"Alice", "Bob"}
		subTasks := make([]types.Task, 0, len(names))
		for i, name := range names {
			subTask, _ := builder.NewTaskBuilder(
				fmt.Sprintf("process-%s", name),
				fmt.Sprintf("处理 %s", name),
				registry,
			).
				WithJobFunction("subTaskFunc", map[string]interface{}{"name": name, "index": i}).
				Build()
			subTasks = append(subTasks, subTask)
		}

		manager.AtomicAddSubTasks(subTasks, tc.TaskID)
		return nil, nil
	}
	registry.Register(ctx, "templateFunc", templateFunc, "模板任务")

	// 下游任务
	downstreamFunc := func(tc *task.TaskContext) (interface{}, error) {
		results := tc.GetSubTaskResults()

		capturedMutex.Lock()
		capturedResults = results
		capturedMutex.Unlock()

		for _, r := range results {
			t.Logf("📊 子任务结果: TaskID=%s, TaskName=%s, Status=%s, IsSuccess=%v",
				r.TaskID, r.TaskName, r.Status, r.IsSuccess())
			if r.Result != nil {
				t.Logf("   Result: %v", r.Result)
			}
			if r.Error != "" {
				t.Logf("   Error: %s", r.Error)
			}
		}

		return nil, nil
	}
	registry.Register(ctx, "downstreamFunc", downstreamFunc, "下游任务")

	// 创建任务
	templateTask, _ := builder.NewTaskBuilder("Template", "模板", registry).
		WithJobFunction("templateFunc", nil).
		WithTemplate(true).
		Build()

	downstreamTask, _ := builder.NewTaskBuilder("Downstream", "下游", registry).
		WithJobFunction("downstreamFunc", nil).
		WithDependency("Template").
		Build()

	wf.AddTask(templateTask)
	wf.AddTask(downstreamTask)

	// 执行
	controller, _ := eng.SubmitWorkflow(ctx, wf)
	waitForWorkflowCompleteByController(t, controller, 30*time.Second)

	// 验证
	capturedMutex.Lock()
	defer capturedMutex.Unlock()

	if len(capturedResults) != 2 {
		t.Errorf("期望 2 个子任务结果，实际 %d 个", len(capturedResults))
	}

	for _, r := range capturedResults {
		if r.TaskID == "" {
			t.Error("SubTaskResult.TaskID 不应为空")
		}
		if r.TaskName == "" {
			t.Error("SubTaskResult.TaskName 不应为空")
		}
		if r.Status != "Success" {
			t.Errorf("期望 Status='Success'，实际为 %s", r.Status)
		}
		if !r.IsSuccess() {
			t.Error("IsSuccess() 应返回 true")
		}
		if r.Result == nil {
			t.Error("Result 不应为空")
		}
		// 验证 GetResultValue
		if r.GetResultValue("processed_name") == nil {
			t.Error("GetResultValue('processed_name') 不应返回 nil")
		}
	}

	t.Log("✅ 子任务结果详情测试通过")
}

// ========== 辅助函数 ==========

// setupUpstreamTestEnv 设置上游结果传递测试环境
func setupUpstreamTestEnv(t *testing.T) (*engine.Engine, task.FunctionRegistry, *workflow.Workflow, func()) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test_upstream.db"

	repos, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("创建 Repository 失败: %v", err)
	}

	eng, err := engine.NewEngine(10, 30, repos.Workflow, repos.WorkflowInstance, repos.Task)
	if err != nil {
		t.Fatalf("创建 Engine 失败: %v", err)
	}

	registry := eng.GetRegistry()
	ctx := context.Background()

	if err := eng.Start(ctx); err != nil {
		t.Fatalf("启动 Engine 失败: %v", err)
	}

	wf := workflow.NewWorkflow("test-upstream-workflow", "上游结果传递测试")

	cleanup := func() {
		eng.Stop()
		repos.Close()
	}

	return eng, registry, wf, cleanup
}

// waitForWorkflowCompleteByController 通过 Controller 等待 Workflow 完成
func waitForWorkflowCompleteByController(t *testing.T, controller workflow.WorkflowController, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if time.Now().After(deadline) {
				t.Fatalf("Workflow 执行超时（%v）", timeout)
				return
			}

			status, err := controller.GetStatus()
			if err != nil {
				continue
			}

			if status == "Success" || status == "Failed" || status == "Completed" {
				t.Logf("📊 Workflow 完成，状态: %s", status)
				return
			}
		}
	}
}
