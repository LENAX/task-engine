// Package realtime 提供实时数据采集任务的配置选项
package realtime

import "time"

// options 内部配置选项
type options struct {
	// 缓冲区配置
	bufferSize            int
	backpressureThreshold float64

	// 日志配置
	debug bool
	trace bool

	// 任务配置
	defaultReconnectEnabled     bool
	defaultMaxReconnectAttempts int
	defaultReconnectBackoff     ReconnectBackoffConfig

	// 超时配置
	shutdownTimeout time.Duration
	reconnectTimeout time.Duration
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

