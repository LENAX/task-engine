package unit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LENAX/task-engine/pkg/core/realtime"
	"github.com/LENAX/task-engine/pkg/core/task"
)

func createRealtimeTestTask(name string) *task.Task {
	return task.NewTask(name, "Test task description", "func-id-1", nil, nil)
}

func TestTaskExecutionMode_Constants(t *testing.T) {
	assert.Equal(t, realtime.TaskExecutionMode("oneshot"), realtime.ExecutionModeOneShot)
	assert.Equal(t, realtime.TaskExecutionMode("continuous"), realtime.ExecutionModeContinuous)
	assert.Equal(t, realtime.TaskExecutionMode("event"), realtime.ExecutionModeEventDriven)
}

func TestNewRealtimeTask(t *testing.T) {
	baseTask := createRealtimeTestTask("test-task")
	rtTask := realtime.NewRealtimeTask(baseTask, realtime.ExecutionModeContinuous)

	assert.Equal(t, baseTask, rtTask.Task)
	assert.Equal(t, realtime.ExecutionModeContinuous, rtTask.ExecutionMode)
	assert.NotNil(t, rtTask.EventSubscriptions)
	assert.Len(t, rtTask.EventSubscriptions, 0)
	assert.Nil(t, rtTask.ContinuousConfig)
}

func TestNewContinuousRealtimeTask(t *testing.T) {
	baseTask := createRealtimeTestTask("continuous-task")
	config := realtime.NewContinuousTaskConfig("cfg-1", "Continuous Config", realtime.TaskTypeDataCollector)

	rtTask := realtime.NewContinuousRealtimeTask(baseTask, config)

	assert.Equal(t, baseTask, rtTask.Task)
	assert.Equal(t, realtime.ExecutionModeContinuous, rtTask.ExecutionMode)
	assert.Equal(t, config, rtTask.ContinuousConfig)
	assert.NotNil(t, rtTask.EventSubscriptions)
}

func TestNewEventDrivenRealtimeTask(t *testing.T) {
	baseTask := createRealtimeTestTask("event-task")
	subscriptions := []realtime.EventSubscription{
		{EventType: realtime.EventDataArrived, HandlerName: "handler1"},
		{EventType: realtime.EventError, HandlerName: "handler2"},
	}

	rtTask := realtime.NewEventDrivenRealtimeTask(baseTask, subscriptions)

	assert.Equal(t, baseTask, rtTask.Task)
	assert.Equal(t, realtime.ExecutionModeEventDriven, rtTask.ExecutionMode)
	assert.Len(t, rtTask.EventSubscriptions, 2)
	assert.Nil(t, rtTask.ContinuousConfig)
}

func TestRealtimeTask_WithContinuousConfig(t *testing.T) {
	baseTask := createRealtimeTestTask("test-task")
	rtTask := realtime.NewRealtimeTask(baseTask, realtime.ExecutionModeContinuous)

	config := realtime.NewContinuousTaskConfig("cfg-1", "Config", realtime.TaskTypeStreamProcessor)
	rtTask.WithContinuousConfig(config)

	assert.Equal(t, config, rtTask.ContinuousConfig)
}

func TestRealtimeTask_WithEventSubscriptions(t *testing.T) {
	baseTask := createRealtimeTestTask("test-task")
	rtTask := realtime.NewRealtimeTask(baseTask, realtime.ExecutionModeEventDriven)

	subscriptions := []realtime.EventSubscription{
		{EventType: realtime.EventDataArrived, HandlerName: "handler1"},
	}
	rtTask.WithEventSubscriptions(subscriptions)

	assert.Len(t, rtTask.EventSubscriptions, 1)
	assert.Equal(t, realtime.EventDataArrived, rtTask.EventSubscriptions[0].EventType)
}

func TestRealtimeTask_AddEventSubscription(t *testing.T) {
	baseTask := createRealtimeTestTask("test-task")
	rtTask := realtime.NewRealtimeTask(baseTask, realtime.ExecutionModeEventDriven)

	rtTask.AddEventSubscription(realtime.EventSubscription{
		EventType:   realtime.EventDataArrived,
		HandlerName: "handler1",
	})
	rtTask.AddEventSubscription(realtime.EventSubscription{
		EventType:   realtime.EventError,
		HandlerName: "handler2",
	})

	assert.Len(t, rtTask.EventSubscriptions, 2)
}

func TestRealtimeTask_IsContinuous(t *testing.T) {
	baseTask := createRealtimeTestTask("test-task")

	continuousTask := realtime.NewRealtimeTask(baseTask, realtime.ExecutionModeContinuous)
	assert.True(t, continuousTask.IsContinuous())
	assert.False(t, continuousTask.IsEventDriven())
	assert.False(t, continuousTask.IsOneShot())

	eventTask := realtime.NewRealtimeTask(baseTask, realtime.ExecutionModeEventDriven)
	assert.False(t, eventTask.IsContinuous())
	assert.True(t, eventTask.IsEventDriven())
	assert.False(t, eventTask.IsOneShot())

	oneShotTask := realtime.NewRealtimeTask(baseTask, realtime.ExecutionModeOneShot)
	assert.False(t, oneShotTask.IsContinuous())
	assert.False(t, oneShotTask.IsEventDriven())
	assert.True(t, oneShotTask.IsOneShot())
}

func TestRealtimeTask_GetExecutionMode(t *testing.T) {
	baseTask := createRealtimeTestTask("test-task")

	rtTask := realtime.NewRealtimeTask(baseTask, realtime.ExecutionModeContinuous)
	assert.Equal(t, realtime.ExecutionModeContinuous, rtTask.GetExecutionMode())

	rtTask = realtime.NewRealtimeTask(baseTask, realtime.ExecutionModeEventDriven)
	assert.Equal(t, realtime.ExecutionModeEventDriven, rtTask.GetExecutionMode())
}

func TestRealtimeTask_GetContinuousConfig(t *testing.T) {
	baseTask := createRealtimeTestTask("test-task")
	rtTask := realtime.NewRealtimeTask(baseTask, realtime.ExecutionModeContinuous)

	assert.Nil(t, rtTask.GetContinuousConfig())

	config := realtime.NewContinuousTaskConfig("cfg-1", "Config", realtime.TaskTypeDataCollector)
	rtTask.WithContinuousConfig(config)
	assert.Equal(t, config, rtTask.GetContinuousConfig())
}

func TestRealtimeTask_GetEventSubscriptions(t *testing.T) {
	baseTask := createRealtimeTestTask("test-task")
	rtTask := realtime.NewRealtimeTask(baseTask, realtime.ExecutionModeEventDriven)

	subs := rtTask.GetEventSubscriptions()
	assert.NotNil(t, subs)
	assert.Len(t, subs, 0)

	rtTask.AddEventSubscription(realtime.EventSubscription{EventType: realtime.EventDataArrived, HandlerName: "handler1"})
	subs = rtTask.GetEventSubscriptions()
	assert.Len(t, subs, 1)

	subs[0].HandlerName = "modified"
	originalSubs := rtTask.GetEventSubscriptions()
	assert.Equal(t, "handler1", originalSubs[0].HandlerName)
}

func TestExtractRealtimeTask_RealtimeTask(t *testing.T) {
	baseTask := createRealtimeTestTask("test-task")
	rtTask := realtime.NewRealtimeTask(baseTask, realtime.ExecutionModeContinuous)

	extracted := realtime.ExtractRealtimeTask(rtTask)
	require.NotNil(t, extracted)
	assert.Equal(t, rtTask, extracted)
}

func TestExtractRealtimeTask_TaskWithParams(t *testing.T) {
	baseTask := createRealtimeTestTask("test-task")
	baseTask.SetParam("execution_mode", "continuous")

	extracted := realtime.ExtractRealtimeTask(baseTask)
	require.NotNil(t, extracted)
	assert.Equal(t, realtime.ExecutionModeContinuous, extracted.ExecutionMode)
}

func TestExtractRealtimeTask_TaskWithEventMode(t *testing.T) {
	baseTask := createRealtimeTestTask("test-task")
	baseTask.SetParam("execution_mode", "event")

	extracted := realtime.ExtractRealtimeTask(baseTask)
	require.NotNil(t, extracted)
	assert.Equal(t, realtime.ExecutionModeEventDriven, extracted.ExecutionMode)
}

func TestExtractRealtimeTask_NormalTask(t *testing.T) {
	baseTask := createRealtimeTestTask("test-task")

	extracted := realtime.ExtractRealtimeTask(baseTask)
	assert.Nil(t, extracted)
}

func TestExtractRealtimeTask_NilTask(t *testing.T) {
	extracted := realtime.ExtractRealtimeTask(nil)
	assert.Nil(t, extracted)
}

func TestExtractRealtimeTask_BatchMode(t *testing.T) {
	baseTask := createRealtimeTestTask("test-task")
	baseTask.SetParam("execution_mode", "batch")

	extracted := realtime.ExtractRealtimeTask(baseTask)
	assert.Nil(t, extracted)
}

func TestIsRealtimeTask(t *testing.T) {
	baseTask := createRealtimeTestTask("test-task")

	rtTask := realtime.NewRealtimeTask(baseTask, realtime.ExecutionModeContinuous)
	assert.True(t, realtime.IsRealtimeTask(rtTask))

	taskWithParam := createRealtimeTestTask("test-task-2")
	taskWithParam.SetParam("execution_mode", "continuous")
	assert.True(t, realtime.IsRealtimeTask(taskWithParam))

	normalTask := createRealtimeTestTask("normal-task")
	assert.False(t, realtime.IsRealtimeTask(normalTask))

	assert.False(t, realtime.IsRealtimeTask(nil))
}

func TestRealtimeTask_ChainedBuilder(t *testing.T) {
	baseTask := createRealtimeTestTask("chained-task")
	config := realtime.NewContinuousTaskConfig("cfg-1", "Config", realtime.TaskTypeDataCollector)

	rtTask := realtime.NewRealtimeTask(baseTask, realtime.ExecutionModeContinuous).
		WithContinuousConfig(config).
		AddEventSubscription(realtime.EventSubscription{EventType: realtime.EventDataArrived, HandlerName: "handler1"}).
		AddEventSubscription(realtime.EventSubscription{EventType: realtime.EventError, HandlerName: "handler2"})

	assert.Equal(t, config, rtTask.ContinuousConfig)
	assert.Len(t, rtTask.EventSubscriptions, 2)
}
