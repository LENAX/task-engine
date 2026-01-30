package engine

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/LENAX/task-engine/pkg/config"
	"github.com/LENAX/task-engine/pkg/core/builder"
	"github.com/LENAX/task-engine/pkg/core/executor"
	"github.com/LENAX/task-engine/pkg/core/realtime"
	"github.com/LENAX/task-engine/pkg/core/task"
	"github.com/LENAX/task-engine/pkg/core/types"
	"github.com/LENAX/task-engine/pkg/core/workflow"
	"github.com/LENAX/task-engine/pkg/plugin"
	"github.com/LENAX/task-engine/pkg/storage"
)

// WorkflowDefinition 工作流定义（封装配置文件加载结果）
type WorkflowDefinition struct {
	ID         string                 // 工作流ID
	Config     *config.WorkflowConfig // 工作流配置
	SourcePath string                 // 配置文件路径
	Workflow   *workflow.Workflow     // 转换后的Workflow对象
}

// InstanceManagerVersion 定义InstanceManager版本类型
type InstanceManagerVersion int

const (
	// InstanceManagerV1 使用V1版本的InstanceManager
	InstanceManagerV1 InstanceManagerVersion = 1
	// InstanceManagerV2 使用V2版本的InstanceManager（默认，支持SAGA事务）
	InstanceManagerV2 InstanceManagerVersion = 2
)

// Engine 调度引擎核心结构体（对外导出）
type Engine struct {
	executor                executor.Executor
	workflowRepo            storage.WorkflowRepository
	workflowInstanceRepo    storage.WorkflowInstanceRepository
	taskRepo                storage.TaskRepository
	aggregateRepo           storage.WorkflowAggregateRepository // 聚合Repository（优先使用）
	registry                task.FunctionRegistry
	cfg                     *config.EngineConfig   // 框架配置
	jobRegistry             sync.Map               // Job函数注册表（funcKey -> function）
	callbackRegistry        sync.Map               // Callback函数注册表（funcKey -> function）
	serviceRegistry         sync.Map               // 服务依赖注册表（serviceKey -> service）
	functionMap             map[string]interface{} // 函数映射表，用于函数恢复
	restoreFunctionsOnStart bool                   // 是否在启动时自动恢复函数
	running                 bool
	MaxConcurrency          int
	Timeout                 int
	managers                map[string]types.WorkflowInstanceManager // WorkflowInstance ID -> Manager映射
	controllers             map[string]workflow.WorkflowController   // WorkflowInstance ID -> Controller映射
	cronScheduler           *CronScheduler                           // 定时调度器
	pluginManager           plugin.PluginManager                     // 插件管理器（接口）
	mu                      sync.RWMutex
	instanceManagerVersion  InstanceManagerVersion // InstanceManager版本，默认V2
}

// NewEngine 创建Engine实例（对外导出的工厂方法）
func NewEngine(
	maxConcurrency, timeout int,
	workflowRepo storage.WorkflowRepository,
	workflowInstanceRepo storage.WorkflowInstanceRepository,
	taskRepo storage.TaskRepository,
) (*Engine, error) {
	return NewEngineWithRepos(
		maxConcurrency, timeout,
		workflowRepo,
		workflowInstanceRepo,
		taskRepo,
		nil, // JobFunctionRepository (可选)
		nil, // TaskHandlerRepository (可选)
	)
}

// NewEngineWithRepos 创建Engine实例（带完整Repository支持，对外导出）
func NewEngineWithRepos(
	maxConcurrency, timeout int,
	workflowRepo storage.WorkflowRepository,
	workflowInstanceRepo storage.WorkflowInstanceRepository,
	taskRepo storage.TaskRepository,
	jobFunctionRepo storage.JobFunctionRepository,
	taskHandlerRepo storage.TaskHandlerRepository,
) (*Engine, error) {
	exec, err := executor.NewExecutor(maxConcurrency)
	if err != nil {
		return nil, err
	}

	// 创建FunctionRegistry，传入JobFunction和TaskHandler的Repository以启用默认存储
	registry := task.NewFunctionRegistry(jobFunctionRepo, taskHandlerRepo)

	eng := &Engine{
		executor:                exec,
		workflowRepo:            workflowRepo,
		workflowInstanceRepo:    workflowInstanceRepo,
		taskRepo:                taskRepo,
		registry:                registry,
		functionMap:             make(map[string]interface{}),
		restoreFunctionsOnStart: false,
		MaxConcurrency:          maxConcurrency,
		Timeout:                 timeout,
		running:                 false,
		managers:                make(map[string]types.WorkflowInstanceManager),
		controllers:             make(map[string]workflow.WorkflowController),
		cronScheduler:           NewCronScheduler(nil), // 稍后设置engine引用
		instanceManagerVersion:  InstanceManagerV2,     // 默认使用V2版本
	}
	// 设置CronScheduler的engine引用
	eng.cronScheduler.engine = eng
	return eng, nil
}

// NewEngineWithAggregateRepo 创建Engine实例（使用聚合Repository，推荐方式，对外导出）
// aggregateRepo: 聚合Repository，统一管理Workflow、WorkflowInstance、TaskInstance的事务操作
// 使用聚合Repository时，所有持久化操作都通过它进行，保证数据一致性和事务完整性
func NewEngineWithAggregateRepo(
	maxConcurrency, timeout int,
	aggregateRepo storage.WorkflowAggregateRepository,
) (*Engine, error) {
	exec, err := executor.NewExecutor(maxConcurrency)
	if err != nil {
		return nil, err
	}

	// 创建FunctionRegistry（不使用默认存储，函数通过aggregateRepo管理）
	registry := task.NewFunctionRegistry(nil, nil)

	eng := &Engine{
		executor:                exec,
		aggregateRepo:           aggregateRepo,
		registry:                registry,
		functionMap:             make(map[string]interface{}),
		restoreFunctionsOnStart: false,
		MaxConcurrency:          maxConcurrency,
		Timeout:                 timeout,
		running:                 false,
		managers:                make(map[string]types.WorkflowInstanceManager),
		controllers:             make(map[string]workflow.WorkflowController),
		cronScheduler:           NewCronScheduler(nil),
		pluginManager:           plugin.NewPluginManager(), // 初始化插件管理器
		instanceManagerVersion:  InstanceManagerV2,
	}
	eng.cronScheduler.engine = eng
	return eng, nil
}

// SetAggregateRepo 设置聚合Repository（对外导出）
// 可以在Engine创建后动态设置聚合Repository
func (e *Engine) SetAggregateRepo(repo storage.WorkflowAggregateRepository) {
	e.aggregateRepo = repo
}

// GetAggregateRepo 获取聚合Repository（对外导出）
func (e *Engine) GetAggregateRepo() storage.WorkflowAggregateRepository {
	return e.aggregateRepo
}

// SetInstanceManagerVersion 设置使用的InstanceManager版本（对外导出）
// 必须在Start()之前调用
func (e *Engine) SetInstanceManagerVersion(version InstanceManagerVersion) {
	e.instanceManagerVersion = version
	log.Printf("InstanceManager版本已设置为: V%d", version)
}

// GetInstanceManagerVersion 获取当前使用的InstanceManager版本（对外导出）
func (e *Engine) GetInstanceManagerVersion() InstanceManagerVersion {
	return e.instanceManagerVersion
}

// Start 启动引擎（对外导出）
// 根据文档要求，启动时需要恢复未完成的WorkflowInstance
func (e *Engine) Start(ctx context.Context) error {
	if e.running {
		return nil
	}

	// 启动Executor
	e.executor.Start()

	// 设置registry到Executor
	e.executor.SetRegistry(e.registry)

	e.running = true
	log.Println("✅ 异步任务调度引擎已启动")

	// 如果配置了自动恢复函数，执行函数恢复
	if e.restoreFunctionsOnStart && len(e.functionMap) > 0 {
		if err := e.restoreFunctions(ctx); err != nil {
			log.Printf("恢复函数失败: %v", err)
			// 不阻止启动，仅记录日志
		}
	}

	// 恢复未完成的WorkflowInstance（文档1.2节要求）
	if err := e.restoreUnfinishedInstances(ctx); err != nil {
		log.Printf("恢复未完成实例失败: %v", err)
		// 不阻止启动，仅记录日志
	}

	// 启动定时调度器
	if e.cronScheduler != nil {
		e.cronScheduler.Start()
	}

	return nil
}

// restoreFunctions 恢复函数（内部方法）
func (e *Engine) restoreFunctions(ctx context.Context) error {
	if e.registry == nil {
		return fmt.Errorf("函数注册中心未配置")
	}

	if len(e.functionMap) == 0 {
		log.Println("⚠️ [函数恢复] 函数映射表为空，跳过恢复")
		return nil
	}

	// 使用FunctionRestorer辅助类
	restorer := task.NewFunctionRestorer(e.registry, e.functionMap)
	if err := restorer.Restore(ctx); err != nil {
		return fmt.Errorf("函数恢复失败: %w", err)
	}

	return nil
}

// SetFunctionMap 设置函数映射表（对外导出）
// 用于在Engine创建后设置函数映射表
func (e *Engine) SetFunctionMap(funcMap map[string]interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if funcMap == nil {
		e.functionMap = make(map[string]interface{})
	} else {
		// 创建副本
		e.functionMap = make(map[string]interface{})
		for k, v := range funcMap {
			e.functionMap[k] = v
		}
	}
}

// EnableFunctionRestoreOnStart 启用启动时自动恢复函数（对外导出）
func (e *Engine) EnableFunctionRestoreOnStart() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.restoreFunctionsOnStart = true
}

// GetRegistry 获取函数注册中心（对外导出，用于测试和函数注册）
func (e *Engine) GetRegistry() task.FunctionRegistry {
	return e.registry
}

// GetPluginManager 获取插件管理器（对外导出）
func (e *Engine) GetPluginManager() plugin.PluginManager {
	return e.pluginManager
}

// AddSubTaskToInstance 向指定的WorkflowInstance添加子任务（对外导出）
// instanceID: WorkflowInstance ID
// subTask: 动态生成的子Task
// parentTaskID: 父Task ID
func (e *Engine) AddSubTaskToInstance(ctx context.Context, instanceID string, subTask workflow.Task, parentTaskID string) error {
	if !e.running {
		return fmt.Errorf("引擎未启动")
	}

	e.mu.RLock()
	manager, exists := e.managers[instanceID]
	e.mu.RUnlock()

	if !exists {
		return fmt.Errorf("WorkflowInstance %s 不存在", instanceID)
	}

	return manager.AddSubTask(subTask, parentTaskID)
}

// GetInstanceManager 获取指定WorkflowInstance的Manager（对外导出）
// 用于Handler中需要访问Manager的场景
func (e *Engine) GetInstanceManager(instanceID string) (interface{}, error) {
	if !e.running {
		return nil, fmt.Errorf("引擎未启动")
	}

	e.mu.RLock()
	manager, exists := e.managers[instanceID]
	e.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("WorkflowInstance %s 不存在", instanceID)
	}

	// 返回一个接口，只暴露AddSubTask方法，避免暴露内部结构
	return &InstanceManagerInterface{
		manager: manager,
	}, nil
}

// InstanceManagerInterface 暴露给Handler的Manager接口，避免循环引用
type InstanceManagerInterface struct {
	manager types.WorkflowInstanceManager
}

// AddSubTask 添加子任务（对外导出）
func (i *InstanceManagerInterface) AddSubTask(subTask workflow.Task, parentTaskID string) error {
	return i.manager.AddSubTask(subTask, parentTaskID)
}

// GetInstanceProgress 获取指定运行中实例的内存进度（对外导出）
// 若实例正在运行则返回 (ProgressSnapshot, true)，否则返回 (零值, false)，供 API 优先使用内存进度
func (e *Engine) GetInstanceProgress(instanceID string) (types.ProgressSnapshot, bool) {
	if !e.running {
		return types.ProgressSnapshot{}, false
	}
	e.mu.RLock()
	manager, exists := e.managers[instanceID]
	e.mu.RUnlock()
	if !exists {
		return types.ProgressSnapshot{}, false
	}
	return manager.GetProgress(), true
}

// restoreUnfinishedInstances 恢复未完成的WorkflowInstance（内部方法）
// 从数据库加载所有状态为Running或Paused的WorkflowInstance，并恢复执行
func (e *Engine) restoreUnfinishedInstances(ctx context.Context) error {
	var runningInstances, pausedInstances []*workflow.WorkflowInstance

	// 优先使用聚合Repository（需要先获取所有Workflow，再获取各Workflow的Instance）
	if e.aggregateRepo != nil {
		workflows, err := e.aggregateRepo.ListWorkflows(ctx)
		if err != nil {
			return fmt.Errorf("查询Workflow列表失败: %w", err)
		}
		for _, wf := range workflows {
			instances, err := e.aggregateRepo.ListWorkflowInstances(ctx, wf.ID)
			if err != nil {
				log.Printf("查询Workflow %s 的Instance失败: %v", wf.ID, err)
				continue
			}
			for _, inst := range instances {
				if inst.Status == "Running" {
					runningInstances = append(runningInstances, inst)
				} else if inst.Status == "Paused" {
					pausedInstances = append(pausedInstances, inst)
				}
			}
		}
	} else if e.workflowInstanceRepo != nil {
		var err error
		// 查询所有Running状态的实例
		runningInstances, err = e.workflowInstanceRepo.ListByStatus(ctx, "Running")
		if err != nil {
			return fmt.Errorf("查询Running状态的WorkflowInstance失败: %w", err)
		}

		// 查询所有Paused状态的实例
		pausedInstances, err = e.workflowInstanceRepo.ListByStatus(ctx, "Paused")
		if err != nil {
			return fmt.Errorf("查询Paused状态的WorkflowInstance失败: %w", err)
		}
	} else {
		return fmt.Errorf("未配置Repository")
	}

	// 恢复Running状态的实例
	for _, instance := range runningInstances {
		if err := e.restoreInstance(ctx, instance, true); err != nil {
			log.Printf("恢复Running状态的WorkflowInstance %s 失败: %v", instance.ID, err)
			continue
		}
		log.Printf("✅ 恢复Running状态的WorkflowInstance: %s", instance.ID)
	}

	// 恢复Paused状态的实例（但不自动启动，等待用户手动恢复）
	for _, instance := range pausedInstances {
		if err := e.restoreInstance(ctx, instance, false); err != nil {
			log.Printf("恢复Paused状态的WorkflowInstance %s 失败: %v", instance.ID, err)
			continue
		}
		log.Printf("✅ 恢复Paused状态的WorkflowInstance: %s（等待手动恢复）", instance.ID)
	}

	return nil
}

// restoreInstance 恢复单个WorkflowInstance（内部方法）
// autoStart: 是否自动启动（Running状态为true，Paused状态为false）
func (e *Engine) restoreInstance(ctx context.Context, instance *workflow.WorkflowInstance, autoStart bool) error {
	// 从数据库加载Workflow模板
	var wf *workflow.Workflow
	var err error

	// 优先使用聚合Repository
	if e.aggregateRepo != nil {
		wf, err = e.aggregateRepo.GetWorkflowWithTasks(ctx, instance.WorkflowID)
	} else if e.workflowRepo != nil {
		wf, err = e.workflowRepo.GetByID(ctx, instance.WorkflowID)
	} else {
		return fmt.Errorf("未配置Repository")
	}
	if err != nil {
		return fmt.Errorf("加载Workflow模板失败: %w", err)
	}
	if wf == nil {
		return fmt.Errorf("Workflow模板不存在: %s", instance.WorkflowID)
	}

	// 根据配置创建对应版本的WorkflowInstanceManager（会自动从断点数据恢复）
	var manager types.WorkflowInstanceManager
	if e.instanceManagerVersion == InstanceManagerV2 {
		manager, err = NewWorkflowInstanceManagerV2WithAggregate(
			instance,
			wf,
			e.executor,
			e.aggregateRepo,
			e.taskRepo,
			e.workflowInstanceRepo,
			e.registry,
			e.pluginManager,
		)
	} else {
		manager, err = NewWorkflowInstanceManager(
			instance,
			wf,
			e.executor,
			e.taskRepo,
			e.workflowInstanceRepo,
			e.registry,
		)
	}
	if err != nil {
		return fmt.Errorf("创建WorkflowInstanceManager失败: %w", err)
	}

	// 如果有断点数据，恢复断点
	if instance.Breakpoint != nil {
		if err := manager.RestoreFromBreakpoint(instance.Breakpoint); err != nil {
			return fmt.Errorf("恢复断点数据失败: %w", err)
		}
	}

	// 创建WorkflowController
	controller := workflow.NewWorkflowControllerWithCallbacks(
		instance.ID,
		func() error {
			return e.PauseWorkflowInstance(ctx, instance.ID)
		},
		func() error {
			return e.ResumeWorkflowInstance(ctx, instance.ID)
		},
		func() error {
			return e.TerminateWorkflowInstance(ctx, instance.ID, "用户终止")
		},
		func() (string, error) {
			return e.GetWorkflowInstanceStatus(ctx, instance.ID)
		},
	)

	// 保存到内存映射
	e.mu.Lock()
	e.managers[instance.ID] = manager
	e.controllers[instance.ID] = controller
	e.mu.Unlock()

	// 启动状态更新转发协程：将Manager的状态更新转发到Controller
	go e.forwardStatusUpdates(instance.ID, manager, controller)

	// 如果autoStart为true，自动启动执行
	if autoStart {
		manager.Start()
	}

	return nil
}

// Stop 停止引擎（对外导出）
// 根据文档5.2节要求，实现优雅退出流程
func (e *Engine) Stop() {
	if !e.running {
		return
	}

	// 1. 设置关闭标志
	e.running = false

	// 2. 停止定时调度器
	if e.cronScheduler != nil {
		e.cronScheduler.Stop()
	}

	// 3. 通知所有WorkflowInstance准备关闭（发送Terminate信号）
	e.mu.RLock()
	instances := make([]types.WorkflowInstanceManager, 0, len(e.managers))
	for _, manager := range e.managers {
		instances = append(instances, manager)
	}
	e.mu.RUnlock()

	// 发送Terminate信号到所有实例
	for _, manager := range instances {
		// 类型转换：从 interface{} 转换为双向通道或只写通道
		controlChanRaw := manager.GetControlSignalChannel()
		if controlChanRaw == nil {
			log.Printf("WorkflowInstance %s 控制信号通道为nil", manager.GetInstanceID())
			continue
		}
		// 尝试转换为双向通道
		if controlChan, ok := controlChanRaw.(chan workflow.ControlSignal); ok {
			select {
			case controlChan <- workflow.SignalTerminate:
			default:
				log.Printf("WorkflowInstance %s 控制信号channel已满，跳过", manager.GetInstanceID())
			}
		} else if controlChanWrite, ok := controlChanRaw.(chan<- workflow.ControlSignal); ok {
			// 尝试转换为只写通道（兼容旧版本）
			select {
			case controlChanWrite <- workflow.SignalTerminate:
			default:
				log.Printf("WorkflowInstance %s 控制信号channel已满，跳过", manager.GetInstanceID())
			}
		} else {
			log.Printf("WorkflowInstance %s 控制信号通道类型转换失败", manager.GetInstanceID())
		}
	}

	// 3. 等待所有WorkflowInstance完成终止流程（最多等待30秒）
	// 使用WaitGroup等待所有Manager的协程完成
	done := make(chan struct{})
	go func() {
		for _, manager := range instances {
			manager.Shutdown()
		}
		close(done)
	}()

	select {
	case <-done:
		// 所有Manager已完成关闭
		log.Println("所有WorkflowInstance已关闭")
	case <-time.After(30 * time.Second):
		// 超时，记录日志
		log.Println("等待WorkflowInstance关闭超时")
	}

	// 4. 保存所有Running状态的WorkflowInstance的断点数据
	ctx := context.Background()
	for _, manager := range instances {
		status := manager.GetStatus()

		if status == "Running" {
			// 记录断点数据
			breakpointValue := manager.CreateBreakpoint()
			// 类型转换：从 interface{} 转换为 *workflow.BreakpointData
			breakpoint, ok := breakpointValue.(*workflow.BreakpointData)
			if !ok {
				log.Printf("WorkflowInstance %s 断点数据类型转换失败", manager.GetInstanceID())
				continue
			}
			instanceID := manager.GetInstanceID()
			// 保存断点和状态（使用可用的Repository）
			if e.workflowInstanceRepo != nil {
				if err := e.workflowInstanceRepo.UpdateBreakpoint(ctx, instanceID, breakpoint); err != nil {
					log.Printf("保存WorkflowInstance %s 断点数据失败: %v", instanceID, err)
				}
				// 更新状态为Paused（而不是Terminated，便于恢复）
				if err := e.workflowInstanceRepo.UpdateStatus(ctx, instanceID, "Paused"); err != nil {
					log.Printf("更新WorkflowInstance %s 状态失败: %v", instanceID, err)
				}
			}
		}
	}

	// 5. 关闭Executor
	e.executor.Shutdown()

	// 6. 清理内存映射
	e.mu.Lock()
	e.managers = make(map[string]types.WorkflowInstanceManager)
	e.controllers = make(map[string]workflow.WorkflowController)
	e.mu.Unlock()

	log.Println("✅ 异步任务任务调度引擎已停止")
}

// LoadWorkflow 加载工作流定义（从文件）
func (e *Engine) LoadWorkflow(workflowConfigPath string) (*WorkflowDefinition, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if !e.running {
		return nil, fmt.Errorf("engine not started, call Start() first")
	}

	// 1. 加载业务配置
	wfConfig, err := config.LoadWorkflowConfig(workflowConfigPath)
	if err != nil {
		return nil, fmt.Errorf("load workflow config failed: %w", err)
	}

	// 2. 应用默认值（基于框架配置）
	defaultTimeout := 30 * time.Second // 默认超时时间
	if e.cfg != nil {
		defaultTimeout = e.cfg.GetDefaultTaskTimeout()
	}
	wfConfig.ApplyDefaults(defaultTimeout)

	// 3. 校验配置合法性
	// 构建jobRegistry map用于校验（从FunctionRegistry中获取）
	jobRegistryMap := make(map[string]interface{})
	// 通过GetIDByName和GetByName方法获取已注册的函数
	// 遍历配置中的func_key，检查是否已注册
	for _, jobDef := range wfConfig.Workflows.Jobs {
		funcID := e.registry.GetIDByName(jobDef.FuncKey)
		if funcID != "" {
			// 函数已注册，添加到map中用于校验
			if fn := e.registry.GetByName(jobDef.FuncKey); fn != nil {
				jobRegistryMap[jobDef.FuncKey] = fn
			}
		}
	}

	if err := config.ValidateWorkflowConfig(wfConfig, jobRegistryMap, defaultTimeout); err != nil {
		return nil, fmt.Errorf("validate workflow config failed: %w", err)
	}

	// 4. 转换为Workflow对象（使用第一个Workflow定义）
	if len(wfConfig.Workflows.Definitions) == 0 {
		return nil, fmt.Errorf("workflow config contains no workflow definitions")
	}

	wfDef := wfConfig.Workflows.Definitions[0]
	wf, err := e.convertWorkflowConfigToWorkflow(wfConfig, &wfDef)
	if err != nil {
		return nil, fmt.Errorf("convert workflow config to workflow failed: %w", err)
	}

	return &WorkflowDefinition{
		ID:         wfDef.WorkflowID,
		Config:     wfConfig,
		SourcePath: workflowConfigPath,
		Workflow:   wf,
	}, nil
}

// LoadWorkflowFromYAML 从YAML字符串加载工作流定义
func (e *Engine) LoadWorkflowFromYAML(content string) (*WorkflowDefinition, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if !e.running {
		return nil, fmt.Errorf("engine not started, call Start() first")
	}

	// 1. 解析YAML内容
	wfConfig, err := config.ParseWorkflowConfigFromYAML(content)
	if err != nil {
		return nil, fmt.Errorf("parse workflow yaml failed: %w", err)
	}

	// 2. 应用默认值（基于框架配置）
	defaultTimeout := 30 * time.Second
	if e.cfg != nil {
		defaultTimeout = e.cfg.GetDefaultTaskTimeout()
	}
	wfConfig.ApplyDefaults(defaultTimeout)

	// 3. 校验配置合法性
	jobRegistryMap := make(map[string]interface{})
	for _, jobDef := range wfConfig.Workflows.Jobs {
		funcID := e.registry.GetIDByName(jobDef.FuncKey)
		if funcID != "" {
			if fn := e.registry.GetByName(jobDef.FuncKey); fn != nil {
				jobRegistryMap[jobDef.FuncKey] = fn
			}
		}
	}

	if err := config.ValidateWorkflowConfig(wfConfig, jobRegistryMap, defaultTimeout); err != nil {
		return nil, fmt.Errorf("validate workflow config failed: %w", err)
	}

	// 4. 转换为Workflow对象
	if len(wfConfig.Workflows.Definitions) == 0 {
		return nil, fmt.Errorf("workflow config contains no workflow definitions")
	}

	wfDef := wfConfig.Workflows.Definitions[0]
	wf, err := e.convertWorkflowConfigToWorkflow(wfConfig, &wfDef)
	if err != nil {
		return nil, fmt.Errorf("convert workflow config to workflow failed: %w", err)
	}

	return &WorkflowDefinition{
		ID:         wfDef.WorkflowID,
		Config:     wfConfig,
		SourcePath: "", // 从YAML字符串加载，没有文件路径
		Workflow:   wf,
	}, nil
}

// convertWorkflowConfigToWorkflow 将WorkflowConfig转换为Workflow对象
func (e *Engine) convertWorkflowConfigToWorkflow(wfConfig *config.WorkflowConfig, wfDef *config.WorkflowDefinition) (*workflow.Workflow, error) {
	// 使用WorkflowBuilder构建Workflow
	wfBuilder := builder.NewWorkflowBuilder(wfDef.WorkflowID, wfDef.Description)

	// 转换Tasks
	for _, taskDef := range wfDef.Tasks {
		// 获取Job定义
		jobDef := wfConfig.GetJobByID(taskDef.JobID)
		if jobDef == nil {
			return nil, fmt.Errorf("job %s not found", taskDef.JobID)
		}

		// 创建TaskBuilder
		taskBuilder := builder.NewTaskBuilder(taskDef.TaskID, "", e.registry)
		taskBuilder = taskBuilder.WithJobFunction(jobDef.FuncKey, convertParamsToInterface(taskDef.Params))

		// 设置超时时间
		if jobDef.Timeout > 0 {
			taskBuilder = taskBuilder.WithTimeout(int(jobDef.Timeout.Seconds()))
		}

		// 设置依赖
		if len(taskDef.Dependencies) > 0 {
			taskBuilder = taskBuilder.WithDependencies(taskDef.Dependencies)
		}

		// 设置RequiredParams
		if len(taskDef.RequiredParams) > 0 {
			taskBuilder = taskBuilder.WithRequiredParams(taskDef.RequiredParams)
		}

		// 设置ResultMapping
		if len(taskDef.ResultMapping) > 0 {
			taskBuilder = taskBuilder.WithResultMapping(taskDef.ResultMapping)
		}

		// 设置Callbacks（转换为TaskHandler）
		for _, callback := range taskDef.Callbacks {
			// 将callback state映射到task status
			status := mapCallbackStateToTaskStatus(callback.State)
			if status != "" {
				taskBuilder = taskBuilder.WithTaskHandler(status, callback.FuncKey)
			}
		}

		// 构建Task
		t, err := taskBuilder.Build()
		if err != nil {
			return nil, fmt.Errorf("build task %s failed: %w", taskDef.TaskID, err)
		}

		// 设置模板任务标记
		if taskDef.IsTemplate {
			t.SetTemplate(true)
		}

		// 添加到WorkflowBuilder
		wfBuilder = wfBuilder.WithTask(t)
	}

	// 构建Workflow
	wf, err := wfBuilder.Build()
	if err != nil {
		return nil, fmt.Errorf("build workflow failed: %w", err)
	}

	return wf, nil
}

// convertParamsToInterface 将map[string]string转换为map[string]interface{}
func convertParamsToInterface(params map[string]string) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range params {
		result[k] = v
	}
	return result
}

// mapCallbackStateToTaskStatus 将callback state映射到task status
func mapCallbackStateToTaskStatus(state string) string {
	switch state {
	case "success":
		return task.TaskStatusSuccess
	case "failed":
		return task.TaskStatusFailed
	case "timeout":
		return task.TaskStatusTimeout
	default:
		return ""
	}
}

// RegisterWorkflow 注册Workflow到引擎（对外导出）
func (e *Engine) RegisterWorkflow(ctx context.Context, wf *workflow.Workflow) error {
	if !e.running {
		return logError("engine_not_running", "引擎未启动")
	}

	// 优先使用聚合Repository（保存Workflow及其Task定义）
	if e.aggregateRepo != nil {
		if err := e.aggregateRepo.SaveWorkflow(ctx, wf); err != nil {
			return err
		}
	} else if e.workflowRepo != nil {
		if err := e.workflowRepo.Save(ctx, wf); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("未配置Repository")
	}

	log.Printf("✅ 注册Workflow成功：%s", wf.ID)
	return nil
}

// SubmitWorkflow 提交Workflow并创建WorkflowInstance（对外导出）
// 返回WorkflowController用于控制WorkflowInstance生命周期
func (e *Engine) SubmitWorkflow(ctx context.Context, wf *workflow.Workflow) (workflow.WorkflowController, error) {
	if !e.running {
		return nil, logError("engine_not_running", "引擎未启动")
	}

	// 校验Workflow合法性
	if err := wf.Validate(); err != nil {
		return nil, fmt.Errorf("Workflow校验失败: %w", err)
	}

	var instance *workflow.WorkflowInstance

	// 优先使用聚合Repository（事务性保存Workflow和创建Instance）
	if e.aggregateRepo != nil {
		// 先保存Workflow定义
		if err := e.aggregateRepo.SaveWorkflow(ctx, wf); err != nil {
			return nil, fmt.Errorf("保存Workflow失败: %w", err)
		}
		// 启动Workflow（创建Instance和TaskInstance）
		var err error
		instance, err = e.aggregateRepo.StartWorkflow(ctx, wf)
		if err != nil {
			return nil, fmt.Errorf("启动Workflow失败: %w", err)
		}
	} else {
		// 使用旧的分离Repository
		instance = workflow.NewWorkflowInstance(wf.GetID())
		instance.Status = "Ready"

		// 持久化WorkflowInstance
		if e.workflowInstanceRepo != nil {
			if err := e.workflowInstanceRepo.Save(ctx, instance); err != nil {
				return nil, fmt.Errorf("保存WorkflowInstance失败: %w", err)
			}
		}

		// 在事务中同时保存Workflow模板和所有预定义的Task
		if e.workflowRepo != nil {
			if err := e.workflowRepo.SaveWithTasks(ctx, wf, instance.ID, e.taskRepo, e.registry); err != nil {
				return nil, fmt.Errorf("保存Workflow和Task失败: %w", err)
			}
		}
	}

	// 根据 Workflow.ExecutionMode 选择 Manager
	var manager types.WorkflowInstanceManager
	var managerErr error

	executionMode := wf.GetExecutionMode()

	switch executionMode {
	case workflow.ExecutionModeStreaming:
		// 流处理模式：创建 RealtimeInstanceManager
		manager, managerErr = e.createRealtimeInstanceManager(ctx, instance, wf)

	case workflow.ExecutionModeBatch:
		fallthrough
	default:
		// 批处理模式：根据版本创建对应的 WorkflowInstanceManager
		if e.instanceManagerVersion == InstanceManagerV2 {
			manager, managerErr = NewWorkflowInstanceManagerV2WithAggregate(
				instance,
				wf,
				e.executor,
				e.aggregateRepo,
				e.taskRepo,
				e.workflowInstanceRepo,
				e.registry,
				e.pluginManager,
			)
		} else {
			manager, managerErr = NewWorkflowInstanceManager(
				instance,
				wf,
				e.executor,
				e.taskRepo,
				e.workflowInstanceRepo,
				e.registry,
			)
		}
	}
	if managerErr != nil {
		return nil, fmt.Errorf("创建WorkflowInstanceManager失败: %w", managerErr)
	}

	// 创建WorkflowController
	controller := workflow.NewWorkflowControllerWithCallbacks(
		instance.ID,
		func() error {
			return e.PauseWorkflowInstance(ctx, instance.ID)
		},
		func() error {
			return e.ResumeWorkflowInstance(ctx, instance.ID)
		},
		func() error {
			return e.TerminateWorkflowInstance(ctx, instance.ID, "用户终止")
		},
		func() (string, error) {
			return e.GetWorkflowInstanceStatus(ctx, instance.ID)
		},
	)

	// 保存到内存映射
	e.mu.Lock()
	e.managers[instance.ID] = manager
	e.controllers[instance.ID] = controller
	e.mu.Unlock()

	// 启动状态更新转发协程：将Manager的状态更新转发到Controller
	go e.forwardStatusUpdates(instance.ID, manager, controller)

	// 立即启动WorkflowInstance执行
	manager.Start()

	// 如果Workflow启用了定时调度，注册到CronScheduler
	if wf.IsCronEnabled() && e.cronScheduler != nil {
		if err := e.cronScheduler.RegisterWorkflow(wf); err != nil {
			log.Printf("⚠️ 注册Workflow到定时调度器失败: WorkflowID=%s, Error=%v", wf.GetID(), err)
			// 不阻止Workflow提交，仅记录日志
		}
	}

	log.Printf("✅ 提交Workflow成功，创建WorkflowInstance: %s，已启动执行", instance.ID)
	return controller, nil
}

// WaitForAllWorkflows 等待所有workflow完成后退出（对外导出）
// timeout: 可选超时参数，如果提供则使用该超时，否则无限等待
// 返回: 如果有任何workflow失败或超时，返回错误
func (e *Engine) WaitForAllWorkflows(timeout ...time.Duration) error {
	e.mu.RLock()
	// 获取所有controllers的快照
	controllers := make([]workflow.WorkflowController, 0, len(e.controllers))
	for _, controller := range e.controllers {
		controllers = append(controllers, controller)
	}
	e.mu.RUnlock()

	// 如果没有workflow，直接返回
	if len(controllers) == 0 {
		return nil
	}

	// 使用WaitGroup并发等待所有workflow
	var wg sync.WaitGroup
	errChan := make(chan error, len(controllers))

	// 为每个workflow启动一个goroutine等待
	for _, controller := range controllers {
		wg.Add(1)
		go func(ctrl workflow.WorkflowController) {
			defer wg.Done()
			if err := ctrl.Wait(timeout...); err != nil {
				errChan <- fmt.Errorf("workflow %s 等待失败: %w", ctrl.InstanceID(), err)
			}
		}(controller)
	}

	// 等待所有goroutine完成
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	// 如果有超时，使用带超时的等待
	if len(timeout) > 0 && timeout[0] > 0 {
		select {
		case <-done:
			// 所有workflow完成
		case <-time.After(timeout[0]):
			// 超时
			return fmt.Errorf("等待所有workflow完成超时（%v）", timeout[0])
		}
	} else {
		// 无限等待
		<-done
	}

	// 检查是否有错误
	close(errChan)
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return fmt.Errorf("部分workflow失败: %v", errors)
	}

	return nil
}

// PauseWorkflowInstance 暂停WorkflowInstance（对外导出）
func (e *Engine) PauseWorkflowInstance(ctx context.Context, instanceID string) error {
	e.mu.RLock()
	manager, exists := e.managers[instanceID]
	e.mu.RUnlock()

	if !exists {
		return logError("instance_not_found", "WorkflowInstance不存在或未运行")
	}

	// 发送暂停信号
	// 类型转换：从 interface{} 转换为双向通道或只写通道
	controlChanRaw := manager.GetControlSignalChannel()
	if controlChanRaw == nil {
		return fmt.Errorf("WorkflowInstance %s 控制信号通道为nil", instanceID)
	}
	// 尝试转换为双向通道
	if controlChan, ok := controlChanRaw.(chan workflow.ControlSignal); ok {
		controlChan <- workflow.SignalPause
	} else if controlChanWrite, ok := controlChanRaw.(chan<- workflow.ControlSignal); ok {
		// 尝试转换为只写通道（兼容旧版本）
		controlChanWrite <- workflow.SignalPause
	} else {
		return fmt.Errorf("WorkflowInstance %s 控制信号通道类型转换失败", instanceID)
	}

	log.Printf("✅ WorkflowInstance %s 已发送暂停信号", instanceID)
	return nil
}

// ResumeWorkflowInstance 恢复WorkflowInstance（对外导出）
func (e *Engine) ResumeWorkflowInstance(ctx context.Context, instanceID string) error {
	e.mu.RLock()
	manager, exists := e.managers[instanceID]
	e.mu.RUnlock()

	if !exists {
		// 如果manager不存在，尝试从数据库恢复
		var instance *workflow.WorkflowInstance
		var err error

		// 优先使用聚合Repository
		if e.aggregateRepo != nil {
			instance, _, err = e.aggregateRepo.GetWorkflowInstanceWithTasks(ctx, instanceID)
		} else if e.workflowInstanceRepo != nil {
			instance, err = e.workflowInstanceRepo.GetByID(ctx, instanceID)
		} else {
			return fmt.Errorf("未配置Repository")
		}
		if err != nil {
			return fmt.Errorf("查询WorkflowInstance失败: %w", err)
		}
		if instance == nil {
			return logError("instance_not_found", "WorkflowInstance不存在")
		}

		if instance.Status != "Paused" {
			return fmt.Errorf("WorkflowInstance %s 当前状态为 %s，无法恢复（仅Paused状态可恢复）", instanceID, instance.Status)
		}

		// 从数据库加载Workflow模板
		var wf *workflow.Workflow
		if e.aggregateRepo != nil {
			wf, err = e.aggregateRepo.GetWorkflowWithTasks(ctx, instance.WorkflowID)
		} else if e.workflowRepo != nil {
			wf, err = e.workflowRepo.GetByID(ctx, instance.WorkflowID)
		}
		if err != nil {
			return fmt.Errorf("加载Workflow模板失败: %w", err)
		}
		if wf == nil {
			return logError("workflow_not_found", "Workflow模板不存在")
		}

		// 根据配置创建对应版本的Manager并恢复
		var managerErr error
		if e.instanceManagerVersion == InstanceManagerV2 {
			manager, managerErr = NewWorkflowInstanceManagerV2WithAggregate(
				instance,
				wf,
				e.executor,
				e.aggregateRepo,
				e.taskRepo,
				e.workflowInstanceRepo,
				e.registry,
				e.pluginManager,
			)
		} else {
			manager, managerErr = NewWorkflowInstanceManager(
				instance,
				wf,
				e.executor,
				e.taskRepo,
				e.workflowInstanceRepo,
				e.registry,
			)
		}
		if managerErr != nil {
			return fmt.Errorf("恢复WorkflowInstanceManager失败: %w", managerErr)
		}

		e.mu.Lock()
		e.managers[instanceID] = manager
		e.mu.Unlock()
	}

	// 发送恢复信号
	// 类型转换：从 interface{} 转换为双向通道或只写通道
	controlChanRaw := manager.GetControlSignalChannel()
	if controlChanRaw == nil {
		return fmt.Errorf("WorkflowInstance %s 控制信号通道为nil", instanceID)
	}
	// 尝试转换为双向通道
	if controlChan, ok := controlChanRaw.(chan workflow.ControlSignal); ok {
		controlChan <- workflow.SignalResume
	} else if controlChanWrite, ok := controlChanRaw.(chan<- workflow.ControlSignal); ok {
		// 尝试转换为只写通道（兼容旧版本）
		controlChanWrite <- workflow.SignalResume
	} else {
		return fmt.Errorf("WorkflowInstance %s 控制信号通道类型转换失败", instanceID)
	}

	log.Printf("✅ WorkflowInstance %s 已发送恢复信号", instanceID)
	return nil
}

// TerminateWorkflowInstance 终止WorkflowInstance（对外导出）
func (e *Engine) TerminateWorkflowInstance(ctx context.Context, instanceID string, reason string) error {
	e.mu.RLock()
	manager, exists := e.managers[instanceID]
	e.mu.RUnlock()

	// 先检查当前状态
	var currentStatus string
	var instance *workflow.WorkflowInstance
	if exists {
		currentStatus = manager.GetStatus()
	} else {
		// 从数据库加载（优先使用聚合Repository）
		var err error
		if e.aggregateRepo != nil {
			instance, _, err = e.aggregateRepo.GetWorkflowInstanceWithTasks(ctx, instanceID)
		} else if e.workflowInstanceRepo != nil {
			instance, err = e.workflowInstanceRepo.GetByID(ctx, instanceID)
		} else {
			return fmt.Errorf("未配置Repository")
		}
		if err != nil {
			return fmt.Errorf("查询WorkflowInstance失败: %w", err)
		}
		if instance == nil {
			return logError("instance_not_found", "WorkflowInstance不存在")
		}
		currentStatus = instance.Status
	}

	// 检查状态是否允许终止
	if currentStatus == "Terminated" || currentStatus == "Success" || currentStatus == "Failed" {
		return fmt.Errorf("WorkflowInstance %s 当前状态为 %s，无法终止", instanceID, currentStatus)
	}

	if !exists {
		// 如果manager不存在，直接从数据库更新状态
		if instance == nil {
			var err error
			if e.aggregateRepo != nil {
				instance, _, err = e.aggregateRepo.GetWorkflowInstanceWithTasks(ctx, instanceID)
			} else if e.workflowInstanceRepo != nil {
				instance, err = e.workflowInstanceRepo.GetByID(ctx, instanceID)
			}
			if err != nil {
				return fmt.Errorf("查询WorkflowInstance失败: %w", err)
			}
		}
		instance.Status = "Terminated"
		instance.ErrorMessage = reason
		if e.workflowInstanceRepo != nil {
			if err := e.workflowInstanceRepo.UpdateStatus(ctx, instanceID, "Terminated"); err != nil {
				return fmt.Errorf("更新WorkflowInstance状态失败: %w", err)
			}
		}

		log.Printf("✅ WorkflowInstance %s 已终止，原因: %s", instanceID, reason)
		return nil
	}

	// 发送终止信号
	// 类型转换：从 interface{} 转换为双向通道或只写通道
	controlChanRaw := manager.GetControlSignalChannel()
	if controlChanRaw == nil {
		return fmt.Errorf("WorkflowInstance %s 控制信号通道为nil", instanceID)
	}
	// 尝试转换为双向通道
	if controlChan, ok := controlChanRaw.(chan workflow.ControlSignal); ok {
		controlChan <- workflow.SignalTerminate
	} else if controlChanWrite, ok := controlChanRaw.(chan<- workflow.ControlSignal); ok {
		// 尝试转换为只写通道（兼容旧版本）
		controlChanWrite <- workflow.SignalTerminate
	} else {
		return fmt.Errorf("WorkflowInstance %s 控制信号通道类型转换失败", instanceID)
	}

	log.Printf("✅ WorkflowInstance %s 已发送终止信号，原因: %s", instanceID, reason)
	return nil
}

// GetWorkflowInstanceStatus 获取WorkflowInstance状态（对外导出）
func (e *Engine) GetWorkflowInstanceStatus(ctx context.Context, instanceID string) (string, error) {
	e.mu.RLock()
	manager, exists := e.managers[instanceID]
	e.mu.RUnlock()

	if exists {
		// 从manager获取状态
		return manager.GetStatus(), nil
	}

	// 从数据库加载（优先使用聚合Repository）
	var instance *workflow.WorkflowInstance
	var err error
	if e.aggregateRepo != nil {
		instance, _, err = e.aggregateRepo.GetWorkflowInstanceWithTasks(ctx, instanceID)
	} else if e.workflowInstanceRepo != nil {
		instance, err = e.workflowInstanceRepo.GetByID(ctx, instanceID)
	} else {
		return "", fmt.Errorf("未配置Repository")
	}
	if err != nil {
		return "", fmt.Errorf("查询WorkflowInstance失败: %w", err)
	}
	if instance == nil {
		return "", logError("instance_not_found", "WorkflowInstance不存在")
	}

	return instance.Status, nil
}

// forwardStatusUpdates 转发状态更新（内部方法）
// 将Manager的状态更新转发到Controller，使Controller能够等待状态变更确认
func (e *Engine) forwardStatusUpdates(instanceID string, manager types.WorkflowInstanceManager, controller workflow.WorkflowController) {
	// 获取Manager的状态更新通道
	managerStatusChan := manager.GetStatusUpdateChannel()

	// 转发状态更新到Controller
	for {
		select {
		case status := <-managerStatusChan:
			// 通过Controller的UpdateStatus方法更新状态
			// UpdateStatus现在是接口方法，可以直接调用
			controller.UpdateStatus(status)
		case <-manager.Context().Done():
			// Manager已关闭，退出转发协程
			return
		}
	}
}

// createRealtimeInstanceManager 创建实时 InstanceManager（内部方法）
func (e *Engine) createRealtimeInstanceManager(
	ctx context.Context,
	instance *workflow.WorkflowInstance,
	wf *workflow.Workflow,
) (types.WorkflowInstanceManager, error) {
	// 校验执行模式
	if wf.GetExecutionMode() != workflow.ExecutionModeStreaming {
		return nil, fmt.Errorf("Workflow 执行模式必须为 'streaming'，当前: %s", wf.GetExecutionMode())
	}

	// 创建 RealtimeInstanceManager
	return realtime.NewRealtimeInstanceManager(
		instance,
		wf,
		e.workflowInstanceRepo,
		// 选项配置
		realtime.WithBufferSize(10000),
		realtime.WithBackpressureThreshold(0.8),
	)
}

// 内部辅助函数（小写，不导出）
func logError(code, msg string) error {
	return fmt.Errorf("%s: %s", code, msg)
}
