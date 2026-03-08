// Package realtime 提供实时数据采集的 DataCollector 接口与注册表
package realtime

import (
	"context"
	"fmt"
	"sync"
)

// PublishFunc 由 Manager 注入，实现者收到数据后调用以将数据写入引擎缓冲
type PublishFunc func(event *RealtimeEvent) error

// DataCollector 数据采集器接口，由用户实现并注册
// Run 阻塞运行直至 ctx.Done() 或返回错误；实现者负责建连、收包、重连（可选），在收到数据时调用 publish
type DataCollector interface {
	Run(ctx context.Context, config *ContinuousTaskConfig, publish PublishFunc) error
}

// DataCollectorRegistry 采集器注册表接口
type DataCollectorRegistry interface {
	Register(name string, collector DataCollector) error
	Get(name string) (DataCollector, bool)
	Exists(name string) bool
	ListNames() []string
}

// defaultCollectorRegistry 默认实现
type defaultCollectorRegistry struct {
	mu   sync.RWMutex
	byName map[string]DataCollector
}

// NewDataCollectorRegistry 创建默认的采集器注册表
func NewDataCollectorRegistry() DataCollectorRegistry {
	return &defaultCollectorRegistry{
		byName: make(map[string]DataCollector),
	}
}

// Register 注册采集器；空 name 或 nil collector 或重复 name 返回 error
func (r *defaultCollectorRegistry) Register(name string, collector DataCollector) error {
	if name == "" {
		return fmt.Errorf("采集器名称不能为空")
	}
	if collector == nil {
		return fmt.Errorf("采集器不能为 nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byName[name]; exists {
		return fmt.Errorf("采集器名称已存在: %s", name)
	}
	r.byName[name] = collector
	return nil
}

// Get 按名称获取采集器
func (r *defaultCollectorRegistry) Get(name string) (DataCollector, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.byName[name]
	return c, ok
}

// Exists 检查名称是否已注册
func (r *defaultCollectorRegistry) Exists(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.byName[name]
	return ok
}

// ListNames 返回已注册名称列表
func (r *defaultCollectorRegistry) ListNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.byName))
	for n := range r.byName {
		names = append(names, n)
	}
	return names
}
