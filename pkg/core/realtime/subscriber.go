// Package realtime 提供多订阅者广播层的订阅者与缓冲策略模型
package realtime

import "sync"

// BufferMode 缓冲模式：Blocking 阻塞写入，NonBlockingDrop 非阻塞满则丢弃
type BufferMode int

const (
	BufferModeBlocking         BufferMode = 0 // 缓冲区满时阻塞生产者
	BufferModeNonBlockingDrop  BufferMode = 1 // 非阻塞，满则丢弃
)

// BufferPolicy 订阅者缓冲策略
type BufferPolicy struct {
	Mode     BufferMode
	Capacity int
}

// Subscriber 下游订阅者，绑定独立 DataBuffer 与策略，供 StreamProcessor 消费
type Subscriber struct {
	Name   string
	Buffer *DataBuffer
	Policy BufferPolicy

	// FilterField 过滤字段名（如 "code"、"symbol"），空时默认 "code"；仅当 FilterValues 非空时生效
	FilterField string
	// FilterValues 允许的字段值集合；空表示全量（不过滤）
	FilterValues map[string]struct{}

	// Processors 关联的 StreamProcessor 任务（按 taskID），用于后续扩展
	Processors map[string]*ContinuousTask
	mu         sync.RWMutex
}

// NewSubscriber 创建订阅者（无过滤，全量）
func NewSubscriber(name string, policy BufferPolicy, backpressureThreshold float64) *Subscriber {
	return NewSubscriberWithFilter(name, policy, backpressureThreshold, "", nil)
}

// NewSubscriberWithFilterCodes 创建订阅者；filterCodes 非空时仅接收 data.code 在列表内的数据（向后兼容）
func NewSubscriberWithFilterCodes(name string, policy BufferPolicy, backpressureThreshold float64, filterCodes []string) *Subscriber {
	return NewSubscriberWithFilter(name, policy, backpressureThreshold, "code", filterCodes)
}

// NewSubscriberWithFilter 创建订阅者；field 为过滤字段名（空则用 "code"），values 为允许的值列表，nil/空表示全量
func NewSubscriberWithFilter(name string, policy BufferPolicy, backpressureThreshold float64, field string, values []string) *Subscriber {
	if policy.Capacity <= 0 {
		policy.Capacity = 10000
	}
	if backpressureThreshold <= 0 || backpressureThreshold > 1 {
		backpressureThreshold = 0.8
	}
	if field == "" {
		field = "code"
	}
	fv := make(map[string]struct{})
	for _, v := range values {
		if v != "" {
			fv[v] = struct{}{}
		}
	}
	return &Subscriber{
		Name:        name,
		Buffer:      NewDataBuffer(policy.Capacity, backpressureThreshold),
		Policy:      policy,
		FilterField: field,
		FilterValues: fv,
		Processors:   make(map[string]*ContinuousTask),
	}
}

// AddProcessor 关联一个 StreamProcessor 任务
func (s *Subscriber) AddProcessor(taskID string, ct *ContinuousTask) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Processors[taskID] = ct
}

// GetProcessors 返回关联任务副本（调用方只读）
func (s *Subscriber) GetProcessors() map[string]*ContinuousTask {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]*ContinuousTask, len(s.Processors))
	for k, v := range s.Processors {
		out[k] = v
	}
	return out
}

// IsBlocking 是否阻塞模式
func (s *Subscriber) IsBlocking() bool {
	return s.Policy.Mode == BufferModeBlocking
}

// GetFilterField 返回当前过滤字段名（供广播层按该字段从 rawData 取值）
func (s *Subscriber) GetFilterField() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.FilterField == "" {
		return "code"
	}
	return s.FilterField
}

// SetFilter 运行时更新过滤字段名与值列表；values 为 nil 或空表示全量（不过滤）
func (s *Subscriber) SetFilter(field string, values []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if field == "" {
		field = "code"
	}
	s.FilterField = field
	fv := make(map[string]struct{})
	for _, v := range values {
		if v != "" {
			fv[v] = struct{}{}
		}
	}
	s.FilterValues = fv
}

// SetFilterCodes 运行时更新订阅代码列表；nil 或空表示全量（向后兼容，等价于 SetFilter("code", codes)）
func (s *Subscriber) SetFilterCodes(codes []string) {
	s.SetFilter("code", codes)
}

// Accept 供广播层调用：根据当前 FilterField 从 rawData 取出的 value，若 FilterValues 为空则接受，否则仅当 value 在集合内时接受
func (s *Subscriber) Accept(value string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.FilterValues) == 0 {
		return true
	}
	_, ok := s.FilterValues[value]
	return ok
}