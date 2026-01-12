// Package realtime 提供实时数据采集任务的背压控制缓冲区
package realtime

import (
	"sync"
	"sync/atomic"
)

// DataBuffer 数据缓冲区（用于背压控制）
type DataBuffer struct {
	data         chan interface{}
	capacity     int
	threshold    float64
	backpressure int32 // atomic，0=正常，1=背压

	// 统计
	totalIn  int64 // atomic，总入队数
	totalOut int64 // atomic，总出队数
	dropped  int64 // atomic，丢弃数

	// 回调函数
	onBackpressure        func(usage float64)
	onBackpressureRelieve func(usage float64)

	mu sync.RWMutex
}

// NewDataBuffer 创建数据缓冲区
func NewDataBuffer(capacity int, threshold float64) *DataBuffer {
	if capacity <= 0 {
		capacity = 10000
	}
	if threshold <= 0 || threshold > 1 {
		threshold = 0.8
	}

	return &DataBuffer{
		data:      make(chan interface{}, capacity),
		capacity:  capacity,
		threshold: threshold,
	}
}

// SetBackpressureCallback 设置背压触发回调
func (b *DataBuffer) SetBackpressureCallback(callback func(usage float64)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onBackpressure = callback
}

// SetBackpressureRelieveCallback 设置背压解除回调
func (b *DataBuffer) SetBackpressureRelieveCallback(callback func(usage float64)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onBackpressureRelieve = callback
}

// Push 推入数据（非阻塞）
// 返回 true 表示成功，false 表示缓冲区已满（数据被丢弃）
func (b *DataBuffer) Push(item interface{}) bool {
	select {
	case b.data <- item:
		atomic.AddInt64(&b.totalIn, 1)
		b.checkBackpressure()
		return true
	default:
		// 缓冲区满，丢弃数据
		atomic.AddInt64(&b.dropped, 1)
		return false
	}
}

// PushBlocking 推入数据（阻塞）
func (b *DataBuffer) PushBlocking(item interface{}) {
	b.data <- item
	atomic.AddInt64(&b.totalIn, 1)
	b.checkBackpressure()
}

// Pop 弹出数据（非阻塞）
// 返回数据和是否成功
func (b *DataBuffer) Pop() (interface{}, bool) {
	select {
	case item := <-b.data:
		atomic.AddInt64(&b.totalOut, 1)
		b.checkBackpressure()
		return item, true
	default:
		return nil, false
	}
}

// PopBlocking 弹出数据（阻塞）
func (b *DataBuffer) PopBlocking() interface{} {
	item := <-b.data
	atomic.AddInt64(&b.totalOut, 1)
	b.checkBackpressure()
	return item
}

// TryPopWithTimeout 尝试在指定通道关闭前弹出数据
// 返回数据、是否成功、是否因为 done 通道关闭
func (b *DataBuffer) TryPopWithDone(done <-chan struct{}) (interface{}, bool, bool) {
	select {
	case item := <-b.data:
		atomic.AddInt64(&b.totalOut, 1)
		b.checkBackpressure()
		return item, true, false
	case <-done:
		return nil, false, true
	}
}

// Len 获取当前缓冲区长度
func (b *DataBuffer) Len() int {
	return len(b.data)
}

// Cap 获取缓冲区容量
func (b *DataBuffer) Cap() int {
	return b.capacity
}

// Usage 获取使用率
func (b *DataBuffer) Usage() float64 {
	return float64(len(b.data)) / float64(b.capacity)
}

// IsBackpressure 是否处于背压状态
func (b *DataBuffer) IsBackpressure() bool {
	return atomic.LoadInt32(&b.backpressure) == 1
}

// checkBackpressure 检查背压状态
func (b *DataBuffer) checkBackpressure() {
	usage := b.Usage()

	if usage >= b.threshold {
		if atomic.CompareAndSwapInt32(&b.backpressure, 0, 1) {
			// 触发背压事件
			b.mu.RLock()
			callback := b.onBackpressure
			b.mu.RUnlock()
			if callback != nil {
				go callback(usage)
			}
		}
	} else if usage < b.threshold*0.5 {
		// 当使用率降到阈值的一半以下时解除背压
		if atomic.CompareAndSwapInt32(&b.backpressure, 1, 0) {
			// 解除背压事件
			b.mu.RLock()
			callback := b.onBackpressureRelieve
			b.mu.RUnlock()
			if callback != nil {
				go callback(usage)
			}
		}
	}
}

// Stats 获取统计信息
func (b *DataBuffer) Stats() (totalIn, totalOut, dropped int64, usage float64) {
	return atomic.LoadInt64(&b.totalIn),
		atomic.LoadInt64(&b.totalOut),
		atomic.LoadInt64(&b.dropped),
		b.Usage()
}

// GetTotalIn 获取总入队数
func (b *DataBuffer) GetTotalIn() int64 {
	return atomic.LoadInt64(&b.totalIn)
}

// GetTotalOut 获取总出队数
func (b *DataBuffer) GetTotalOut() int64 {
	return atomic.LoadInt64(&b.totalOut)
}

// GetDropped 获取丢弃数
func (b *DataBuffer) GetDropped() int64 {
	return atomic.LoadInt64(&b.dropped)
}

// GetThreshold 获取背压阈值
func (b *DataBuffer) GetThreshold() float64 {
	return b.threshold
}

// Clear 清空缓冲区
func (b *DataBuffer) Clear() int {
	count := 0
	for {
		select {
		case <-b.data:
			count++
		default:
			return count
		}
	}
}

// Reset 重置统计信息
func (b *DataBuffer) Reset() {
	atomic.StoreInt64(&b.totalIn, 0)
	atomic.StoreInt64(&b.totalOut, 0)
	atomic.StoreInt64(&b.dropped, 0)
	atomic.StoreInt32(&b.backpressure, 0)
}

// Drain 排空缓冲区并返回所有数据
func (b *DataBuffer) Drain() []interface{} {
	var items []interface{}
	for {
		select {
		case item := <-b.data:
			items = append(items, item)
			atomic.AddInt64(&b.totalOut, 1)
		default:
			return items
		}
	}
}
