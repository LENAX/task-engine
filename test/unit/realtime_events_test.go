package unit

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LENAX/task-engine/pkg/core/realtime"
)

func TestEventType_Constants(t *testing.T) {
	assert.Equal(t, realtime.EventType("data.arrived"), realtime.EventDataArrived)
	assert.Equal(t, realtime.EventType("data.processed"), realtime.EventDataProcessed)
	assert.Equal(t, realtime.EventType("task.started"), realtime.EventTaskStarted)
	assert.Equal(t, realtime.EventType("connection.established"), realtime.EventConnected)
	assert.Equal(t, realtime.EventType("connection.lost"), realtime.EventDisconnected)
	assert.Equal(t, realtime.EventType("error.occurred"), realtime.EventError)
	assert.Equal(t, realtime.EventType("backpressure.triggered"), realtime.EventBackpressure)
}

func TestNewRealtimeEvent(t *testing.T) {
	payload := &realtime.DataArrivedPayload{
		Data:   "test data",
		Source: "test_source",
		Size:   100,
	}

	event := realtime.NewRealtimeEvent(realtime.EventDataArrived, "task-1", "instance-1", payload)

	assert.NotEmpty(t, event.ID)
	assert.Equal(t, realtime.EventDataArrived, event.Type)
	assert.Equal(t, "task-1", event.TaskID)
	assert.Equal(t, "instance-1", event.InstanceID)
	assert.NotZero(t, event.Timestamp)
	assert.Equal(t, payload, event.Payload)
	assert.NotNil(t, event.Metadata)
}

func TestRealtimeEvent_WithMetadata(t *testing.T) {
	event := realtime.NewRealtimeEvent(realtime.EventDataArrived, "task-1", "instance-1", nil)

	event.WithMetadata("key1", "value1").WithMetadata("key2", "value2")

	assert.Equal(t, "value1", event.Metadata["key1"])
	assert.Equal(t, "value2", event.Metadata["key2"])
}

func TestRealtimeEvent_WithCorrelationID(t *testing.T) {
	event := realtime.NewRealtimeEvent(realtime.EventDataArrived, "task-1", "instance-1", nil)

	event.WithCorrelationID("correlation-123")

	assert.Equal(t, "correlation-123", event.CorrelationID)
}

func TestRealtimeEvent_JSON_Serialization(t *testing.T) {
	payload := &realtime.DataArrivedPayload{
		Data:     map[string]interface{}{"field": "value"},
		Source:   "test_source",
		Size:     256,
		Sequence: 1,
		BatchID:  "batch-001",
	}

	event := realtime.NewRealtimeEvent(realtime.EventDataArrived, "task-1", "instance-1", payload)
	event.WithMetadata("env", "test").WithCorrelationID("corr-123")

	data, err := json.Marshal(event)
	require.NoError(t, err)

	var decoded realtime.RealtimeEvent
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, event.ID, decoded.ID)
	assert.Equal(t, event.Type, decoded.Type)
	assert.Equal(t, event.TaskID, decoded.TaskID)
	assert.Equal(t, event.InstanceID, decoded.InstanceID)
	assert.Equal(t, event.CorrelationID, decoded.CorrelationID)
	assert.Equal(t, "test", decoded.Metadata["env"])
}

func TestDataArrivedPayload_Fields(t *testing.T) {
	payload := &realtime.DataArrivedPayload{
		Data:     []byte("raw data"),
		Source:   "websocket://api.example.com",
		Size:     1024,
		Sequence: 42,
		BatchID:  "batch-100",
	}

	assert.NotNil(t, payload.Data)
	assert.Equal(t, "websocket://api.example.com", payload.Source)
	assert.Equal(t, int64(1024), payload.Size)
	assert.Equal(t, int64(42), payload.Sequence)
	assert.Equal(t, "batch-100", payload.BatchID)
}

func TestConnectionPayload_Fields(t *testing.T) {
	payload := &realtime.ConnectionPayload{
		Endpoint:   "wss://api.tushare.pro/ws",
		Protocol:   "websocket",
		RetryCount: 3,
		Error:      "connection timeout",
	}

	assert.Equal(t, "wss://api.tushare.pro/ws", payload.Endpoint)
	assert.Equal(t, "websocket", payload.Protocol)
	assert.Equal(t, 3, payload.RetryCount)
	assert.Equal(t, "connection timeout", payload.Error)
}

func TestErrorPayload_Fields(t *testing.T) {
	payload := &realtime.ErrorPayload{
		ErrorCode:   "E001",
		Message:     "connection lost",
		Stack:       "at main.go:123",
		Recoverable: true,
	}

	assert.Equal(t, "E001", payload.ErrorCode)
	assert.Equal(t, "connection lost", payload.Message)
	assert.Equal(t, "at main.go:123", payload.Stack)
	assert.True(t, payload.Recoverable)
}

func TestBackpressurePayload_Fields(t *testing.T) {
	payload := &realtime.BackpressurePayload{
		BufferUsage: 0.85,
		QueueLength: 8500,
		Threshold:   0.8,
		Action:      "throttle",
	}

	assert.Equal(t, 0.85, payload.BufferUsage)
	assert.Equal(t, 8500, payload.QueueLength)
	assert.Equal(t, 0.8, payload.Threshold)
	assert.Equal(t, "throttle", payload.Action)
}

func TestTaskStatusPayload_Fields(t *testing.T) {
	payload := &realtime.TaskStatusPayload{
		TaskID:    "task-123",
		TaskName:  "quote_collector",
		OldStatus: "running",
		NewStatus: "paused",
		Reason:    "user request",
	}

	assert.Equal(t, "task-123", payload.TaskID)
	assert.Equal(t, "quote_collector", payload.TaskName)
	assert.Equal(t, "running", payload.OldStatus)
	assert.Equal(t, "paused", payload.NewStatus)
	assert.Equal(t, "user request", payload.Reason)
}

func TestEventSubscription_Fields(t *testing.T) {
	subscription := realtime.EventSubscription{
		EventType:   realtime.EventDataArrived,
		HandlerName: "process_quote",
		Filter:      "ts_code == '000001.SZ'",
		Priority:    10,
	}

	assert.Equal(t, realtime.EventDataArrived, subscription.EventType)
	assert.Equal(t, "process_quote", subscription.HandlerName)
	assert.Equal(t, "ts_code == '000001.SZ'", subscription.Filter)
	assert.Equal(t, 10, subscription.Priority)
}

func TestRealtimeEvent_TimestampIsRecent(t *testing.T) {
	before := time.Now()
	event := realtime.NewRealtimeEvent(realtime.EventDataArrived, "task-1", "instance-1", nil)
	after := time.Now()

	assert.True(t, event.Timestamp.After(before) || event.Timestamp.Equal(before))
	assert.True(t, event.Timestamp.Before(after) || event.Timestamp.Equal(after))
}
