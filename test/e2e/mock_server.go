// Package e2e 提供端到端测试
package e2e

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// MockTushareDocServer 模拟Tushare文档服务器
type MockTushareDocServer struct {
	server *httptest.Server
	mu     sync.RWMutex
}

// NewMockTushareDocServer 创建mock文档服务器
func NewMockTushareDocServer() *MockTushareDocServer {
	s := &MockTushareDocServer{}
	return s
}

// Start 启动mock服务器
func (s *MockTushareDocServer) Start() string {
	mux := http.NewServeMux()

	// 文档目录页面
	mux.HandleFunc("/document/2", s.handleDocumentIndex)
	// API详情页面
	mux.HandleFunc("/document/2/", s.handleAPIDetail)

	s.server = httptest.NewServer(mux)
	return s.server.URL
}

// Stop 停止服务器
func (s *MockTushareDocServer) Stop() {
	if s.server != nil {
		s.server.Close()
	}
}

// URL 获取服务器URL
func (s *MockTushareDocServer) URL() string {
	if s.server == nil {
		return ""
	}
	return s.server.URL
}

// handleDocumentIndex 处理文档首页
func (s *MockTushareDocServer) handleDocumentIndex(w http.ResponseWriter, r *http.Request) {
	html := getMockDocumentIndexHTML()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// handleAPIDetail 处理API详情页面
func (s *MockTushareDocServer) handleAPIDetail(w http.ResponseWriter, r *http.Request) {
	// 从URL提取doc_id
	path := r.URL.Path
	parts := strings.Split(path, "/")
	docID := ""
	for i, p := range parts {
		if p == "2" && i+1 < len(parts) {
			docID = parts[i+1]
			break
		}
	}

	html := getMockAPIDetailHTML(docID)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// MockTushareAPIServer 模拟Tushare数据API服务器
type MockTushareAPIServer struct {
	server *httptest.Server
	token  string
	mu     sync.RWMutex
}

// NewMockTushareAPIServer 创建mock API服务器
func NewMockTushareAPIServer(token string) *MockTushareAPIServer {
	return &MockTushareAPIServer{
		token: token,
	}
}

// Start 启动服务器
func (s *MockTushareAPIServer) Start() string {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleAPI)

	s.server = httptest.NewServer(mux)
	return s.server.URL
}

// Stop 停止服务器
func (s *MockTushareAPIServer) Stop() {
	if s.server != nil {
		s.server.Close()
	}
}

// URL 获取服务器URL
func (s *MockTushareAPIServer) URL() string {
	if s.server == nil {
		return ""
	}
	return s.server.URL
}

// TushareRequest Tushare API请求
type TushareRequest struct {
	APIName string                 `json:"api_name"`
	Token   string                 `json:"token"`
	Params  map[string]interface{} `json:"params"`
	Fields  string                 `json:"fields"`
}

// TushareResponse Tushare API响应
type TushareResponse struct {
	Code    int         `json:"code"`
	Msg     string      `json:"msg"`
	Data    interface{} `json:"data"`
}

// TushareDataFrame Tushare数据帧格式
type TushareDataFrame struct {
	Fields []string        `json:"fields"`
	Items  [][]interface{} `json:"items"`
}

// handleAPI 处理API请求
func (s *MockTushareAPIServer) handleAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.sendError(w, -1, "Method not allowed")
		return
	}

	var req TushareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, -1, "Invalid request")
		return
	}

	// 验证token
	if req.Token != s.token {
		s.sendError(w, -2001, "Token invalid")
		return
	}

	// 根据API名称返回mock数据
	data := s.getMockData(req.APIName, req.Params)
	s.sendSuccess(w, data)
}

// getMockData 获取mock数据
func (s *MockTushareAPIServer) getMockData(apiName string, params map[string]interface{}) *TushareDataFrame {
	switch apiName {
	case "trade_cal":
		return s.getMockTradeCal(params)
	case "stock_basic":
		return s.getMockStockBasic(params)
	case "daily":
		return s.getMockDaily(params)
	case "adj_factor":
		return s.getMockAdjFactor(params)
	case "income":
		return s.getMockIncome(params)
	case "balancesheet":
		return s.getMockBalanceSheet(params)
	case "cashflow":
		return s.getMockCashFlow(params)
	case "top_list":
		return s.getMockTopList(params)
	default:
		return &TushareDataFrame{Fields: []string{}, Items: [][]interface{}{}}
	}
}

// getMockTradeCal 获取交易日历mock数据
func (s *MockTushareAPIServer) getMockTradeCal(params map[string]interface{}) *TushareDataFrame {
	fields := []string{"exchange", "cal_date", "is_open", "pretrade_date"}
	items := [][]interface{}{}

	// 生成2025年12月的交易日
	dates := []string{
		"20251201", "20251202", "20251203", "20251204", "20251205",
		"20251208", "20251209", "20251210", "20251211", "20251212",
		"20251215", "20251216", "20251217", "20251218", "20251219",
		"20251222", "20251223", "20251224", "20251225", "20251226",
		"20251229", "20251230", "20251231",
	}

	preDates := []string{
		"20251128", "20251201", "20251202", "20251203", "20251204",
		"20251205", "20251208", "20251209", "20251210", "20251211",
		"20251212", "20251215", "20251216", "20251217", "20251218",
		"20251219", "20251222", "20251223", "20251224", "20251225",
		"20251226", "20251229", "20251230",
	}

	// 周末不开盘（简化处理）
	isOpen := []int{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 1, 1, 1, 1}

	for i, date := range dates {
		items = append(items, []interface{}{"SSE", date, isOpen[i], preDates[i]})
	}

	return &TushareDataFrame{Fields: fields, Items: items}
}

// getMockStockBasic 获取股票基本信息mock数据
func (s *MockTushareAPIServer) getMockStockBasic(params map[string]interface{}) *TushareDataFrame {
	fields := []string{"ts_code", "symbol", "name", "area", "industry", "market", "list_date"}
	items := [][]interface{}{
		{"000001.SZ", "000001", "平安银行", "深圳", "银行", "主板", "19910403"},
		{"000002.SZ", "000002", "万科A", "深圳", "房地产", "主板", "19910129"},
		{"600000.SH", "600000", "浦发银行", "上海", "银行", "主板", "19991110"},
		{"600036.SH", "600036", "招商银行", "深圳", "银行", "主板", "20020409"},
		{"000858.SZ", "000858", "五粮液", "四川", "白酒", "主板", "19980427"},
	}
	return &TushareDataFrame{Fields: fields, Items: items}
}

// getMockDaily 获取日线数据mock数据
func (s *MockTushareAPIServer) getMockDaily(params map[string]interface{}) *TushareDataFrame {
	fields := []string{"ts_code", "trade_date", "open", "high", "low", "close", "pre_close", "change", "pct_chg", "vol", "amount"}
	items := [][]interface{}{}

	// 获取参数
	tsCode, _ := params["ts_code"].(string)
	tradeDate, _ := params["trade_date"].(string)
	startDate, _ := params["start_date"].(string)
	endDate, _ := params["end_date"].(string)

	// 如果指定了单个日期
	if tradeDate != "" {
		item := s.generateDailyItem(tsCode, tradeDate)
		items = append(items, item)
		return &TushareDataFrame{Fields: fields, Items: items}
	}

	// 生成日期范围内的数据
	if startDate == "" {
		startDate = "20251201"
	}
	if endDate == "" {
		endDate = "20251231"
	}

	// 简化：生成5个交易日的数据
	tradeDates := []string{"20251201", "20251202", "20251203", "20251204", "20251205"}
	codes := []string{"000001.SZ", "000002.SZ", "600000.SH", "600036.SH", "000858.SZ"}

	if tsCode != "" {
		codes = []string{tsCode}
	}

	for _, code := range codes {
		for _, date := range tradeDates {
			items = append(items, s.generateDailyItem(code, date))
		}
	}

	return &TushareDataFrame{Fields: fields, Items: items}
}

// generateDailyItem 生成单条日线数据
func (s *MockTushareAPIServer) generateDailyItem(tsCode, tradeDate string) []interface{} {
	basePrice := 10.0 + rand.Float64()*90
	change := (rand.Float64() - 0.5) * 2
	pctChg := change / basePrice * 100
	return []interface{}{
		tsCode,
		tradeDate,
		basePrice,
		basePrice + rand.Float64()*2,
		basePrice - rand.Float64()*2,
		basePrice + change,
		basePrice,
		change,
		pctChg,
		float64(rand.Intn(1000000) + 100000),
		float64(rand.Intn(100000000) + 10000000),
	}
}

// getMockAdjFactor 获取复权因子mock数据
func (s *MockTushareAPIServer) getMockAdjFactor(params map[string]interface{}) *TushareDataFrame {
	fields := []string{"ts_code", "trade_date", "adj_factor"}
	items := [][]interface{}{}

	codes := []string{"000001.SZ", "000002.SZ", "600000.SH", "600036.SH", "000858.SZ"}
	dates := []string{"20251201", "20251202", "20251203", "20251204", "20251205"}

	for _, code := range codes {
		for _, date := range dates {
			items = append(items, []interface{}{code, date, 1.0 + rand.Float64()*0.1})
		}
	}

	return &TushareDataFrame{Fields: fields, Items: items}
}

// getMockIncome 获取利润表mock数据
func (s *MockTushareAPIServer) getMockIncome(params map[string]interface{}) *TushareDataFrame {
	fields := []string{"ts_code", "ann_date", "end_date", "total_revenue", "revenue", "n_income", "total_cogs", "operate_profit"}
	items := [][]interface{}{}

	// 从参数中获取 period，默认为 20240930
	period := "20240930"
	if p, ok := params["period"].(string); ok && p != "" {
		period = p
	}

	codes := []string{"000001.SZ", "000002.SZ", "600000.SH"}
	for _, code := range codes {
		items = append(items, []interface{}{
			code, "20240915", period,
			float64(rand.Intn(1000000000)),
			float64(rand.Intn(900000000)),
			float64(rand.Intn(100000000)),
			float64(rand.Intn(800000000)),
			float64(rand.Intn(150000000)),
		})
	}

	return &TushareDataFrame{Fields: fields, Items: items}
}

// getMockBalanceSheet 获取资产负债表mock数据
func (s *MockTushareAPIServer) getMockBalanceSheet(params map[string]interface{}) *TushareDataFrame {
	fields := []string{"ts_code", "ann_date", "end_date", "total_assets", "total_liab", "total_hldr_eqy_exc_min_int"}
	items := [][]interface{}{}

	// 从参数中获取 period，默认为 20240930
	period := "20240930"
	if p, ok := params["period"].(string); ok && p != "" {
		period = p
	}

	codes := []string{"000001.SZ", "000002.SZ", "600000.SH"}
	for _, code := range codes {
		totalAssets := float64(rand.Intn(10000000000))
		totalLiab := totalAssets * (0.5 + rand.Float64()*0.3)
		items = append(items, []interface{}{
			code, "20240915", period,
			totalAssets,
			totalLiab,
			totalAssets - totalLiab,
		})
	}

	return &TushareDataFrame{Fields: fields, Items: items}
}

// getMockCashFlow 获取现金流量表mock数据
func (s *MockTushareAPIServer) getMockCashFlow(params map[string]interface{}) *TushareDataFrame {
	fields := []string{"ts_code", "ann_date", "end_date", "n_cashflow_act", "n_cashflow_inv_act", "n_cash_flows_fnc_act"}
	items := [][]interface{}{}

	// 从参数中获取 period，默认为 20240930
	period := "20240930"
	if p, ok := params["period"].(string); ok && p != "" {
		period = p
	}

	codes := []string{"000001.SZ", "000002.SZ", "600000.SH"}
	for _, code := range codes {
		items = append(items, []interface{}{
			code, "20240915", period,
			float64(rand.Intn(500000000) - 100000000),
			float64(rand.Intn(300000000) - 200000000),
			float64(rand.Intn(400000000) - 200000000),
		})
	}

	return &TushareDataFrame{Fields: fields, Items: items}
}

// getMockTopList 获取龙虎榜mock数据
func (s *MockTushareAPIServer) getMockTopList(params map[string]interface{}) *TushareDataFrame {
	fields := []string{"trade_date", "ts_code", "name", "close", "pct_change", "turnover_rate", "amount", "l_sell", "net_amount", "reason"}
	items := [][]interface{}{}

	dates := []string{"20251201", "20251202", "20251203"}
	for _, date := range dates {
		items = append(items, []interface{}{
			date, "000001.SZ", "平安银行",
			12.5 + rand.Float64()*2,
			(rand.Float64() - 0.5) * 20,
			rand.Float64() * 10,
			float64(rand.Intn(1000000000)),
			float64(rand.Intn(500000000)),
			float64(rand.Intn(200000000) - 100000000),
			"日涨幅偏离值达7%",
		})
	}

	return &TushareDataFrame{Fields: fields, Items: items}
}

// sendSuccess 发送成功响应
func (s *MockTushareAPIServer) sendSuccess(w http.ResponseWriter, data *TushareDataFrame) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TushareResponse{
		Code: 0,
		Msg:  "",
		Data: data,
	})
}

// sendError 发送错误响应
func (s *MockTushareAPIServer) sendError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TushareResponse{
		Code: code,
		Msg:  msg,
		Data: nil,
	})
}

// getMockDocumentIndexHTML 获取mock文档首页HTML
func getMockDocumentIndexHTML() string {
	return `<!DOCTYPE html>
<html>
<head><title>Tushare数据接口文档</title></head>
<body>
<div id="jstree">
  <ul>
    <li id="j1_1" data-jstree='{"type":"folder"}'>
      <a href="#" id="j1_1_anchor">股票数据</a>
      <ul>
        <li id="j1_2" data-jstree='{"type":"folder"}'>
          <a href="#" id="j1_2_anchor">基础数据</a>
          <ul>
            <li id="j1_3" data-jstree='{"type":"file"}'>
              <a href="/document/2/25" id="j1_3_anchor">股票列表</a>
            </li>
            <li id="j1_4" data-jstree='{"type":"file"}'>
              <a href="/document/2/26" id="j1_4_anchor">交易日历</a>
            </li>
          </ul>
        </li>
        <li id="j1_5" data-jstree='{"type":"folder"}'>
          <a href="#" id="j1_5_anchor">行情数据</a>
          <ul>
            <li id="j1_6" data-jstree='{"type":"file"}'>
              <a href="/document/2/27" id="j1_6_anchor">日线行情</a>
            </li>
            <li id="j1_7" data-jstree='{"type":"file"}'>
              <a href="/document/2/28" id="j1_7_anchor">复权因子</a>
            </li>
          </ul>
        </li>
        <li id="j1_8" data-jstree='{"type":"folder"}'>
          <a href="#" id="j1_8_anchor">财务数据</a>
          <ul>
            <li id="j1_9" data-jstree='{"type":"file"}'>
              <a href="/document/2/33" id="j1_9_anchor">利润表</a>
            </li>
            <li id="j1_10" data-jstree='{"type":"file"}'>
              <a href="/document/2/36" id="j1_10_anchor">资产负债表</a>
            </li>
            <li id="j1_11" data-jstree='{"type":"file"}'>
              <a href="/document/2/44" id="j1_11_anchor">现金流量表</a>
            </li>
          </ul>
        </li>
        <li id="j1_12" data-jstree='{"type":"folder"}'>
          <a href="#" id="j1_12_anchor">市场参考</a>
          <ul>
            <li id="j1_13" data-jstree='{"type":"file"}'>
              <a href="/document/2/106" id="j1_13_anchor">龙虎榜每日榜单</a>
            </li>
          </ul>
        </li>
      </ul>
    </li>
  </ul>
</div>
</body>
</html>`
}

// getMockAPIDetailHTML 获取mock API详情页HTML
func getMockAPIDetailHTML(docID string) string {
	apiDetails := map[string]struct {
		name       string
		apiName    string
		desc       string
		permission string
		inputs     []APIParamDetail
		outputs    []APIFieldDetail
	}{
		"25": {
			name:       "股票列表",
			apiName:    "stock_basic",
			desc:       "获取基础信息数据，包括股票代码、名称、上市日期、退市日期等",
			permission: "120积分/每分钟",
			inputs: []APIParamDetail{
				{Name: "is_hs", Type: "str", Required: "N", Description: "是否沪深港通标的"},
				{Name: "list_status", Type: "str", Required: "N", Description: "上市状态"},
				{Name: "exchange", Type: "str", Required: "N", Description: "交易所"},
			},
			outputs: []APIFieldDetail{
				{Name: "ts_code", Type: "str", Default: "Y", Description: "TS代码"},
				{Name: "symbol", Type: "str", Default: "Y", Description: "股票代码"},
				{Name: "name", Type: "str", Default: "Y", Description: "股票名称"},
				{Name: "area", Type: "str", Default: "Y", Description: "地区"},
				{Name: "industry", Type: "str", Default: "Y", Description: "行业"},
				{Name: "market", Type: "str", Default: "Y", Description: "市场类型"},
				{Name: "list_date", Type: "str", Default: "Y", Description: "上市日期"},
			},
		},
		"26": {
			name:       "交易日历",
			apiName:    "trade_cal",
			desc:       "获取各大交易所交易日历数据",
			permission: "无限制",
			inputs: []APIParamDetail{
				{Name: "exchange", Type: "str", Required: "N", Description: "交易所"},
				{Name: "start_date", Type: "str", Required: "N", Description: "开始日期"},
				{Name: "end_date", Type: "str", Required: "N", Description: "结束日期"},
				{Name: "is_open", Type: "str", Required: "N", Description: "是否交易"},
			},
			outputs: []APIFieldDetail{
				{Name: "exchange", Type: "str", Default: "Y", Description: "交易所"},
				{Name: "cal_date", Type: "str", Default: "Y", Description: "日历日期"},
				{Name: "is_open", Type: "int", Default: "Y", Description: "是否交易"},
				{Name: "pretrade_date", Type: "str", Default: "N", Description: "上一交易日"},
			},
		},
		"27": {
			name:       "日线行情",
			apiName:    "daily",
			desc:       "获取股票行情数据，包括开高低收、成交量、成交额等",
			permission: "600积分/每分钟",
			inputs: []APIParamDetail{
				{Name: "ts_code", Type: "str", Required: "N", Description: "股票代码"},
				{Name: "trade_date", Type: "str", Required: "N", Description: "交易日期"},
				{Name: "start_date", Type: "str", Required: "N", Description: "开始日期"},
				{Name: "end_date", Type: "str", Required: "N", Description: "结束日期"},
			},
			outputs: []APIFieldDetail{
				{Name: "ts_code", Type: "str", Default: "Y", Description: "股票代码"},
				{Name: "trade_date", Type: "str", Default: "Y", Description: "交易日期"},
				{Name: "open", Type: "float", Default: "Y", Description: "开盘价"},
				{Name: "high", Type: "float", Default: "Y", Description: "最高价"},
				{Name: "low", Type: "float", Default: "Y", Description: "最低价"},
				{Name: "close", Type: "float", Default: "Y", Description: "收盘价"},
				{Name: "pre_close", Type: "float", Default: "Y", Description: "昨收价"},
				{Name: "change", Type: "float", Default: "Y", Description: "涨跌额"},
				{Name: "pct_chg", Type: "float", Default: "Y", Description: "涨跌幅"},
				{Name: "vol", Type: "float", Default: "Y", Description: "成交量"},
				{Name: "amount", Type: "float", Default: "Y", Description: "成交额"},
			},
		},
		"28": {
			name:       "复权因子",
			apiName:    "adj_factor",
			desc:       "获取股票复权因子，可用于计算后复权价格",
			permission: "400积分/每分钟",
			inputs: []APIParamDetail{
				{Name: "ts_code", Type: "str", Required: "Y", Description: "股票代码"},
				{Name: "trade_date", Type: "str", Required: "N", Description: "交易日期"},
				{Name: "start_date", Type: "str", Required: "N", Description: "开始日期"},
				{Name: "end_date", Type: "str", Required: "N", Description: "结束日期"},
			},
			outputs: []APIFieldDetail{
				{Name: "ts_code", Type: "str", Default: "Y", Description: "股票代码"},
				{Name: "trade_date", Type: "str", Default: "Y", Description: "交易日期"},
				{Name: "adj_factor", Type: "float", Default: "Y", Description: "复权因子"},
			},
		},
		"33": {
			name:       "利润表",
			apiName:    "income",
			desc:       "获取上市公司财务利润表数据",
			permission: "2000积分/每分钟",
			inputs: []APIParamDetail{
				{Name: "ts_code", Type: "str", Required: "Y", Description: "股票代码"},
				{Name: "ann_date", Type: "str", Required: "N", Description: "公告日期"},
				{Name: "period", Type: "str", Required: "N", Description: "报告期"},
			},
			outputs: []APIFieldDetail{
				{Name: "ts_code", Type: "str", Default: "Y", Description: "股票代码"},
				{Name: "ann_date", Type: "str", Default: "Y", Description: "公告日期"},
				{Name: "end_date", Type: "str", Default: "Y", Description: "报告期"},
				{Name: "total_revenue", Type: "float", Default: "Y", Description: "营业总收入"},
				{Name: "revenue", Type: "float", Default: "Y", Description: "营业收入"},
				{Name: "n_income", Type: "float", Default: "Y", Description: "净利润"},
				{Name: "total_cogs", Type: "float", Default: "N", Description: "营业总成本"},
				{Name: "operate_profit", Type: "float", Default: "N", Description: "营业利润"},
			},
		},
		"36": {
			name:       "资产负债表",
			apiName:    "balancesheet",
			desc:       "获取上市公司资产负债表数据",
			permission: "2000积分/每分钟",
			inputs: []APIParamDetail{
				{Name: "ts_code", Type: "str", Required: "Y", Description: "股票代码"},
				{Name: "ann_date", Type: "str", Required: "N", Description: "公告日期"},
				{Name: "period", Type: "str", Required: "N", Description: "报告期"},
			},
			outputs: []APIFieldDetail{
				{Name: "ts_code", Type: "str", Default: "Y", Description: "股票代码"},
				{Name: "ann_date", Type: "str", Default: "Y", Description: "公告日期"},
				{Name: "end_date", Type: "str", Default: "Y", Description: "报告期"},
				{Name: "total_assets", Type: "float", Default: "Y", Description: "总资产"},
				{Name: "total_liab", Type: "float", Default: "Y", Description: "总负债"},
				{Name: "total_hldr_eqy_exc_min_int", Type: "float", Default: "Y", Description: "股东权益"},
			},
		},
		"44": {
			name:       "现金流量表",
			apiName:    "cashflow",
			desc:       "获取上市公司现金流量表数据",
			permission: "2000积分/每分钟",
			inputs: []APIParamDetail{
				{Name: "ts_code", Type: "str", Required: "Y", Description: "股票代码"},
				{Name: "ann_date", Type: "str", Required: "N", Description: "公告日期"},
				{Name: "period", Type: "str", Required: "N", Description: "报告期"},
			},
			outputs: []APIFieldDetail{
				{Name: "ts_code", Type: "str", Default: "Y", Description: "股票代码"},
				{Name: "ann_date", Type: "str", Default: "Y", Description: "公告日期"},
				{Name: "end_date", Type: "str", Default: "Y", Description: "报告期"},
				{Name: "n_cashflow_act", Type: "float", Default: "Y", Description: "经营活动现金流净额"},
				{Name: "n_cashflow_inv_act", Type: "float", Default: "Y", Description: "投资活动现金流净额"},
				{Name: "n_cash_flows_fnc_act", Type: "float", Default: "Y", Description: "筹资活动现金流净额"},
			},
		},
		"106": {
			name:       "龙虎榜每日榜单",
			apiName:    "top_list",
			desc:       "龙虎榜每日交易明细",
			permission: "600积分/每分钟",
			inputs: []APIParamDetail{
				{Name: "trade_date", Type: "str", Required: "Y", Description: "交易日期"},
				{Name: "ts_code", Type: "str", Required: "N", Description: "股票代码"},
			},
			outputs: []APIFieldDetail{
				{Name: "trade_date", Type: "str", Default: "Y", Description: "交易日期"},
				{Name: "ts_code", Type: "str", Default: "Y", Description: "股票代码"},
				{Name: "name", Type: "str", Default: "Y", Description: "股票名称"},
				{Name: "close", Type: "float", Default: "Y", Description: "收盘价"},
				{Name: "pct_change", Type: "float", Default: "Y", Description: "涨跌幅"},
				{Name: "turnover_rate", Type: "float", Default: "N", Description: "换手率"},
				{Name: "amount", Type: "float", Default: "Y", Description: "成交额"},
				{Name: "l_sell", Type: "float", Default: "N", Description: "龙虎榜卖出额"},
				{Name: "net_amount", Type: "float", Default: "N", Description: "龙虎榜净买入"},
				{Name: "reason", Type: "str", Default: "Y", Description: "上榜原因"},
			},
		},
	}

	detail, ok := apiDetails[docID]
	if !ok {
		return "<html><body><h1>404 Not Found</h1></body></html>"
	}

	// 生成输入参数表格
	inputRows := ""
	for _, p := range detail.inputs {
		inputRows += fmt.Sprintf("<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>", p.Name, p.Type, p.Required, p.Description)
	}

	// 生成输出参数表格
	outputRows := ""
	for _, f := range detail.outputs {
		outputRows += fmt.Sprintf("<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>", f.Name, f.Type, f.Default, f.Description)
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><title>%s - Tushare</title></head>
<body>
<div class="content">
  <h1>%s</h1>
  <div class="api-info">
    <p><strong>接口：</strong>%s</p>
    <p><strong>描述：</strong>%s</p>
    <p><strong>权限：</strong>%s</p>
  </div>
  <h2>输入参数</h2>
  <table class="params-table">
    <thead><tr><th>名称</th><th>类型</th><th>必选</th><th>描述</th></tr></thead>
    <tbody>%s</tbody>
  </table>
  <h2>输出参数</h2>
  <table class="fields-table">
    <thead><tr><th>名称</th><th>类型</th><th>默认显示</th><th>描述</th></tr></thead>
    <tbody>%s</tbody>
  </table>
</div>
</body>
</html>`, detail.name, detail.name, detail.apiName, detail.desc, detail.permission, inputRows, outputRows)
}

func init() {
	rand.Seed(time.Now().UnixNano())
}
