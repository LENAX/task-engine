package realtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stevelan1995/task-engine/pkg/core/task"
)

func createTestTask(name string) *task.Task {
	return task.NewTask(name, "Test task description", "func-id-1", nil, nil)
}

func TestTaskExecutionMode_Constants(t *testing.T) {
	assert.Equal(t, TaskExecutionMode("oneshot"), ExecutionModeOneShot)
	assert.Equal(t, TaskExecutionMode("continuous"), ExecutionModeContinuous)
	assert.Equal(t, TaskExecutionMode("event"), ExecutionModeEventDriven)
}

func TestNewRealtimeTask(t *testing.T) {
	baseTask := createTestTask("test-task")
	rtTask := NewRealtimeTask(baseTask, ExecutionModeContinuous)

	assert.Equal(t, baseTask, rtTask.Task)
	assert.Equal(t, ExecutionModeContinuous, rtTask.ExecutionMode)
	assert.NotNil(t, rtTask.EventSubscriptions)
	assert.Len(t, rtTask.EventSubscriptions, 0)
	assert.Nil(t, rtTask.ContinuousConfig)
}

func TestNewContinuousRealtimeTask(t *testing.T) {
	baseTask := createTestTask("continuous-task")
	config := NewContinuousTaskConfig("cfg-1", "Continuous Config", TaskTypeDataCollector)

	rtTask := NewContinuousRealtimeTask(baseTask, config)

	assert.Equal(t, baseTask, rtTask.Task)
	assert.Equal(t, ExecutionModeContinuous, rtTask.ExecutionMode)
	assert.Equal(t, config, rtTask.ContinuousConfig)
	assert.NotNil(t, rtTask.EventSubscriptions)
}

func TestNewEventDrivenRealtimeTask(t *testing.T) {
	baseTask := createTestTask("event-task")
	subscriptions := []EventSubscription{
		{EventType: EventDataArrived, HandlerName: "handler1"},
		{EventType: EventError, HandlerName: "handler2"},
	}

	rtTask := NewEventDrivenRealtimeTask(baseTask, subscriptions)

	assert.Equal(t, baseTask, rtTask.Task)
	assert.Equal(t, ExecutionModeEventDriven, rtTask.ExecutionMode)
	assert.Len(t, rtTask.EventSubscriptions, 2)
	assert.Nil(t, rtTask.ContinuousConfig)
}

func TestRealtimeTask_WithContinuousConfig(t *testing.T) {
	baseTask := createTestTask("test-task")
	rtTask := NewRealtimeTask(baseTask, ExecutionModeContinuous)

	config := NewContinuousTaskConfig("cfg-1", "Config", TaskTypeStreamProcessor)
	rtTask.WithContinuousConfig(config)

	assert.Equal(t, config, rtTask.ContinuousConfig)
}

func TestRealtimeTask_WithEventSubscriptions(t *testing.T) {
	baseTask := createTestTask("test-task")
	rtTask := NewRealtimeTask(baseTask, ExecutionModeEventDriven)

	subscriptions := []EventSubscription{
		{EventType: EventDataArrived, HandlerName: "handler1"},
	}
	rtTask.WithEventSubscriptions(subscriptions)

	assert.Len(t, rtTask.EventSubscriptions, 1)
	assert.Equal(t, EventDataArrived, rtTask.EventSubscriptions[0].EventType)
}

func TestRealtimeTask_AddEventSubscription(t *testing.T) {
	baseTask := createTestTask("test-task")
	rtTask := NewRealtimeTask(baseTask, ExecutionModeEventDriven)

	rtTask.AddEventSubscription(EventSubscription{
		EventType:   EventDataArrived,
		HandlerName: "handler1",
	})
	rtTask.AddEventSubscription(EventSubscription{
		EventType:   EventError,
		HandlerName: "handler2",
	})

	assert.Len(t, rtTask.EventSubscriptions, 2)
}

func TestRealtimeTask_IsContinuous(t *testing.T) {
	baseTask := createTestTask("test-task")

	continuousTask := NewRealtimeTask(baseTask, ExecutionModeContinuous)
	assert.True(t, continuousTask.IsContinuous())
	assert.False(t, continuousTask.IsEventDriven())
	assert.False(t, continuousTask.IsOneShot())

	eventTask := NewRealtimeTask(baseTask, ExecutionModeEventDriven)
	assert.False(t, eventTask.IsContinuous())
	assert.True(t, eventTask.IsEventDriven())
	assert.False(t, eventTask.IsOneShot())

	oneShotTask := NewRealtimeTask(baseTask, ExecutionModeOneShot)
	assert.False(t, oneShotTask.IsContinuous())
	assert.False(t, oneShotTask.IsEventDriven())
	assert.True(t, oneShotTask.IsOneShot())
}

func TestRealtimeTask_GetExecutionMode(t *testing.T) {
	baseTask := createTestTask("test-task")

	rtTask := NewRealtimeTask(baseTask, ExecutionModeContinuous)
	assert.Equal(t, ExecutionModeContinuous, rtTask.GetExecutionMode())

	rtTask = NewRealtimeTask(baseTask, ExecutionModeEventDriven)
	assert.Equal(t, ExecutionModeEventDriven, rtTask.GetExecutionMode())
}

func TestRealtimeTask_GetContinuousConfig(t *testing.T) {
	baseTask := createTestTask("test-task")
	rtTask := NewRealtimeTask(baseTask, ExecutionModeContinuous)

	// 初始为 nil
	assert.Nil(t, rtTask.GetContinuousConfig())

	// 设置后应该返回配置
	config := NewContinuousTaskConfig("cfg-1", "Config", TaskTypeDataCollector)
	rtTask.WithContinuousConfig(config)
	assert.Equal(t, config, rtTask.GetContinuousConfig())
}

func TestRealtimeTask_GetEventSubscriptions(t *testing.T) {
	baseTask := createTestTask("test-task")
	rtTask := NewRealtimeTask(baseTask, ExecutionModeEventDriven)

	// 初始为空
	subs := rtTask.GetEventSubscriptions()
	assert.NotNil(t, subs)
	assert.Len(t, subs, 0)

	// 添加订阅后
	rtTask.AddEventSubscription(EventSubscription{EventType: EventDataArrived, HandlerName: "handler1"})
	subs = rtTask.GetEventSubscriptions()
	assert.Len(t, subs, 1)

	// 验证返回的是副本
	subs[0].HandlerName = "modified"
	originalSubs := rtTask.GetEventSubscriptions()
	assert.Equal(t, "handler1", originalSubs[0].HandlerName)
}

func TestExtractRealtimeTask_RealtimeTask(t *testing.T) {
	baseTask := createTestTask("test-task")
	rtTask := NewRealtimeTask(baseTask, ExecutionModeContinuous)

	extracted := ExtractRealtimeTask(rtTask)
	require.NotNil(t, extracted)
	assert.Equal(t, rtTask, extracted)
}

func TestExtractRealtimeTask_TaskWithParams(t *testing.T) {
	baseTask := createTestTask("test-task")
	baseTask.SetParam("execution_mode", "continuous")

	extracted := ExtractRealtimeTask(baseTask)
	require.NotNil(t, extracted)
	assert.Equal(t, ExecutionModeContinuous, extracted.ExecutionMode)
}

func TestExtractRealtimeTask_TaskWithEventMode(t *testing.T) {
	baseTask := createTestTask("test-task")
	baseTask.SetParam("execution_mode", "event")

	extracted := ExtractRealtimeTask(baseTask)
	require.NotNil(t, extracted)
	assert.Equal(t, ExecutionModeEventDriven, extracted.ExecutionMode)
}

func TestExtractRealtimeTask_NormalTask(t *testing.T) {
	baseTask := createTestTask("test-task")

	// 没有 execution_mode 参数
	extracted := ExtractRealtimeTask(baseTask)
	assert.Nil(t, extracted)
}

func TestExtractRealtimeTask_NilTask(t *testing.T) {
	extracted := ExtractRealtimeTask(nil)
	assert.Nil(t, extracted)
}

func TestExtractRealtimeTask_BatchMode(t *testing.T) {
	baseTask := createTestTask("test-task")
	baseTask.SetParam("execution_mode", "batch")

	// batch 模式不是实时任务
	extracted := ExtractRealtimeTask(baseTask)
	assert.Nil(t, extracted)
}

func TestIsRealtimeTask(t *testing.T) {
	baseTask := createTestTask("test-task")

	// RealtimeTask 应该返回 true
	rtTask := NewRealtimeTask(baseTask, ExecutionModeContinuous)
	assert.True(t, IsRealtimeTask(rtTask))

	// 带 execution_mode 参数的 Task 应该返回 true
	taskWithParam := createTestTask("test-task-2")
	taskWithParam.SetParam("execution_mode", "continuous")
	assert.True(t, IsRealtimeTask(taskWithParam))

	// 普通 Task 应该返回 false
	normalTask := createTestTask("normal-task")
	assert.False(t, IsRealtimeTask(normalTask))

	// nil 应该返回 false
	assert.False(t, IsRealtimeTask(nil))
}

func TestRealtimeTask_ChainedBuilder(t *testing.T) {
	baseTask := createTestTask("chained-task")
	config := NewContinuousTaskConfig("cfg-1", "Config", TaskTypeDataCollector)

	rtTask := NewRealtimeTask(baseTask, ExecutionModeContinuous).
		WithContinuousConfig(config).
		AddEventSubscription(EventSubscription{EventType: EventDataArrived, HandlerName: "handler1"}).
		AddEventSubscription(EventSubscription{EventType: EventError, HandlerName: "handler2"})

	assert.Equal(t, config, rtTask.ContinuousConfig)
	assert.Len(t, rtTask.EventSubscriptions, 2)
}

