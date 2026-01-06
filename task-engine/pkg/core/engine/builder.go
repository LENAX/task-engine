package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"reflect"

	"github.com/stevelan1995/task-engine/internal/storage"
	"github.com/stevelan1995/task-engine/internal/storage/sqlite"
	"github.com/stevelan1995/task-engine/pkg/config"
	"github.com/stevelan1995/task-engine/pkg/core/task"
	"github.com/stevelan1995/task-engine/pkg/plugin"
)

// JobFunc Job函数类型（兼容现有代码）
// 实际类型是 task.JobFunctionType，但为了简化API，这里使用interface{}
type JobFunc interface{}

// CallbackFunc Callback函数类型
type CallbackFunc interface{}

// EngineBuilder 引擎构建器（链式调用）
type EngineBuilder struct {
	engineConfigPath        string
	jobFuncs                map[string]JobFunc
	callbackFuncs           map[string]CallbackFunc
	services                map[string]interface{}
	functionMap             map[string]interface{}        // 函数映射表，用于函数恢复
	restoreFunctionsOnStart bool                        // 是否在启动时自动恢复函数
	plugins                 map[string]plugin.Plugin     // 已注册的插件
	pluginBindings          []plugin.PluginBinding       // 插件绑定规则
	err                     error
}

// NewEngineBuilder 创建引擎构建器（入口）
func NewEngineBuilder(engineConfigPath string) *EngineBuilder {
	return &EngineBuilder{
		engineConfigPath:        engineConfigPath,
		jobFuncs:                make(map[string]JobFunc),
		callbackFuncs:           make(map[string]CallbackFunc),
		services:                make(map[string]interface{}),
		functionMap:             make(map[string]interface{}),
		restoreFunctionsOnStart: false,
		plugins:                 make(map[string]plugin.Plugin),
		pluginBindings:          make([]plugin.PluginBinding, 0),
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

// WithPlugin 注册插件（链式）
func (b *EngineBuilder) WithPlugin(p plugin.Plugin) *EngineBuilder {
	if b.err != nil {
		return b
	}
	if p == nil {
		b.err = errors.New("plugin cannot be nil")
		return b
	}
	name := p.Name()
	if name == "" {
		b.err = errors.New("plugin name cannot be empty")
		return b
	}
	b.plugins[name] = p
	return b
}

// WithPluginBinding 绑定插件到事件（链式）
func (b *EngineBuilder) WithPluginBinding(binding plugin.PluginBinding) *EngineBuilder {
	if b.err != nil {
		return b
	}
	if binding.PluginName == "" {
		b.err = errors.New("plugin name cannot be empty")
		return b
	}
	if binding.Event == "" {
		b.err = errors.New("trigger event cannot be empty")
		return b
	}
	// 检查插件是否已注册
	if _, exists := b.plugins[binding.PluginName]; !exists {
		b.err = fmt.Errorf("plugin %s not registered, please register it first using WithPlugin", binding.PluginName)
		return b
	}
	b.pluginBindings = append(b.pluginBindings, binding)
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

	// 5. 创建Engine实例（优先使用聚合Repository，推荐方式）
	var engine *Engine
	if repos.WorkflowAggregate != nil {
		// 使用聚合Repository创建Engine（推荐方式）
		engine, err = NewEngineWithAggregateRepo(
			maxConcurrency,
			timeoutSeconds,
			repos.WorkflowAggregate,
		)
		if err != nil {
			return nil, fmt.Errorf("create engine with aggregate repo failed: %w", err)
		}
		// 如果提供了JobFunction和TaskHandler的Repository，用于FunctionRegistry
		if repos.JobFunction != nil && repos.TaskHandler != nil {
			// 创建带持久化的FunctionRegistry
			registry := task.NewFunctionRegistry(repos.JobFunction, repos.TaskHandler)
			engine.registry = registry
		}
	} else {
		// 使用旧的Repository接口创建Engine（兼容模式）
		engine, err = NewEngineWithRepos(
			maxConcurrency,
			timeoutSeconds,
			repos.Workflow,
			repos.WorkflowInstance,
			repos.Task,
			repos.JobFunction, // 启用JobFunction默认存储
			repos.TaskHandler, // 启用TaskHandler默认存储
		)
		if err != nil {
			return nil, fmt.Errorf("create engine failed: %w", err)
		}
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

	// 12. 注册插件并应用绑定规则
	if len(b.plugins) > 0 {
		for name, p := range b.plugins {
			if err := engine.pluginManager.Register(p); err != nil {
				return nil, fmt.Errorf("register plugin %s failed: %w", name, err)
			}
			log.Printf("📝 [EngineBuilder] 已注册插件: %s", name)
		}
	}
	if len(b.pluginBindings) > 0 {
		for _, binding := range b.pluginBindings {
			if err := engine.pluginManager.Bind(binding); err != nil {
				return nil, fmt.Errorf("bind plugin %s to event %s failed: %w", binding.PluginName, binding.Event, err)
			}
			log.Printf("📝 [EngineBuilder] 已绑定插件: %s -> %s", binding.PluginName, binding.Event)
		}
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
func (b *EngineBuilder) initStorage(cfg *config.EngineConfig) (*storage.Repositories, error) {
	dbType := cfg.GetDatabaseType()
	dsn := cfg.GetDatabaseDSN()

	// 创建数据库工厂
	factory, err := storage.NewDatabaseFactory(dbType, dsn)
	if err != nil {
		return nil, fmt.Errorf("create database factory failed: %w", err)
	}

	// 创建聚合Repository
	aggregateRepo, err := factory.CreateWorkflowAggregateRepo(dsn)
	if err != nil {
		return nil, fmt.Errorf("create aggregate repository failed: %w", err)
	}

	// 构建Repositories结构
	repos := &storage.Repositories{
		WorkflowAggregate: aggregateRepo,
	}

	// 对于SQLite，同时创建旧的Repository接口以兼容JobFunction和TaskHandler的持久化
	if dbType == "sqlite" {
		sqliteRepos, err := sqlite.NewRepositories(dsn)
		if err != nil {
			return nil, fmt.Errorf("create sqlite repositories failed: %w", err)
		}
		// 保留旧的Repository接口用于JobFunction和TaskHandler
		repos.Workflow = sqliteRepos.Workflow
		repos.WorkflowInstance = sqliteRepos.WorkflowInstance
		repos.Task = sqliteRepos.Task
		repos.JobFunction = sqliteRepos.JobFunction
		repos.TaskHandler = sqliteRepos.TaskHandler
	}
	// 对于MySQL和PostgreSQL，JobFunction和TaskHandler的持久化可以通过扩展聚合Repository实现
	// 目前先不创建，使用nil（FunctionRegistry会使用内存存储）

	return repos, nil
}
