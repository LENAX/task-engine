package unit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/LENAX/task-engine/pkg/core/realtime"
)

func TestDefaultReconnectBackoffConfig(t *testing.T) {
	config := realtime.DefaultReconnectBackoffConfig()

	assert.Equal(t, time.Second, config.InitialInterval)
	assert.Equal(t, 30*time.Second, config.MaxInterval)
	assert.Equal(t, 2.0, config.Multiplier)
	assert.Equal(t, 0.1, config.Jitter)
}

func TestNewContinuousTaskConfig(t *testing.T) {
	config := realtime.NewContinuousTaskConfig("task-1", "Quote Collector", realtime.TaskTypeDataCollector)

	assert.Equal(t, "task-1", config.ID)
	assert.Equal(t, "Quote Collector", config.Name)
	assert.Equal(t, realtime.TaskTypeDataCollector, config.Type)
	assert.True(t, config.ReconnectEnabled)
	assert.Equal(t, 0, config.MaxReconnectAttempts) // 无限重连
	assert.Equal(t, 10000, config.BufferSize)
	assert.Equal(t, 100, config.BatchSize)
	assert.Equal(t, time.Second, config.FlushInterval)
	assert.Equal(t, 0.8, config.BackpressureThreshold)
	assert.Equal(t, "throttle", config.BackpressureAction)
	assert.NotNil(t, config.SubscribedEvents)
	assert.NotNil(t, config.Params)
}

func TestContinuousTaskType_Constants(t *testing.T) {
	assert.Equal(t, realtime.ContinuousTaskType("data_collector"), realtime.TaskTypeDataCollector)
	assert.Equal(t, realtime.ContinuousTaskType("stream_processor"), realtime.TaskTypeStreamProcessor)
	assert.Equal(t, realtime.ContinuousTaskType("event_listener"), realtime.TaskTypeEventListener)
	assert.Equal(t, realtime.ContinuousTaskType("scheduled_poller"), realtime.TaskTypeScheduledPoller)
}

func TestContinuousTaskState_Constants(t *testing.T) {
	assert.Equal(t, realtime.ContinuousTaskState("initializing"), realtime.StateInitializing)
	assert.Equal(t, realtime.ContinuousTaskState("running"), realtime.StateRunning)
	assert.Equal(t, realtime.ContinuousTaskState("paused"), realtime.StatePaused)
	assert.Equal(t, realtime.ContinuousTaskState("reconnecting"), realtime.StateReconnecting)
	assert.Equal(t, realtime.ContinuousTaskState("stopping"), realtime.StateStopping)
	assert.Equal(t, realtime.ContinuousTaskState("stopped"), realtime.StateStopped)
	assert.Equal(t, realtime.ContinuousTaskState("error"), realtime.StateError)
}

func TestNewContinuousTask(t *testing.T) {
	config := *realtime.NewContinuousTaskConfig("task-1", "Test Task", realtime.TaskTypeDataCollector)
	ct := realtime.NewContinuousTask(config)

	assert.Equal(t, config, ct.Config)
	assert.Equal(t, realtime.StateInitializing, ct.GetState())
	assert.NotZero(t, ct.StartTime)
	assert.Equal(t, int64(0), ct.GetDataCount())
	assert.Equal(t, int64(0), ct.GetErrorCount())
	assert.False(t, ct.IsConnected())
	assert.Equal(t, 0, ct.GetReconnectCount())
}

func TestContinuousTask_Start(t *testing.T) {
	config := *realtime.NewContinuousTaskConfig("task-1", "Test Task", realtime.TaskTypeDataCollector)
	ct := realtime.NewContinuousTask(config)

	ctx := context.Background()
	ct.Start(ctx)

	assert.Equal(t, realtime.StateRunning, ct.GetState())
	assert.NotNil(t, ct.Context())
}

func TestContinuousTask_Stop(t *testing.T) {
	config := *realtime.NewContinuousTaskConfig("task-1", "Test Task", realtime.TaskTypeDataCollector)
	ct := realtime.NewContinuousTask(config)

	ctx := context.Background()
	ct.Start(ctx)
	ct.Stop()

	assert.Equal(t, realtime.StateStopped, ct.GetState())
}

func TestContinuousTask_Pause_Resume(t *testing.T) {
	config := *realtime.NewContinuousTaskConfig("task-1", "Test Task", realtime.TaskTypeDataCollector)
	ct := realtime.NewContinuousTask(config)

	ctx := context.Background()
	ct.Start(ctx)

	ct.Pause()
	assert.Equal(t, realtime.StatePaused, ct.GetState())

	ct.Resume()
	assert.Equal(t, realtime.StateRunning, ct.GetState())
}

func TestContinuousTask_Pause_OnlyRunning(t *testing.T) {
	config := *realtime.NewContinuousTaskConfig("task-1", "Test Task", realtime.TaskTypeDataCollector)
	ct := realtime.NewContinuousTask(config)

	ct.Pause()
	assert.Equal(t, realtime.StateInitializing, ct.GetState())
}

func TestContinuousTask_Resume_OnlyPaused(t *testing.T) {
	config := *realtime.NewContinuousTaskConfig("task-1", "Test Task", realtime.TaskTypeDataCollector)
	ct := realtime.NewContinuousTask(config)

	ctx := context.Background()
	ct.Start(ctx)

	ct.Resume()
	assert.Equal(t, realtime.StateRunning, ct.GetState())
}

func TestContinuousTask_SetState(t *testing.T) {
	config := *realtime.NewContinuousTaskConfig("task-1", "Test Task", realtime.TaskTypeDataCollector)
	ct := realtime.NewContinuousTask(config)

	ct.SetState(realtime.StateReconnecting)
	assert.Equal(t, realtime.StateReconnecting, ct.GetState())

	ct.SetState(realtime.StateError)
	assert.Equal(t, realtime.StateError, ct.GetState())
}

func TestContinuousTask_Connected(t *testing.T) {
	config := *realtime.NewContinuousTaskConfig("task-1", "Test Task", realtime.TaskTypeDataCollector)
	ct := realtime.NewContinuousTask(config)

	assert.False(t, ct.IsConnected())

	ct.SetConnected(true)
	assert.True(t, ct.IsConnected())

	ct.SetConnected(false)
	assert.False(t, ct.IsConnected())
}

func TestContinuousTask_IncrementDataCount(t *testing.T) {
	config := *realtime.NewContinuousTaskConfig("task-1", "Test Task", realtime.TaskTypeDataCollector)
	ct := realtime.NewContinuousTask(config)

	count := ct.IncrementDataCount()
	assert.Equal(t, int64(1), count)
	assert.Equal(t, int64(1), ct.GetDataCount())

	count = ct.IncrementDataCount()
	assert.Equal(t, int64(2), count)
	assert.Equal(t, int64(2), ct.GetDataCount())
}

func TestContinuousTask_IncrementErrorCount(t *testing.T) {
	config := *realtime.NewContinuousTaskConfig("task-1", "Test Task", realtime.TaskTypeDataCollector)
	ct := realtime.NewContinuousTask(config)

	count := ct.IncrementErrorCount()
	assert.Equal(t, int64(1), count)
	assert.Equal(t, int64(1), ct.GetErrorCount())
}

func TestContinuousTask_UpdateLastDataTime(t *testing.T) {
	config := *realtime.NewContinuousTaskConfig("task-1", "Test Task", realtime.TaskTypeDataCollector)
	ct := realtime.NewContinuousTask(config)

	before := time.Now()
	ct.UpdateLastDataTime()
	after := time.Now()

	lastDataTime := ct.GetLastDataTime()
	assert.True(t, lastDataTime.After(before) || lastDataTime.Equal(before))
	assert.True(t, lastDataTime.Before(after) || lastDataTime.Equal(after))
}

func TestContinuousTask_IncrementReconnectCount(t *testing.T) {
	config := *realtime.NewContinuousTaskConfig("task-1", "Test Task", realtime.TaskTypeDataCollector)
	ct := realtime.NewContinuousTask(config)

	count := ct.IncrementReconnectCount()
	assert.Equal(t, 1, count)
	assert.Equal(t, 1, ct.GetReconnectCount())

	count = ct.IncrementReconnectCount()
	assert.Equal(t, 2, count)
}

func TestContinuousTask_Context(t *testing.T) {
	config := *realtime.NewContinuousTaskConfig("task-1", "Test Task", realtime.TaskTypeDataCollector)
	ct := realtime.NewContinuousTask(config)

	assert.Nil(t, ct.Context())

	ctx := context.Background()
	ct.Start(ctx)
	assert.NotNil(t, ct.Context())
}

func TestContinuousTask_GetSnapshot(t *testing.T) {
	config := *realtime.NewContinuousTaskConfig("task-1", "Test Task", realtime.TaskTypeDataCollector)
	ct := realtime.NewContinuousTask(config)

	ctx := context.Background()
	ct.Start(ctx)
	ct.SetConnected(true)
	ct.IncrementDataCount()
	ct.IncrementDataCount()
	ct.IncrementErrorCount()
	ct.IncrementReconnectCount()
	ct.UpdateLastDataTime()

	snapshot := ct.GetSnapshot()

	assert.Equal(t, string(realtime.StateRunning), snapshot["state"])
	assert.Equal(t, true, snapshot["connected"])
	assert.Equal(t, int64(2), snapshot["data_count"])
	assert.Equal(t, int64(1), snapshot["error_count"])
	assert.Equal(t, 1, snapshot["reconnect_count"])
	assert.NotZero(t, snapshot["last_data_time"])
	assert.NotZero(t, snapshot["start_time"])
}

func TestContinuousTaskConfig_WithParams(t *testing.T) {
	config := realtime.NewContinuousTaskConfig("task-1", "Test Task", realtime.TaskTypeDataCollector)
	config.Endpoint = "wss://api.example.com/ws"
	config.Protocol = "websocket"
	config.ReconnectEnabled = true
	config.MaxReconnectAttempts = 10
	config.BufferSize = 5000
	config.DataHandler = "process_data"
	config.ErrorHandler = "handle_error"
	config.Params["api_key"] = "test-key"

	assert.Equal(t, "wss://api.example.com/ws", config.Endpoint)
	assert.Equal(t, "websocket", config.Protocol)
	assert.True(t, config.ReconnectEnabled)
	assert.Equal(t, 10, config.MaxReconnectAttempts)
	assert.Equal(t, 5000, config.BufferSize)
	assert.Equal(t, "process_data", config.DataHandler)
	assert.Equal(t, "handle_error", config.ErrorHandler)
	assert.Equal(t, "test-key", config.Params["api_key"])
}
