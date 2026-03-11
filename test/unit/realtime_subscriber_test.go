package unit

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/LENAX/task-engine/pkg/core/realtime"
)

func TestNewSubscriber_BlockingPolicy(t *testing.T) {
	policy := realtime.BufferPolicy{
		Mode:     realtime.BufferModeBlocking,
		Capacity: 1000,
	}
	sub := realtime.NewSubscriber("db_sink", policy, 0.8)
	assert.Equal(t, "db_sink", sub.Name)
	assert.Equal(t, 1000, sub.Buffer.Cap())
	assert.True(t, sub.IsBlocking())
}

func TestNewSubscriber_NonBlockingDropPolicy(t *testing.T) {
	policy := realtime.BufferPolicy{
		Mode:     realtime.BufferModeNonBlockingDrop,
		Capacity: 500,
	}
	sub := realtime.NewSubscriber("frontend_sink", policy, 0.8)
	assert.Equal(t, "frontend_sink", sub.Name)
	assert.Equal(t, 500, sub.Buffer.Cap())
	assert.False(t, sub.IsBlocking())
}

func TestSubscriber_AddProcessor(t *testing.T) {
	sub := realtime.NewSubscriber("s1", realtime.BufferPolicy{Capacity: 10}, 0.8)
	cfg := realtime.NewContinuousTaskConfig("task1", "Task1", realtime.TaskTypeStreamProcessor)
	ct := realtime.NewContinuousTask(*cfg)
	sub.AddProcessor("task1", ct)
	procs := sub.GetProcessors()
	assert.Len(t, procs, 1)
	assert.Contains(t, procs, "task1")
}

func TestSubscriber_NewSubscriberWithFilter_FullMode(t *testing.T) {
	policy := realtime.BufferPolicy{Mode: realtime.BufferModeNonBlockingDrop, Capacity: 100}
	sub := realtime.NewSubscriberWithFilter("fe", policy, 0.8, "code", nil)
	assert.True(t, sub.Accept(""))   // 全量：空值也接受
	assert.True(t, sub.Accept("600863.SH"))
	assert.Equal(t, "code", sub.GetFilterField())
}

func TestSubscriber_NewSubscriberWithFilter_ByCode(t *testing.T) {
	policy := realtime.BufferPolicy{Mode: realtime.BufferModeNonBlockingDrop, Capacity: 100}
	sub := realtime.NewSubscriberWithFilter("fe", policy, 0.8, "code", []string{"600863.SH", "601169.SH"})
	assert.False(t, sub.Accept(""))
	assert.True(t, sub.Accept("600863.SH"))
	assert.True(t, sub.Accept("601169.SH"))
	assert.False(t, sub.Accept("000001.SZ"))
	assert.Equal(t, "code", sub.GetFilterField())
}

func TestSubscriber_NewSubscriberWithFilter_BySymbol(t *testing.T) {
	policy := realtime.BufferPolicy{Mode: realtime.BufferModeNonBlockingDrop, Capacity: 100}
	sub := realtime.NewSubscriberWithFilter("fe", policy, 0.8, "symbol", []string{"AAPL", "MSFT"})
	assert.Equal(t, "symbol", sub.GetFilterField())
	assert.True(t, sub.Accept("AAPL"))
	assert.True(t, sub.Accept("MSFT"))
	assert.False(t, sub.Accept("GOOG"))
}

func TestSubscriber_SetFilter_RuntimeUpdate(t *testing.T) {
	policy := realtime.BufferPolicy{Mode: realtime.BufferModeNonBlockingDrop, Capacity: 100}
	sub := realtime.NewSubscriberWithFilter("fe", policy, 0.8, "code", []string{"A"})
	assert.True(t, sub.Accept("A"))
	assert.False(t, sub.Accept("B"))
	sub.SetFilter("code", []string{"B", "C"})
	assert.False(t, sub.Accept("A"))
	assert.True(t, sub.Accept("B"))
	assert.True(t, sub.Accept("C"))
	sub.SetFilter("symbol", nil) // 全量
	assert.Equal(t, "symbol", sub.GetFilterField())
	assert.True(t, sub.Accept("X"))
}

func TestSubscriber_SetFilterCodes_BackwardCompat(t *testing.T) {
	policy := realtime.BufferPolicy{Mode: realtime.BufferModeNonBlockingDrop, Capacity: 100}
	sub := realtime.NewSubscriberWithFilterCodes("fe", policy, 0.8, []string{"600863.SH"})
	assert.Equal(t, "code", sub.GetFilterField())
	assert.True(t, sub.Accept("600863.SH"))
	assert.False(t, sub.Accept("000001.SZ"))
	sub.SetFilterCodes([]string{"000001.SZ"})
	assert.True(t, sub.Accept("000001.SZ"))
	assert.False(t, sub.Accept("600863.SH"))
}
