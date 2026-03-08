package unit

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LENAX/task-engine/pkg/core/realtime"
)

// mockDataCollector 记录 Run 调用与 publish 调用，供单测/集成复用
type mockDataCollector struct {
	runCount    int
	lastConfig  *realtime.ContinuousTaskConfig
	publishCount int
	lastPayload interface{}
}

func (m *mockDataCollector) Run(ctx context.Context, config *realtime.ContinuousTaskConfig, publish realtime.PublishFunc) error {
	m.runCount++
	if config != nil {
		m.lastConfig = &realtime.ContinuousTaskConfig{}
		*m.lastConfig = *config
	}
	// 调用一次 publish 便于测试
	taskID := ""
	if config != nil {
		taskID = config.ID
	}
	e := realtime.NewRealtimeEvent(realtime.EventDataArrived, taskID, "", &realtime.DataArrivedPayload{Data: "test", Source: "mock"})
	if err := publish(e); err != nil {
		return err
	}
	m.publishCount++
	m.lastPayload = e.Payload
	return nil
}

func TestDataCollectorRegistry_Register_Get(t *testing.T) {
	reg := realtime.NewDataCollectorRegistry()
	c := &mockDataCollector{}
	err := reg.Register("mc", c)
	require.NoError(t, err)

	got, ok := reg.Get("mc")
	require.True(t, ok)
	assert.Same(t, c, got)
}

func TestDataCollectorRegistry_Register_DuplicateName(t *testing.T) {
	reg := realtime.NewDataCollectorRegistry()
	c := &mockDataCollector{}
	require.NoError(t, reg.Register("mc", c))
	err := reg.Register("mc", &mockDataCollector{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "已存在")
}

func TestDataCollectorRegistry_Register_EmptyName(t *testing.T) {
	reg := realtime.NewDataCollectorRegistry()
	err := reg.Register("", &mockDataCollector{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不能为空")
}

func TestDataCollectorRegistry_Register_NilCollector(t *testing.T) {
	reg := realtime.NewDataCollectorRegistry()
	err := reg.Register("mc", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不能为 nil")
}

func TestDataCollectorRegistry_Get_NotExists(t *testing.T) {
	reg := realtime.NewDataCollectorRegistry()
	_, ok := reg.Get("nonexistent")
	assert.False(t, ok)
}

func TestDataCollectorRegistry_Exists(t *testing.T) {
	reg := realtime.NewDataCollectorRegistry()
	assert.False(t, reg.Exists("a"))
	require.NoError(t, reg.Register("a", &mockDataCollector{}))
	assert.True(t, reg.Exists("a"))
}

func TestDataCollectorRegistry_ListNames(t *testing.T) {
	reg := realtime.NewDataCollectorRegistry()
	names := reg.ListNames()
	assert.Empty(t, names)

	require.NoError(t, reg.Register("b", &mockDataCollector{}))
	require.NoError(t, reg.Register("a", &mockDataCollector{}))
	names = reg.ListNames()
	assert.Len(t, names, 2)
	assert.Contains(t, names, "a")
	assert.Contains(t, names, "b")
}
