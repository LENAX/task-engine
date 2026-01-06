package task

import (
	"context"

	"github.com/stevelan1995/task-engine/pkg/storage"
)

// FunctionRegistry 函数注册中心接口（对外导出）
type FunctionRegistry interface {
	// Register 注册Job函数
	Register(ctx context.Context, name string, fn interface{}, description string) (string, error)
	// Get 根据函数ID获取包装后的函数
	Get(funcID string) JobFunctionType
	// GetByName 根据函数名获取包装后的函数
	GetByName(name string) JobFunctionType
	// GetIDByName 根据函数名称获取函数ID
	GetIDByName(name string) string
	// GetMeta 根据函数ID获取元数据
	GetMeta(funcID string) *storage.JobFunctionMeta
	// Exists 检查函数是否已注册
	Exists(funcID string) bool
	// Unregister 注销函数
	Unregister(ctx context.Context, funcID string) error
	// ListAll 列出所有已注册的函数ID
	ListAll() []string
	// RegisterTaskHandler 注册Task Handler
	RegisterTaskHandler(ctx context.Context, name string, handler TaskHandlerType, description string) (string, error)
	// GetTaskHandler 根据Handler ID获取Task Handler
	GetTaskHandler(handlerID string) TaskHandlerType
	// GetTaskHandlerByName 根据Handler名称获取Task Handler
	GetTaskHandlerByName(name string) TaskHandlerType
	// GetTaskHandlerIDByName 根据Handler名称获取Handler ID
	GetTaskHandlerIDByName(name string) string
	// UnregisterTaskHandler 注销Task Handler
	UnregisterTaskHandler(ctx context.Context, handlerID string) error
	// ListAllTaskHandlers 列出所有已注册的Handler ID
	ListAllTaskHandlers() []string
	// TaskHandlerExists 检查Handler是否已注册
	TaskHandlerExists(handlerID string) bool
	// RegisterDependency 注册依赖（通过类型）
	RegisterDependency(dep interface{}) error
	// RegisterDependencyWithKey 注册依赖（通过字符串key）
	RegisterDependencyWithKey(key string, dep interface{}) error
	// WithDependencies 将依赖注入到context中
	WithDependencies(ctx context.Context) context.Context
	// RestoreFunctions 从函数映射表恢复函数
	RestoreFunctions(ctx context.Context, funcMap map[string]interface{}) error
}
