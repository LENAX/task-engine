// Package e2e 端到端测试：Realtime DataCollector 注册与 Streaming Workflow 完整链路
package e2e

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
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

// TestRealtimeCollector_E2E_StreamingWorkflow 完整链路：Engine 注册有限次 publish 采集器 → 提交 Streaming Workflow → 断言 Run 被调用与数据/指标
func TestRealtimeCollector_E2E_StreamingWorkflow(t *testing.T) {
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

// TestRealtimeCollector_E2E_PullBasedWorkflow 使用 StkMinsMockServer 的 pull 接口 + pull DataCollector 跑通流程
func TestRealtimeCollector_E2E_PullBasedWorkflow(t *testing.T) {
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
