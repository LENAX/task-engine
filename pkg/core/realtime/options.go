// Package realtime 提供实时数据采集任务的配置选项
package realtime

import (
	"time"

	"github.com/LENAX/task-engine/pkg/core/task"
)

// DataHandlerRegistry 用于 runStreamProcessor 按名获取并调用 DataHandler 的最小接口
type DataHandlerRegistry interface {
	GetByName(name string) task.JobFunctionType
}

// options 内部配置选项
type options struct {
	// 缓冲区配置
	bufferSize            int
	backpressureThreshold float64

	// 采集器注册表（可选，用于 runDataCollector 按名查找）
	collectorRegistry DataCollectorRegistry

	// 函数注册表（可选，用于 runStreamProcessor 调用 DataHandler）
	functionRegistry DataHandlerRegistry

	// 日志配置
	debug bool
	trace bool

	// 任务配置
	defaultReconnectEnabled     bool
	defaultMaxReconnectAttempts int
	defaultReconnectBackoff     ReconnectBackoffConfig

	// 超时配置
	shutdownTimeout  time.Duration
	reconnectTimeout time.Duration

	// 多订阅者广播与 WAL
	broadcastEnabled bool
	walEnabled       bool
	walStore         WalStore
	walPath          string // 未设置 walStore 时用此路径创建 Badger（如按 instanceID 分目录）
}

// defaultOptions 返回默认配置
func defaultOptions() *options {
	return &options{
		bufferSize:                  10000,
		backpressureThreshold:       0.8,
		debug:                       false,
		trace:                       false,
		defaultReconnectEnabled:     true,
		defaultMaxReconnectAttempts: 0, // 无限重连
		defaultReconnectBackoff:     DefaultReconnectBackoffConfig(),
		shutdownTimeout:             30 * time.Second,
		reconnectTimeout:            5 * time.Minute,
	}
}

// Option 配置选项函数类型
type Option func(*options)

// WithBufferSize 设置缓冲区大小
func WithBufferSize(size int) Option {
	return func(o *options) {
		if size > 0 {
			o.bufferSize = size
		}
	}
}

// WithBackpressureThreshold 设置背压阈值
func WithBackpressureThreshold(threshold float64) Option {
	return func(o *options) {
		if threshold > 0 && threshold <= 1 {
			o.backpressureThreshold = threshold
		}
	}
}

// WithCollectorRegistry 设置采集器注册表
func WithCollectorRegistry(registry DataCollectorRegistry) Option {
	return func(o *options) {
		o.collectorRegistry = registry
	}
}

// WithFunctionRegistry 设置函数注册表（用于 runStreamProcessor 调用 DataHandler）
func WithFunctionRegistry(registry DataHandlerRegistry) Option {
	return func(o *options) {
		o.functionRegistry = registry
	}
}

// WithDebug 启用调试日志
func WithDebug(debug bool) Option {
	return func(o *options) {
		o.debug = debug
	}
}

// WithTrace 启用追踪日志
func WithTrace(trace bool) Option {
	return func(o *options) {
		o.trace = trace
	}
}

// WithDefaultReconnectEnabled 设置默认是否启用重连
func WithDefaultReconnectEnabled(enabled bool) Option {
	return func(o *options) {
		o.defaultReconnectEnabled = enabled
	}
}

// WithDefaultMaxReconnectAttempts 设置默认最大重连次数
func WithDefaultMaxReconnectAttempts(attempts int) Option {
	return func(o *options) {
		o.defaultMaxReconnectAttempts = attempts
	}
}

// WithDefaultReconnectBackoff 设置默认重连退避配置
func WithDefaultReconnectBackoff(config ReconnectBackoffConfig) Option {
	return func(o *options) {
		o.defaultReconnectBackoff = config
	}
}

// WithShutdownTimeout 设置关闭超时时间
func WithShutdownTimeout(timeout time.Duration) Option {
	return func(o *options) {
		if timeout > 0 {
			o.shutdownTimeout = timeout
		}
	}
}

// WithReconnectTimeout 设置重连超时时间
func WithReconnectTimeout(timeout time.Duration) Option {
	return func(o *options) {
		if timeout > 0 {
			o.reconnectTimeout = timeout
		}
	}
}

// WithBroadcast 启用/禁用多订阅者广播
func WithBroadcast(enabled bool) Option {
	return func(o *options) {
		o.broadcastEnabled = enabled
	}
}

// WithWalEnabled 启用/禁用 WAL
func WithWalEnabled(enabled bool) Option {
	return func(o *options) {
		o.walEnabled = enabled
	}
}

// WithWalStore 设置 WAL 存储（启用 WAL 时使用）
func WithWalStore(store WalStore) Option {
	return func(o *options) {
		o.walStore = store
	}
}

// WithWalPath 设置 WAL 存储路径（当未设置 WalStore 时，创建 Badger 使用）
func WithWalPath(path string) Option {
	return func(o *options) {
		o.walPath = path
	}
}

