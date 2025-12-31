package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stevelan1995/task-engine/internal/storage/sqlite"
	"github.com/stevelan1995/task-engine/pkg/core/builder"
	"github.com/stevelan1995/task-engine/pkg/core/engine"
	"github.com/stevelan1995/task-engine/pkg/core/task"
	"github.com/stevelan1995/task-engine/pkg/storage"
)

// ==================== 数据结构定义 ====================

// 预设模拟数据数量常量
const (
	// 交易日历数据：5个交易日
	ExpectedTradeCalDates = 5

	// 股票列表数据：5只股票
	ExpectedStockCount = 5

	// 预期子任务生成数量
	ExpectedDailySubTaskCount     = 5 // daily应该为5个交易日各生成1个子任务
	ExpectedAdjFactorSubTaskCount = 5 // adj_factor应该为5只股票各生成1个子任务

	// 预期数据总数（如果动态子任务完全实现）
	// 5个trade_cal（每个交易日1条）+ 5个stock_basic（每只股票1条）+ 5个daily（每个子任务1条）+ 5个adj_factor（每个子任务1条）= 20条
	ExpectedTotalDataCountWithDynamicTasks = 20

	// 预期数据总数（只执行第一组任务）
	// TestTushareWorkflow_Basic: 5个trade_cal（每个交易日1条）+ 5个stock_basic（每只股票1条）= 10条
	ExpectedTotalDataCountBasic = 10

	// TestTushareWorkflow_WithDependencies: 5个trade_cal + 5个stock_basic + 1个daily + 1个adj_factor = 12条
	ExpectedTotalDataCountWithDependencies = 12
)

// TradeCalResult 交易日历数据结果
type TradeCalResult struct {
	Exchange string   `json:"exchange"`
	CalDates []string `json:"cal_dates"` // yyyymmdd格式
	IsOpen   []string `json:"is_open"`
	PreDates []string `json:"pre_dates"`
}

// StockBasicResult 股票列表数据结果
type StockBasicResult struct {
	TSCodes    []string `json:"ts_codes"` // 股票代码列表
	Symbols    []string `json:"symbols"`
	Names      []string `json:"names"`
	Areas      []string `json:"areas"`
	Industries []string `json:"industries"`
	ListDates  []string `json:"list_dates"`
}

// DailyResult 日线数据结果
// 根据需求文档：ts_code(str), trade_date(str), open(str), high(float), low(float), close(str), pre_close(float), change(float), pct_chg(float), vol(int), amount(float)
type DailyResult struct {
	TSCode    string  `json:"ts_code"`
	TradeDate string  `json:"trade_date"`
	Open      string  `json:"open"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Close     string  `json:"close"`
	PreClose  float64 `json:"pre_close"` // 需求文档要求是 float
	Change    float64 `json:"change"`
	PctChg    float64 `json:"pct_chg"`
	Vol       int     `json:"vol"`
	Amount    float64 `json:"amount"`
}

// AdjFactorResult 复权因子结果
type AdjFactorResult struct {
	TSCode    string  `json:"ts_code"`
	TradeDate string  `json:"trade_date"`
	AdjFactor float64 `json:"adj_factor"`
}

// QuantDataRepository 模拟的数据仓库（依赖注入）
type QuantDataRepository struct {
	savedData []map[string]interface{}
}

func NewQuantDataRepository() *QuantDataRepository {
	return &QuantDataRepository{
		savedData: make([]map[string]interface{}, 0),
	}
}

func (r *QuantDataRepository) Save(data map[string]interface{}) error {
	r.savedData = append(r.savedData, data)
	log.Printf("💾 [保存数据] 类型=%v, 数据=%v", data["type"], data)
	return nil
}

func (r *QuantDataRepository) GetSavedData() []map[string]interface{} {
	return r.savedData
}

// ==================== 任务函数实现 ====================

// QueryTushare 模拟Tushare API查询
func QueryTushare(ctx *task.TaskContext) (interface{}, error) {
	apiName := ctx.GetParamString("api_name")
	log.Printf("📡 [QueryTushare] API=%s, 开始查询...", apiName)

	// 模拟API调用延迟
	time.Sleep(50 * time.Millisecond)

	switch apiName {
	case "trade_cal":
		// 模拟返回交易日历数据
		result := TradeCalResult{
			Exchange: "SSE",
			CalDates: []string{"20251201", "20251202", "20251203", "20251204", "20251205"},
			IsOpen:   []string{"1", "1", "1", "0", "1"},
			PreDates: []string{"20251130", "20251201", "20251202", "20251203", "20251204"},
		}
		log.Printf("✅ [QueryTushare] trade_cal 查询成功，返回 %d 条记录", len(result.CalDates))
		return result, nil

	case "stock_basic":
		// 模拟返回股票列表数据
		result := StockBasicResult{
			TSCodes:    []string{"000001.SZ", "000002.SZ", "000003.SZ", "000004.SZ", "000005.SZ"},
			Symbols:    []string{"000001", "000002", "000003", "000004", "000005"},
			Names:      []string{"平安银行", "万科A", "国农科技", "华联控股", "世纪星源"},
			Areas:      []string{"深圳", "深圳", "深圳", "深圳", "深圳"},
			Industries: []string{"银行", "房地产", "综合", "房地产", "综合"},
			ListDates:  []string{"19910403", "19910129", "19910412", "19920106", "19900303"},
		}
		log.Printf("✅ [QueryTushare] stock_basic 查询成功，返回 %d 条记录", len(result.TSCodes))
		return result, nil

	case "daily":
		// 模拟返回日线数据
		// 根据需求文档，返回参数：ts_code(str), trade_date(str), open(str), high(float), low(float), close(str), pre_close(float), change(float), pct_chg(float), vol(int), amount(float)
		tradeDate := ctx.GetParamString("trade_date")
		result := DailyResult{
			TSCode:    ctx.GetParamString("ts_code"),
			TradeDate: tradeDate,
			Open:      "10.50",
			High:      10.80,
			Low:       10.30,
			Close:     "10.60",
			PreClose:  10.40, // 需求文档要求是 float
			Change:    0.20,
			PctChg:    1.92,
			Vol:       1000000,
			Amount:    10600000.0,
		}
		log.Printf("✅ [QueryTushare] daily 查询成功，ts_code=%s, trade_date=%s", result.TSCode, result.TradeDate)
		return result, nil

	case "adj_factor":
		// 模拟返回复权因子数据
		tsCode := ctx.GetParamString("ts_code")
		result := AdjFactorResult{
			TSCode:    tsCode,
			TradeDate: "20251201",
			AdjFactor: 1.0,
		}
		log.Printf("✅ [QueryTushare] adj_factor 查询成功，ts_code=%s", tsCode)
		return result, nil

	default:
		return nil, fmt.Errorf("未知的API名称: %s", apiName)
	}
}

// GenerateSubTasks 根据依赖任务的结果生成子任务
// 这个函数作为Success Handler被调用，从结果数据中提取信息并生成子任务
func GenerateSubTasks(ctx *task.TaskContext) {
	// 获取任务结果数据
	resultData := ctx.GetParam("_result_data")
	if resultData == nil {
		log.Printf("⚠️ [GenerateSubTasks] 未找到结果数据")
		return
	}

	// 获取父任务名称和ID
	parentTaskName := ctx.TaskName
	parentTaskID := ctx.TaskID
	workflowInstanceID := ctx.WorkflowInstanceID
	log.Printf("🔄 [GenerateSubTasks] 父任务=%s (ID=%s), 开始生成子任务...", parentTaskName, parentTaskID)

	// 获取Engine依赖（通过依赖注入）
	engineInterface, ok := ctx.GetDependency("Engine")
	if !ok {
		log.Printf("⚠️ [GenerateSubTasks] 未找到Engine依赖，无法添加子任务")
		return
	}
	eng, ok := engineInterface.(*engine.Engine)
	if !ok {
		log.Printf("⚠️ [GenerateSubTasks] Engine类型转换失败")
		return
	}

	// 获取Registry（用于创建子任务）
	registry := eng.GetRegistry()
	if registry == nil {
		log.Printf("⚠️ [GenerateSubTasks] 无法获取Registry")
		return
	}

	// 根据父任务类型生成不同的子任务
	switch parentTaskName {
	case "获取交易日历":
		// 从交易日历结果中提取日期，生成日线任务
		// 注意：应该为所有5个交易日生成子任务，不管是否开盘
		if tradeCalResult, ok := resultData.(TradeCalResult); ok {
			log.Printf("📝 [GenerateSubTasks] 交易日历结果: %d 个交易日", len(tradeCalResult.CalDates))
			generatedCount := 0
			// 为所有交易日生成子任务（不管是否开盘）
			for _, calDate := range tradeCalResult.CalDates {
				log.Printf("📝 [GenerateSubTasks] 生成日线任务: trade_date=%s", calDate)

				// 创建子任务（使用TaskBuilder）
				subTaskName := fmt.Sprintf("获取日线数据_%s", calDate)
				subTask, err := builder.NewTaskBuilder(subTaskName, fmt.Sprintf("获取%s的日线数据", calDate), registry).
					WithJobFunction("QueryTushare", map[string]interface{}{
						"api_name":   "daily",
						"trade_date": calDate,
						"ts_code":    "000001.SZ", // 默认股票代码，实际应该从stock_basic获取
					}).
					WithDependency(parentTaskName). // 子任务依赖父任务
					WithTaskHandler(task.TaskStatusSuccess, "SaveResult").
					WithTaskHandler(task.TaskStatusFailed, "LogError").
					Build()
				if err != nil {
					log.Printf("❌ [GenerateSubTasks] 创建daily子任务失败: trade_date=%s, error=%v", calDate, err)
					continue
				}

				// 添加子任务到WorkflowInstance
				context := context.Background()
				if err := eng.AddSubTaskToInstance(context, workflowInstanceID, subTask, parentTaskID); err != nil {
					log.Printf("❌ [GenerateSubTasks] 添加daily子任务失败: trade_date=%s, error=%v", calDate, err)
					continue
				}

				generatedCount++
				log.Printf("✅ [GenerateSubTasks] daily子任务已添加: %s (ID=%s)", subTaskName, subTask.GetID())
			}
			log.Printf("✅ [GenerateSubTasks] 共生成 %d 个daily子任务（预期: %d）", generatedCount, ExpectedDailySubTaskCount)
			if generatedCount != ExpectedDailySubTaskCount {
				log.Printf("⚠️ [GenerateSubTasks] daily子任务数量不符合预期: 期望=%d, 实际=%d", ExpectedDailySubTaskCount, generatedCount)
			}
		}

	case "获取股票列表":
		// 从股票列表结果中提取股票代码，生成复权因子任务
		if stockBasicResult, ok := resultData.(StockBasicResult); ok {
			log.Printf("📝 [GenerateSubTasks] 股票列表结果: %d 只股票", len(stockBasicResult.TSCodes))
			generatedCount := 0
			// 为所有股票生成子任务
			for _, tsCode := range stockBasicResult.TSCodes {
				log.Printf("📝 [GenerateSubTasks] 生成复权因子任务: ts_code=%s", tsCode)

				// 创建子任务（使用TaskBuilder）
				subTaskName := fmt.Sprintf("获取复权因子_%s", tsCode)
				subTask, err := builder.NewTaskBuilder(subTaskName, fmt.Sprintf("获取%s的复权因子", tsCode), registry).
					WithJobFunction("QueryTushare", map[string]interface{}{
						"api_name": "adj_factor",
						"ts_code":  tsCode,
					}).
					WithDependency(parentTaskName). // 子任务依赖父任务
					WithTaskHandler(task.TaskStatusSuccess, "SaveResult").
					WithTaskHandler(task.TaskStatusFailed, "LogError").
					Build()
				if err != nil {
					log.Printf("❌ [GenerateSubTasks] 创建adj_factor子任务失败: ts_code=%s, error=%v", tsCode, err)
					continue
				}

				// 添加子任务到WorkflowInstance
				context := context.Background()
				if err := eng.AddSubTaskToInstance(context, workflowInstanceID, subTask, parentTaskID); err != nil {
					log.Printf("❌ [GenerateSubTasks] 添加adj_factor子任务失败: ts_code=%s, error=%v", tsCode, err)
					continue
				}

				generatedCount++
				log.Printf("✅ [GenerateSubTasks] adj_factor子任务已添加: %s (ID=%s)", subTaskName, subTask.GetID())
			}
			log.Printf("✅ [GenerateSubTasks] 共生成 %d 个adj_factor子任务（预期: %d）", generatedCount, ExpectedAdjFactorSubTaskCount)
			if generatedCount != ExpectedAdjFactorSubTaskCount {
				log.Printf("⚠️ [GenerateSubTasks] adj_factor子任务数量不符合预期: 期望=%d, 实际=%d", ExpectedAdjFactorSubTaskCount, generatedCount)
			}
		}
	}
}

// SaveResult 保存结果数据（Success Handler）
func SaveResult(ctx *task.TaskContext) {
	log.Printf("💾 [SaveResult] 被调用，TaskName=%s, TaskID=%s", ctx.TaskName, ctx.TaskID)

	// 获取结果数据（尝试多个可能的参数名）
	resultData := ctx.GetParam("_result_data")
	if resultData == nil {
		resultData = ctx.GetParam("result")
	}
	if resultData == nil {
		// 尝试从所有参数中查找结果数据
		for k, v := range ctx.Params {
			if k != "_status" && k != "_previous_status" && k != "_error_message" {
				resultData = v
				log.Printf("📝 [SaveResult] 从参数 %s 获取结果数据", k)
				break
			}
		}
	}
	if resultData == nil {
		log.Printf("⚠️ [SaveResult] 未找到结果数据，所有参数键: %v", func() []string {
			keys := make([]string, 0, len(ctx.Params))
			for k := range ctx.Params {
				keys = append(keys, k)
			}
			return keys
		}())
		return
	}

	// 获取数据仓库（通过依赖注入，使用字符串key）
	repoInterface, ok := ctx.GetDependency("QuantDataRepository")
	if !ok {
		log.Printf("⚠️ [SaveResult] 未找到QuantDataRepository依赖")
		return
	}
	repo, ok := repoInterface.(*QuantDataRepository)
	if !ok {
		log.Printf("⚠️ [SaveResult] QuantDataRepository类型转换失败")
		return
	}

	// 根据结果类型保存数据
	// 注意：trade_cal 和 stock_basic 应该为每个交易日/股票保存 1 条数据
	switch result := resultData.(type) {
	case TradeCalResult:
		// 为每个交易日保存 1 条数据
		for i, calDate := range result.CalDates {
			dataToSave := map[string]interface{}{
				"type":     "trade_cal",
				"exchange": result.Exchange,
				"cal_date": calDate,
				"is_open":  result.IsOpen[i],
				"pre_date": result.PreDates[i],
			}
			if err := repo.Save(dataToSave); err != nil {
				log.Printf("❌ [SaveResult] 保存trade_cal数据失败: %v", err)
			} else {
				log.Printf("✅ [SaveResult] trade_cal数据保存成功: cal_date=%s", calDate)
			}
		}
		// 注意：不在这里调用 GenerateSubTasks，而是通过配置 Handler 来控制
		// 如果需要在保存后生成子任务，应该配置 GenerateSubTasks 作为 Success Handler
		return
	case StockBasicResult:
		// 为每只股票保存 1 条数据
		for i, tsCode := range result.TSCodes {
			dataToSave := map[string]interface{}{
				"type":      "stock_basic",
				"ts_code":   tsCode,
				"symbol":    result.Symbols[i],
				"name":      result.Names[i],
				"area":      result.Areas[i],
				"industry":  result.Industries[i],
				"list_date": result.ListDates[i],
			}
			if err := repo.Save(dataToSave); err != nil {
				log.Printf("❌ [SaveResult] 保存stock_basic数据失败: %v", err)
			} else {
				log.Printf("✅ [SaveResult] stock_basic数据保存成功: ts_code=%s", tsCode)
			}
		}
		// 注意：不在这里调用 GenerateSubTasks，而是通过配置 Handler 来控制
		// 如果需要在保存后生成子任务，应该配置 GenerateSubTasks 作为 Success Handler
		return
	}

	// 对于其他类型（daily、adj_factor），保存单条数据
	var dataType string
	var dataToSave map[string]interface{}

	switch result := resultData.(type) {
	case DailyResult:
		dataType = "daily"
		dataToSave = map[string]interface{}{
			"type":       dataType,
			"ts_code":    result.TSCode,
			"trade_date": result.TradeDate,
			"open":       result.Open,
			"high":       result.High,
			"low":        result.Low,
			"close":      result.Close,
			"pre_close":  result.PreClose,
			"change":     result.Change,
			"pct_chg":    result.PctChg,
			"vol":        result.Vol,
			"amount":     result.Amount,
		}
	case AdjFactorResult:
		dataType = "adj_factor"
		dataToSave = map[string]interface{}{
			"type":       dataType,
			"ts_code":    result.TSCode,
			"trade_date": result.TradeDate,
			"adj_factor": result.AdjFactor,
		}
	default:
		// 尝试JSON序列化
		jsonData, err := json.Marshal(result)
		if err != nil {
			log.Printf("❌ [SaveResult] 无法序列化结果数据: %v", err)
			return
		}
		dataType = "unknown"
		dataToSave = map[string]interface{}{
			"type": dataType,
			"data": string(jsonData),
		}
	}

	// 保存数据
	if err := repo.Save(dataToSave); err != nil {
		log.Printf("❌ [SaveResult] 保存数据失败: %v", err)
	} else {
		log.Printf("✅ [SaveResult] 数据保存成功，类型=%s", dataType)
	}
}

// SaveResultAndGenerateSubTasks 保存结果数据并生成子任务（Success Handler）
// 这个 Handler 同时执行 SaveResult 和 GenerateSubTasks
func SaveResultAndGenerateSubTasks(ctx *task.TaskContext) {
	// 先执行 SaveResult
	SaveResult(ctx)
	// 然后执行 GenerateSubTasks（只有 trade_cal 和 stock_basic 会生成子任务）
	GenerateSubTasks(ctx)
}

// LogError 记录错误（Failed Handler）
func LogError(ctx *task.TaskContext) {
	errorMsg := ctx.GetParamString("_error_message")
	taskName := ctx.TaskName
	log.Printf("❌ [LogError] 任务=%s, 错误=%s", taskName, errorMsg)
}

// ==================== 字段完整性验证函数 ====================

// validateDailyDataFields 验证 daily 数据字段完整性
// 根据需求文档，daily 应该包含：ts_code(str), trade_date(str), open(str), high(float), low(float), close(str), pre_close(float), change(float), pct_chg(float), vol(int), amount(float)
func validateDailyDataFields(t *testing.T, data map[string]interface{}, index int) {
	requiredFields := []string{
		"type",       // 额外字段，用于标识数据类型
		"ts_code",    // str
		"trade_date", // str
		"open",       // str
		"high",       // float
		"low",        // float
		"close",      // str
		"pre_close",  // float
		"change",     // float
		"pct_chg",    // float
		"vol",        // int
		"amount",     // float
	}

	missingFields := make([]string, 0)
	for _, field := range requiredFields {
		if _, exists := data[field]; !exists {
			missingFields = append(missingFields, field)
		}
	}

	if len(missingFields) > 0 {
		t.Errorf("daily数据[%d]缺少必需字段: %v", index, missingFields)
	}

	// 验证字段类型
	if tsCode, ok := data["ts_code"].(string); !ok || tsCode == "" {
		t.Errorf("daily数据[%d] ts_code 字段类型错误或为空", index)
	}
	if tradeDate, ok := data["trade_date"].(string); !ok || tradeDate == "" {
		t.Errorf("daily数据[%d] trade_date 字段类型错误或为空", index)
	}
	if open, ok := data["open"].(string); !ok || open == "" {
		t.Errorf("daily数据[%d] open 字段类型错误或为空", index)
	}
	if _, ok := data["high"].(float64); !ok {
		t.Errorf("daily数据[%d] high 字段类型错误，期望 float64", index)
	}
	if _, ok := data["low"].(float64); !ok {
		t.Errorf("daily数据[%d] low 字段类型错误，期望 float64", index)
	}
	if close, ok := data["close"].(string); !ok || close == "" {
		t.Errorf("daily数据[%d] close 字段类型错误或为空", index)
	}
	if _, ok := data["pre_close"].(float64); !ok {
		t.Errorf("daily数据[%d] pre_close 字段类型错误，期望 float64", index)
	}
	if _, ok := data["change"].(float64); !ok {
		t.Errorf("daily数据[%d] change 字段类型错误，期望 float64", index)
	}
	if _, ok := data["pct_chg"].(float64); !ok {
		t.Errorf("daily数据[%d] pct_chg 字段类型错误，期望 float64", index)
	}
	if _, ok := data["vol"].(int); !ok {
		t.Errorf("daily数据[%d] vol 字段类型错误，期望 int", index)
	}
	if _, ok := data["amount"].(float64); !ok {
		t.Errorf("daily数据[%d] amount 字段类型错误，期望 float64", index)
	}
}

// validateAdjFactorDataFields 验证 adj_factor 数据字段完整性
// 根据需求文档，adj_factor 应该包含：ts_code(str), trade_date(str), adj_factor(float)
func validateAdjFactorDataFields(t *testing.T, data map[string]interface{}, index int) {
	requiredFields := []string{
		"type",       // 额外字段，用于标识数据类型
		"ts_code",    // str
		"trade_date", // str
		"adj_factor", // float
	}

	missingFields := make([]string, 0)
	for _, field := range requiredFields {
		if _, exists := data[field]; !exists {
			missingFields = append(missingFields, field)
		}
	}

	if len(missingFields) > 0 {
		t.Errorf("adj_factor数据[%d]缺少必需字段: %v", index, missingFields)
	}

	// 验证字段类型
	if tsCode, ok := data["ts_code"].(string); !ok || tsCode == "" {
		t.Errorf("adj_factor数据[%d] ts_code 字段类型错误或为空", index)
	}
	if tradeDate, ok := data["trade_date"].(string); !ok || tradeDate == "" {
		t.Errorf("adj_factor数据[%d] trade_date 字段类型错误或为空", index)
	}
	if _, ok := data["adj_factor"].(float64); !ok {
		t.Errorf("adj_factor数据[%d] adj_factor 字段类型错误，期望 float64", index)
	}
}

// validateTradeCalDataFields 验证 trade_cal 数据字段完整性
// 根据需求文档，trade_cal 应该包含：exchange(str), cal_date(str, yyyymmdd), is_open(str), pre_date(str)
func validateTradeCalDataFields(t *testing.T, data map[string]interface{}, index int) {
	requiredFields := []string{
		"type",     // 额外字段
		"exchange", // str
		"cal_date", // str
		"is_open",  // str
		"pre_date", // str
	}

	missingFields := make([]string, 0)
	for _, field := range requiredFields {
		if _, exists := data[field]; !exists {
			missingFields = append(missingFields, field)
		}
	}

	if len(missingFields) > 0 {
		t.Errorf("trade_cal数据[%d]缺少必需字段: %v", index, missingFields)
	}
}

// validateStockBasicDataFields 验证 stock_basic 数据字段完整性
// 根据需求文档，stock_basic 应该包含：ts_code(str), symbol(str), name(str), area(str), industry(str), list_date(str)
func validateStockBasicDataFields(t *testing.T, data map[string]interface{}, index int) {
	requiredFields := []string{
		"type",      // 额外字段
		"ts_code",   // str
		"symbol",    // str
		"name",      // str
		"area",      // str
		"industry",  // str
		"list_date", // str
	}

	missingFields := make([]string, 0)
	for _, field := range requiredFields {
		if _, exists := data[field]; !exists {
			missingFields = append(missingFields, field)
		}
	}

	if len(missingFields) > 0 {
		t.Errorf("stock_basic数据[%d]缺少必需字段: %v", index, missingFields)
	}
}

// ==================== 测试函数 ====================

func setupTushareTest(t *testing.T) (*engine.Engine, *task.FunctionRegistry, *QuantDataRepository, storage.TaskRepository, func()) {
	// 创建临时数据库
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "tushare_test.db")

	// 创建Repository
	repos, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("创建Repository失败: %v", err)
	}

	// 创建Engine
	eng, err := engine.NewEngine(10, 30, repos.Workflow, repos.WorkflowInstance, repos.Task)
	if err != nil {
		t.Fatalf("创建Engine失败: %v", err)
	}

	// 获取Registry
	registry := eng.GetRegistry()
	if registry == nil {
		t.Fatalf("获取registry失败")
	}

	// 创建数据仓库
	repo := NewQuantDataRepository()

	// 注册依赖
	ctx := context.Background()
	if err := registry.RegisterDependencyWithKey("QuantDataRepository", repo); err != nil {
		t.Fatalf("注册QuantDataRepository依赖失败: %v", err)
	}
	if err := registry.RegisterDependencyWithKey("Engine", eng); err != nil {
		t.Fatalf("注册Engine依赖失败: %v", err)
	}

	// 启动Engine
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("启动Engine失败: %v", err)
	}

	// 注册Job函数
	_, err = registry.Register(ctx, "QueryTushare", QueryTushare, "模拟Tushare API查询")
	if err != nil {
		t.Fatalf("注册QueryTushare失败: %v", err)
	}

	// 注册Task Handler
	_, err = registry.RegisterTaskHandler(ctx, "SaveResult", SaveResult, "保存结果数据")
	if err != nil {
		t.Fatalf("注册SaveResult失败: %v", err)
	}

	_, err = registry.RegisterTaskHandler(ctx, "LogError", LogError, "记录错误")
	if err != nil {
		t.Fatalf("注册LogError失败: %v", err)
	}

	_, err = registry.RegisterTaskHandler(ctx, "GenerateSubTasks", GenerateSubTasks, "生成子任务")
	if err != nil {
		t.Fatalf("注册GenerateSubTasks失败: %v", err)
	}

	_, err = registry.RegisterTaskHandler(ctx, "SaveResultAndGenerateSubTasks", SaveResultAndGenerateSubTasks, "保存结果数据并生成子任务")
	if err != nil {
		t.Fatalf("注册SaveResultAndGenerateSubTasks失败: %v", err)
	}

	cleanup := func() {
		eng.Stop()
		repos.Close()
		os.Remove(dbPath)
	}

	return eng, registry, repo, repos.Task, cleanup
}

func TestTushareWorkflow_Basic(t *testing.T) {
	eng, registry, repo, taskRepo, cleanup := setupTushareTest(t)
	defer cleanup()

	ctx := context.Background()

	// 创建任务组1：无依赖任务
	// 注意：TestTushareWorkflow_Basic 只执行前两个任务，不生成子任务，所以只使用 SaveResult
	task1, err := builder.NewTaskBuilder("获取交易日历", "获取Tushare交易日历数据", registry).
		WithJobFunction("QueryTushare", map[string]interface{}{
			"api_name": "trade_cal",
		}).
		WithTaskHandler(task.TaskStatusSuccess, "SaveResult"). // 只保存数据，不生成子任务
		WithTaskHandler(task.TaskStatusFailed, "LogError").
		Build()
	if err != nil {
		t.Fatalf("构建Task1失败: %v", err)
	}

	// 验证StatusHandlers是否正确设置
	if len(task1.StatusHandlers) == 0 {
		t.Fatal("Task1的StatusHandlers为空")
	}
	log.Printf("✅ [测试] Task1 StatusHandlers: %v", task1.StatusHandlers)

	task2, err := builder.NewTaskBuilder("获取股票列表", "获取Tushare股票列表数据", registry).
		WithJobFunction("QueryTushare", map[string]interface{}{
			"api_name": "stock_basic",
		}).
		WithTaskHandler(task.TaskStatusSuccess, "SaveResult"). // 只保存数据，不生成子任务
		WithTaskHandler(task.TaskStatusFailed, "LogError").
		Build()
	if err != nil {
		t.Fatalf("构建Task2失败: %v", err)
	}

	// 创建Workflow
	wf, err := builder.NewWorkflowBuilder("Tushare数据下载工作流", "模拟从Tushare批量下载数据的流程").
		WithTask(task1).
		WithTask(task2).
		Build()
	if err != nil {
		t.Fatalf("构建Workflow失败: %v", err)
	}

	// 提交Workflow
	controller, err := eng.SubmitWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("提交Workflow失败: %v", err)
	}

	instanceID := controller.GetInstanceID()
	if instanceID == "" {
		t.Fatal("InstanceID为空")
	}

	// 等待工作流执行完成（最多等待30秒）
	timeout := 30 * time.Second
	startTime := time.Now()
	for {
		status, err := controller.GetStatus()
		if err != nil {
			t.Fatalf("获取状态失败: %v", err)
		}

		if status == "Success" || status == "Failed" || status == "Terminated" {
			log.Printf("✅ [工作流完成] 状态=%s, 耗时=%v", status, time.Since(startTime))
			break
		}

		if time.Since(startTime) > timeout {
			t.Fatalf("工作流执行超时，当前状态=%s", status)
		}

		time.Sleep(100 * time.Millisecond)
	}

	// 验证最终状态
	finalStatus, err := controller.GetStatus()
	if err != nil {
		t.Fatalf("获取最终状态失败: %v", err)
	}

	if finalStatus != "Success" {
		t.Errorf("期望工作流状态为Success，实际为%s", finalStatus)
	}

	// 等待一小段时间，确保Handler执行完成
	time.Sleep(500 * time.Millisecond)

	// 验证并打印保存的数据（需求第7条：需要打印最后保存的数据）
	savedData := repo.GetSavedData()
	if len(savedData) == 0 {
		t.Logf("⚠️ 未保存任何数据，这可能是因为Handler未被调用或依赖注入失败")
		// 暂时不失败，因为Handler调用可能有问题
		t.Error("未保存任何数据")
	} else {
		// 统计各类型数据数量
		dataCountByType := make(map[string]int)
		for _, data := range savedData {
			if dataType, ok := data["type"].(string); ok {
				dataCountByType[dataType]++
			}
		}

		log.Printf("✅ [数据验证] 共保存 %d 条数据", len(savedData))
		log.Printf("📊 [数据统计] trade_cal=%d, stock_basic=%d, daily=%d, adj_factor=%d",
			dataCountByType["trade_cal"],
			dataCountByType["stock_basic"],
			dataCountByType["daily"],
			dataCountByType["adj_factor"])

		// 验证数据数量是否符合预期（需求第7条：需要符合预设模拟数据的数量）
		// 只执行第一组任务，应该保存 10 条数据（5 trade_cal + 5 stock_basic）
		expectedCount := ExpectedTotalDataCountBasic

		log.Printf("📊 [数量验证] 当前数据: %d 条, 预期: %d 条（5 trade_cal + 5 stock_basic）",
			len(savedData), expectedCount)

		if len(savedData) != expectedCount {
			t.Errorf("数据数量不符合预期: 期望=%d, 实际=%d", expectedCount, len(savedData))
		} else {
			log.Printf("✅ [数量验证] 数据数量符合预期: %d 条", expectedCount)
		}

		// 验证各类型数据数量
		if dataCountByType["trade_cal"] != 5 {
			t.Errorf("trade_cal数据数量不符合预期: 期望=5, 实际=%d", dataCountByType["trade_cal"])
		}
		if dataCountByType["stock_basic"] != 5 {
			t.Errorf("stock_basic数据数量不符合预期: 期望=5, 实际=%d", dataCountByType["stock_basic"])
		}

		// 重要：验证子任务确实没有生成和执行
		// TestTushareWorkflow_Basic 只使用 SaveResult，不生成子任务，所以 daily 和 adj_factor 应该为 0
		if dataCountByType["daily"] != 0 {
			t.Errorf("daily数据数量不符合预期: 期望=0（不生成子任务）, 实际=%d", dataCountByType["daily"])
		}
		if dataCountByType["adj_factor"] != 0 {
			t.Errorf("adj_factor数据数量不符合预期: 期望=0（不生成子任务）, 实际=%d", dataCountByType["adj_factor"])
		}

		// 验证任务实例：应该只有2个任务（trade_cal 和 stock_basic），没有子任务
		ctx := context.Background()
		taskInstances, err := taskRepo.GetByWorkflowInstanceID(ctx, instanceID)
		if err != nil {
			t.Logf("⚠️ 无法查询任务实例: %v", err)
		} else {
			// 统计任务数量
			taskCount := len(taskInstances)
			expectedTaskCount := 2 // 只有 trade_cal 和 stock_basic
			if taskCount != expectedTaskCount {
				t.Errorf("任务实例数量不符合预期: 期望=%d（不生成子任务）, 实际=%d", expectedTaskCount, taskCount)
			} else {
				log.Printf("✅ [任务实例验证] 任务数量符合预期: %d 个（无子任务）", taskCount)
			}

			// 验证所有任务都成功完成
			for _, taskInstance := range taskInstances {
				if taskInstance.Status != "Success" {
					t.Errorf("任务 %s 状态不符合预期: 期望=Success, 实际=%s", taskInstance.Name, taskInstance.Status)
				}
			}
		}

		// 打印所有保存的数据（需求第7条）
		separator := strings.Repeat("=", 80)
		log.Printf("\n%s", separator)
		log.Printf("📊 [最终保存的数据] (预期: %d 条, 实际: %d 条)", expectedCount, len(savedData))
		log.Printf("%s", separator)
		for i, data := range savedData {
			log.Printf("\n[数据 %d/%d]", i+1, len(savedData))
			if dataType, ok := data["type"].(string); ok {
				log.Printf("  类型: %s", dataType)
			}
			// 完整打印所有字段
			for k, v := range data {
				if k != "type" {
					log.Printf("  %s: %v", k, v)
				}
			}
		}
		log.Printf("%s\n", separator)

		// 验证至少包含交易日历和股票列表数据
		hasTradeCal := false
		hasStockBasic := false
		for _, data := range savedData {
			if dataType, ok := data["type"].(string); ok {
				if dataType == "trade_cal" {
					hasTradeCal = true
				}
				if dataType == "stock_basic" {
					hasStockBasic = true
				}
			}
		}
		if !hasTradeCal {
			t.Error("未保存交易日历数据")
		}
		if !hasStockBasic {
			t.Error("未保存股票列表数据")
		}
	}
}

func TestTushareWorkflow_WithDependencies(t *testing.T) {
	eng, registry, repo, _, cleanup := setupTushareTest(t)
	defer cleanup()

	ctx := context.Background()

	// 创建任务组1：无依赖任务
	task1, _ := builder.NewTaskBuilder("获取交易日历", "获取Tushare交易日历数据", registry).
		WithJobFunction("QueryTushare", map[string]interface{}{
			"api_name": "trade_cal",
		}).
		WithTaskHandler(task.TaskStatusSuccess, "SaveResult").
		WithTaskHandler(task.TaskStatusFailed, "LogError").
		Build()

	task2, _ := builder.NewTaskBuilder("获取股票列表", "获取Tushare股票列表数据", registry).
		WithJobFunction("QueryTushare", map[string]interface{}{
			"api_name": "stock_basic",
		}).
		WithTaskHandler(task.TaskStatusSuccess, "SaveResult").
		WithTaskHandler(task.TaskStatusFailed, "LogError").
		Build()

	// 创建任务组2：依赖任务组1（注意：由于动态子任务机制尚未完全实现，这里先测试静态任务）
	// 实际场景中，这些任务应该由GenerateSubTasks动态生成
	task3, _ := builder.NewTaskBuilder("获取日线数据_20251201", "获取20251201的日线数据", registry).
		WithJobFunction("QueryTushare", map[string]interface{}{
			"api_name":   "daily",
			"trade_date": "20251201",
			"ts_code":    "000001.SZ",
		}).
		WithDependency("获取交易日历").
		WithTaskHandler(task.TaskStatusSuccess, "SaveResult").
		WithTaskHandler(task.TaskStatusFailed, "LogError").
		Build()

	task4, _ := builder.NewTaskBuilder("获取复权因子_000001.SZ", "获取000001.SZ的复权因子", registry).
		WithJobFunction("QueryTushare", map[string]interface{}{
			"api_name": "adj_factor",
			"ts_code":  "000001.SZ",
		}).
		WithDependency("获取股票列表").
		WithTaskHandler(task.TaskStatusSuccess, "SaveResult").
		WithTaskHandler(task.TaskStatusFailed, "LogError").
		Build()

	// 创建Workflow
	wf, err := builder.NewWorkflowBuilder("Tushare数据下载工作流（含依赖）", "测试依赖关系的正确执行顺序").
		WithTask(task1).
		WithTask(task2).
		WithTask(task3).
		WithTask(task4).
		Build()
	if err != nil {
		t.Fatalf("构建Workflow失败: %v", err)
	}

	// 提交Workflow
	controller, err := eng.SubmitWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("提交Workflow失败: %v", err)
	}

	// 等待工作流执行完成
	timeout := 30 * time.Second
	startTime := time.Now()
	for {
		status, err := controller.GetStatus()
		if err != nil {
			t.Fatalf("获取状态失败: %v", err)
		}

		if status == "Success" || status == "Failed" || status == "Terminated" {
			log.Printf("✅ [工作流完成] 状态=%s, 耗时=%v", status, time.Since(startTime))
			break
		}

		if time.Since(startTime) > timeout {
			t.Fatalf("工作流执行超时，当前状态=%s", status)
		}

		time.Sleep(100 * time.Millisecond)
	}

	// 验证最终状态
	finalStatus, err := controller.GetStatus()
	if err != nil {
		t.Fatalf("获取最终状态失败: %v", err)
	}

	if finalStatus != "Success" {
		t.Errorf("期望工作流状态为Success，实际为%s", finalStatus)
	}

	// 等待一小段时间，确保Handler执行完成
	time.Sleep(500 * time.Millisecond)

	// 验证并打印保存的数据（需求第7条：需要打印最后保存的数据）
	savedData := repo.GetSavedData()

	// 统计各类型数据数量
	dataCountByType := make(map[string]int)
	for _, data := range savedData {
		if dataType, ok := data["type"].(string); ok {
			dataCountByType[dataType]++
		}
	}

	log.Printf("✅ [数据验证] 共保存 %d 条数据", len(savedData))
	log.Printf("📊 [数据统计] trade_cal=%d, stock_basic=%d, daily=%d, adj_factor=%d",
		dataCountByType["trade_cal"],
		dataCountByType["stock_basic"],
		dataCountByType["daily"],
		dataCountByType["adj_factor"])

	// 验证数据数量是否符合预期（需求第7条：需要符合预设模拟数据的数量）
	// 静态任务场景：5 trade_cal + 5 stock_basic + 1 daily + 1 adj_factor = 12条
	expectedCount := ExpectedTotalDataCountWithDependencies

	log.Printf("📊 [数量验证] 当前数据: %d 条, 预期: %d 条（5 trade_cal + 5 stock_basic + 1 daily + 1 adj_factor）",
		len(savedData), expectedCount)

	if len(savedData) != expectedCount {
		t.Errorf("数据数量不符合预期: 期望=%d, 实际=%d", expectedCount, len(savedData))
	} else {
		log.Printf("✅ [数量验证] 数据数量符合预期: %d 条", expectedCount)
	}

	// 验证各类型数据数量
	if dataCountByType["trade_cal"] != 5 {
		t.Errorf("trade_cal数据数量不符合预期: 期望=5, 实际=%d", dataCountByType["trade_cal"])
	}
	if dataCountByType["stock_basic"] != 5 {
		t.Errorf("stock_basic数据数量不符合预期: 期望=5, 实际=%d", dataCountByType["stock_basic"])
	}
	if dataCountByType["daily"] != 1 {
		t.Errorf("daily数据数量不符合预期: 期望=1, 实际=%d", dataCountByType["daily"])
	}
	if dataCountByType["adj_factor"] != 1 {
		t.Errorf("adj_factor数据数量不符合预期: 期望=1, 实际=%d", dataCountByType["adj_factor"])
	}

	// 验证字段完整性：检查所有保存的数据是否包含必需的字段
	log.Printf("🔍 [字段完整性验证] 开始验证所有数据的字段完整性...")
	dailyIndex := 0
	adjFactorIndex := 0
	tradeCalIndex := 0
	stockBasicIndex := 0
	for i, data := range savedData {
		if dataType, ok := data["type"].(string); ok {
			switch dataType {
			case "daily":
				validateDailyDataFields(t, data, dailyIndex)
				dailyIndex++
			case "adj_factor":
				validateAdjFactorDataFields(t, data, adjFactorIndex)
				adjFactorIndex++
			case "trade_cal":
				validateTradeCalDataFields(t, data, tradeCalIndex)
				tradeCalIndex++
			case "stock_basic":
				validateStockBasicDataFields(t, data, stockBasicIndex)
				stockBasicIndex++
			default:
				t.Logf("⚠️ 未知数据类型: %s (数据索引: %d)", dataType, i)
			}
		}
	}
	log.Printf("✅ [字段完整性验证] 完成，验证了 %d 条 daily 数据，%d 条 adj_factor 数据，%d 条 trade_cal 数据，%d 条 stock_basic 数据",
		dailyIndex, adjFactorIndex, tradeCalIndex, stockBasicIndex)

	// 打印所有保存的数据（需求第7条）
	if len(savedData) > 0 {
		separator := strings.Repeat("=", 80)
		log.Printf("\n%s", separator)
		log.Printf("📊 [最终保存的数据] (预期: %d 条, 实际: %d 条)", expectedCount, len(savedData))
		log.Printf("%s", separator)
		for i, data := range savedData {
			log.Printf("\n[数据 %d/%d]", i+1, len(savedData))
			if dataType, ok := data["type"].(string); ok {
				log.Printf("  类型: %s", dataType)
			}
			// 完整打印所有字段
			for k, v := range data {
				if k != "type" {
					log.Printf("  %s: %v", k, v)
				}
			}
		}
		log.Printf("%s\n", separator)
	}

	// 验证数据包含所有类型
	dataTypes := make(map[string]bool)
	for _, data := range savedData {
		if dataType, ok := data["type"].(string); ok {
			dataTypes[dataType] = true
		}
	}

	expectedTypes := []string{"trade_cal", "stock_basic", "daily", "adj_factor"}
	for _, expectedType := range expectedTypes {
		if !dataTypes[expectedType] {
			t.Errorf("缺少数据类型: %s", expectedType)
		}
	}
}

// TestTushareWorkflow_Full 完整测试：执行所有任务，包括动态生成的子任务
// 预期保存 20 条数据：5 trade_cal + 5 stock_basic + 5 daily + 5 adj_factor
func TestTushareWorkflow_Full(t *testing.T) {
	eng, registry, repo, taskRepo, cleanup := setupTushareTest(t)
	defer cleanup()

	ctx := context.Background()

	// 创建任务组1：无依赖任务
	// 注意：TestTushareWorkflow_Full 需要生成子任务，所以使用 SaveResultAndGenerateSubTasks
	task1, err := builder.NewTaskBuilder("获取交易日历", "获取Tushare交易日历数据", registry).
		WithJobFunction("QueryTushare", map[string]interface{}{
			"api_name": "trade_cal",
		}).
		WithTaskHandler(task.TaskStatusSuccess, "SaveResultAndGenerateSubTasks"). // 保存数据并生成子任务
		WithTaskHandler(task.TaskStatusFailed, "LogError").
		Build()
	if err != nil {
		t.Fatalf("构建Task1失败: %v", err)
	}

	task2, err := builder.NewTaskBuilder("获取股票列表", "获取Tushare股票列表数据", registry).
		WithJobFunction("QueryTushare", map[string]interface{}{
			"api_name": "stock_basic",
		}).
		WithTaskHandler(task.TaskStatusSuccess, "SaveResultAndGenerateSubTasks"). // 保存数据并生成子任务
		WithTaskHandler(task.TaskStatusFailed, "LogError").
		Build()
	if err != nil {
		t.Fatalf("构建Task2失败: %v", err)
	}

	// 创建Workflow
	wf, err := builder.NewWorkflowBuilder("Tushare数据下载工作流（完整）", "测试完整流程，包括动态子任务").
		WithTask(task1).
		WithTask(task2).
		Build()
	if err != nil {
		t.Fatalf("构建Workflow失败: %v", err)
	}

	// 提交Workflow
	controller, err := eng.SubmitWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("提交Workflow失败: %v", err)
	}

	instanceID := controller.GetInstanceID()
	if instanceID == "" {
		t.Fatal("InstanceID为空")
	}

	// 等待工作流执行完成（最多等待60秒，因为需要执行更多任务）
	timeout := 60 * time.Second
	startTime := time.Now()
	for {
		status, err := controller.GetStatus()
		if err != nil {
			t.Fatalf("获取状态失败: %v", err)
		}

		if status == "Success" || status == "Failed" || status == "Terminated" {
			log.Printf("✅ [工作流完成] 状态=%s, 耗时=%v", status, time.Since(startTime))
			break
		}

		if time.Since(startTime) > timeout {
			t.Fatalf("工作流执行超时，当前状态=%s", status)
		}

		time.Sleep(100 * time.Millisecond)
	}

	// 验证最终状态
	finalStatus, err := controller.GetStatus()
	if err != nil {
		t.Fatalf("获取最终状态失败: %v", err)
	}

	if finalStatus != "Success" {
		t.Errorf("期望工作流状态为Success，实际为%s", finalStatus)
	}

	// 等待一小段时间，确保所有Handler执行完成
	time.Sleep(1 * time.Second)

	// 验证并打印保存的数据
	savedData := repo.GetSavedData()
	if len(savedData) == 0 {
		t.Fatal("未保存任何数据")
	}

	// 统计各类型数据数量
	dataCountByType := make(map[string]int)
	for _, data := range savedData {
		if dataType, ok := data["type"].(string); ok {
			dataCountByType[dataType]++
		}
	}

	log.Printf("✅ [数据验证] 共保存 %d 条数据", len(savedData))
	log.Printf("📊 [数据统计] trade_cal=%d, stock_basic=%d, daily=%d, adj_factor=%d",
		dataCountByType["trade_cal"],
		dataCountByType["stock_basic"],
		dataCountByType["daily"],
		dataCountByType["adj_factor"])

	// 验证数据数量是否符合预期：20条（5 trade_cal + 5 stock_basic + 5 daily + 5 adj_factor）
	expectedCount := ExpectedTotalDataCountWithDynamicTasks

	log.Printf("📊 [数量验证] 当前数据: %d 条, 预期: %d 条（5 trade_cal + 5 stock_basic + 5 daily + 5 adj_factor）",
		len(savedData), expectedCount)

	if len(savedData) != expectedCount {
		t.Errorf("数据数量不符合预期: 期望=%d, 实际=%d", expectedCount, len(savedData))
	} else {
		log.Printf("✅ [数量验证] 数据数量符合预期: %d 条", expectedCount)
	}

	// 验证各类型数据数量
	if dataCountByType["trade_cal"] != 5 {
		t.Errorf("trade_cal数据数量不符合预期: 期望=5, 实际=%d", dataCountByType["trade_cal"])
	}
	if dataCountByType["stock_basic"] != 5 {
		t.Errorf("stock_basic数据数量不符合预期: 期望=5, 实际=%d", dataCountByType["stock_basic"])
	}
	if dataCountByType["daily"] != 5 {
		t.Errorf("daily数据数量不符合预期: 期望=5, 实际=%d", dataCountByType["daily"])
	}
	if dataCountByType["adj_factor"] != 5 {
		t.Errorf("adj_factor数据数量不符合预期: 期望=5, 实际=%d", dataCountByType["adj_factor"])
	}

	// 验证任务实例：应该包含父任务和所有子任务
	ctxVerify := context.Background()
	taskInstances, err := taskRepo.GetByWorkflowInstanceID(ctxVerify, instanceID)
	if err != nil {
		t.Logf("⚠️ 无法查询任务实例: %v", err)
	} else {
		// 统计任务数量
		taskCount := len(taskInstances)
		expectedTaskCount := 2 + ExpectedDailySubTaskCount + ExpectedAdjFactorSubTaskCount // 2个父任务 + 5个daily子任务 + 5个adj_factor子任务
		if taskCount != expectedTaskCount {
			t.Errorf("任务实例数量不符合预期: 期望=%d（2个父任务 + %d个子任务）, 实际=%d", expectedTaskCount, ExpectedDailySubTaskCount+ExpectedAdjFactorSubTaskCount, taskCount)
		} else {
			log.Printf("✅ [任务实例验证] 任务数量符合预期: %d 个（包含 %d 个子任务）", taskCount, ExpectedDailySubTaskCount+ExpectedAdjFactorSubTaskCount)
		}

		// 统计子任务数量
		dailySubTaskCount := 0
		adjFactorSubTaskCount := 0
		for _, taskInstance := range taskInstances {
			if strings.HasPrefix(taskInstance.Name, "获取日线数据_") {
				dailySubTaskCount++
			} else if strings.HasPrefix(taskInstance.Name, "获取复权因子_") {
				adjFactorSubTaskCount++
			}
			// 验证所有任务都成功完成
			if taskInstance.Status != "Success" {
				t.Errorf("任务 %s 状态不符合预期: 期望=Success, 实际=%s", taskInstance.Name, taskInstance.Status)
			}
		}

		// 验证子任务数量
		if dailySubTaskCount != ExpectedDailySubTaskCount {
			t.Errorf("daily子任务数量不符合预期: 期望=%d, 实际=%d", ExpectedDailySubTaskCount, dailySubTaskCount)
		}
		if adjFactorSubTaskCount != ExpectedAdjFactorSubTaskCount {
			t.Errorf("adj_factor子任务数量不符合预期: 期望=%d, 实际=%d", ExpectedAdjFactorSubTaskCount, adjFactorSubTaskCount)
		}

		log.Printf("✅ [子任务验证] daily子任务=%d个, adj_factor子任务=%d个", dailySubTaskCount, adjFactorSubTaskCount)
	}

	// 验证字段完整性：检查所有保存的数据是否包含必需的字段
	log.Printf("🔍 [字段完整性验证] 开始验证所有数据的字段完整性...")
	dailyIndex := 0
	adjFactorIndex := 0
	tradeCalIndex := 0
	stockBasicIndex := 0
	for i, data := range savedData {
		if dataType, ok := data["type"].(string); ok {
			switch dataType {
			case "daily":
				validateDailyDataFields(t, data, dailyIndex)
				dailyIndex++
			case "adj_factor":
				validateAdjFactorDataFields(t, data, adjFactorIndex)
				adjFactorIndex++
			case "trade_cal":
				validateTradeCalDataFields(t, data, tradeCalIndex)
				tradeCalIndex++
			case "stock_basic":
				validateStockBasicDataFields(t, data, stockBasicIndex)
				stockBasicIndex++
			default:
				t.Logf("⚠️ 未知数据类型: %s (数据索引: %d)", dataType, i)
			}
		}
	}
	log.Printf("✅ [字段完整性验证] 完成，验证了 %d 条 daily 数据，%d 条 adj_factor 数据，%d 条 trade_cal 数据，%d 条 stock_basic 数据",
		dailyIndex, adjFactorIndex, tradeCalIndex, stockBasicIndex)

	// 打印所有保存的数据
	separator := strings.Repeat("=", 80)
	log.Printf("\n%s", separator)
	log.Printf("📊 [最终保存的数据] (预期: %d 条, 实际: %d 条)", expectedCount, len(savedData))
	log.Printf("%s", separator)
	for i, data := range savedData {
		log.Printf("\n[数据 %d/%d]", i+1, len(savedData))
		if dataType, ok := data["type"].(string); ok {
			log.Printf("  类型: %s", dataType)
		}
		// 完整打印所有字段
		for k, v := range data {
			if k != "type" {
				log.Printf("  %s: %v", k, v)
			}
		}
	}
	log.Printf("%s\n", separator)
}
