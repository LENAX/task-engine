// Package e2e 提供端到端测试
// 支持两种模式：mock模式和真实模式
// 通过环境变量 E2E_MODE 控制：mock（默认）或 real
// 真实模式需要设置 TUSHARE_TOKEN 环境变量
package e2e

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stevelan1995/task-engine/internal/storage/sqlite"
	"github.com/stevelan1995/task-engine/pkg/core/builder"
	"github.com/stevelan1995/task-engine/pkg/core/engine"
	"github.com/stevelan1995/task-engine/pkg/core/task"
	"github.com/stevelan1995/task-engine/pkg/core/workflow"
)

// ==================== 测试配置 ====================

// E2EConfig E2E测试配置
type E2EConfig struct {
	Mode           string // mock 或 real
	TushareToken   string // Tushare API Token（真实模式需要）
	DocServerURL   string // 文档服务器URL
	APIServerURL   string // API服务器URL
	MetadataDBPath string // 元数据数据库路径
	StockDBPath    string // 股票数据数据库路径
	StartDate      string // 数据开始日期
	EndDate        string // 数据结束日期
}

// getE2EConfig 获取E2E测试配置
func getE2EConfig(t *testing.T) *E2EConfig {
	mode := os.Getenv("E2E_MODE")
	if mode == "" {
		mode = "mock"
	}

	cfg := &E2EConfig{
		Mode:      mode,
		StartDate: "20251201",
		EndDate:   "20251231",
	}

	if mode == "real" {
		cfg.TushareToken = os.Getenv("TUSHARE_TOKEN")
		if cfg.TushareToken == "" {
			t.Skip("真实模式需要设置 TUSHARE_TOKEN 环境变量")
		}
		cfg.DocServerURL = "https://tushare.pro"
		cfg.APIServerURL = "http://api.tushare.pro"
	}

	// 设置数据库路径
	dataDir := filepath.Join(os.TempDir(), "task-engine-e2e", time.Now().Format("20060102150405"))
	os.MkdirAll(dataDir, 0755)
	cfg.MetadataDBPath = filepath.Join(dataDir, "metadata.db")
	cfg.StockDBPath = filepath.Join(dataDir, "stock_data.db")

	return cfg
}

// ==================== E2E测试上下文 ====================

// E2EContext E2E测试上下文
type E2EContext struct {
	Config      *E2EConfig
	Engine      *engine.Engine
	Registry    task.FunctionRegistry
	DocServer   *MockTushareDocServer
	APIServer   *MockTushareAPIServer
	MetadataDB  *sql.DB
	StockDB     *sql.DB
	CrawlResult *CrawlResult
	cleanup     func()
}

// setupE2E 设置E2E测试环境
func setupE2E(t *testing.T) *E2EContext {
	cfg := getE2EConfig(t)

	ctx := &E2EContext{
		Config: cfg,
	}

	// 创建临时数据库目录用于engine
	tmpDir := t.TempDir()
	engineDBPath := filepath.Join(tmpDir, "engine.db")

	// 创建Repository
	repos, err := sqlite.NewRepositories(engineDBPath)
	if err != nil {
		t.Fatalf("创建Repository失败: %v", err)
	}

	// 创建Engine
	eng, err := engine.NewEngine(10, 60, repos.Workflow, repos.WorkflowInstance, repos.Task)
	if err != nil {
		t.Fatalf("创建Engine失败: %v", err)
	}
	ctx.Engine = eng

	// 获取Registry
	ctx.Registry = eng.GetRegistry()

	// 启动mock服务器（如果是mock模式）
	if cfg.Mode == "mock" {
		ctx.DocServer = NewMockTushareDocServer()
		cfg.DocServerURL = ctx.DocServer.Start()

		ctx.APIServer = NewMockTushareAPIServer("test_token")
		cfg.APIServerURL = ctx.APIServer.Start()
		cfg.TushareToken = "test_token"
	}

	// 启动Engine
	if err := eng.Start(context.Background()); err != nil {
		t.Fatalf("启动Engine失败: %v", err)
	}

	// 注册任务函数
	registerE2EFunctions(t, ctx)

	ctx.cleanup = func() {
		eng.Stop()
		repos.Close()
		if ctx.DocServer != nil {
			ctx.DocServer.Stop()
		}
		if ctx.APIServer != nil {
			ctx.APIServer.Stop()
		}
		if ctx.MetadataDB != nil {
			ctx.MetadataDB.Close()
		}
		if ctx.StockDB != nil {
			ctx.StockDB.Close()
		}

		// 复制数据库到 test/e2e/data 目录
		copyDatabasesForInspection(t, cfg)
	}

	return ctx
}

// copyDatabasesForInspection 复制数据库文件到指定目录供检查
func copyDatabasesForInspection(t *testing.T, cfg *E2EConfig) {
	// 获取项目根目录
	wd, _ := os.Getwd()
	dataDir := filepath.Join(wd, "data")
	os.MkdirAll(dataDir, 0755)

	// 复制元数据数据库
	if _, err := os.Stat(cfg.MetadataDBPath); err == nil {
		destPath := filepath.Join(dataDir, "metadata.db")
		copyFile(cfg.MetadataDBPath, destPath)
		t.Logf("元数据数据库已保存到: %s", destPath)
	}

	// 复制股票数据数据库
	if _, err := os.Stat(cfg.StockDBPath); err == nil {
		destPath := filepath.Join(dataDir, "stock_data.db")
		copyFile(cfg.StockDBPath, destPath)
		t.Logf("股票数据数据库已保存到: %s", destPath)
	}
}

// copyFile 复制文件
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// registerE2EFunctions 注册E2E测试所需的函数
func registerE2EFunctions(t *testing.T, ctx *E2EContext) {
	bgCtx := context.Background()
	registry := ctx.Registry

	// 注册依赖
	registry.RegisterDependencyWithKey("E2EContext", ctx)

	// 注册爬取文档目录函数
	registry.Register(bgCtx, "CrawlDocCatalog", CrawlDocCatalog, "爬取Tushare文档目录")

	// 注册爬取API详情函数
	registry.Register(bgCtx, "CrawlAPIDetail", CrawlAPIDetail, "爬取API详情")

	// 注册保存元数据函数
	registry.Register(bgCtx, "SaveMetadata", SaveMetadata, "保存元数据到SQLite")

	// 注册建表函数
	registry.Register(bgCtx, "CreateTables", CreateTables, "基于元数据创建数据表")

	// 注册数据获取函数
	registry.Register(bgCtx, "FetchTradeCal", FetchTradeCal, "获取交易日历")
	registry.Register(bgCtx, "FetchStockBasic", FetchStockBasic, "获取股票基本信息")
	registry.Register(bgCtx, "FetchDaily", FetchDaily, "获取日线行情")
	registry.Register(bgCtx, "FetchAdjFactor", FetchAdjFactor, "获取复权因子")
	registry.Register(bgCtx, "FetchIncome", FetchIncome, "获取利润表")
	registry.Register(bgCtx, "FetchBalanceSheet", FetchBalanceSheet, "获取资产负债表")
	registry.Register(bgCtx, "FetchCashFlow", FetchCashFlow, "获取现金流量表")
	registry.Register(bgCtx, "FetchTopList", FetchTopList, "获取龙虎榜")

	// 注册通用Handler
	registry.RegisterTaskHandler(bgCtx, "LogSuccess", func(tc *task.TaskContext) {
		log.Printf("✅ [任务成功] %s", tc.TaskName)
	}, "记录成功")

	registry.RegisterTaskHandler(bgCtx, "LogError", func(tc *task.TaskContext) {
		errMsg := tc.GetParamString("_error_message")
		log.Printf("❌ [任务失败] %s: %s", tc.TaskName, errMsg)
	}, "记录错误")
}

// ==================== Workflow 1: 文档爬取和元数据保存 ====================

// CrawlDocCatalog 爬取文档目录
func CrawlDocCatalog(tc *task.TaskContext) (interface{}, error) {
	e2eCtx, ok := tc.GetDependency("E2EContext")
	if !ok {
		return nil, fmt.Errorf("未找到E2EContext依赖")
	}
	ctx := e2eCtx.(*E2EContext)

	url := ctx.Config.DocServerURL + "/document/2"
	log.Printf("📡 [CrawlDocCatalog] 开始爬取: %s", url)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	// 解析目录结构
	catalogs := parseDocCatalog(string(body), ctx.Config.DocServerURL)
	log.Printf("✅ [CrawlDocCatalog] 解析到 %d 个目录项", len(catalogs))

	return catalogs, nil
}

// parseDocCatalog 解析文档目录HTML
func parseDocCatalog(html, baseURL string) []APICatalog {
	var catalogs []APICatalog

	// 使用正则表达式解析（简化实现）
	// 匹配 <li> 中的链接
	linkPattern := regexp.MustCompile(`<a href="(/document/2/\d+)"[^>]*>([^<]+)</a>`)
	matches := linkPattern.FindAllStringSubmatch(html, -1)

	for i, match := range matches {
		if len(match) >= 3 {
			catalogs = append(catalogs, APICatalog{
				ID:        i + 1,
				Name:      strings.TrimSpace(match[2]),
				Link:      baseURL + match[1],
				IsLeaf:    true,
				Level:     3,
				SortOrder: i + 1,
				CreatedAt: time.Now(),
			})
		}
	}

	return catalogs
}

// CrawlAPIDetail 爬取API详情
func CrawlAPIDetail(tc *task.TaskContext) (interface{}, error) {
	e2eCtx, ok := tc.GetDependency("E2EContext")
	if !ok {
		return nil, fmt.Errorf("未找到E2EContext依赖")
	}
	ctx := e2eCtx.(*E2EContext)

	// 从上游任务获取目录列表（数据通过 _cached_{taskID} 参数传递）
	var catalogsRaw interface{}
	for key, val := range tc.Params {
		if strings.HasPrefix(key, "_cached_") {
			catalogsRaw = val
			break
		}
	}
	if catalogsRaw == nil {
		// 也尝试 _result_data
		catalogsRaw = tc.GetParam("_result_data")
	}
	if catalogsRaw == nil {
		return nil, fmt.Errorf("未找到目录数据")
	}

	var catalogs []APICatalog
	// 类型断言
	switch v := catalogsRaw.(type) {
	case []APICatalog:
		catalogs = v
	case []interface{}:
		// 需要转换
		data, _ := json.Marshal(v)
		json.Unmarshal(data, &catalogs)
	default:
		data, _ := json.Marshal(catalogsRaw)
		json.Unmarshal(data, &catalogs)
	}

	log.Printf("📡 [CrawlAPIDetail] 开始爬取 %d 个API详情", len(catalogs))

	var params []APIParam
	var fields []APIDataField

	for _, catalog := range catalogs {
		if catalog.Link == "" {
			continue
		}

		log.Printf("  - 爬取: %s", catalog.Name)

		resp, err := http.Get(catalog.Link)
		if err != nil {
			log.Printf("    ⚠️ 请求失败: %v", err)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}

		// 解析API详情
		detail := parseAPIDetail(string(body), catalog.ID)
		params = append(params, detail.params...)
		fields = append(fields, detail.fields...)

		// 更新catalog的API信息
		catalog.APIName = detail.apiName
		catalog.Description = detail.description
		catalog.Permission = detail.permission
	}

	// 构建完整结果
	result := &CrawlResult{
		Provider: DataProvider{
			ID:          1,
			Name:        "Tushare",
			BaseURL:     ctx.Config.APIServerURL,
			Description: "Tushare金融大数据平台",
			CreatedAt:   time.Now(),
		},
		Catalogs:   catalogs,
		Params:     params,
		DataFields: fields,
	}

	ctx.CrawlResult = result

	log.Printf("✅ [CrawlAPIDetail] 完成，共获取 %d 个参数，%d 个字段", len(params), len(fields))
	return result, nil
}

// apiDetailResult API详情解析结果
type apiDetailResult struct {
	apiName     string
	description string
	permission  string
	params      []APIParam
	fields      []APIDataField
}

// parseAPIDetail 解析API详情HTML
func parseAPIDetail(html string, catalogID int) *apiDetailResult {
	result := &apiDetailResult{}

	// 提取接口名称
	apiNamePattern := regexp.MustCompile(`<strong>接口：</strong>(\w+)`)
	if match := apiNamePattern.FindStringSubmatch(html); len(match) >= 2 {
		result.apiName = match[1]
	}

	// 提取描述
	descPattern := regexp.MustCompile(`<strong>描述：</strong>([^<]+)`)
	if match := descPattern.FindStringSubmatch(html); len(match) >= 2 {
		result.description = strings.TrimSpace(match[1])
	}

	// 提取权限
	permPattern := regexp.MustCompile(`<strong>权限：</strong>([^<]+)`)
	if match := permPattern.FindStringSubmatch(html); len(match) >= 2 {
		result.permission = strings.TrimSpace(match[1])
	}

	// 提取输入参数表格
	inputPattern := regexp.MustCompile(`<table class="params-table">.*?<tbody>(.*?)</tbody>`)
	if match := inputPattern.FindStringSubmatch(html); len(match) >= 2 {
		result.params = parseParamsTable(match[1], catalogID)
	}

	// 提取输出字段表格
	outputPattern := regexp.MustCompile(`<table class="fields-table">.*?<tbody>(.*?)</tbody>`)
	if match := outputPattern.FindStringSubmatch(html); len(match) >= 2 {
		result.fields = parseFieldsTable(match[1], catalogID)
	}

	return result
}

// parseParamsTable 解析参数表格
func parseParamsTable(tbody string, catalogID int) []APIParam {
	var params []APIParam
	rowPattern := regexp.MustCompile(`<tr><td>(\w+)</td><td>(\w+)</td><td>([YN])</td><td>([^<]*)</td></tr>`)
	matches := rowPattern.FindAllStringSubmatch(tbody, -1)

	for i, match := range matches {
		if len(match) >= 5 {
			params = append(params, APIParam{
				ID:          catalogID*100 + i + 1,
				CatalogID:   catalogID,
				Name:        match[1],
				Type:        match[2],
				Required:    match[3] == "Y",
				Description: match[4],
				SortOrder:   i + 1,
				CreatedAt:   time.Now(),
			})
		}
	}

	return params
}

// parseFieldsTable 解析字段表格
func parseFieldsTable(tbody string, catalogID int) []APIDataField {
	var fields []APIDataField
	rowPattern := regexp.MustCompile(`<tr><td>(\w+)</td><td>(\w+)</td><td>([YN])</td><td>([^<]*)</td></tr>`)
	matches := rowPattern.FindAllStringSubmatch(tbody, -1)

	for i, match := range matches {
		if len(match) >= 5 {
			fields = append(fields, APIDataField{
				ID:          catalogID*100 + i + 1,
				CatalogID:   catalogID,
				Name:        match[1],
				Type:        match[2],
				Default:     match[3] == "Y",
				Description: match[4],
				SortOrder:   i + 1,
				CreatedAt:   time.Now(),
			})
		}
	}

	return fields
}

// SaveMetadata 保存元数据到SQLite
func SaveMetadata(tc *task.TaskContext) (interface{}, error) {
	e2eCtx, ok := tc.GetDependency("E2EContext")
	if !ok {
		return nil, fmt.Errorf("未找到E2EContext依赖")
	}
	ctx := e2eCtx.(*E2EContext)

	if ctx.CrawlResult == nil {
		return nil, fmt.Errorf("未找到爬取结果")
	}

	log.Printf("💾 [SaveMetadata] 开始保存元数据到: %s", ctx.Config.MetadataDBPath)

	// 创建数据库连接
	db, err := sql.Open("sqlite3", ctx.Config.MetadataDBPath)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	ctx.MetadataDB = db

	// 创建表
	if err := createMetadataTables(db); err != nil {
		return nil, fmt.Errorf("创建表失败: %w", err)
	}

	// 开启事务
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	// 保存Provider
	_, err = tx.Exec(`INSERT INTO data_provider (id, name, base_url, description, created_at) VALUES (?, ?, ?, ?, ?)`,
		ctx.CrawlResult.Provider.ID,
		ctx.CrawlResult.Provider.Name,
		ctx.CrawlResult.Provider.BaseURL,
		ctx.CrawlResult.Provider.Description,
		ctx.CrawlResult.Provider.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("保存Provider失败: %w", err)
	}

	// 保存Catalogs
	for _, c := range ctx.CrawlResult.Catalogs {
		_, err = tx.Exec(`INSERT INTO api_catalog (id, provider_id, name, level, is_leaf, link, api_name, description, permission, sort_order, created_at) 
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.ID, 1, c.Name, c.Level, c.IsLeaf, c.Link, c.APIName, c.Description, c.Permission, c.SortOrder, c.CreatedAt,
		)
		if err != nil {
			log.Printf("  ⚠️ 保存Catalog失败: %v", err)
		}
	}

	// 保存Params
	for _, p := range ctx.CrawlResult.Params {
		_, err = tx.Exec(`INSERT INTO api_param (id, catalog_id, name, type, required, description, sort_order, created_at) 
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			p.ID, p.CatalogID, p.Name, p.Type, p.Required, p.Description, p.SortOrder, p.CreatedAt,
		)
		if err != nil {
			log.Printf("  ⚠️ 保存Param失败: %v", err)
		}
	}

	// 保存Fields
	for _, f := range ctx.CrawlResult.DataFields {
		_, err = tx.Exec(`INSERT INTO api_data_field (id, catalog_id, name, type, is_default, description, sort_order, created_at) 
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			f.ID, f.CatalogID, f.Name, f.Type, f.Default, f.Description, f.SortOrder, f.CreatedAt,
		)
		if err != nil {
			log.Printf("  ⚠️ 保存Field失败: %v", err)
		}
	}

	// 提交事务
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("提交事务失败: %w", err)
	}

	log.Printf("✅ [SaveMetadata] 保存完成: Provider=1, Catalogs=%d, Params=%d, Fields=%d",
		len(ctx.CrawlResult.Catalogs), len(ctx.CrawlResult.Params), len(ctx.CrawlResult.DataFields))

	return map[string]int{
		"providers": 1,
		"catalogs":  len(ctx.CrawlResult.Catalogs),
		"params":    len(ctx.CrawlResult.Params),
		"fields":    len(ctx.CrawlResult.DataFields),
	}, nil
}

// createMetadataTables 创建元数据表
func createMetadataTables(db *sql.DB) error {
	sqls := []string{
		`CREATE TABLE IF NOT EXISTS data_provider (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			base_url TEXT,
			description TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS api_catalog (
			id INTEGER PRIMARY KEY,
			provider_id INTEGER,
			parent_id INTEGER,
			name TEXT NOT NULL,
			level INTEGER DEFAULT 1,
			is_leaf INTEGER DEFAULT 0,
			link TEXT,
			api_name TEXT,
			description TEXT,
			permission TEXT,
			sort_order INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (provider_id) REFERENCES data_provider(id)
		)`,
		`CREATE TABLE IF NOT EXISTS api_param (
			id INTEGER PRIMARY KEY,
			catalog_id INTEGER,
			name TEXT NOT NULL,
			type TEXT,
			required INTEGER DEFAULT 0,
			description TEXT,
			sort_order INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (catalog_id) REFERENCES api_catalog(id)
		)`,
		`CREATE TABLE IF NOT EXISTS api_data_field (
			id INTEGER PRIMARY KEY,
			catalog_id INTEGER,
			name TEXT NOT NULL,
			type TEXT,
			is_default INTEGER DEFAULT 0,
			description TEXT,
			sort_order INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (catalog_id) REFERENCES api_catalog(id)
		)`,
	}

	for _, s := range sqls {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}

	return nil
}

// ==================== Workflow 2: 基于元数据建表 ====================

// CreateTables 基于元数据创建数据表
func CreateTables(tc *task.TaskContext) (interface{}, error) {
	e2eCtx, ok := tc.GetDependency("E2EContext")
	if !ok {
		return nil, fmt.Errorf("未找到E2EContext依赖")
	}
	ctx := e2eCtx.(*E2EContext)

	log.Printf("🔨 [CreateTables] 开始在 %s 创建数据表", ctx.Config.StockDBPath)

	// 创建股票数据数据库
	db, err := sql.Open("sqlite3", ctx.Config.StockDBPath)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	ctx.StockDB = db

	// 开启事务创建所有表
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	// 创建各数据表
	tables := getStockDataTableDDLs()
	createdTables := 0

	for name, ddl := range tables {
		log.Printf("  - 创建表: %s", name)
		if _, err := tx.Exec(ddl); err != nil {
			return nil, fmt.Errorf("创建表 %s 失败: %w", name, err)
		}
		createdTables++
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("提交事务失败: %w", err)
	}

	log.Printf("✅ [CreateTables] 创建完成，共 %d 个表", createdTables)
	return map[string]int{"tables_created": createdTables}, nil
}

// getStockDataTableDDLs 获取股票数据表DDL
func getStockDataTableDDLs() map[string]string {
	return map[string]string{
		"trade_cal": `CREATE TABLE IF NOT EXISTS trade_cal (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			exchange TEXT,
			cal_date TEXT NOT NULL,
			is_open INTEGER,
			pre_date TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(exchange, cal_date)
		)`,
		"stock_basic": `CREATE TABLE IF NOT EXISTS stock_basic (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts_code TEXT NOT NULL UNIQUE,
			symbol TEXT,
			name TEXT,
			area TEXT,
			industry TEXT,
			market TEXT,
			list_date TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		"daily": `CREATE TABLE IF NOT EXISTS daily (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts_code TEXT NOT NULL,
			trade_date TEXT NOT NULL,
			open REAL,
			high REAL,
			low REAL,
			close REAL,
			pre_close REAL,
			change REAL,
			pct_chg REAL,
			vol REAL,
			amount REAL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(ts_code, trade_date)
		)`,
		"adj_factor": `CREATE TABLE IF NOT EXISTS adj_factor (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts_code TEXT NOT NULL,
			trade_date TEXT NOT NULL,
			adj_factor REAL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(ts_code, trade_date)
		)`,
		"income": `CREATE TABLE IF NOT EXISTS income (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts_code TEXT NOT NULL,
			ann_date TEXT,
			end_date TEXT,
			total_revenue REAL,
			revenue REAL,
			n_income REAL,
			total_cogs REAL,
			operate_profit REAL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(ts_code, end_date)
		)`,
		"balancesheet": `CREATE TABLE IF NOT EXISTS balancesheet (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts_code TEXT NOT NULL,
			ann_date TEXT,
			end_date TEXT,
			total_assets REAL,
			total_liab REAL,
			total_hldr_eqy_exc_min_int REAL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(ts_code, end_date)
		)`,
		"cashflow": `CREATE TABLE IF NOT EXISTS cashflow (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts_code TEXT NOT NULL,
			ann_date TEXT,
			end_date TEXT,
			n_cashflow_act REAL,
			n_cashflow_inv_act REAL,
			n_cash_flows_fnc_act REAL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(ts_code, end_date)
		)`,
		"top_list": `CREATE TABLE IF NOT EXISTS top_list (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			trade_date TEXT NOT NULL,
			ts_code TEXT NOT NULL,
			name TEXT,
			close REAL,
			pct_change REAL,
			turnover_rate REAL,
			amount REAL,
			l_sell REAL,
			net_amount REAL,
			reason TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(trade_date, ts_code)
		)`,
	}
}

// ==================== Workflow 3: 数据获取 ====================

// callTushareAPI 调用Tushare API
func callTushareAPI(ctx *E2EContext, apiName string, params map[string]interface{}) (*TushareDataFrame, error) {
	reqBody := TushareRequest{
		APIName: apiName,
		Token:   ctx.Config.TushareToken,
		Params:  params,
	}

	jsonData, _ := json.Marshal(reqBody)
	resp, err := http.Post(ctx.Config.APIServerURL, "application/json", strings.NewReader(string(jsonData)))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result TushareResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("API错误: %s", result.Msg)
	}

	// 转换Data为DataFrame
	dataBytes, _ := json.Marshal(result.Data)
	var df TushareDataFrame
	if err := json.Unmarshal(dataBytes, &df); err != nil {
		return nil, err
	}

	return &df, nil
}

// FetchTradeCal 获取交易日历
func FetchTradeCal(tc *task.TaskContext) (interface{}, error) {
	e2eCtx, ok := tc.GetDependency("E2EContext")
	if !ok {
		return nil, fmt.Errorf("未找到E2EContext依赖")
	}
	ctx := e2eCtx.(*E2EContext)

	log.Printf("📡 [FetchTradeCal] 获取交易日历: %s - %s", ctx.Config.StartDate, ctx.Config.EndDate)

	df, err := callTushareAPI(ctx, "trade_cal", map[string]interface{}{
		"exchange":   "SSE",
		"start_date": ctx.Config.StartDate,
		"end_date":   ctx.Config.EndDate,
	})
	if err != nil {
		return nil, err
	}

	// 保存到数据库
	count, err := saveDataFrame(ctx.StockDB, "trade_cal", df)
	if err != nil {
		return nil, err
	}

	log.Printf("✅ [FetchTradeCal] 保存 %d 条记录", count)
	return map[string]int{"count": count}, nil
}

// FetchStockBasic 获取股票基本信息
func FetchStockBasic(tc *task.TaskContext) (interface{}, error) {
	e2eCtx, ok := tc.GetDependency("E2EContext")
	if !ok {
		return nil, fmt.Errorf("未找到E2EContext依赖")
	}
	ctx := e2eCtx.(*E2EContext)

	log.Printf("📡 [FetchStockBasic] 获取股票基本信息")

	df, err := callTushareAPI(ctx, "stock_basic", map[string]interface{}{
		"list_status": "L",
	})
	if err != nil {
		return nil, err
	}

	count, err := saveDataFrame(ctx.StockDB, "stock_basic", df)
	if err != nil {
		return nil, err
	}

	log.Printf("✅ [FetchStockBasic] 保存 %d 条记录", count)
	return map[string]int{"count": count}, nil
}

// FetchDaily 获取日线行情
func FetchDaily(tc *task.TaskContext) (interface{}, error) {
	e2eCtx, ok := tc.GetDependency("E2EContext")
	if !ok {
		return nil, fmt.Errorf("未找到E2EContext依赖")
	}
	ctx := e2eCtx.(*E2EContext)

	log.Printf("📡 [FetchDaily] 获取日线行情: %s - %s", ctx.Config.StartDate, ctx.Config.EndDate)

	df, err := callTushareAPI(ctx, "daily", map[string]interface{}{
		"start_date": ctx.Config.StartDate,
		"end_date":   ctx.Config.EndDate,
	})
	if err != nil {
		return nil, err
	}

	count, err := saveDataFrame(ctx.StockDB, "daily", df)
	if err != nil {
		return nil, err
	}

	log.Printf("✅ [FetchDaily] 保存 %d 条记录", count)
	return map[string]int{"count": count}, nil
}

// FetchAdjFactor 获取复权因子
func FetchAdjFactor(tc *task.TaskContext) (interface{}, error) {
	e2eCtx, ok := tc.GetDependency("E2EContext")
	if !ok {
		return nil, fmt.Errorf("未找到E2EContext依赖")
	}
	ctx := e2eCtx.(*E2EContext)

	log.Printf("📡 [FetchAdjFactor] 获取复权因子")

	df, err := callTushareAPI(ctx, "adj_factor", map[string]interface{}{
		"start_date": ctx.Config.StartDate,
		"end_date":   ctx.Config.EndDate,
	})
	if err != nil {
		return nil, err
	}

	count, err := saveDataFrame(ctx.StockDB, "adj_factor", df)
	if err != nil {
		return nil, err
	}

	log.Printf("✅ [FetchAdjFactor] 保存 %d 条记录", count)
	return map[string]int{"count": count}, nil
}

// FetchIncome 获取利润表
func FetchIncome(tc *task.TaskContext) (interface{}, error) {
	e2eCtx, ok := tc.GetDependency("E2EContext")
	if !ok {
		return nil, fmt.Errorf("未找到E2EContext依赖")
	}
	ctx := e2eCtx.(*E2EContext)

	log.Printf("📡 [FetchIncome] 获取利润表")

	df, err := callTushareAPI(ctx, "income", map[string]interface{}{
		"period": "20251231",
	})
	if err != nil {
		return nil, err
	}

	count, err := saveDataFrame(ctx.StockDB, "income", df)
	if err != nil {
		return nil, err
	}

	log.Printf("✅ [FetchIncome] 保存 %d 条记录", count)
	return map[string]int{"count": count}, nil
}

// FetchBalanceSheet 获取资产负债表
func FetchBalanceSheet(tc *task.TaskContext) (interface{}, error) {
	e2eCtx, ok := tc.GetDependency("E2EContext")
	if !ok {
		return nil, fmt.Errorf("未找到E2EContext依赖")
	}
	ctx := e2eCtx.(*E2EContext)

	log.Printf("📡 [FetchBalanceSheet] 获取资产负债表")

	df, err := callTushareAPI(ctx, "balancesheet", map[string]interface{}{
		"period": "20251231",
	})
	if err != nil {
		return nil, err
	}

	count, err := saveDataFrame(ctx.StockDB, "balancesheet", df)
	if err != nil {
		return nil, err
	}

	log.Printf("✅ [FetchBalanceSheet] 保存 %d 条记录", count)
	return map[string]int{"count": count}, nil
}

// FetchCashFlow 获取现金流量表
func FetchCashFlow(tc *task.TaskContext) (interface{}, error) {
	e2eCtx, ok := tc.GetDependency("E2EContext")
	if !ok {
		return nil, fmt.Errorf("未找到E2EContext依赖")
	}
	ctx := e2eCtx.(*E2EContext)

	log.Printf("📡 [FetchCashFlow] 获取现金流量表")

	df, err := callTushareAPI(ctx, "cashflow", map[string]interface{}{
		"period": "20251231",
	})
	if err != nil {
		return nil, err
	}

	count, err := saveDataFrame(ctx.StockDB, "cashflow", df)
	if err != nil {
		return nil, err
	}

	log.Printf("✅ [FetchCashFlow] 保存 %d 条记录", count)
	return map[string]int{"count": count}, nil
}

// FetchTopList 获取龙虎榜
func FetchTopList(tc *task.TaskContext) (interface{}, error) {
	e2eCtx, ok := tc.GetDependency("E2EContext")
	if !ok {
		return nil, fmt.Errorf("未找到E2EContext依赖")
	}
	ctx := e2eCtx.(*E2EContext)

	log.Printf("📡 [FetchTopList] 获取龙虎榜")

	df, err := callTushareAPI(ctx, "top_list", map[string]interface{}{
		"trade_date": ctx.Config.StartDate,
	})
	if err != nil {
		return nil, err
	}

	count, err := saveDataFrame(ctx.StockDB, "top_list", df)
	if err != nil {
		return nil, err
	}

	log.Printf("✅ [FetchTopList] 保存 %d 条记录", count)
	return map[string]int{"count": count}, nil
}

// saveDataFrame 保存DataFrame到数据库
func saveDataFrame(db *sql.DB, tableName string, df *TushareDataFrame) (int, error) {
	if len(df.Items) == 0 {
		return 0, nil
	}

	// 构建INSERT语句
	placeholders := make([]string, len(df.Fields))
	for i := range placeholders {
		placeholders[i] = "?"
	}

	query := fmt.Sprintf("INSERT OR REPLACE INTO %s (%s) VALUES (%s)",
		tableName,
		strings.Join(df.Fields, ", "),
		strings.Join(placeholders, ", "))

	// 批量插入
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(query)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	count := 0
	for _, item := range df.Items {
		if _, err := stmt.Exec(item...); err != nil {
			log.Printf("  ⚠️ 插入失败: %v", err)
			continue
		}
		count++
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return count, nil
}

// ==================== 测试函数 ====================

// TestE2E_Workflow1_MetadataCrawl 测试Workflow 1: 元数据爬取
func TestE2E_Workflow1_MetadataCrawl(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	bgCtx := context.Background()

	// 构建Workflow 1: 元数据爬取
	task1, _ := builder.NewTaskBuilder("爬取文档目录", "爬取Tushare文档目录结构", ctx.Registry).
		WithJobFunction("CrawlDocCatalog", nil).
		WithTaskHandler(task.TaskStatusSuccess, "LogSuccess").
		WithTaskHandler(task.TaskStatusFailed, "LogError").
		Build()

	task2, _ := builder.NewTaskBuilder("爬取API详情", "爬取每个API的详细信息", ctx.Registry).
		WithJobFunction("CrawlAPIDetail", nil).
		WithDependency("爬取文档目录").
		WithTaskHandler(task.TaskStatusSuccess, "LogSuccess").
		WithTaskHandler(task.TaskStatusFailed, "LogError").
		Build()

	task3, _ := builder.NewTaskBuilder("保存元数据", "保存元数据到SQLite", ctx.Registry).
		WithJobFunction("SaveMetadata", nil).
		WithDependency("爬取API详情").
		WithTaskHandler(task.TaskStatusSuccess, "LogSuccess").
		WithTaskHandler(task.TaskStatusFailed, "LogError").
		Build()

	wf, err := builder.NewWorkflowBuilder("Tushare元数据爬取", "爬取Tushare API文档并保存元数据").
		WithTask(task1).
		WithTask(task2).
		WithTask(task3).
		Build()
	if err != nil {
		t.Fatalf("构建Workflow失败: %v", err)
	}

	// 执行
	controller, err := ctx.Engine.SubmitWorkflow(bgCtx, wf)
	if err != nil {
		t.Fatalf("提交Workflow失败: %v", err)
	}

	// 等待完成
	waitForWorkflow(t, controller, 60*time.Second)

	// 验证结果
	status, _ := controller.GetStatus()
	if status != "Success" {
		t.Errorf("Workflow状态不正确: 期望=Success, 实际=%s", status)
	}

	// 验证元数据已保存
	if ctx.CrawlResult == nil {
		t.Error("爬取结果为空")
	} else {
		t.Logf("✅ 爬取结果: Provider=%s, Catalogs=%d, Params=%d, Fields=%d",
			ctx.CrawlResult.Provider.Name,
			len(ctx.CrawlResult.Catalogs),
			len(ctx.CrawlResult.Params),
			len(ctx.CrawlResult.DataFields))
	}
}

// TestE2E_Workflow2_CreateTables 测试Workflow 2: 建表
func TestE2E_Workflow2_CreateTables(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	bgCtx := context.Background()

	// 先执行Workflow 1获取元数据
	runMetadataCrawlWorkflow(t, ctx)

	// 构建Workflow 2: 建表
	task1, _ := builder.NewTaskBuilder("创建数据表", "基于元数据创建股票数据表", ctx.Registry).
		WithJobFunction("CreateTables", nil).
		WithTaskHandler(task.TaskStatusSuccess, "LogSuccess").
		WithTaskHandler(task.TaskStatusFailed, "LogError").
		Build()

	wf, err := builder.NewWorkflowBuilder("创建数据表", "基于元数据在SQLite中创建数据表").
		WithTask(task1).
		Build()
	if err != nil {
		t.Fatalf("构建Workflow失败: %v", err)
	}

	// 执行
	controller, err := ctx.Engine.SubmitWorkflow(bgCtx, wf)
	if err != nil {
		t.Fatalf("提交Workflow失败: %v", err)
	}

	waitForWorkflow(t, controller, 30*time.Second)

	status, _ := controller.GetStatus()
	if status != "Success" {
		t.Errorf("Workflow状态不正确: 期望=Success, 实际=%s", status)
	}

	// 验证表已创建
	if ctx.StockDB != nil {
		tables := []string{"trade_cal", "stock_basic", "daily", "adj_factor", "income", "balancesheet", "cashflow", "top_list"}
		for _, table := range tables {
			var count int
			err := ctx.StockDB.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='%s'", table)).Scan(&count)
			if err != nil || count == 0 {
				t.Errorf("表 %s 未创建", table)
			}
		}
		t.Logf("✅ 所有数据表已创建")
	}
}

// TestE2E_Workflow3_DataAcquisition 测试Workflow 3: 数据获取
func TestE2E_Workflow3_DataAcquisition(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	bgCtx := context.Background()

	// 先执行Workflow 1和2
	runMetadataCrawlWorkflow(t, ctx)
	runCreateTablesWorkflow(t, ctx)

	// 构建Workflow 3: 数据获取
	tasks := []*task.Task{}

	// 交易日历和股票基础信息（无依赖）
	t1, _ := builder.NewTaskBuilder("获取交易日历", "获取2025年12月交易日历", ctx.Registry).
		WithJobFunction("FetchTradeCal", nil).
		WithTaskHandler(task.TaskStatusSuccess, "LogSuccess").
		WithTaskHandler(task.TaskStatusFailed, "LogError").
		Build()
	tasks = append(tasks, t1)

	t2, _ := builder.NewTaskBuilder("获取股票信息", "获取股票基本信息", ctx.Registry).
		WithJobFunction("FetchStockBasic", nil).
		WithTaskHandler(task.TaskStatusSuccess, "LogSuccess").
		WithTaskHandler(task.TaskStatusFailed, "LogError").
		Build()
	tasks = append(tasks, t2)

	// 依赖交易日历的任务
	t3, _ := builder.NewTaskBuilder("获取日线数据", "获取历史日线行情", ctx.Registry).
		WithJobFunction("FetchDaily", nil).
		WithDependency("获取交易日历").
		WithDependency("获取股票信息").
		WithTaskHandler(task.TaskStatusSuccess, "LogSuccess").
		WithTaskHandler(task.TaskStatusFailed, "LogError").
		Build()
	tasks = append(tasks, t3)

	t4, _ := builder.NewTaskBuilder("获取复权因子", "获取复权因子数据", ctx.Registry).
		WithJobFunction("FetchAdjFactor", nil).
		WithDependency("获取股票信息").
		WithTaskHandler(task.TaskStatusSuccess, "LogSuccess").
		WithTaskHandler(task.TaskStatusFailed, "LogError").
		Build()
	tasks = append(tasks, t4)

	// 财务数据
	t5, _ := builder.NewTaskBuilder("获取利润表", "获取利润表数据", ctx.Registry).
		WithJobFunction("FetchIncome", nil).
		WithDependency("获取股票信息").
		WithTaskHandler(task.TaskStatusSuccess, "LogSuccess").
		WithTaskHandler(task.TaskStatusFailed, "LogError").
		Build()
	tasks = append(tasks, t5)

	t6, _ := builder.NewTaskBuilder("获取资产负债表", "获取资产负债表数据", ctx.Registry).
		WithJobFunction("FetchBalanceSheet", nil).
		WithDependency("获取股票信息").
		WithTaskHandler(task.TaskStatusSuccess, "LogSuccess").
		WithTaskHandler(task.TaskStatusFailed, "LogError").
		Build()
	tasks = append(tasks, t6)

	t7, _ := builder.NewTaskBuilder("获取现金流量表", "获取现金流量表数据", ctx.Registry).
		WithJobFunction("FetchCashFlow", nil).
		WithDependency("获取股票信息").
		WithTaskHandler(task.TaskStatusSuccess, "LogSuccess").
		WithTaskHandler(task.TaskStatusFailed, "LogError").
		Build()
	tasks = append(tasks, t7)

	t8, _ := builder.NewTaskBuilder("获取龙虎榜", "获取龙虎榜数据", ctx.Registry).
		WithJobFunction("FetchTopList", nil).
		WithDependency("获取交易日历").
		WithTaskHandler(task.TaskStatusSuccess, "LogSuccess").
		WithTaskHandler(task.TaskStatusFailed, "LogError").
		Build()
	tasks = append(tasks, t8)

	// 构建Workflow
	wfBuilder := builder.NewWorkflowBuilder("数据获取", "获取2025年12月股票数据")
	for _, tk := range tasks {
		wfBuilder.WithTask(tk)
	}

	wf, err := wfBuilder.Build()
	if err != nil {
		t.Fatalf("构建Workflow失败: %v", err)
	}

	// 执行
	controller, err := ctx.Engine.SubmitWorkflow(bgCtx, wf)
	if err != nil {
		t.Fatalf("提交Workflow失败: %v", err)
	}

	waitForWorkflow(t, controller, 120*time.Second)

	status, _ := controller.GetStatus()
	if status != "Success" {
		t.Errorf("Workflow状态不正确: 期望=Success, 实际=%s", status)
	}

	// 验证数据已保存
	printDataSummary(t, ctx.StockDB)
}

// TestE2E_FullPipeline 完整流程测试
func TestE2E_FullPipeline(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	t.Log("========== E2E完整流程测试开始 ==========")
	t.Logf("模式: %s", ctx.Config.Mode)
	t.Logf("元数据库: %s", ctx.Config.MetadataDBPath)
	t.Logf("股票数据库: %s", ctx.Config.StockDBPath)

	// Workflow 1: 元数据爬取
	t.Log("\n----- Workflow 1: 元数据爬取 -----")
	runMetadataCrawlWorkflow(t, ctx)

	// Workflow 2: 建表
	t.Log("\n----- Workflow 2: 创建数据表 -----")
	runCreateTablesWorkflow(t, ctx)

	// Workflow 3: 数据获取
	t.Log("\n----- Workflow 3: 数据获取 -----")
	runDataAcquisitionWorkflow(t, ctx)

	// 输出最终结果
	t.Log("\n========== 测试结果汇总 ==========")
	printMetadataSummary(t, ctx.MetadataDB)
	printDataSummary(t, ctx.StockDB)

	t.Log("========== E2E完整流程测试完成 ==========")
}

// ==================== 辅助函数 ====================

func waitForWorkflow(t *testing.T, controller workflow.WorkflowController, timeout time.Duration) {
	startTime := time.Now()
	for {
		status, err := controller.GetStatus()
		if err != nil {
			t.Fatalf("获取状态失败: %v", err)
		}

		if status == "Success" || status == "Failed" || status == "Terminated" {
			t.Logf("Workflow完成，状态=%s，耗时=%v", status, time.Since(startTime))
			return
		}

		if time.Since(startTime) > timeout {
			t.Fatalf("Workflow执行超时，当前状态=%s", status)
		}

		time.Sleep(100 * time.Millisecond)
	}
}

func runMetadataCrawlWorkflow(t *testing.T, ctx *E2EContext) {
	bgCtx := context.Background()

	task1, _ := builder.NewTaskBuilder("爬取文档目录", "爬取Tushare文档目录结构", ctx.Registry).
		WithJobFunction("CrawlDocCatalog", nil).
		Build()

	task2, _ := builder.NewTaskBuilder("爬取API详情", "爬取每个API的详细信息", ctx.Registry).
		WithJobFunction("CrawlAPIDetail", nil).
		WithDependency("爬取文档目录").
		Build()

	task3, _ := builder.NewTaskBuilder("保存元数据", "保存元数据到SQLite", ctx.Registry).
		WithJobFunction("SaveMetadata", nil).
		WithDependency("爬取API详情").
		Build()

	wf, _ := builder.NewWorkflowBuilder("Tushare元数据爬取", "").
		WithTask(task1).WithTask(task2).WithTask(task3).Build()

	controller, _ := ctx.Engine.SubmitWorkflow(bgCtx, wf)
	waitForWorkflow(t, controller, 60*time.Second)
}

func runCreateTablesWorkflow(t *testing.T, ctx *E2EContext) {
	bgCtx := context.Background()

	task1, _ := builder.NewTaskBuilder("创建数据表", "", ctx.Registry).
		WithJobFunction("CreateTables", nil).Build()

	wf, _ := builder.NewWorkflowBuilder("创建数据表", "").WithTask(task1).Build()

	controller, _ := ctx.Engine.SubmitWorkflow(bgCtx, wf)
	waitForWorkflow(t, controller, 30*time.Second)
}

func runDataAcquisitionWorkflow(t *testing.T, ctx *E2EContext) {
	bgCtx := context.Background()

	t1, _ := builder.NewTaskBuilder("获取交易日历", "", ctx.Registry).WithJobFunction("FetchTradeCal", nil).Build()
	t2, _ := builder.NewTaskBuilder("获取股票信息", "", ctx.Registry).WithJobFunction("FetchStockBasic", nil).Build()
	t3, _ := builder.NewTaskBuilder("获取日线数据", "", ctx.Registry).WithJobFunction("FetchDaily", nil).WithDependency("获取交易日历").WithDependency("获取股票信息").Build()
	t4, _ := builder.NewTaskBuilder("获取复权因子", "", ctx.Registry).WithJobFunction("FetchAdjFactor", nil).WithDependency("获取股票信息").Build()
	t5, _ := builder.NewTaskBuilder("获取利润表", "", ctx.Registry).WithJobFunction("FetchIncome", nil).WithDependency("获取股票信息").Build()
	t6, _ := builder.NewTaskBuilder("获取资产负债表", "", ctx.Registry).WithJobFunction("FetchBalanceSheet", nil).WithDependency("获取股票信息").Build()
	t7, _ := builder.NewTaskBuilder("获取现金流量表", "", ctx.Registry).WithJobFunction("FetchCashFlow", nil).WithDependency("获取股票信息").Build()
	t8, _ := builder.NewTaskBuilder("获取龙虎榜", "", ctx.Registry).WithJobFunction("FetchTopList", nil).WithDependency("获取交易日历").Build()

	wf, _ := builder.NewWorkflowBuilder("数据获取", "").
		WithTask(t1).WithTask(t2).WithTask(t3).WithTask(t4).
		WithTask(t5).WithTask(t6).WithTask(t7).WithTask(t8).Build()

	controller, _ := ctx.Engine.SubmitWorkflow(bgCtx, wf)
	waitForWorkflow(t, controller, 120*time.Second)
}

func printMetadataSummary(t *testing.T, db *sql.DB) {
	if db == nil {
		return
	}

	t.Log("\n📊 元数据统计:")
	tables := []string{"data_provider", "api_catalog", "api_param", "api_data_field"}
	for _, table := range tables {
		var count int
		db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
		t.Logf("  - %s: %d 条", table, count)
	}
}

func printDataSummary(t *testing.T, db *sql.DB) {
	if db == nil {
		return
	}

	t.Log("\n📊 股票数据统计:")
	tables := []string{"trade_cal", "stock_basic", "daily", "adj_factor", "income", "balancesheet", "cashflow", "top_list"}
	for _, table := range tables {
		var count int
		db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
		t.Logf("  - %s: %d 条", table, count)
	}
}
