package unit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/LENAX/task-engine/pkg/core/realtime"
)

func TestNewDataBuffer_DefaultValues(t *testing.T) {
	buffer := realtime.NewDataBuffer(0, 0)

	assert.Equal(t, 10000, buffer.Cap())
	assert.Equal(t, 0.8, buffer.GetThreshold())
}

func TestNewDataBuffer_CustomValues(t *testing.T) {
	buffer := realtime.NewDataBuffer(5000, 0.9)

	assert.Equal(t, 5000, buffer.Cap())
	assert.Equal(t, 0.9, buffer.GetThreshold())
}

func TestDataBuffer_Push_NonBlocking(t *testing.T) {
	buffer := realtime.NewDataBuffer(10, 0.8)

	for i := 0; i < 10; i++ {
		ok := buffer.Push(i)
		assert.True(t, ok, "Push should succeed for item %d", i)
	}

	ok := buffer.Push(100)
	assert.False(t, ok, "Push should fail when buffer is full")

	assert.Equal(t, int64(10), buffer.GetTotalIn())
	assert.Equal(t, int64(1), buffer.GetDropped())
}

func TestDataBuffer_Pop_NonBlocking(t *testing.T) {
	buffer := realtime.NewDataBuffer(10, 0.8)

	_, ok := buffer.Pop()
	assert.False(t, ok, "Pop should fail on empty buffer")

	buffer.Push("test")

	item, ok := buffer.Pop()
	assert.True(t, ok, "Pop should succeed")
	assert.Equal(t, "test", item)

	_, ok = buffer.Pop()
	assert.False(t, ok, "Pop should fail on empty buffer")

	assert.Equal(t, int64(1), buffer.GetTotalOut())
}

func TestDataBuffer_PushBlocking(t *testing.T) {
	buffer := realtime.NewDataBuffer(10, 0.8)

	done := make(chan struct{})
	go func() {
		buffer.PushBlocking("blocking-test")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("PushBlocking should not block when buffer has space")
	}

	item, ok := buffer.Pop()
	assert.True(t, ok)
	assert.Equal(t, "blocking-test", item)
}

func TestDataBuffer_PopBlocking(t *testing.T) {
	buffer := realtime.NewDataBuffer(10, 0.8)

	done := make(chan interface{})
	go func() {
		item := buffer.PopBlocking()
		done <- item
	}()

	time.Sleep(50 * time.Millisecond)

	buffer.Push("blocking-pop-test")

	select {
	case item := <-done:
		assert.Equal(t, "blocking-pop-test", item)
	case <-time.After(time.Second):
		t.Fatal("PopBlocking should return when data is available")
	}
}

func TestDataBuffer_TryPopWithDone(t *testing.T) {
	buffer := realtime.NewDataBuffer(10, 0.8)
	done := make(chan struct{})

	go func() {
		time.Sleep(50 * time.Millisecond)
		close(done)
	}()

	_, ok, cancelled := buffer.TryPopWithDone(done)
	assert.False(t, ok)
	assert.True(t, cancelled)

	buffer.Push("test")
	done2 := make(chan struct{})
	item, ok, cancelled := buffer.TryPopWithDone(done2)
	assert.True(t, ok)
	assert.False(t, cancelled)
	assert.Equal(t, "test", item)
}

func TestDataBuffer_Usage(t *testing.T) {
	buffer := realtime.NewDataBuffer(100, 0.8)

	assert.Equal(t, 0.0, buffer.Usage())

	for i := 0; i < 50; i++ {
		buffer.Push(i)
	}
	assert.Equal(t, 0.5, buffer.Usage())

	for i := 0; i < 50; i++ {
		buffer.Push(i)
	}
	assert.Equal(t, 1.0, buffer.Usage())
}

func TestDataBuffer_Backpressure(t *testing.T) {
	buffer := realtime.NewDataBuffer(100, 0.8)

	assert.False(t, buffer.IsBackpressure())

	for i := 0; i < 80; i++ {
		buffer.Push(i)
	}
	assert.True(t, buffer.IsBackpressure(), "Should be in backpressure state when usage >= threshold")

	for i := 0; i < 50; i++ {
		buffer.Pop()
	}
	assert.False(t, buffer.IsBackpressure(), "Backpressure should be relieved when usage < threshold * 0.5")
}

func TestDataBuffer_BackpressureCallback(t *testing.T) {
	buffer := realtime.NewDataBuffer(100, 0.8)

	var backpressureTriggered int32
	var backpressureRelieved int32

	buffer.SetBackpressureCallback(func(usage float64) {
		atomic.AddInt32(&backpressureTriggered, 1)
	})

	buffer.SetBackpressureRelieveCallback(func(usage float64) {
		atomic.AddInt32(&backpressureRelieved, 1)
	})

	for i := 0; i < 80; i++ {
		buffer.Push(i)
	}

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int32(1), atomic.LoadInt32(&backpressureTriggered))

	for i := 0; i < 50; i++ {
		buffer.Pop()
	}

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int32(1), atomic.LoadInt32(&backpressureRelieved))
}

func TestDataBuffer_Stats(t *testing.T) {
	buffer := realtime.NewDataBuffer(100, 0.8)

	for i := 0; i < 30; i++ {
		buffer.Push(i)
	}

	for i := 0; i < 10; i++ {
		buffer.Pop()
	}

	totalIn, totalOut, dropped, usage := buffer.Stats()

	assert.Equal(t, int64(30), totalIn)
	assert.Equal(t, int64(10), totalOut)
	assert.Equal(t, int64(0), dropped)
	assert.Equal(t, 0.2, usage)
}

func TestDataBuffer_Clear(t *testing.T) {
	buffer := realtime.NewDataBuffer(100, 0.8)

	for i := 0; i < 50; i++ {
		buffer.Push(i)
	}

	count := buffer.Clear()
	assert.Equal(t, 50, count)
	assert.Equal(t, 0, buffer.Len())
}

func TestDataBuffer_Reset(t *testing.T) {
	buffer := realtime.NewDataBuffer(100, 0.8)

	for i := 0; i < 50; i++ {
		buffer.Push(i)
	}
	buffer.Pop()

	buffer.Reset()

	assert.Equal(t, int64(0), buffer.GetTotalIn())
	assert.Equal(t, int64(0), buffer.GetTotalOut())
	assert.Equal(t, int64(0), buffer.GetDropped())
	assert.False(t, buffer.IsBackpressure())
}

func TestDataBuffer_Drain(t *testing.T) {
	buffer := realtime.NewDataBuffer(100, 0.8)

	for i := 0; i < 5; i++ {
		buffer.Push(i)
	}

	items := buffer.Drain()

	assert.Len(t, items, 5)
	assert.Equal(t, 0, buffer.Len())
	assert.Equal(t, int64(5), buffer.GetTotalOut())
}

func TestDataBuffer_Concurrent(t *testing.T) {
	buffer := realtime.NewDataBuffer(1000, 0.8)
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				buffer.Push(id*100 + j)
			}
		}(i)
	}

	var readCount int64
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if _, ok := buffer.Pop(); ok {
					atomic.AddInt64(&readCount, 1)
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()

	totalIn, totalOut, _, _ := buffer.Stats()
	assert.Equal(t, int64(1000), totalIn)
	assert.Equal(t, readCount, totalOut)
}

func TestDataBuffer_LenAndCap(t *testing.T) {
	buffer := realtime.NewDataBuffer(50, 0.8)

	assert.Equal(t, 50, buffer.Cap())
	assert.Equal(t, 0, buffer.Len())

	for i := 0; i < 25; i++ {
		buffer.Push(i)
	}

	assert.Equal(t, 25, buffer.Len())
	assert.Equal(t, 50, buffer.Cap())
}

func TestDataBuffer_GetThreshold(t *testing.T) {
	buffer := realtime.NewDataBuffer(100, 0.75)
	assert.Equal(t, 0.75, buffer.GetThreshold())
}
