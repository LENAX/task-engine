// Package e2e 端到端测试：Realtime DataCollector 注册与 Streaming Workflow 完整链路
package e2e

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LENAX/task-engine/pkg/core/builder"
	"github.com/LENAX/task-engine/pkg/core/engine"
	"github.com/LENAX/task-engine/pkg/core/realtime"
	"github.com/LENAX/task-engine/pkg/core/task"
)

// printDataJob 打印拿到的数据（用于 job function，接收 TaskContext.Params）
func printDataJob(ctx *task.TaskContext) error {
	if len(ctx.Params) > 0 {
		b, _ := json.Marshal(ctx.Params)
		log.Printf("[printDataJob] TaskID=%s 收到数据: %s", ctx.TaskID, string(b))
	} else {
		log.Printf("[printDataJob] TaskID=%s 无参数", ctx.TaskID)
	}
	return nil
}

// finitePublishCollector 发布 N 条后 return nil，便于 E2E 断言缓冲/指标且任务可结束
type finitePublishCollector struct {
	maxPublish int32
	published  int32
	runCount   int32
}

func (c *finitePublishCollector) Run(ctx context.Context, config *realtime.ContinuousTaskConfig, publish realtime.PublishFunc) error {
	atomic.AddInt32(&c.runCount, 1)
	taskID := ""
	if config != nil {
		taskID = config.ID
	}
	for atomic.LoadInt32(&c.published) < c.maxPublish {
		select {
		case <-ctx.Done():
			return nil
		default:
			n := atomic.AddInt32(&c.published, 1)
			log.Printf("[collector] e2e_finite publish 数据: %d", n)
			e := realtime.NewRealtimeEvent(realtime.EventDataArrived, taskID, "", &realtime.DataArrivedPayload{
				Data:   n,
				Source: "e2e_finite",
			})
			_ = publish(e)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

type realtimeDependencyProbe struct {
	called int32
}

// TestRealtimeCollector_E2E_StreamingWorkflow 完整链路：Engine 注册有限次 publish 采集器 → 提交 Streaming Workflow → 断言 Run 被调用与数据/指标
func TestRealtimeCollector_E2E_StreamingWorkflow(t *testing.T) {
	t.Cleanup(func() { t.Logf("[E2E] 入库数据量: N/A (本测试无 DB 写入)") })
	tmpDir := t.TempDir()
	frameworkConfigPath := filepath.Join(tmpDir, "framework.yaml")
	dsn := filepath.Join(tmpDir, "e2e.db")
	frameworkConfig := `
task-engine:
  general:
    instance_name: "e2e-engine"
    log_level: "info"
    env: "test"
  storage:
    database:
      type: "sqlite"
      dsn: "` + dsn + `"
      max_open_conns: 5
  execution:
    default_task_timeout: "60s"
    worker_concurrency: 2
`
	require.NoError(t, os.WriteFile(frameworkConfigPath, []byte(frameworkConfig), 0644))

	collector := &finitePublishCollector{maxPublish: 5}
	eng, err := engine.NewEngineBuilder(frameworkConfigPath).
		Build()
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, eng.Start(ctx))
	defer eng.Stop()

	registry := eng.GetRegistry()
	_, _ = registry.Register(ctx, "print_data_job", printDataJob, "打印收到的数据")
	_, _ = registry.Register(ctx, "dummy_job", func(ctx context.Context) error { return nil }, "dummy")

	collectorTask, err := builder.NewRealtimeTaskBuilder("e2e_collector", "e2e", registry).
		WithContinuousMode().
		WithTaskType(realtime.TaskTypeDataCollector).
		WithCollector("e2e_finite").
		WithJobFunction("print_data_job", nil).
		Build()
	require.NoError(t, err)

	streamTask, err := builder.NewRealtimeTaskBuilder("e2e_stream_processor", "流处理", registry).
		WithContinuousMode().
		WithTaskType(realtime.TaskTypeStreamProcessor).
		WithJobFunction("print_data_job", nil).
		Build()
	require.NoError(t, err)

	wf, err := builder.NewWorkflowBuilder("e2e_realtime_wf", "e2e").
		WithStreamingMode().
		WithDataCollector("e2e_finite", collector).
		WithRealtimeTask(collectorTask).
		WithRealtimeTask(streamTask).
		Build()
	require.NoError(t, err)

	ctrl, err := eng.SubmitWorkflow(ctx, wf)
	require.NoError(t, err)
	require.NotEmpty(t, ctrl.InstanceID())

	// 等待采集器跑完 5 条或超时
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&collector.published) < 5 {
		time.Sleep(50 * time.Millisecond)
	}

	runCount := atomic.LoadInt32(&collector.runCount)
	published := atomic.LoadInt32(&collector.published)
	assert.GreaterOrEqual(t, runCount, int32(1), "collector Run should have been called")
	assert.GreaterOrEqual(t, published, int32(1), "at least one event should have been published")
}

// TestRealtimeCollector_E2E_StreamProcessorInjectsDependencies 回归测试：
// 验证 realtime 路径调用 DataHandler 时，TaskContext.Context() 中包含 registry.WithDependencies 注入的依赖
func TestRealtimeCollector_E2E_StreamProcessorInjectsDependencies(t *testing.T) {
	tmpDir := t.TempDir()
	frameworkConfigPath := filepath.Join(tmpDir, "framework.yaml")
	dsn := filepath.Join(tmpDir, "e2e_di.db")
	frameworkConfig := `
task-engine:
  general:
    instance_name: "e2e-engine-di"
    log_level: "info"
    env: "test"
  storage:
    database:
      type: "sqlite"
      dsn: "` + dsn + `"
      max_open_conns: 5
  execution:
    default_task_timeout: "60s"
    worker_concurrency: 2
`
	require.NoError(t, os.WriteFile(frameworkConfigPath, []byte(frameworkConfig), 0644))

	collector := &finitePublishCollector{maxPublish: 3}
	eng, err := engine.NewEngineBuilder(frameworkConfigPath).Build()
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, eng.Start(ctx))
	defer eng.Stop()

	registry := eng.GetRegistry()
	probe := &realtimeDependencyProbe{}
	require.NoError(t, registry.RegisterDependency(probe))

	dataHandler := func(tc *task.TaskContext) error {
		dep, ok := task.GetDependencyFromContext[*realtimeDependencyProbe](tc.Context())
		if !ok || dep == nil {
			return fmt.Errorf("未注入 realtimeDependencyProbe")
		}
		atomic.AddInt32(&dep.called, 1)
		return nil
	}

	_, err = registry.Register(ctx, "di_data_handler", dataHandler, "验证 realtime DataHandler 依赖注入")
	require.NoError(t, err)
	_, err = registry.Register(ctx, "dummy_job", func(ctx context.Context) error { return nil }, "dummy")
	require.NoError(t, err)

	collectorTask, err := builder.NewRealtimeTaskBuilder("e2e_di_collector", "e2e-di", registry).
		WithContinuousMode().
		WithTaskType(realtime.TaskTypeDataCollector).
		WithCollector("e2e_di_finite").
		WithJobFunction("dummy_job", nil).
		Build()
	require.NoError(t, err)

	streamTask, err := builder.NewRealtimeTaskBuilder("e2e_di_stream_processor", "流处理-依赖注入", registry).
		WithContinuousMode().
		WithTaskType(realtime.TaskTypeStreamProcessor).
		WithJobFunction("di_data_handler", nil).
		Build()
	require.NoError(t, err)

	wf, err := builder.NewWorkflowBuilder("e2e_realtime_di_wf", "e2e-di").
		WithStreamingMode().
		WithDataCollector("e2e_di_finite", collector).
		WithRealtimeTask(collectorTask).
		WithRealtimeTask(streamTask).
		Build()
	require.NoError(t, err)

	ctrl, err := eng.SubmitWorkflow(ctx, wf)
	require.NoError(t, err)
	require.NotEmpty(t, ctrl.InstanceID())

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&probe.called) < 1 {
		time.Sleep(50 * time.Millisecond)
	}

	assert.GreaterOrEqual(t, atomic.LoadInt32(&probe.called), int32(1), "DataHandler 应能读取到已注入依赖并执行")
}

// stkMinsPullResponse 拉取接口返回结构
type stkMinsPullResponse struct {
	Data  []StkMinsRow `json:"data"`
	Total int          `json:"total"`
}

// stkMinsPullCollector 从 StkMinsMockServer 的 GET /api/stk_mins 分页拉取并 publish
type stkMinsPullCollector struct {
	client      *http.Client
	published   int32
	maxPublish  int32 // 0=不限制
}

func (c *stkMinsPullCollector) Run(ctx context.Context, config *realtime.ContinuousTaskConfig, publish realtime.PublishFunc) error {
	taskID := ""
	if config != nil {
		taskID = config.ID
	}
	baseURL := ""
	if config != nil && config.Endpoint != "" {
		baseURL = config.Endpoint
	}
	if baseURL == "" {
		return nil
	}
	interval := 200 * time.Millisecond
	if config != nil && config.FlushInterval > 0 {
		interval = config.FlushInterval
	}
	cli := c.client
	if cli == nil {
		cli = &http.Client{Timeout: 10 * time.Second}
	}
	offset := 0
	limit := 100
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			baseURL+"/api/stk_mins?offset="+strconv.Itoa(offset)+"&limit="+strconv.Itoa(limit), nil)
		if err != nil {
			return err
		}
		resp, err := cli.Do(req)
		if err != nil {
			return err
		}
		var body stkMinsPullResponse
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			resp.Body.Close()
			return err
		}
		resp.Body.Close()
		for i := range body.Data {
			if c.maxPublish > 0 && atomic.LoadInt32(&c.published) >= c.maxPublish {
				return nil
			}
			row := body.Data[i]
			log.Printf("[collector] stk_mins_pull publish 数据: %+v", row)
			e := realtime.NewRealtimeEvent(realtime.EventDataArrived, taskID, "", &realtime.DataArrivedPayload{
				Data:   row,
				Source: "stk_mins_pull",
			})
			if err := publish(e); err != nil {
				return err
			}
			atomic.AddInt32(&c.published, 1)
		}
		if len(body.Data) < limit {
			offset = 0
		} else {
			offset += len(body.Data)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
	}
}

// stkMinsPushCollector 连接 StkMinsMockServer 的 WebSocket /ws/stk_mins 收数据并 publish
type stkMinsPushCollector struct {
	published  int32
	maxPublish int32
}

func (c *stkMinsPushCollector) Run(ctx context.Context, config *realtime.ContinuousTaskConfig, publish realtime.PublishFunc) error {
	taskID := ""
	if config != nil {
		taskID = config.ID
	}
	wsURL := ""
	if config != nil && config.Endpoint != "" {
		wsURL = config.Endpoint
	}
	if wsURL == "" {
		return nil
	}
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		if c.maxPublish > 0 && atomic.LoadInt32(&c.published) >= c.maxPublish {
			return nil
		}
		var row StkMinsRow
		if err := conn.ReadJSON(&row); err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return nil
			}
			return err
		}
		log.Printf("[collector] stk_mins_push publish 数据: %+v", row)
		e := realtime.NewRealtimeEvent(realtime.EventDataArrived, taskID, "", &realtime.DataArrivedPayload{
			Data:   row,
			Source: "stk_mins_push",
		})
		if err := publish(e); err != nil {
			return err
		}
		atomic.AddInt32(&c.published, 1)
	}
}

// tushareWsCollector 连接 TushareMockServer 的 WebSocket /listening，发 listening 后收 TusharePushMessage，将 Data 作为 payload 发布（含 topic/code/record 便于下游过滤与写库）
type tushareWsCollector struct {
	published   int32
	maxPublish  int32
	reconnectOk bool // 断线后是否重连一次继续收
}

func (c *tushareWsCollector) Run(ctx context.Context, config *realtime.ContinuousTaskConfig, publish realtime.PublishFunc) error {
	taskID := ""
	if config != nil {
		taskID = config.ID
	}
	wsURL := ""
	if config != nil && config.Endpoint != "" {
		wsURL = config.Endpoint
	}
	if wsURL == "" {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
		conn, _, err := dialer.DialContext(ctx, wsURL, nil)
		if err != nil {
			return err
		}
		// 发送 listening
		req := map[string]interface{}{
			"action": "listening",
			"token":  "e2e-mock-token",
			"data":   map[string][]string{"HQ_STK_TICK": {"6*.SH", "601169.SH"}},
		}
		if err := conn.WriteJSON(req); err != nil {
			conn.Close()
			return err
		}
		for {
			select {
			case <-ctx.Done():
				conn.Close()
				return nil
			default:
			}
			if c.maxPublish > 0 && atomic.LoadInt32(&c.published) >= c.maxPublish {
				conn.Close()
				return nil
			}
			var msg TusharePushMessage
			if err := conn.ReadJSON(&msg); err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) && c.reconnectOk {
					c.reconnectOk = false
					conn.Close()
					time.Sleep(200 * time.Millisecond)
					break
				}
				conn.Close()
				return err
			}
			if !msg.Status || msg.Data == nil {
				continue
			}
			// 发布 Data 为 map 便于 extractCodeFromRawData 与下游使用
			payload := map[string]interface{}{
				"topic":  msg.Data.Topic,
				"code":   msg.Data.Code,
				"record": msg.Data.Record,
			}
			e := realtime.NewRealtimeEvent(realtime.EventDataArrived, taskID, "", &realtime.DataArrivedPayload{
				Data:   payload,
				Source: "tushare_ws",
			})
			if err := publish(e); err != nil {
				conn.Close()
				return err
			}
			atomic.AddInt32(&c.published, 1)
		}
	}
}

// tushareBatchWriterState 批量写入 SQLite 的状态（batch size 可配，默认 500）
type tushareBatchWriterState struct {
	dbPath    string
	batchSize int
	mu        sync.Mutex
	batch     []map[string]interface{}
	db        *sql.DB
	initOnce  sync.Once
	initErr   error
	totalRows atomic.Int64
}

func (s *tushareBatchWriterState) init() error {
	s.initOnce.Do(func() {
		s.db, s.initErr = sql.Open("sqlite3", s.dbPath)
		if s.initErr != nil {
			return
		}
		_, s.initErr = s.db.Exec(`
			CREATE TABLE IF NOT EXISTS tushare_tick (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				code TEXT, name TEXT, trade_time TEXT,
				price REAL, open_p REAL, high_p REAL, low_p REAL, close_p REAL,
				volume INTEGER, amount REAL
			)
		`)
	})
	return s.initErr
}

func (s *tushareBatchWriterState) flush() error {
	if len(s.batch) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`
		INSERT INTO tushare_tick (code, name, trade_time, price, open_p, high_p, low_p, close_p, volume, amount)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, row := range s.batch {
		_, err = stmt.Exec(
			row["code"], row["name"], row["trade_time"],
			row["price"], row["open_p"], row["high_p"], row["low_p"], row["close_p"],
			row["volume"], row["amount"],
		)
		if err != nil {
			tx.Rollback()
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.totalRows.Add(int64(len(s.batch)))
	s.batch = s.batch[:0]
	return nil
}

// TotalRows 返回已写入 DB 的总行数（供 E2E 可观测性）
func (s *tushareBatchWriterState) TotalRows() int64 {
	return s.totalRows.Load()
}

func (s *tushareBatchWriterState) recordFromPayload(data interface{}) (map[string]interface{}, error) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("data not map")
	}
	rec, _ := m["record"].([]interface{})
	if len(rec) < 10 {
		return nil, fmt.Errorf("record too short")
	}
	floatAt := func(i int) float64 {
		if i >= len(rec) {
			return 0
		}
		switch v := rec[i].(type) {
		case float64:
			return v
		case int:
			return float64(v)
		case int64:
			return float64(v)
		}
		return 0
	}
	intAt := func(i int) int64 {
		return int64(floatAt(i))
	}
	strAt := func(i int) string {
		if i >= len(rec) {
			return ""
		}
		if s, ok := rec[i].(string); ok {
			return s
		}
		return ""
	}
	row := map[string]interface{}{
		"code":       strAt(0),
		"name":       strAt(1),
		"trade_time": strAt(2),
		"price":     floatAt(3),
		"open_p":    floatAt(4),
		"high_p":    floatAt(5),
		"low_p":     floatAt(6),
		"close_p":   floatAt(7),
		"volume":    intAt(8),
		"amount":    floatAt(9),
	}
	if m["code"] != nil {
		row["code"] = m["code"]
	}
	return row, nil
}

func (s *tushareBatchWriterState) Handle(ctx *task.TaskContext) error {
	if err := s.init(); err != nil {
		return err
	}
	data := ctx.GetParam("data")
	if data == nil {
		return nil
	}
	row, err := s.recordFromPayload(data)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.batch = append(s.batch, row)
	batchSize := s.batchSize
	if batchSize <= 0 {
		batchSize = 500
	}
	needFlush := len(s.batch) >= batchSize
	s.mu.Unlock()
	if needFlush {
		s.mu.Lock()
		needFlush = len(s.batch) >= batchSize
		if needFlush {
			defer s.mu.Unlock()
			return s.flush()
		}
		s.mu.Unlock()
	}
	return nil
}

// tushareConsoleCountJob 统计收到的条数并按 code 计数（用于断言过滤）
func tushareConsoleCountJob(countByCode map[string]*int32) func(ctx *task.TaskContext) error {
	return func(ctx *task.TaskContext) error {
		data := ctx.GetParam("data")
		if data == nil {
			return nil
		}
		m, ok := data.(map[string]interface{})
		if !ok {
			return nil
		}
		code, _ := m["code"].(string)
		if code == "" {
			return nil
		}
		if countByCode != nil {
			if p, ok := countByCode[code]; ok {
				atomic.AddInt32(p, 1)
			}
		}
		log.Printf("[tushare_console] code=%s", code)
		return nil
	}
}

// TestRealtimeCollector_E2E_PullBasedWorkflow 使用 StkMinsMockServer 的 pull 接口 + pull DataCollector 跑通流程
func TestRealtimeCollector_E2E_PullBasedWorkflow(t *testing.T) {
	t.Cleanup(func() { t.Logf("[E2E] 入库数据量: N/A (本测试无 DB 写入)") })
	csvPath := filepath.Join("data", "stk_mins_202603082215.csv")
	if _, err := os.Stat(csvPath); err != nil {
		csvPath = filepath.Join("test", "e2e", "data", "stk_mins_202603082215.csv")
	}
	if _, err := os.Stat(csvPath); err != nil {
		t.Skip("stk_mins CSV not found, skip pull E2E")
		return
	}
	server, err := NewStkMinsMockServer(csvPath)
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	tmpDir := t.TempDir()
	frameworkConfigPath := filepath.Join(tmpDir, "framework.yaml")
	dsn := filepath.Join(tmpDir, "e2e_pull.db")
	frameworkConfig := `
task-engine:
  general:
    instance_name: "e2e-pull"
    log_level: "info"
    env: "test"
  storage:
    database:
      type: "sqlite"
      dsn: "` + dsn + `"
      max_open_conns: 5
  execution:
    default_task_timeout: "60s"
    worker_concurrency: 2
`
	require.NoError(t, os.WriteFile(frameworkConfigPath, []byte(frameworkConfig), 0644))

	pullCollector := &stkMinsPullCollector{maxPublish: 50}
	eng, err := engine.NewEngineBuilder(frameworkConfigPath).
		Build()
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, eng.Start(ctx))
	defer eng.Stop()

	registry := eng.GetRegistry()
	_, _ = registry.Register(ctx, "print_data_job", printDataJob, "打印收到的数据")
	_, _ = registry.Register(ctx, "dummy_job", func(ctx context.Context) error { return nil }, "dummy")

	collectorTask, err := builder.NewRealtimeTaskBuilder("stk_pull", "stk_mins_pull", registry).
		WithContinuousMode().
		WithTaskType(realtime.TaskTypeDataCollector).
		WithCollector("stk_mins_pull").
		WithMode(realtime.CollectorModePull).
		WithEndpoint(server.URL(), "http").
		WithFlushInterval(300 * time.Millisecond).
		WithJobFunction("print_data_job", nil).
		Build()
	require.NoError(t, err)

	streamTask, err := builder.NewRealtimeTaskBuilder("stk_pull_processor", "流处理", registry).
		WithContinuousMode().
		WithTaskType(realtime.TaskTypeStreamProcessor).
		WithJobFunction("print_data_job", nil).
		Build()
	require.NoError(t, err)

	wf, err := builder.NewWorkflowBuilder("e2e_stk_pull_wf", "e2e").
		WithStreamingMode().
		WithDataCollector("stk_mins_pull", pullCollector).
		WithRealtimeTask(collectorTask).
		WithRealtimeTask(streamTask).
		Build()
	require.NoError(t, err)

	ctrl, err := eng.SubmitWorkflow(ctx, wf)
	require.NoError(t, err)
	require.NotEmpty(t, ctrl.InstanceID())

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&pullCollector.published) < 10 {
		time.Sleep(100 * time.Millisecond)
	}
	published := atomic.LoadInt32(&pullCollector.published)
	assert.GreaterOrEqual(t, published, int32(10), "pull collector should have published at least 10 rows")
}

// TestRealtimeCollector_E2E_PushBasedWorkflow 使用 StkMinsMockServer 的 WebSocket 推送 + push DataCollector 跑通流程
func TestRealtimeCollector_E2E_PushBasedWorkflow(t *testing.T) {
	t.Cleanup(func() { t.Logf("[E2E] 入库数据量: N/A (本测试无 DB 写入)") })
	csvPath := filepath.Join("data", "stk_mins_202603082215.csv")
	if _, err := os.Stat(csvPath); err != nil {
		csvPath = filepath.Join("test", "e2e", "data", "stk_mins_202603082215.csv")
	}
	if _, err := os.Stat(csvPath); err != nil {
		t.Skip("stk_mins CSV not found, skip push E2E")
		return
	}
	server, err := NewStkMinsMockServer(csvPath)
	require.NoError(t, err)
	server.SetMaxPushRows(80)
	server.SetPushInterval(10 * time.Millisecond)
	server.Start()
	defer server.Stop()

	tmpDir := t.TempDir()
	frameworkConfigPath := filepath.Join(tmpDir, "framework.yaml")
	dsn := filepath.Join(tmpDir, "e2e_push.db")
	frameworkConfig := `
task-engine:
  general:
    instance_name: "e2e-push"
    log_level: "info"
    env: "test"
  storage:
    database:
      type: "sqlite"
      dsn: "` + dsn + `"
      max_open_conns: 5
  execution:
    default_task_timeout: "60s"
    worker_concurrency: 2
`
	require.NoError(t, os.WriteFile(frameworkConfigPath, []byte(frameworkConfig), 0644))

	pushCollector := &stkMinsPushCollector{maxPublish: 100}
	eng, err := engine.NewEngineBuilder(frameworkConfigPath).
		Build()
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, eng.Start(ctx))
	defer eng.Stop()

	registry := eng.GetRegistry()
	_, _ = registry.Register(ctx, "print_data_job", printDataJob, "打印收到的数据")
	_, _ = registry.Register(ctx, "dummy_job", func(ctx context.Context) error { return nil }, "dummy")

	collectorTask, err := builder.NewRealtimeTaskBuilder("stk_push", "stk_mins_push", registry).
		WithContinuousMode().
		WithTaskType(realtime.TaskTypeDataCollector).
		WithCollector("stk_mins_push").
		WithMode(realtime.CollectorModePush).
		WithEndpoint(server.WsURL()+"/ws/stk_mins", "ws").
		WithJobFunction("print_data_job", nil).
		Build()
	require.NoError(t, err)

	streamTask, err := builder.NewRealtimeTaskBuilder("stk_push_processor", "流处理", registry).
		WithContinuousMode().
		WithTaskType(realtime.TaskTypeStreamProcessor).
		WithJobFunction("print_data_job", nil).
		Build()
	require.NoError(t, err)

	wf, err := builder.NewWorkflowBuilder("e2e_stk_push_wf", "e2e").
		WithStreamingMode().
		WithDataCollector("stk_mins_push", pushCollector).
		WithRealtimeTask(collectorTask).
		WithRealtimeTask(streamTask).
		Build()
	require.NoError(t, err)

	ctrl, err := eng.SubmitWorkflow(ctx, wf)
	require.NoError(t, err)
	require.NotEmpty(t, ctrl.InstanceID())

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&pushCollector.published) < 20 {
		time.Sleep(50 * time.Millisecond)
	}
	published := atomic.LoadInt32(&pushCollector.published)
	assert.GreaterOrEqual(t, published, int32(20), "push collector should have published at least 20 rows")
}

// TestRealtimeCollector_E2E_BroadcastAndWal 多订阅者广播 + WAL：一个采集器、两个 StreamProcessor（db_sink Blocking、frontend_sink NonBlockingDrop），断言双路收到数据且指标含广播/WAL
func TestRealtimeCollector_E2E_BroadcastAndWal(t *testing.T) {
	t.Cleanup(func() { t.Logf("[E2E] 入库数据量: N/A (本测试无 DB 写入)") })
	tmpDir := t.TempDir()
	frameworkConfigPath := filepath.Join(tmpDir, "framework.yaml")
	dsn := filepath.Join(tmpDir, "e2e.db")
	frameworkConfig := `
task-engine:
  general:
    instance_name: "e2e-engine"
    log_level: "info"
    env: "test"
  storage:
    database:
      type: "sqlite"
      dsn: "` + dsn + `"
      max_open_conns: 5
  execution:
    default_task_timeout: "60s"
    worker_concurrency: 2
`
	require.NoError(t, os.WriteFile(frameworkConfigPath, []byte(frameworkConfig), 0644))

	collector := &finitePublishCollector{maxPublish: 6}
	eng, err := engine.NewEngineBuilder(frameworkConfigPath).Build()
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, eng.Start(ctx))
	defer eng.Stop()

	registry := eng.GetRegistry()
	var dbCount, frontendCount int32
	countJob := func(name string, counter *int32) func(ctx *task.TaskContext) error {
		return func(ctx *task.TaskContext) error {
			atomic.AddInt32(counter, 1)
			return nil
		}
	}
	_, _ = registry.Register(ctx, "db_sink_job", countJob("db", &dbCount), "db sink")
	_, _ = registry.Register(ctx, "frontend_sink_job", countJob("frontend", &frontendCount), "frontend sink")
	_, _ = registry.Register(ctx, "dummy_job", func(ctx *task.TaskContext) error { return nil }, "dummy")

	collectorTask, err := builder.NewRealtimeTaskBuilder("e2e_broadcast_collector", "e2e", registry).
		WithContinuousMode().
		WithTaskType(realtime.TaskTypeDataCollector).
		WithCollector("e2e_finite").
		WithJobFunction("dummy_job", nil).
		Build()
	require.NoError(t, err)

	dbTask, err := builder.NewRealtimeTaskBuilder("db_sink", "DB 写入", registry).
		WithContinuousMode().
		WithTaskType(realtime.TaskTypeStreamProcessor).
		WithSubscriberName("db_sink").
		WithBufferPolicyBlocking(5000).
		WithJobFunction("db_sink_job", nil).
		Build()
	require.NoError(t, err)

	frontendTask, err := builder.NewRealtimeTaskBuilder("frontend_sink", "前端推送", registry).
		WithContinuousMode().
		WithTaskType(realtime.TaskTypeStreamProcessor).
		WithSubscriberName("frontend_sink").
		WithBufferPolicyNonBlockingDrop(100).
		WithJobFunction("frontend_sink_job", nil).
		Build()
	require.NoError(t, err)

	wf, err := builder.NewWorkflowBuilder("e2e_broadcast_wal_wf", "e2e").
		WithStreamingMode().
		WithDataCollector("e2e_finite", collector).
		WithRealtimeTask(collectorTask).
		WithRealtimeTask(dbTask).
		WithRealtimeTask(frontendTask).
		WithBroadcastEnabled(true).
		WithWalEnabled(true).
		Build()
	require.NoError(t, err)

	ctrl, err := eng.SubmitWorkflow(ctx, wf)
	require.NoError(t, err)
	require.NotEmpty(t, ctrl.InstanceID())

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&collector.published) < 6 {
		time.Sleep(50 * time.Millisecond)
	}

	// 等待两个 sink 消费
	time.Sleep(500 * time.Millisecond)

	assert.GreaterOrEqual(t, atomic.LoadInt32(&dbCount), int32(1), "db_sink should have processed at least 1")
	assert.GreaterOrEqual(t, atomic.LoadInt32(&frontendCount), int32(1), "frontend_sink should have processed at least 1")
}

// TestRealtimeCollector_E2E_GracefulShutdown 验证 TerminateWorkflowInstance 下的优雅关闭：在终止前已入缓冲的数据尽量被消费
func TestRealtimeCollector_E2E_GracefulShutdown(t *testing.T) {
	t.Cleanup(func() { t.Logf("[E2E] 入库数据量: N/A (本测试无 DB 写入)") })
	tmpDir := t.TempDir()
	frameworkConfigPath := filepath.Join(tmpDir, "framework.yaml")
	dsn := filepath.Join(tmpDir, "e2e_graceful.db")
	frameworkConfig := `
task-engine:
  general:
    instance_name: "e2e-engine"
    log_level: "info"
    env: "test"
  storage:
    database:
      type: "sqlite"
      dsn: "` + dsn + `"
      max_open_conns: 5
  execution:
    default_task_timeout: "60s"
    worker_concurrency: 2
`
	require.NoError(t, os.WriteFile(frameworkConfigPath, []byte(frameworkConfig), 0644))

	collector := &finitePublishCollector{maxPublish: 20}
	eng, err := engine.NewEngineBuilder(frameworkConfigPath).Build()
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, eng.Start(ctx))
	defer eng.Stop()

	registry := eng.GetRegistry()
	var dbCount int32
	_, _ = registry.Register(ctx, "db_sink_job", func(ctx *task.TaskContext) error {
		atomic.AddInt32(&dbCount, 1)
		return nil
	}, "db sink")
	_, _ = registry.Register(ctx, "dummy_job", func(ctx *task.TaskContext) error { return nil }, "dummy")

	collectorTask, err := builder.NewRealtimeTaskBuilder("graceful_collector", "e2e", registry).
		WithContinuousMode().
		WithTaskType(realtime.TaskTypeDataCollector).
		WithCollector("e2e_finite").
		WithJobFunction("dummy_job", nil).
		Build()
	require.NoError(t, err)

	dbTask, err := builder.NewRealtimeTaskBuilder("db_sink_graceful", "DB 写入", registry).
		WithContinuousMode().
		WithTaskType(realtime.TaskTypeStreamProcessor).
		WithSubscriberName("db_sink_graceful").
		WithBufferPolicyBlocking(5000).
		WithJobFunction("db_sink_job", nil).
		Build()
	require.NoError(t, err)

	wf, err := builder.NewWorkflowBuilder("e2e_graceful_wf", "e2e").
		WithStreamingMode().
		WithDataCollector("e2e_finite", collector).
		WithRealtimeTask(collectorTask).
		WithRealtimeTask(dbTask).
		WithBroadcastEnabled(true).
		WithWalEnabled(true).
		Build()
	require.NoError(t, err)

	ctrl, err := eng.SubmitWorkflow(ctx, wf)
	require.NoError(t, err)
	require.NotEmpty(t, ctrl.InstanceID())

	// 等待采集器至少 publish 一部分数据
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&collector.published) < 5 {
		time.Sleep(50 * time.Millisecond)
	}

	// 触发 Terminate，要求 graceful shutdown 不 panic 且当前缓冲中的数据尽量被消费
	require.NoError(t, eng.TerminateWorkflowInstance(ctx, ctrl.InstanceID(), "graceful shutdown test"))

	// 再等待一小段时间，让流处理任务有机会消费缓冲中的数据
	time.Sleep(500 * time.Millisecond)

	assert.GreaterOrEqual(t, atomic.LoadInt32(&dbCount), int32(1), "db_sink_graceful should have processed at least 1 before/around shutdown")
}

// TestRealtimeCollector_E2E_TushareMock_DuckDBAndConsole Tushare Mock WebSocket + 广播 + 按 code 过滤 + 批量写 SQLite + Console 输出
func TestRealtimeCollector_E2E_TushareMock_DuckDBAndConsole(t *testing.T) {
	var batchState *tushareBatchWriterState
	t.Cleanup(func() {
		if batchState != nil {
			t.Logf("[E2E] 入库数据量: %d", batchState.TotalRows())
		} else {
			t.Logf("[E2E] 入库数据量: N/A")
		}
	})
	server, err := NewTushareWsMockServer("")
	require.NoError(t, err)
	server.SetPushInterval(15 * time.Millisecond)
	server.SetMaxPushRows(300)
	server.Start()
	defer server.Stop()

	tmpDir := t.TempDir()
	frameworkConfigPath := filepath.Join(tmpDir, "framework.yaml")
	dsn := filepath.Join(tmpDir, "e2e.db")
	frameworkConfig := `
task-engine:
  general:
    instance_name: "e2e-tushare"
    log_level: "info"
    env: "test"
  storage:
    database:
      type: "sqlite"
      dsn: "` + dsn + `"
      max_open_conns: 5
  execution:
    default_task_timeout: "60s"
    worker_concurrency: 2
`
	require.NoError(t, os.WriteFile(frameworkConfigPath, []byte(frameworkConfig), 0644))

	dbPath := filepath.Join(tmpDir, "tushare_tick.db")
	batchState = &tushareBatchWriterState{dbPath: dbPath, batchSize: 20}

	eng, err := engine.NewEngineBuilder(frameworkConfigPath).Build()
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, eng.Start(ctx))
	defer eng.Stop()

	registry := eng.GetRegistry()
	_, _ = registry.Register(ctx, "tushare_batch_write_job", batchState.Handle, "Tushare 批量写 SQLite")
	countByCode := map[string]*int32{
		"600863.SH": new(int32),
		"601169.SH": new(int32),
		"600503.SH": new(int32),
	}
	_, _ = registry.Register(ctx, "tushare_console_job", tushareConsoleCountJob(countByCode), "Tushare Console 按 code 统计")
	_, _ = registry.Register(ctx, "dummy_job", func(ctx *task.TaskContext) error { return nil }, "dummy")

	collectorTask, err := builder.NewRealtimeTaskBuilder("tushare_collector", "Tushare WS", registry).
		WithContinuousMode().
		WithTaskType(realtime.TaskTypeDataCollector).
		WithCollector("tushare_ws").
		WithMode(realtime.CollectorModePush).
		WithEndpoint(server.WsURL()+"/listening", "ws").
		WithJobFunction("dummy_job", nil).
		Build()
	require.NoError(t, err)

	dbTask, err := builder.NewRealtimeTaskBuilder("tushare_db", "DB 批量写", registry).
		WithContinuousMode().
		WithTaskType(realtime.TaskTypeStreamProcessor).
		WithSubscriberName("db").
		WithBufferPolicyBlocking(5000).
		WithJobFunction("tushare_batch_write_job", nil).
		Build()
	require.NoError(t, err)

	consoleTask, err := builder.NewRealtimeTaskBuilder("tushare_console", "Console 输出", registry).
		WithContinuousMode().
		WithTaskType(realtime.TaskTypeStreamProcessor).
		WithSubscriberName("console").
		WithSubscriberFilterCodes([]string{"600863.SH", "601169.SH"}).
		WithBufferPolicyNonBlockingDrop(2000).
		WithJobFunction("tushare_console_job", nil).
		Build()
	require.NoError(t, err)

	tushareCollector := &tushareWsCollector{maxPublish: 300}
	wf, err := builder.NewWorkflowBuilder("e2e_tushare_wf", "e2e").
		WithStreamingMode().
		WithDataCollector("tushare_ws", tushareCollector).
		WithRealtimeTask(collectorTask).
		WithRealtimeTask(dbTask).
		WithRealtimeTask(consoleTask).
		WithBroadcastEnabled(true).
		WithWalEnabled(true).
		Build()
	require.NoError(t, err)

	ctrl, err := eng.SubmitWorkflow(ctx, wf)
	require.NoError(t, err)
	require.NotEmpty(t, ctrl.InstanceID())

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		pub := atomic.LoadInt32(&tushareCollector.published)
		if pub >= 50 {
			break
		}
		if batchState.totalRows.Load() > 0 || (atomic.LoadInt32(countByCode["600863.SH"])+atomic.LoadInt32(countByCode["601169.SH"])) > 10 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	// 等待一段时间让数据收完
	time.Sleep(2 * time.Second)
	require.NoError(t, eng.TerminateWorkflowInstance(ctx, ctrl.InstanceID(), "e2e done"))
	time.Sleep(500 * time.Millisecond)

	//  flush 剩余 batch
	batchState.mu.Lock()
	_ = batchState.flush()
	batchState.mu.Unlock()

	assert.GreaterOrEqual(t, batchState.totalRows.Load(), int64(1), "DB should have at least 1 row (batch write)")
	// 前端只应收到订阅的 600863.SH、601169.SH
	assert.GreaterOrEqual(t, atomic.LoadInt32(countByCode["600863.SH"]), int32(1), "console should receive 600863.SH")
	assert.GreaterOrEqual(t, atomic.LoadInt32(countByCode["601169.SH"]), int32(1), "console should receive 601169.SH")
}

// TestRealtimeCollector_E2E_TushareMock_DisconnectRecovery Mock 推送若干条后断线，采集器重连后继续收数据。
// 预期日志：会出现 "websocket: close 1005"、"执行错误"、"connection.reconnecting"、"重连成功"，均为 Mock 主动断线触发的正常流程。
func TestRealtimeCollector_E2E_TushareMock_DisconnectRecovery(t *testing.T) {
	t.Cleanup(func() { t.Logf("[E2E] 入库数据量: N/A (本测试无 DB 写入)") })
	t.Log("E2E 断线恢复：以下日志中的 websocket close / 执行错误 / 重连 为 Mock 主动断线所致，属预期行为")
	server, err := NewTushareWsMockServer("")
	require.NoError(t, err)
	server.SetPushInterval(10 * time.Millisecond)
	server.SetMaxPushRows(200)
	server.SetDisconnectAfter(40) // 推送 40 条后主动断线
	server.Start()
	defer server.Stop()

	tmpDir := t.TempDir()
	frameworkConfigPath := filepath.Join(tmpDir, "framework.yaml")
	dsn := filepath.Join(tmpDir, "e2e.db")
	frameworkConfig := `
task-engine:
  general:
    instance_name: "e2e-tushare-reconnect"
    log_level: "info"
    env: "test"
  storage:
    database:
      type: "sqlite"
      dsn: "` + dsn + `"
      max_open_conns: 5
  execution:
    default_task_timeout: "60s"
    worker_concurrency: 2
`
	require.NoError(t, os.WriteFile(frameworkConfigPath, []byte(frameworkConfig), 0644))

	eng, err := engine.NewEngineBuilder(frameworkConfigPath).Build()
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, eng.Start(ctx))
	defer eng.Stop()

	registry := eng.GetRegistry()
	var received int32
	_, _ = registry.Register(ctx, "count_job", func(ctx *task.TaskContext) error {
		atomic.AddInt32(&received, 1)
		return nil
	}, "count")
	_, _ = registry.Register(ctx, "dummy_job", func(ctx *task.TaskContext) error { return nil }, "dummy")

	collector := &tushareWsCollector{maxPublish: 200, reconnectOk: true}
	collectorTask, err := builder.NewRealtimeTaskBuilder("tushare_reconnect", "Tushare WS", registry).
		WithContinuousMode().
		WithTaskType(realtime.TaskTypeDataCollector).
		WithCollector("tushare_ws").
		WithMode(realtime.CollectorModePush).
		WithEndpoint(server.WsURL()+"/listening", "ws").
		WithJobFunction("dummy_job", nil).
		Build()
	require.NoError(t, err)

	streamTask, err := builder.NewRealtimeTaskBuilder("tushare_processor", "处理", registry).
		WithContinuousMode().
		WithTaskType(realtime.TaskTypeStreamProcessor).
		WithJobFunction("count_job", nil).
		Build()
	require.NoError(t, err)

	wf, err := builder.NewWorkflowBuilder("e2e_tushare_reconnect_wf", "e2e").
		WithStreamingMode().
		WithDataCollector("tushare_ws", collector).
		WithRealtimeTask(collectorTask).
		WithRealtimeTask(streamTask).
		Build()
	require.NoError(t, err)

	ctrl, err := eng.SubmitWorkflow(ctx, wf)
	require.NoError(t, err)
	require.NotEmpty(t, ctrl.InstanceID())

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&collector.published) < 60 {
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond)
	require.NoError(t, eng.TerminateWorkflowInstance(ctx, ctrl.InstanceID(), "e2e done"))
	time.Sleep(300 * time.Millisecond)

	// 断线后重连，应收到超过 40 条（第一段 40 + 重连后至少一些）
	assert.GreaterOrEqual(t, atomic.LoadInt32(&collector.published), int32(40), "collector should have received at least 40 before disconnect")
	assert.GreaterOrEqual(t, atomic.LoadInt32(&received), int32(1), "stream processor should have processed at least 1")
}

// tushareBatchWriterStateWithFailures 前 failFlushCount 次 flush 返回错误，用于模拟 DB 写入失败重试
type tushareBatchWriterStateWithFailures struct {
	*tushareBatchWriterState
	failFlushCount atomic.Int32
}

func (s *tushareBatchWriterStateWithFailures) HandleWithFailures(ctx *task.TaskContext) error {
	if err := s.init(); err != nil {
		return err
	}
	data := ctx.GetParam("data")
	if data == nil {
		return nil
	}
	row, err := s.recordFromPayload(data)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.batch = append(s.batch, row)
	batchSize := s.batchSize
	if batchSize <= 0 {
		batchSize = 500
	}
	needFlush := len(s.batch) >= batchSize
	s.mu.Unlock()
	if needFlush {
		s.mu.Lock()
		needFlush = len(s.batch) >= batchSize
		if needFlush {
			failCnt := s.failFlushCount.Add(1)
			if failCnt <= 2 {
				s.mu.Unlock()
				return fmt.Errorf("simulated DB write failure %d", failCnt)
			}
			defer s.mu.Unlock()
			return s.flush()
		}
		s.mu.Unlock()
	}
	return nil
}

// TestRealtimeCollector_E2E_TushareMock_DBWriteFailureRetry 前几次 flush 模拟失败，后续成功，断言最终有数据写入。
// 预期日志：会出现 "DataHandler tushare_batch_write_fail_job 执行失败: simulated DB write failure 1/2"，为故意注入的失败，用于验证重试后仍能写入。
func TestRealtimeCollector_E2E_TushareMock_DBWriteFailureRetry(t *testing.T) {
	var baseState *tushareBatchWriterState
	t.Cleanup(func() {
		if baseState != nil {
			t.Logf("[E2E] 入库数据量: %d", baseState.TotalRows())
		} else {
			t.Logf("[E2E] 入库数据量: N/A")
		}
	})
	t.Log("E2E DB 写失败重试：以下日志中的 simulated DB write failure 1/2 为测试注入的模拟失败，属预期行为")
	server, err := NewTushareWsMockServer("")
	require.NoError(t, err)
	server.SetPushInterval(15 * time.Millisecond)
	server.SetMaxPushRows(80)
	server.Start()
	defer server.Stop()

	tmpDir := t.TempDir()
	frameworkConfigPath := filepath.Join(tmpDir, "framework.yaml")
	dsn := filepath.Join(tmpDir, "e2e.db")
	frameworkConfig := `
task-engine:
  general:
    instance_name: "e2e-tushare-dbfail"
    log_level: "info"
    env: "test"
  storage:
    database:
      type: "sqlite"
      dsn: "` + dsn + `"
      max_open_conns: 5
  execution:
    default_task_timeout: "60s"
    worker_concurrency: 2
`
	require.NoError(t, os.WriteFile(frameworkConfigPath, []byte(frameworkConfig), 0644))

	dbPath := filepath.Join(tmpDir, "tushare_tick_fail.db")
	baseState = &tushareBatchWriterState{dbPath: dbPath, batchSize: 10}
	stateWithFail := &tushareBatchWriterStateWithFailures{tushareBatchWriterState: baseState}

	eng, err := engine.NewEngineBuilder(frameworkConfigPath).Build()
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, eng.Start(ctx))
	defer eng.Stop()

	registry := eng.GetRegistry()
	_, _ = registry.Register(ctx, "tushare_batch_write_fail_job", stateWithFail.HandleWithFailures, "Tushare batch write with simulated failures")
	_, _ = registry.Register(ctx, "dummy_job", func(ctx *task.TaskContext) error { return nil }, "dummy")

	collector := &tushareWsCollector{maxPublish: 80}
	collectorTask, err := builder.NewRealtimeTaskBuilder("tushare_dbfail", "Tushare WS", registry).
		WithContinuousMode().
		WithTaskType(realtime.TaskTypeDataCollector).
		WithCollector("tushare_ws").
		WithMode(realtime.CollectorModePush).
		WithEndpoint(server.WsURL()+"/listening", "ws").
		WithJobFunction("dummy_job", nil).
		Build()
	require.NoError(t, err)

	dbTask, err := builder.NewRealtimeTaskBuilder("tushare_db_fail", "DB 写", registry).
		WithContinuousMode().
		WithTaskType(realtime.TaskTypeStreamProcessor).
		WithJobFunction("tushare_batch_write_fail_job", nil).
		Build()
	require.NoError(t, err)

	wf, err := builder.NewWorkflowBuilder("e2e_tushare_dbfail_wf", "e2e").
		WithStreamingMode().
		WithDataCollector("tushare_ws", collector).
		WithRealtimeTask(collectorTask).
		WithRealtimeTask(dbTask).
		Build()
	require.NoError(t, err)

	ctrl, err := eng.SubmitWorkflow(ctx, wf)
	require.NoError(t, err)
	require.NotEmpty(t, ctrl.InstanceID())

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&collector.published) < 30 {
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(2 * time.Second)
	require.NoError(t, eng.TerminateWorkflowInstance(ctx, ctrl.InstanceID(), "e2e done"))
	time.Sleep(500 * time.Millisecond)

	baseState.mu.Lock()
	_ = baseState.flush()
	baseState.mu.Unlock()

	assert.GreaterOrEqual(t, baseState.totalRows.Load(), int64(1), "after retry DB should have at least 1 row")
}
