package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"reflect"

	"github.com/stevelan1995/task-engine/internal/storage/sqlite"
	"github.com/stevelan1995/task-engine/pkg/config"
	"github.com/stevelan1995/task-engine/pkg/core/task"
)

// JobFunc Job函数类型（兼容现有代码）
// 实际类型是 task.JobFunctionType，但为了简化API，这里使用interface{}
type JobFunc interface{}

// CallbackFunc Callback函数类型
type CallbackFunc interface{}

// EngineBuilder 引擎构建器（链式调用）
type EngineBuilder struct {
	engineConfigPath      string
	jobFuncs              map[string]JobFunc
	callbackFuncs         map[string]CallbackFunc
	services              map[string]interface{}
	functionMap           map[string]interface{} // 函数映射表，用于函数恢复
	restoreFunctionsOnStart bool                 // 是否在启动时自动恢复函数
	err                   error
}

// NewEngineBuilder 创建引擎构建器（入口）
func NewEngineBuilder(engineConfigPath string) *EngineBuilder {
	return &EngineBuilder{
		engineConfigPath:       engineConfigPath,
		jobFuncs:               make(map[string]JobFunc),
		callbackFuncs:           make(map[string]CallbackFunc),
		services:               make(map[string]interface{}),
		functionMap:            make(map[string]interface{}),
		restoreFunctionsOnStart: false,
	}
}

// WithJobFunc 注册Job函数（链式）
func (b *EngineBuilder) WithJobFunc(funcKey string, fn JobFunc) *EngineBuilder {
	if b.err != nil {
		return b
	}
	if funcKey == "" || fn == nil {
		b.err = errors.New("job func key or function is empty")
		return b
	}
	b.jobFuncs[funcKey] = fn
	return b
}

// WithCallbackFunc 注册Callback函数（链式）
func (b *EngineBuilder) WithCallbackFunc(funcKey string, fn CallbackFunc) *EngineBuilder {
	if b.err != nil {
		return b
	}
	if funcKey == "" || fn == nil {
		b.err = errors.New("callback func key or function is empty")
		return b
	}
	b.callbackFuncs[funcKey] = fn
	return b
}

// WithService 注册服务依赖（替代WithDependency，语义更优）
func (b *EngineBuilder) WithService(serviceKey string, service interface{}) *EngineBuilder {
	if b.err != nil {
		return b
	}
	if serviceKey == "" || service == nil {
		b.err = errors.New("service key or instance is empty")
		return b
	}
	b.services[serviceKey] = service
	return b
}

// WithFunctionMap 设置函数映射表，用于函数恢复（链式）
// funcMap: 函数名称 -> 函数实例的映射
// 注意：函数名称必须与注册时使用的名称一致
func (b *EngineBuilder) WithFunctionMap(funcMap map[string]interface{}) *EngineBuilder {
	if b.err != nil {
		return b
	}
	if funcMap == nil {
		b.functionMap = make(map[string]interface{})
	} else {
		// 创建副本，避免外部修改
		b.functionMap = make(map[string]interface{})
		for k, v := range funcMap {
			b.functionMap[k] = v
		}
	}
	return b
}

// RestoreFunctionsOnStart 设置在启动时自动恢复函数（链式）
// 如果设置了此选项，Engine.Start() 时会自动从数据库恢复函数
func (b *EngineBuilder) RestoreFunctionsOnStart() *EngineBuilder {
	if b.err != nil {
		return b
	}
	b.restoreFunctionsOnStart = true
	return b
}

// Build 构建引擎实例（最终步骤）
func (b *EngineBuilder) Build() (*Engine, error) {
	// 检查构建过程是否有错误
	if b.err != nil {
		return nil, b.err
	}

	// 1. 加载引擎配置
	cfg, err := config.LoadFrameworkConfig(b.engineConfigPath)
	if err != nil {
		return nil, fmt.Errorf("load engine config failed: %w", err)
	}

	// 2. 校验配置
	if err := config.ValidateFrameworkConfig(cfg); err != nil {
		return nil, fmt.Errorf("validate engine config failed: %w", err)
	}

	// 3. 初始化存储层（根据配置创建Repository）
	repos, err := b.initStorage(cfg)
	if err != nil {
		return nil, fmt.Errorf("init storage failed: %w", err)
	}

	// 4. 获取配置参数
	maxConcurrency := cfg.GetWorkerConcurrency()
	timeoutSeconds := int(cfg.GetDefaultTaskTimeout().Seconds())

	// 5. 创建Engine实例（使用配置的并发数和超时，Engine内部会创建Executor）
	// 传入JobFunction和TaskHandler的Repository以启用默认存储
	engine, err := NewEngineWithRepos(
		maxConcurrency,
		timeoutSeconds,
		repos.Workflow,
		repos.WorkflowInstance,
		repos.Task,
		repos.JobFunction,  // 启用JobFunction默认存储
		repos.TaskHandler,  // 启用TaskHandler默认存储
	)
	if err != nil {
		return nil, fmt.Errorf("create engine failed: %w", err)
	}

	// 6. 保存配置到Engine
	engine.cfg = cfg

	// 7. 注册Job函数到FunctionRegistry
	ctx := context.Background()
	for funcKey, fn := range b.jobFuncs {
		_, err := engine.registry.Register(ctx, funcKey, fn, fmt.Sprintf("Job function: %s", funcKey))
		if err != nil {
			return nil, fmt.Errorf("register job func %s failed: %w", funcKey, err)
		}
	}

	// 8. 注册Callback函数到FunctionRegistry（作为TaskHandler）
	for funcKey, fn := range b.callbackFuncs {
		// 将Callback函数包装为TaskHandlerType
		// 使用统一的包装函数，它会自动处理不同的函数签名
		handler := wrapCallbackToTaskHandler(fn)

		_, err := engine.registry.RegisterTaskHandler(ctx, funcKey, handler, fmt.Sprintf("Callback function: %s", funcKey))
		if err != nil {
			return nil, fmt.Errorf("register callback func %s failed: %w", funcKey, err)
		}
	}

	// 9. 注册服务依赖到FunctionRegistry（支持字符串key和类型两种方式）
	for serviceKey, service := range b.services {
		// 使用字符串key注册，支持通过 ctx.GetDependency("ExampleService") 方式获取
		if err := engine.registry.RegisterDependencyWithKey(serviceKey, service); err != nil {
			// 依赖已存在时忽略错误（允许重复注册）
			log.Printf("注册服务依赖 %s 失败（可能已存在）: %v", serviceKey, err)
		}
	}

	// 10. 如果提供了functionMap，保存到Engine中，供Start()时恢复使用
	if len(b.functionMap) > 0 {
		engine.SetFunctionMap(b.functionMap)
		log.Printf("📝 [EngineBuilder] 已设置函数映射表，包含 %d 个函数", len(b.functionMap))
	}

	// 11. 如果设置了自动恢复选项，启用Engine的自动恢复功能
	if b.restoreFunctionsOnStart {
		engine.EnableFunctionRestoreOnStart()
		log.Printf("📝 [EngineBuilder] 已启用启动时自动恢复函数功能")
	}

	return engine, nil
}

// wrapCallbackToTaskHandler 将Callback函数包装为TaskHandlerType
// 支持多种函数签名：
//  1. func(*TaskContext) - 直接匹配TaskHandlerType
//  2. func(context.Context) error - 需要包装
//  3. func(context.Context) - 需要包装
func wrapCallbackToTaskHandler(fn interface{}) task.TaskHandlerType {
	fnValue := reflect.ValueOf(fn)
	fnType := fnValue.Type()

	// 检查是否为函数类型
	if fnType.Kind() != reflect.Func {
		return func(ctx *task.TaskContext) {
			log.Printf("警告: Callback不是函数类型，无法调用")
		}
	}

	// 检查参数数量
	if fnType.NumIn() == 0 {
		return func(ctx *task.TaskContext) {
			log.Printf("警告: Callback函数没有参数，无法调用")
		}
	}

	firstParamType := fnType.In(0)
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	taskContextType := reflect.TypeOf((*task.TaskContext)(nil))

	// 如果第一个参数是*TaskContext，使用反射调用原函数
	if firstParamType == taskContextType {
		return func(ctx *task.TaskContext) {
			// 使用反射调用原函数，传入*TaskContext
			args := []reflect.Value{reflect.ValueOf(ctx)}
			fnValue.Call(args)
		}
	}

	// 如果第一个参数是context.Context，需要包装
	if firstParamType.Implements(contextType) || firstParamType == contextType {
		return func(ctx *task.TaskContext) {
			// 调用原函数，传入context.Context
			args := []reflect.Value{reflect.ValueOf(ctx.Context())}
			fnValue.Call(args)
		}
	}

	// 其他情况，返回空handler
	return func(ctx *task.TaskContext) {
		log.Printf("警告: Callback函数签名不匹配，无法调用。期望: func(context.Context) error 或 func(*TaskContext)，实际: %v", fnType)
	}
}

// initStorage 初始化存储层（根据配置创建Repository）
func (b *EngineBuilder) initStorage(cfg *config.EngineConfig) (*sqlite.Repositories, error) {
	dbType := cfg.GetDatabaseType()
	dsn := cfg.GetDatabaseDSN()

	switch dbType {
	case "sqlite":
		repos, err := sqlite.NewRepositories(dsn)
		if err != nil {
			return nil, fmt.Errorf("create sqlite repositories failed: %w", err)
		}
		return repos, nil
	case "postgres", "postgresql":
		// TODO: 实现PostgreSQL支持
		return nil, fmt.Errorf("postgresql not implemented yet")
	case "mysql":
		// TODO: 实现MySQL支持
		return nil, fmt.Errorf("mysql not implemented yet")
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}
}
