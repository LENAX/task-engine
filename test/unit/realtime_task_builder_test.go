package unit

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LENAX/task-engine/pkg/core/builder"
	"github.com/LENAX/task-engine/pkg/core/realtime"
	"github.com/LENAX/task-engine/pkg/core/task"
)

func TestRealtimeTaskBuilder_WithCollector(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()
	registry.Register(ctx, "job1", func(ctx context.Context) error { return nil }, "job1")

	rt, err := builder.NewRealtimeTaskBuilder("t1", "task1", registry).
		WithContinuousMode().
		WithCollector("my_collector").
		WithJobFunction("job1", nil).
		Build()
	require.NoError(t, err)
	require.NotNil(t, rt.ContinuousConfig)
	assert.Equal(t, "my_collector", rt.ContinuousConfig.CollectorName)
	assert.Equal(t, realtime.CollectorModePush, rt.ContinuousConfig.Mode)
}

func TestRealtimeTaskBuilder_WithCollector_Unset(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()
	registry.Register(ctx, "job1", func(ctx context.Context) error { return nil }, "job1")

	rt, err := builder.NewRealtimeTaskBuilder("t1", "task1", registry).
		WithContinuousMode().
		WithJobFunction("job1", nil).
		Build()
	require.NoError(t, err)
	if rt.ContinuousConfig != nil {
		assert.Empty(t, rt.ContinuousConfig.CollectorName)
	}
}

func TestRealtimeTaskBuilder_WithMode_Pull(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()
	registry.Register(ctx, "job1", func(ctx context.Context) error { return nil }, "job1")

	rt, err := builder.NewRealtimeTaskBuilder("t1", "task1", registry).
		WithContinuousMode().
		WithMode(realtime.CollectorModePull).
		WithJobFunction("job1", nil).
		Build()
	require.NoError(t, err)
	require.NotNil(t, rt.ContinuousConfig)
	assert.Equal(t, realtime.CollectorModePull, rt.ContinuousConfig.Mode)
}

func TestRealtimeTaskBuilder_WithMode_Default(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()
	registry.Register(ctx, "job1", func(ctx context.Context) error { return nil }, "job1")

	rt, err := builder.NewRealtimeTaskBuilder("t1", "task1", registry).
		WithContinuousMode().
		WithJobFunction("job1", nil).
		Build()
	require.NoError(t, err)
	if rt.ContinuousConfig != nil {
		assert.Equal(t, realtime.CollectorModePush, rt.ContinuousConfig.Mode)
	}
}

func TestRealtimeTaskBuilder_WithTaskType_ScheduledPoller_SetsModePull(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()
	registry.Register(ctx, "job1", func(ctx context.Context) error { return nil }, "job1")

	rt, err := builder.NewRealtimeTaskBuilder("t1", "task1", registry).
		WithContinuousMode().
		WithTaskType(realtime.TaskTypeScheduledPoller).
		WithJobFunction("job1", nil).
		Build()
	require.NoError(t, err)
	require.NotNil(t, rt.ContinuousConfig)
	assert.Equal(t, realtime.TaskTypeScheduledPoller, rt.ContinuousConfig.Type)
	assert.Equal(t, realtime.CollectorModePull, rt.ContinuousConfig.Mode)
}
