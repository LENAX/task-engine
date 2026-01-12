package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/LENAX/task-engine/internal/storage/sqlite"
	"github.com/LENAX/task-engine/pkg/core/builder"
	"github.com/LENAX/task-engine/pkg/core/engine"
	"github.com/LENAX/task-engine/pkg/core/task"
)

// ========== 业务场景：股票数据采集流程 ==========
// 这是一个贴近实际业务的示例，展示了如何使用模板任务动态生成子任务
// 业务场景：
// 1. 获取股票列表（普通任务）
// 2. 为每个股票获取日线数据（模板任务，动态生成子任务）
// 3. 数据汇总（普通任务，依赖所有子任务完成）

// ========== 示例Job函数实现 ==========

// FetchStockList 获取股票列表（模拟业务函数）
// 返回股票代码列表，供下游模板任务使用
func FetchStockList(tc *task.TaskContext) (interface{}, error) {
	log.Printf("📊 [FetchStockList] 开始获取股票列表")

	// 模拟从数据源获取股票列表
	// 在实际业务中，这里可能是调用API、查询数据库等
	stockCodes := []string{
		"000001.SZ", // 平安银行
		"000002.SZ", // 万科A
		"600000.SH", // 浦发银行
		"600036.SH", // 招商银行
		"600519.SH", // 贵州茅台
	}

	log.Printf("✅ [FetchStockList] 获取到 %d 个股票代码: %v", len(stockCodes), stockCodes)

	// 返回结果，供下游任务使用
	return map[string]interface{}{
		"count":       len(stockCodes),
		"stock_codes": stockCodes,
	}, nil
}

// GenerateDailyDataSubTasks 生成日线数据子任务的Job Function（模板任务使用）
// 这个函数会从上游任务的结果中提取股票代码，为每个股票生成一个子任务
func GenerateDailyDataSubTasks(tc *task.TaskContext) (interface{}, error) {
	log.Printf("🔧 [GenerateDailyDataSubTasks] 开始生成子任务")

	// 获取Engine依赖（用于添加子任务）
	engineInterface, ok := tc.GetDependency("Engine")
	if !ok {
		return nil, fmt.Errorf("未找到Engine依赖")
	}
	eng, ok := engineInterface.(*engine.Engine)
	if !ok {
		return nil, fmt.Errorf("Engine类型转换失败")
	}

	registry := eng.GetRegistry()
	if registry == nil {
		return nil, fmt.Errorf("无法获取Registry")
	}

	// 从上游任务结果中提取股票代码列表
	// 上游任务的结果会通过 _cached_ 参数传递下来
	var stockCodes []string
	for key, val := range tc.Params {
		if key == "_cached_FetchStockList" {
			if resultMap, ok := val.(map[string]interface{}); ok {
				if codesRaw, ok := resultMap["stock_codes"]; ok {
					switch v := codesRaw.(type) {
					case []string:
						stockCodes = v
					case []interface{}:
						for _, item := range v {
							if s, ok := item.(string); ok {
								stockCodes = append(stockCodes, s)
							}
						}
					}
				}
			}
		}
	}

	if len(stockCodes) == 0 {
		log.Printf("⚠️ [GenerateDailyDataSubTasks] 未找到股票代码，跳过子任务生成")
		return map[string]interface{}{
			"status":    "no_data",
			"generated": 0,
			"message":   "未找到股票代码",
		}, nil
	}

	log.Printf("📡 [GenerateDailyDataSubTasks] 从上游任务获取到 %d 个股票代码: %v", len(stockCodes), stockCodes)

	// 为每个股票代码生成一个子任务
	parentTaskID := tc.TaskID
	workflowInstanceID := tc.WorkflowInstanceID
	generatedCount := 0

	var subTaskInfos []map[string]interface{}
	for _, stockCode := range stockCodes {
		subTaskName := fmt.Sprintf("获取日线数据_%s", stockCode)
		subTask, err := builder.NewTaskBuilder(subTaskName, fmt.Sprintf("获取%s的日线数据", stockCode), registry).
			WithJobFunction("FetchDailyData", map[string]interface{}{
				"stock_code": stockCode,
			}).
			WithTaskHandler(task.TaskStatusSuccess, "LogSuccess").
			WithTaskHandler(task.TaskStatusFailed, "LogError").
			Build()
		if err != nil {
			log.Printf("❌ [GenerateDailyDataSubTasks] 创建子任务失败: %s, error=%v", subTaskName, err)
			continue
		}

		bgCtx := context.Background()
		if err := eng.AddSubTaskToInstance(bgCtx, workflowInstanceID, subTask, parentTaskID); err != nil {
			log.Printf("❌ [GenerateDailyDataSubTasks] 添加子任务失败: %s, error=%v", subTaskName, err)
			continue
		}

		generatedCount++
		subTaskInfos = append(subTaskInfos, map[string]interface{}{
			"name":       subTaskName,
			"stock_code": stockCode,
		})
		log.Printf("✅ [GenerateDailyDataSubTasks] 子任务已添加: %s (stock_code=%s)", subTaskName, stockCode)
	}

	log.Printf("✅ [GenerateDailyDataSubTasks] 共生成 %d 个子任务", generatedCount)

	return map[string]interface{}{
		"status":    "success",
		"generated": generatedCount,
		"sub_tasks": subTaskInfos,
	}, nil
}

// FetchDailyData 获取单个股票的日线数据（子任务使用）
func FetchDailyData(tc *task.TaskContext) (interface{}, error) {
	// 从参数中获取股票代码
	stockCode := tc.GetParamString("stock_code")
	if stockCode == "" {
		return nil, fmt.Errorf("未找到 stock_code 参数")
	}

	log.Printf("📈 [FetchDailyData] 开始获取股票 %s 的日线数据", stockCode)

	// 模拟数据获取（在实际业务中，这里可能是调用API、查询数据库等）
	time.Sleep(100 * time.Millisecond) // 模拟网络请求

	// 模拟返回数据
	dataCount := 20 // 假设获取到20条日线数据
	log.Printf("✅ [FetchDailyData] 股票 %s 获取完成，共 %d 条数据", stockCode, dataCount)

	return map[string]interface{}{
		"stock_code": stockCode,
		"count":      dataCount,
		"status":     "success",
	}, nil
}

// AggregateData 数据汇总任务（依赖所有子任务完成）
func AggregateData(tc *task.TaskContext) (interface{}, error) {
	log.Printf("📊 [AggregateData] 开始汇总数据")

	// 在实际业务中，这里可能是：
	// 1. 从数据库查询所有子任务的结果
	// 2. 进行数据聚合、统计、分析
	// 3. 生成报告等

	time.Sleep(50 * time.Millisecond) // 模拟处理时间

	totalCount := 100 // 假设汇总了100条数据
	log.Printf("✅ [AggregateData] 数据汇总完成，总计 %d 条数据", totalCount)

	return map[string]interface{}{
		"total_count": totalCount,
		"status":      "success",
	}, nil
}

// ========== 示例Handler函数 ==========

// LogSuccess 成功日志Handler
func LogSuccess(tc *task.TaskContext) {
	log.Printf("✅ [任务成功] %s (TaskID=%s)", tc.TaskName, tc.TaskID)
}

// LogError 错误日志Handler
func LogError(tc *task.TaskContext) {
	errMsg := tc.GetParamString("_error_message")
	log.Printf("❌ [任务失败] %s (TaskID=%s): %s", tc.TaskName, tc.TaskID, errMsg)
}

// ========== 主函数 ==========

func main() {
	log.Println("========== 股票数据采集流程示例 ==========")

	// ========== 1. 创建临时数据库 ==========
	tmpDir := filepath.Join(os.TempDir(), "task-engine-example", time.Now().Format("20060102150405"))
	os.MkdirAll(tmpDir, 0755)
	dbPath := filepath.Join(tmpDir, "engine.db")
	log.Printf("📁 数据库路径: %s", dbPath)

	// ========== 2. 创建Repository和Engine ==========
	repos, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		log.Fatalf("❌ 创建Repository失败: %v", err)
	}
	defer repos.Close()

	// 创建Engine（10个并发worker，60秒超时）
	eng, err := engine.NewEngine(10, 60, repos.Workflow, repos.WorkflowInstance, repos.Task)
	if err != nil {
		log.Fatalf("❌ 创建Engine失败: %v", err)
	}

	// ========== 3. 启动Engine ==========
	bgCtx := context.Background()
	if err := eng.Start(bgCtx); err != nil {
		log.Fatalf("❌ 启动Engine失败: %v", err)
	}
	defer eng.Stop()
	log.Println("✅ Engine已启动")

	// ========== 4. 注册函数 ==========
	registry := eng.GetRegistry()

	// 注册依赖（Engine自身，用于模板任务生成子任务）
	registry.RegisterDependencyWithKey("Engine", eng)

	// 注册Job函数
	registry.Register(bgCtx, "FetchStockList", FetchStockList, "获取股票列表")
	registry.Register(bgCtx, "GenerateDailyDataSubTasks", GenerateDailyDataSubTasks, "生成日线数据子任务（模板任务）")
	registry.Register(bgCtx, "FetchDailyData", FetchDailyData, "获取单个股票的日线数据")
	registry.Register(bgCtx, "AggregateData", AggregateData, "数据汇总")

	// 注册Task Handler
	registry.RegisterTaskHandler(bgCtx, "LogSuccess", LogSuccess, "记录成功")
	registry.RegisterTaskHandler(bgCtx, "LogError", LogError, "记录错误")

	log.Println("✅ 函数注册完成")

	// ========== 5. 构建Workflow ==========
	// 任务结构：
	// Level 0: 获取股票列表
	// Level 1: 生成日线数据子任务（模板任务，依赖获取股票列表）
	// Level 2: 动态生成的子任务（获取日线数据_000001.SZ, 获取日线数据_000002.SZ, ...）
	// Level 3: 数据汇总（依赖所有子任务完成）

	// 任务1: 获取股票列表
	task1, err := builder.NewTaskBuilder("获取股票列表", "获取股票代码列表", registry).
		WithJobFunction("FetchStockList", nil).
		WithTaskHandler(task.TaskStatusSuccess, "LogSuccess").
		WithTaskHandler(task.TaskStatusFailed, "LogError").
		Build()
	if err != nil {
		log.Fatalf("❌ 构建任务1失败: %v", err)
	}

	// 任务2: 生成日线数据子任务（模板任务）
	// 注意：模板任务需要在Job Function中生成子任务，而不是在Handler中
	task2, err := builder.NewTaskBuilder("生成日线数据子任务", "为每个股票生成日线数据子任务", registry).
		WithJobFunction("GenerateDailyDataSubTasks", nil).
		WithDependency("获取股票列表"). // 依赖任务1
		WithTaskHandler(task.TaskStatusSuccess, "LogSuccess").
		WithTaskHandler(task.TaskStatusFailed, "LogError").
		WithTemplate(true). // 标记为模板任务
		Build()
	if err != nil {
		log.Fatalf("❌ 构建任务2失败: %v", err)
	}

	// 任务3: 数据汇总（依赖模板任务，实际上会等待所有子任务完成）
	task3, err := builder.NewTaskBuilder("数据汇总", "汇总所有股票的数据", registry).
		WithJobFunction("AggregateData", nil).
		WithDependency("生成日线数据子任务"). // 依赖模板任务
		WithTaskHandler(task.TaskStatusSuccess, "LogSuccess").
		WithTaskHandler(task.TaskStatusFailed, "LogError").
		Build()
	if err != nil {
		log.Fatalf("❌ 构建任务3失败: %v", err)
	}

	// 构建Workflow
	wf, err := builder.NewWorkflowBuilder("股票数据采集流程", "获取股票列表并采集日线数据").
		WithTask(task1).
		WithTask(task2).
		WithTask(task3).
		Build()
	if err != nil {
		log.Fatalf("❌ 构建Workflow失败: %v", err)
	}

	log.Println("✅ Workflow构建完成")

	// ========== 6. 提交Workflow ==========
	controller, err := eng.SubmitWorkflow(bgCtx, wf)
	if err != nil {
		log.Fatalf("❌ 提交Workflow失败: %v", err)
	}

	log.Printf("✅ Workflow已提交，实例ID: %s", controller.GetInstanceID())

	// ========== 7. 等待Workflow完成 ==========
	log.Println("⏳ 等待Workflow执行完成...")
	startTime := time.Now()
	for {
		status := controller.Status()
		log.Printf("📊 当前状态: %s", status)

		if status == "Success" || status == "Failed" || status == "Terminated" {
			duration := time.Since(startTime)
			log.Printf("✅ Workflow执行完成，状态: %s，耗时: %v", status, duration)
			break
		}

		// 超时检查（最多等待60秒）
		if time.Since(startTime) > 60*time.Second {
			log.Fatalf("❌ Workflow执行超时")
		}

		time.Sleep(500 * time.Millisecond)
	}

	// ========== 8. 输出结果 ==========
	log.Println("\n========== 执行结果 ==========")
	log.Printf("Workflow状态: %s", controller.Status())
	log.Printf("数据库路径: %s", dbPath)
	log.Println("✅ 示例执行完成")
}
