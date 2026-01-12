package realtime

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultReconnectBackoffConfig(t *testing.T) {
	config := DefaultReconnectBackoffConfig()

	assert.Equal(t, time.Second, config.InitialInterval)
	assert.Equal(t, 30*time.Second, config.MaxInterval)
	assert.Equal(t, 2.0, config.Multiplier)
	assert.Equal(t, 0.1, config.Jitter)
}

func TestNewContinuousTaskConfig(t *testing.T) {
	config := NewContinuousTaskConfig("task-1", "Quote Collector", TaskTypeDataCollector)

	assert.Equal(t, "task-1", config.ID)
	assert.Equal(t, "Quote Collector", config.Name)
	assert.Equal(t, TaskTypeDataCollector, config.Type)
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
	assert.Equal(t, ContinuousTaskType("data_collector"), TaskTypeDataCollector)
	assert.Equal(t, ContinuousTaskType("stream_processor"), TaskTypeStreamProcessor)
	assert.Equal(t, ContinuousTaskType("event_listener"), TaskTypeEventListener)
	assert.Equal(t, ContinuousTaskType("scheduled_poller"), TaskTypeScheduledPoller)
}

func TestContinuousTaskState_Constants(t *testing.T) {
	assert.Equal(t, ContinuousTaskState("initializing"), StateInitializing)
	assert.Equal(t, ContinuousTaskState("running"), StateRunning)
	assert.Equal(t, ContinuousTaskState("paused"), StatePaused)
	assert.Equal(t, ContinuousTaskState("reconnecting"), StateReconnecting)
	assert.Equal(t, ContinuousTaskState("stopping"), StateStopping)
	assert.Equal(t, ContinuousTaskState("stopped"), StateStopped)
	assert.Equal(t, ContinuousTaskState("error"), StateError)
}

func TestNewContinuousTask(t *testing.T) {
	config := *NewContinuousTaskConfig("task-1", "Test Task", TaskTypeDataCollector)
	task := NewContinuousTask(config)

	assert.Equal(t, config, task.Config)
	assert.Equal(t, StateInitializing, task.State)
	assert.NotZero(t, task.StartTime)
	assert.Equal(t, int64(0), task.DataCount)
	assert.Equal(t, int64(0), task.ErrorCount)
	assert.False(t, task.Connected)
	assert.Equal(t, 0, task.ReconnectCount)
}

func TestContinuousTask_Start(t *testing.T) {
	config := *NewContinuousTaskConfig("task-1", "Test Task", TaskTypeDataCollector)
	task := NewContinuousTask(config)

	ctx := context.Background()
	task.Start(ctx)

	assert.Equal(t, StateRunning, task.GetState())
	assert.NotNil(t, task.Context())
}

func TestContinuousTask_Stop(t *testing.T) {
	config := *NewContinuousTaskConfig("task-1", "Test Task", TaskTypeDataCollector)
	task := NewContinuousTask(config)

	ctx := context.Background()
	task.Start(ctx)
	task.Stop()

	assert.Equal(t, StateStopped, task.GetState())
}

func TestContinuousTask_Pause_Resume(t *testing.T) {
	config := *NewContinuousTaskConfig("task-1", "Test Task", TaskTypeDataCollector)
	task := NewContinuousTask(config)

	ctx := context.Background()
	task.Start(ctx)

	// 暂停
	task.Pause()
	assert.Equal(t, StatePaused, task.GetState())

	// 恢复
	task.Resume()
	assert.Equal(t, StateRunning, task.GetState())
}

func TestContinuousTask_Pause_OnlyRunning(t *testing.T) {
	config := *NewContinuousTaskConfig("task-1", "Test Task", TaskTypeDataCollector)
	task := NewContinuousTask(config)

	// 未运行时暂停不应改变状态
	task.Pause()
	assert.Equal(t, StateInitializing, task.GetState())
}

func TestContinuousTask_Resume_OnlyPaused(t *testing.T) {
	config := *NewContinuousTaskConfig("task-1", "Test Task", TaskTypeDataCollector)
	task := NewContinuousTask(config)

	ctx := context.Background()
	task.Start(ctx)

	// 运行时恢复不应改变状态
	task.Resume()
	assert.Equal(t, StateRunning, task.GetState())
}

func TestContinuousTask_SetState(t *testing.T) {
	config := *NewContinuousTaskConfig("task-1", "Test Task", TaskTypeDataCollector)
	task := NewContinuousTask(config)

	task.SetState(StateReconnecting)
	assert.Equal(t, StateReconnecting, task.GetState())

	task.SetState(StateError)
	assert.Equal(t, StateError, task.GetState())
}

func TestContinuousTask_Connected(t *testing.T) {
	config := *NewContinuousTaskConfig("task-1", "Test Task", TaskTypeDataCollector)
	task := NewContinuousTask(config)

	assert.False(t, task.IsConnected())

	task.SetConnected(true)
	assert.True(t, task.IsConnected())

	task.SetConnected(false)
	assert.False(t, task.IsConnected())
}

func TestContinuousTask_IncrementDataCount(t *testing.T) {
	config := *NewContinuousTaskConfig("task-1", "Test Task", TaskTypeDataCollector)
	task := NewContinuousTask(config)

	count := task.IncrementDataCount()
	assert.Equal(t, int64(1), count)
	assert.Equal(t, int64(1), task.GetDataCount())

	count = task.IncrementDataCount()
	assert.Equal(t, int64(2), count)
	assert.Equal(t, int64(2), task.GetDataCount())
}

func TestContinuousTask_IncrementErrorCount(t *testing.T) {
	config := *NewContinuousTaskConfig("task-1", "Test Task", TaskTypeDataCollector)
	task := NewContinuousTask(config)

	count := task.IncrementErrorCount()
	assert.Equal(t, int64(1), count)
	assert.Equal(t, int64(1), task.GetErrorCount())
}

func TestContinuousTask_UpdateLastDataTime(t *testing.T) {
	config := *NewContinuousTaskConfig("task-1", "Test Task", TaskTypeDataCollector)
	task := NewContinuousTask(config)

	before := time.Now()
	task.UpdateLastDataTime()
	after := time.Now()

	lastDataTime := task.GetLastDataTime()
	assert.True(t, lastDataTime.After(before) || lastDataTime.Equal(before))
	assert.True(t, lastDataTime.Before(after) || lastDataTime.Equal(after))
}

func TestContinuousTask_IncrementReconnectCount(t *testing.T) {
	config := *NewContinuousTaskConfig("task-1", "Test Task", TaskTypeDataCollector)
	task := NewContinuousTask(config)

	count := task.IncrementReconnectCount()
	assert.Equal(t, 1, count)
	assert.Equal(t, 1, task.GetReconnectCount())

	count = task.IncrementReconnectCount()
	assert.Equal(t, 2, count)
}

func TestContinuousTask_Context(t *testing.T) {
	config := *NewContinuousTaskConfig("task-1", "Test Task", TaskTypeDataCollector)
	task := NewContinuousTask(config)

	// 未启动时 context 应该是 nil
	assert.Nil(t, task.Context())

	// 启动后应该有 context
	ctx := context.Background()
	task.Start(ctx)
	assert.NotNil(t, task.Context())
}

func TestContinuousTask_GetSnapshot(t *testing.T) {
	config := *NewContinuousTaskConfig("task-1", "Test Task", TaskTypeDataCollector)
	task := NewContinuousTask(config)

	ctx := context.Background()
	task.Start(ctx)
	task.SetConnected(true)
	task.IncrementDataCount()
	task.IncrementDataCount()
	task.IncrementErrorCount()
	task.IncrementReconnectCount()
	task.UpdateLastDataTime()

	snapshot := task.GetSnapshot()

	assert.Equal(t, string(StateRunning), snapshot["state"])
	assert.Equal(t, true, snapshot["connected"])
	assert.Equal(t, int64(2), snapshot["data_count"])
	assert.Equal(t, int64(1), snapshot["error_count"])
	assert.Equal(t, 1, snapshot["reconnect_count"])
	assert.NotZero(t, snapshot["last_data_time"])
	assert.NotZero(t, snapshot["start_time"])
}

func TestContinuousTaskConfig_WithParams(t *testing.T) {
	config := NewContinuousTaskConfig("task-1", "Test Task", TaskTypeDataCollector)
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

