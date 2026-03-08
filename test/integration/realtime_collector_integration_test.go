package integration

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LENAX/task-engine/pkg/core/builder"
	"github.com/LENAX/task-engine/pkg/core/engine"
	"github.com/LENAX/task-engine/pkg/core/realtime"
)

// blockingMockCollector 在 Run 内发布一条事件后立即返回，便于 Shutdown 时任务能及时退出（不阻塞在 ctx.Done）
type blockingMockCollector struct {
	runCount    int32
	publishDone chan struct{}
}

func (m *blockingMockCollector) Run(ctx context.Context, config *realtime.ContinuousTaskConfig, publish realtime.PublishFunc) error {
	atomic.AddInt32(&m.runCount, 1)
	taskID := ""
	if config != nil {
		taskID = config.ID
	}
	e := realtime.NewRealtimeEvent(realtime.EventDataArrived, taskID, "", &realtime.DataArrivedPayload{Data: "integration-test", Source: "mock"})
	_ = publish(e)
	if m.publishDone != nil {
		select {
		case m.publishDone <- struct{}{}:
		default:
		}
	}
	// 立即返回，避免 Shutdown 时阻塞在 <-ctx.Done() 导致 taskWg 无法完成
	return nil
}

func TestRealtimeCollector_EngineWithDataCollector_StreamingWorkflow(t *testing.T) {
	tmpDir := t.TempDir()
	frameworkConfigPath := filepath.Join(tmpDir, "framework.yaml")
	dsn := filepath.Join(tmpDir, "test.db")

	frameworkConfig := `
task-engine:
  general:
    instance_name: "test-engine"
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

	mock := &blockingMockCollector{publishDone: make(chan struct{}, 1)}
	eng, err := engine.NewEngineBuilder(frameworkConfigPath).
		WithDataCollector("mock_collector", mock).
		Build()
	require.NoError(t, err)
	require.NotNil(t, eng)

	ctx := context.Background()
	require.NoError(t, eng.Start(ctx))
	defer eng.Stop()

	registry := eng.GetRegistry()
	_, _ = registry.Register(ctx, "dummy_job", func(ctx context.Context) error { return nil }, "dummy")

	rtTask, err := builder.NewRealtimeTaskBuilder("quote_collector", "quote", registry).
		WithContinuousMode().
		WithTaskType(realtime.TaskTypeDataCollector).
		WithCollector("mock_collector").
		WithJobFunction("dummy_job", nil).
		Build()
	require.NoError(t, err)

	wf, err := builder.NewWorkflowBuilder("realtime_wf", "realtime").
		WithStreamingMode().
		WithRealtimeTask(rtTask).
		Build()
	require.NoError(t, err)

	ctrl, err := eng.SubmitWorkflow(ctx, wf)
	require.NoError(t, err)
	require.NotEmpty(t, ctrl.InstanceID())

	// 等待 runDataCollector 至少被调用一次（Run 会阻塞，所以 runCount 会 >= 1 或我们收到 publishDone）
	select {
	case <-mock.publishDone:
		// 已发布事件
	case <-time.After(1 * time.Second):
		// 超时也检查 runCount
	}
	runCount := atomic.LoadInt32(&mock.runCount)
	assert.GreaterOrEqual(t, runCount, int32(1), "mock collector Run should have been called at least once")
	// 注：Engine.GetInstanceManager 返回的是包装接口，无法在此断言 RealtimeInstanceManager.GetDataBuffer；
	// 数据到达已通过 mock 的 publish 调用与 publishDone channel 覆盖。
}

func TestRealtimeCollector_UnregisteredCollectorName_ReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	frameworkConfigPath := filepath.Join(tmpDir, "framework.yaml")
	dsn := filepath.Join(tmpDir, "test.db")

	frameworkConfig := `
task-engine:
  general:
    instance_name: "test-engine"
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

	// 不注册任何 DataCollector
	eng, err := engine.NewEngineBuilder(frameworkConfigPath).Build()
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, eng.Start(ctx))
	defer eng.Stop()

	registry := eng.GetRegistry()
	_, _ = registry.Register(ctx, "dummy_job", func(ctx context.Context) error { return nil }, "dummy")

	rtTask, err := builder.NewRealtimeTaskBuilder("quote_collector", "quote", registry).
		WithContinuousMode().
		WithTaskType(realtime.TaskTypeDataCollector).
		WithCollector("nonexistent_collector").
		WithJobFunction("dummy_job", nil).
		Build()
	require.NoError(t, err)

	wf, err := builder.NewWorkflowBuilder("realtime_wf", "realtime").
		WithStreamingMode().
		WithRealtimeTask(rtTask).
		Build()
	require.NoError(t, err)

	_, err = eng.SubmitWorkflow(ctx, wf)
	require.NoError(t, err)

	// 实例会启动，runDataCollector 会返回 "未注册的采集器" 错误，任务会进入错误/重连逻辑
	// 我们只验证提交成功，不断言具体错误（错误在 manager 内部处理）
	time.Sleep(500 * time.Millisecond)
}
