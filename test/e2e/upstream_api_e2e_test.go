// Package e2e 提供端到端测试
// 本文件测试上下游参数传递 API 的完整工作流
package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

// TestE2E_UpstreamResultPassing_StockDataPipeline 测试完整的股票数据处理管道
// 场景：
// 1. FetchStockList - 获取股票列表
// 2. FetchDailyData (模板任务) - 为每个股票生成子任务获取日线数据
// 3. AggregateData - 使用新 API 获取所有子任务结果并聚合
func TestE2E_UpstreamResultPassing_StockDataPipeline(t *testing.T) {
	// 设置测试环境
	dataDir := filepath.Join(os.TempDir(), "task-engine-e2e-upstream", time.Now().Format("20060102150405"))
	os.MkdirAll(dataDir, 0755)
	dbPath := filepath.Join(dataDir, "test.db")

	repos, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("创建 Repository 失败: %v", err)
	}
	defer repos.Close()

	eng, err := engine.NewEngine(10, 30, repos.Workflow, repos.WorkflowInstance, repos.Task)
	if err != nil {
		t.Fatalf("创建 Engine 失败: %v", err)
	}

	ctx := context.Background()
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("启动 Engine 失败: %v", err)
	}
	defer eng.Stop()

	registry := eng.GetRegistry()

	// 结果捕获
	var capturedStockCodes []string
	var capturedDailyDataCount int
	var capturedAllSucceeded bool
	var capturedAggregatedData []map[string]interface{}
	capturedMutex := sync.Mutex{}

	// ========== 注册 Job Functions ==========

	// 1. 获取股票列表
	fetchStockListFunc := func(tc *task.TaskContext) (interface{}, error) {
		// 模拟获取股票列表
		stockCodes := []string{"000001.SZ", "000002.SZ", "600000.SH", "600036.SH", "601398.SH"}
		t.Logf("📊 [FetchStockList] 获取到 %d 只股票", len(stockCodes))

		return map[string]interface{}{
			"stock_codes": stockCodes,
			"count":       len(stockCodes),
			"source":      "mock",
		}, nil
	}
	registry.Register(ctx, "fetchStockListFunc", fetchStockListFunc, "获取股票列表")

	// 2. 模板任务 - 生成日线数据子任务
	fetchDailyTemplateFunc := func(tc *task.TaskContext) (interface{}, error) {
		// 使用新 API 获取上游结果
		stockCodes := tc.GetUpstreamStringSlice("FetchStockList", "stock_codes")
		if len(stockCodes) == 0 {
			return nil, fmt.Errorf("未获取到股票代码列表")
		}

		capturedMutex.Lock()
		capturedStockCodes = stockCodes
		capturedMutex.Unlock()

		t.Logf("📊 [FetchDailyTemplate] 使用新 API 获取到 %d 只股票: %v", len(stockCodes), stockCodes)

		// 获取 InstanceManager 添加子任务
		type ManagerInterface interface {
			AtomicAddSubTasks(subTasks []types.Task, parentTaskID string) error
		}
		managerRaw := tc.GetInstanceManager()
		if managerRaw == nil {
			return nil, fmt.Errorf("无法获取 InstanceManager")
		}
		manager := managerRaw.(ManagerInterface)

		// 为每个股票生成子任务
		subTasks := make([]types.Task, 0, len(stockCodes))
		for i, code := range stockCodes {
			subTask, err := builder.NewTaskBuilder(
				fmt.Sprintf("fetch-daily-%d", i),
				fmt.Sprintf("获取 %s 日线数据", code),
				registry,
			).
				WithJobFunction("fetchDailySubFunc", map[string]interface{}{
					"stock_code": code,
					"index":      i,
				}).
				Build()
			if err != nil {
				continue
			}
			subTasks = append(subTasks, subTask)
		}

		if err := manager.AtomicAddSubTasks(subTasks, tc.TaskID); err != nil {
			return nil, fmt.Errorf("添加子任务失败: %v", err)
		}

		t.Logf("📊 [FetchDailyTemplate] 生成 %d 个子任务", len(subTasks))

		return map[string]interface{}{
			"generated_count": len(subTasks),
		}, nil
	}
	registry.Register(ctx, "fetchDailyTemplateFunc", fetchDailyTemplateFunc, "生成日线数据子任务")

	// 3. 子任务 - 获取单只股票日线数据
	fetchDailySubFunc := func(tc *task.TaskContext) (interface{}, error) {
		stockCode := tc.GetParamString("stock_code")
		index, _ := tc.GetParamInt("index")

		// 模拟获取日线数据
		dailyData := map[string]interface{}{
			"stock_code": stockCode,
			"open":       10.5 + float64(index)*0.1,
			"close":      10.8 + float64(index)*0.1,
			"high":       11.0 + float64(index)*0.1,
			"low":        10.2 + float64(index)*0.1,
			"volume":     1000000 + index*100000,
			"trade_date": "20251220",
		}

		t.Logf("📊 [FetchDailySub] 获取 %s 日线数据完成", stockCode)

		return map[string]interface{}{
			"daily_data": dailyData,
		}, nil
	}
	registry.Register(ctx, "fetchDailySubFunc", fetchDailySubFunc, "获取单只股票日线数据")

	// 4. 聚合任务 - 使用新 API 获取所有子任务结果
	aggregateDataFunc := func(tc *task.TaskContext) (interface{}, error) {
		// 使用新 API 获取子任务结果
		allResults := tc.GetSubTaskResults()
		successResults := tc.GetSuccessfulSubTaskResults()
		allSucceeded := tc.AllSubTasksSucceeded()
		subtaskCount := tc.GetSubTaskCount()

		t.Logf("📊 [AggregateData] 使用新 API:")
		t.Logf("   - 子任务总数: %d", subtaskCount)
		t.Logf("   - 所有结果数: %d", len(allResults))
		t.Logf("   - 成功结果数: %d", len(successResults))
		t.Logf("   - 全部成功: %v", allSucceeded)

		// 使用 ExtractMapsFromSubTasks 提取 daily_data
		dailyDataMaps := tc.ExtractMapsFromSubTasks("daily_data")
		t.Logf("📊 [AggregateData] 提取到 %d 条日线数据", len(dailyDataMaps))

		for _, data := range dailyDataMaps {
			t.Logf("   - %s: open=%.2f, close=%.2f",
				data["stock_code"], data["open"], data["close"])
		}

		capturedMutex.Lock()
		capturedDailyDataCount = len(dailyDataMaps)
		capturedAllSucceeded = allSucceeded
		capturedAggregatedData = dailyDataMaps
		capturedMutex.Unlock()

		return map[string]interface{}{
			"aggregated_count": len(dailyDataMaps),
			"all_succeeded":    allSucceeded,
		}, nil
	}
	registry.Register(ctx, "aggregateDataFunc", aggregateDataFunc, "聚合日线数据")

	// ========== 构建 Workflow ==========
	wf := workflow.NewWorkflow("stock-data-pipeline", "股票数据处理管道")

	// Task 1: 获取股票列表
	task1, _ := builder.NewTaskBuilder("FetchStockList", "获取股票列表", registry).
		WithJobFunction("fetchStockListFunc", nil).
		Build()

	// Task 2: 模板任务 - 生成日线数据子任务
	task2, _ := builder.NewTaskBuilder("FetchDailyData", "获取日线数据", registry).
		WithJobFunction("fetchDailyTemplateFunc", nil).
		WithDependency("FetchStockList").
		WithTemplate(true).
		Build()

	// Task 3: 聚合数据
	task3, _ := builder.NewTaskBuilder("AggregateData", "聚合数据", registry).
		WithJobFunction("aggregateDataFunc", nil).
		WithDependency("FetchDailyData").
		Build()

	wf.AddTask(task1)
	wf.AddTask(task2)
	wf.AddTask(task3)

	// ========== 执行 Workflow ==========
	controller, err := eng.SubmitWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("提交 Workflow 失败: %v", err)
	}

	// 等待完成
	deadline := time.Now().Add(60 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if time.Now().After(deadline) {
				t.Fatalf("Workflow 执行超时")
			}

			status, err := controller.GetStatus()
			if err != nil {
				continue
			}

			if status == "Success" || status == "Failed" || status == "Completed" {
				t.Logf("📊 Workflow 完成，状态: %s", status)
				goto verify
			}
		}
	}

verify:
	// ========== 验证结果 ==========
	capturedMutex.Lock()
	defer capturedMutex.Unlock()

	// 验证获取到股票代码
	if len(capturedStockCodes) != 5 {
		t.Errorf("期望获取 5 只股票代码，实际获取 %d 只", len(capturedStockCodes))
	}

	// 验证日线数据聚合
	if capturedDailyDataCount != 5 {
		t.Errorf("期望聚合 5 条日线数据，实际聚合 %d 条", capturedDailyDataCount)
	}

	// 验证全部成功
	if !capturedAllSucceeded {
		t.Error("期望所有子任务成功，实际有失败")
	}

	// 验证聚合数据内容
	if len(capturedAggregatedData) > 0 {
		for _, data := range capturedAggregatedData {
			if data["stock_code"] == nil {
				t.Error("聚合数据缺少 stock_code 字段")
			}
			if data["close"] == nil {
				t.Error("聚合数据缺少 close 字段")
			}
		}
	}

	t.Log("✅ E2E 测试通过：股票数据处理管道")
}

// TestE2E_UpstreamResultPassing_MultiLevelDependency 测试多层依赖的参数传递
// 场景：A -> B -> C，验证 C 能否获取 A 和 B 的结果
func TestE2E_UpstreamResultPassing_MultiLevelDependency(t *testing.T) {
	// 设置测试环境
	dataDir := filepath.Join(os.TempDir(), "task-engine-e2e-multi", time.Now().Format("20060102150405"))
	os.MkdirAll(dataDir, 0755)
	dbPath := filepath.Join(dataDir, "test.db")

	repos, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("创建 Repository 失败: %v", err)
	}
	defer repos.Close()

	eng, err := engine.NewEngine(10, 30, repos.Workflow, repos.WorkflowInstance, repos.Task)
	if err != nil {
		t.Fatalf("创建 Engine 失败: %v", err)
	}

	ctx := context.Background()
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("启动 Engine 失败: %v", err)
	}
	defer eng.Stop()

	registry := eng.GetRegistry()

	// 结果捕获
	var capturedFromA string
	var capturedFromB string
	var capturedUpstreamCount int
	capturedMutex := sync.Mutex{}

	// Task A
	taskAFunc := func(tc *task.TaskContext) (interface{}, error) {
		return map[string]interface{}{
			"message": "from_task_A",
			"level":   1,
		}, nil
	}
	registry.Register(ctx, "taskAFunc", taskAFunc, "任务A")

	// Task B (依赖 A)
	taskBFunc := func(tc *task.TaskContext) (interface{}, error) {
		// 使用新 API 获取 A 的结果
		messageFromA := tc.GetUpstreamString("TaskA", "message")
		t.Logf("📊 [TaskB] 从 TaskA 获取: message=%s", messageFromA)

		return map[string]interface{}{
			"message":       "from_task_B",
			"level":         2,
			"received_from": messageFromA,
		}, nil
	}
	registry.Register(ctx, "taskBFunc", taskBFunc, "任务B")

	// Task C (依赖 B，间接依赖 A)
	taskCFunc := func(tc *task.TaskContext) (interface{}, error) {
		// 使用新 API 获取所有上游结果
		allUpstream := tc.GetAllUpstreamResults()
		t.Logf("📊 [TaskC] 获取到 %d 个上游结果", len(allUpstream))

		// 获取 B 的结果
		messageFromB := tc.GetUpstreamString("TaskB", "message")
		receivedFromA := tc.GetUpstreamString("TaskB", "received_from")

		t.Logf("📊 [TaskC] 从 TaskB 获取: message=%s, received_from=%s", messageFromB, receivedFromA)

		capturedMutex.Lock()
		capturedFromA = receivedFromA
		capturedFromB = messageFromB
		capturedUpstreamCount = len(allUpstream)
		capturedMutex.Unlock()

		return map[string]interface{}{
			"final_message": fmt.Sprintf("%s -> %s", receivedFromA, messageFromB),
		}, nil
	}
	registry.Register(ctx, "taskCFunc", taskCFunc, "任务C")

	// 构建 Workflow
	wf := workflow.NewWorkflow("multi-level-test", "多层依赖测试")

	taskA, _ := builder.NewTaskBuilder("TaskA", "任务A", registry).
		WithJobFunction("taskAFunc", nil).
		Build()

	taskB, _ := builder.NewTaskBuilder("TaskB", "任务B", registry).
		WithJobFunction("taskBFunc", nil).
		WithDependency("TaskA").
		Build()

	taskC, _ := builder.NewTaskBuilder("TaskC", "任务C", registry).
		WithJobFunction("taskCFunc", nil).
		WithDependency("TaskB").
		Build()

	wf.AddTask(taskA)
	wf.AddTask(taskB)
	wf.AddTask(taskC)

	// 执行
	controller, err := eng.SubmitWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("提交 Workflow 失败: %v", err)
	}

	// 等待完成
	deadline := time.Now().Add(30 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if time.Now().After(deadline) {
				t.Fatalf("Workflow 执行超时")
			}

			status, _ := controller.GetStatus()
			if status == "Success" || status == "Failed" {
				t.Logf("📊 Workflow 完成，状态: %s", status)
				goto verify
			}
		}
	}

verify:
	capturedMutex.Lock()
	defer capturedMutex.Unlock()

	// 验证 C 收到了 B 传递的 A 的数据
	if capturedFromA != "from_task_A" {
		t.Errorf("期望 capturedFromA='from_task_A'，实际为 '%s'", capturedFromA)
	}

	if capturedFromB != "from_task_B" {
		t.Errorf("期望 capturedFromB='from_task_B'，实际为 '%s'", capturedFromB)
	}

	// C 只直接依赖 B，所以只有 1 个直接上游
	if capturedUpstreamCount < 1 {
		t.Errorf("期望至少 1 个上游结果，实际为 %d", capturedUpstreamCount)
	}

	t.Log("✅ E2E 测试通过：多层依赖参数传递")
}

// TestE2E_UpstreamResultPassing_ParallelSubTasks 测试并行子任务的结果聚合
// 场景：模板任务生成多个并行子任务，部分成功部分失败，验证聚合结果
func TestE2E_UpstreamResultPassing_ParallelSubTasks(t *testing.T) {
	// 设置测试环境
	dataDir := filepath.Join(os.TempDir(), "task-engine-e2e-parallel", time.Now().Format("20060102150405"))
	os.MkdirAll(dataDir, 0755)
	dbPath := filepath.Join(dataDir, "test.db")

	repos, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("创建 Repository 失败: %v", err)
	}
	defer repos.Close()

	eng, err := engine.NewEngine(10, 30, repos.Workflow, repos.WorkflowInstance, repos.Task)
	if err != nil {
		t.Fatalf("创建 Engine 失败: %v", err)
	}

	ctx := context.Background()
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("启动 Engine 失败: %v", err)
	}
	defer eng.Stop()

	registry := eng.GetRegistry()

	// 结果捕获
	var capturedSuccessCount int
	var capturedFailedCount int
	var capturedSubTaskCount int
	var capturedAllSucceeded bool
	capturedMutex := sync.Mutex{}

	// 模板任务
	templateFunc := func(tc *task.TaskContext) (interface{}, error) {
		type ManagerInterface interface {
			AtomicAddSubTasks(subTasks []types.Task, parentTaskID string) error
		}
		manager := tc.GetInstanceManager().(ManagerInterface)

		// 生成 10 个子任务，其中 index=3 和 index=7 会失败
		subTasks := make([]types.Task, 0, 10)
		for i := 0; i < 10; i++ {
			subTask, _ := builder.NewTaskBuilder(
				fmt.Sprintf("parallel-sub-%d", i),
				fmt.Sprintf("并行子任务 %d", i),
				registry,
			).
				WithJobFunction("parallelSubFunc", map[string]interface{}{
					"index": i,
				}).
				Build()
			subTasks = append(subTasks, subTask)
		}

		manager.AtomicAddSubTasks(subTasks, tc.TaskID)
		t.Logf("📊 [Template] 生成 %d 个并行子任务", len(subTasks))

		return map[string]interface{}{"generated": len(subTasks)}, nil
	}
	registry.Register(ctx, "templateFunc", templateFunc, "模板任务")

	// 子任务 (index=3 和 index=7 失败)
	parallelSubFunc := func(tc *task.TaskContext) (interface{}, error) {
		index, _ := tc.GetParamInt("index")

		// 模拟部分失败
		if index == 3 || index == 7 {
			return nil, fmt.Errorf("子任务 %d 模拟失败", index)
		}

		return map[string]interface{}{
			"result": fmt.Sprintf("success_%d", index),
			"index":  index,
		}, nil
	}
	registry.Register(ctx, "parallelSubFunc", parallelSubFunc, "并行子任务")

	// 聚合任务
	aggregateFunc := func(tc *task.TaskContext) (interface{}, error) {
		successResults := tc.GetSuccessfulSubTaskResults()
		failedResults := tc.GetFailedSubTaskResults()
		allSucceeded := tc.AllSubTasksSucceeded()
		subtaskCount := tc.GetSubTaskCount()

		t.Logf("📊 [Aggregate] 结果统计:")
		t.Logf("   - 子任务总数: %d", subtaskCount)
		t.Logf("   - 成功: %d", len(successResults))
		t.Logf("   - 失败: %d", len(failedResults))
		t.Logf("   - 全部成功: %v", allSucceeded)

		// 打印失败详情
		for _, r := range failedResults {
			t.Logf("   - 失败任务: %s, 错误: %s", r.TaskName, r.Error)
		}

		capturedMutex.Lock()
		capturedSuccessCount = len(successResults)
		capturedFailedCount = len(failedResults)
		capturedSubTaskCount = subtaskCount
		capturedAllSucceeded = allSucceeded
		capturedMutex.Unlock()

		return map[string]interface{}{
			"success_count": len(successResults),
			"failed_count":  len(failedResults),
		}, nil
	}
	registry.Register(ctx, "aggregateFunc", aggregateFunc, "聚合任务")

	// 构建 Workflow
	wf := workflow.NewWorkflow("parallel-subtasks-test", "并行子任务测试")

	task1, _ := builder.NewTaskBuilder("ParallelTemplate", "并行模板任务", registry).
		WithJobFunction("templateFunc", nil).
		WithTemplate(true).
		Build()

	task2, _ := builder.NewTaskBuilder("Aggregate", "聚合结果", registry).
		WithJobFunction("aggregateFunc", nil).
		WithDependency("ParallelTemplate").
		Build()

	wf.AddTask(task1)
	wf.AddTask(task2)

	// 执行
	controller, _ := eng.SubmitWorkflow(ctx, wf)

	// 等待完成
	deadline := time.Now().Add(60 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if time.Now().After(deadline) {
				t.Fatalf("Workflow 执行超时")
			}

			status, _ := controller.GetStatus()
			if status == "Success" || status == "Failed" {
				t.Logf("📊 Workflow 完成，状态: %s", status)
				goto verify
			}
		}
	}

verify:
	capturedMutex.Lock()
	defer capturedMutex.Unlock()

	// 验证结果
	if capturedSubTaskCount != 10 {
		t.Errorf("期望 10 个子任务，实际 %d 个", capturedSubTaskCount)
	}

	if capturedSuccessCount != 8 {
		t.Errorf("期望 8 个成功子任务，实际 %d 个", capturedSuccessCount)
	}

	if capturedFailedCount != 2 {
		t.Errorf("期望 2 个失败子任务，实际 %d 个", capturedFailedCount)
	}

	if capturedAllSucceeded {
		t.Error("期望 AllSubTasksSucceeded=false，实际为 true")
	}

	t.Log("✅ E2E 测试通过：并行子任务结果聚合")
}
