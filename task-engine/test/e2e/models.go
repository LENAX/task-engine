// Package e2e 提供端到端测试
package e2e

import (
	"time"
)

// ==================== 元数据模型定义 ====================

// DataProvider 数据提供者（如Tushare）
type DataProvider struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`        // 提供者名称，如 "Tushare"
	BaseURL     string    `json:"base_url"`    // 基础URL
	Description string    `json:"description"` // 描述
	CreatedAt   time.Time `json:"created_at"`
}

// APICatalog API目录（层级结构）
type APICatalog struct {
	ID           int       `json:"id"`
	ProviderID   int       `json:"provider_id"`   // 关联DataProvider
	ParentID     *int      `json:"parent_id"`     // 父级ID，顶级为nil
	Name         string    `json:"name"`          // 目录/API名称
	Level        int       `json:"level"`         // 层级深度（1-4）
	IsLeaf       bool      `json:"is_leaf"`       // 是否叶子节点（实际API）
	Link         string    `json:"link"`          // API文档链接（叶子节点有）
	APIName      string    `json:"api_name"`      // API接口名称（如 daily, stock_basic）
	Description  string    `json:"description"`   // API描述
	Permission   string    `json:"permission"`    // 权限要求
	SortOrder    int       `json:"sort_order"`    // 排序
	CreatedAt    time.Time `json:"created_at"`
}

// APIParam API输入参数
type APIParam struct {
	ID          int       `json:"id"`
	CatalogID   int       `json:"catalog_id"`  // 关联APICatalog
	Name        string    `json:"name"`        // 参数名
	Type        string    `json:"type"`        // 参数类型（str, int, float, date等）
	Required    bool      `json:"required"`    // 是否必填
	Description string    `json:"description"` // 参数描述
	SortOrder   int       `json:"sort_order"`
	CreatedAt   time.Time `json:"created_at"`
}

// APIDataField API输出字段
type APIDataField struct {
	ID          int       `json:"id"`
	CatalogID   int       `json:"catalog_id"`  // 关联APICatalog
	Name        string    `json:"name"`        // 字段名
	Type        string    `json:"type"`        // 字段类型
	Default     bool      `json:"default"`     // 是否默认显示
	Description string    `json:"description"` // 字段描述
	SortOrder   int       `json:"sort_order"`
	CreatedAt   time.Time `json:"created_at"`
}

// ==================== 股票数据模型定义 ====================

// TradeCal 交易日历
type TradeCal struct {
	ID        int       `json:"id"`
	Exchange  string    `json:"exchange"`  // 交易所（SSE/SZSE）
	CalDate   string    `json:"cal_date"`  // 日期 yyyymmdd
	IsOpen    int       `json:"is_open"`   // 是否开盘 1/0
	PreDate   string    `json:"pre_date"`  // 上一交易日
	CreatedAt time.Time `json:"created_at"`
}

// StockBasic 股票基本信息
type StockBasic struct {
	ID       int       `json:"id"`
	TSCode   string    `json:"ts_code"`   // 股票代码
	Symbol   string    `json:"symbol"`    // 股票简码
	Name     string    `json:"name"`      // 股票名称
	Area     string    `json:"area"`      // 地区
	Industry string    `json:"industry"`  // 行业
	Market   string    `json:"market"`    // 市场（主板/创业板等）
	ListDate string    `json:"list_date"` // 上市日期
	CreatedAt time.Time `json:"created_at"`
}

// Daily 日线行情
type Daily struct {
	ID        int       `json:"id"`
	TSCode    string    `json:"ts_code"`    // 股票代码
	TradeDate string    `json:"trade_date"` // 交易日期
	Open      float64   `json:"open"`       // 开盘价
	High      float64   `json:"high"`       // 最高价
	Low       float64   `json:"low"`        // 最低价
	Close     float64   `json:"close"`      // 收盘价
	PreClose  float64   `json:"pre_close"`  // 昨收价
	Change    float64   `json:"change"`     // 涨跌额
	PctChg    float64   `json:"pct_chg"`    // 涨跌幅
	Vol       float64   `json:"vol"`        // 成交量（手）
	Amount    float64   `json:"amount"`     // 成交额（千元）
	CreatedAt time.Time `json:"created_at"`
}

// AdjFactor 复权因子
type AdjFactor struct {
	ID        int       `json:"id"`
	TSCode    string    `json:"ts_code"`
	TradeDate string    `json:"trade_date"`
	AdjFactor float64   `json:"adj_factor"` // 复权因子
	CreatedAt time.Time `json:"created_at"`
}

// Income 利润表
type Income struct {
	ID            int       `json:"id"`
	TSCode        string    `json:"ts_code"`
	AnnDate       string    `json:"ann_date"`       // 公告日期
	EndDate       string    `json:"end_date"`       // 报告期
	TotalRevenue  float64   `json:"total_revenue"`  // 营业总收入
	Revenue       float64   `json:"revenue"`        // 营业收入
	NetProfit     float64   `json:"n_income"`       // 净利润
	TotalCogs     float64   `json:"total_cogs"`     // 营业总成本
	OperateProfit float64   `json:"operate_profit"` // 营业利润
	CreatedAt     time.Time `json:"created_at"`
}

// BalanceSheet 资产负债表
type BalanceSheet struct {
	ID              int       `json:"id"`
	TSCode          string    `json:"ts_code"`
	AnnDate         string    `json:"ann_date"`
	EndDate         string    `json:"end_date"`
	TotalAssets     float64   `json:"total_assets"`     // 总资产
	TotalLiab       float64   `json:"total_liab"`       // 总负债
	TotalEquity     float64   `json:"total_hldr_eqy_exc_min_int"` // 股东权益
	CreatedAt       time.Time `json:"created_at"`
}

// CashFlow 现金流量表
type CashFlow struct {
	ID             int       `json:"id"`
	TSCode         string    `json:"ts_code"`
	AnnDate        string    `json:"ann_date"`
	EndDate        string    `json:"end_date"`
	NetOperateCF   float64   `json:"n_cashflow_act"`    // 经营活动现金流净额
	NetInvestCF    float64   `json:"n_cashflow_inv_act"` // 投资活动现金流净额
	NetFinanceCF   float64   `json:"n_cash_flows_fnc_act"` // 筹资活动现金流净额
	CreatedAt      time.Time `json:"created_at"`
}

// TopList 龙虎榜
type TopList struct {
	ID         int       `json:"id"`
	TradeDate  string    `json:"trade_date"`
	TSCode     string    `json:"ts_code"`
	Name       string    `json:"name"`
	Close      float64   `json:"close"`
	PctChange  float64   `json:"pct_change"` // 涨跌幅
	Turnover   float64   `json:"turnover_rate"` // 换手率
	Amount     float64   `json:"amount"`     // 成交额
	LAmount    float64   `json:"l_sell"`     // 龙虎榜卖出额
	NetAmount  float64   `json:"net_amount"` // 龙虎榜净买入额
	Reason     string    `json:"reason"`     // 上榜原因
	CreatedAt  time.Time `json:"created_at"`
}

// ==================== 爬取结果模型 ====================

// CrawlResult 爬取结果
type CrawlResult struct {
	Provider   DataProvider   `json:"provider"`
	Catalogs   []APICatalog   `json:"catalogs"`
	Params     []APIParam     `json:"params"`
	DataFields []APIDataField `json:"data_fields"`
}

// APIDetail API详细信息（爬取中间结果）
type APIDetail struct {
	Name        string           `json:"name"`        // API名称
	APIName     string           `json:"api_name"`    // 接口名
	Description string           `json:"description"` // 描述
	Permission  string           `json:"permission"`  // 权限
	InputParams []APIParamDetail `json:"input_params"`
	OutputFields []APIFieldDetail `json:"output_fields"`
}

// APIParamDetail API参数详情
type APIParamDetail struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    string `json:"required"` // Y/N
	Description string `json:"description"`
}

// APIFieldDetail API字段详情
type APIFieldDetail struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Default     string `json:"default"` // Y/N
	Description string `json:"description"`
}
