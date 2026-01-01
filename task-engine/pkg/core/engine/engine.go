package engine

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/stevelan1995/task-engine/pkg/config"
	"github.com/stevelan1995/task-engine/pkg/core/builder"
	"github.com/stevelan1995/task-engine/pkg/core/executor"
	"github.com/stevelan1995/task-engine/pkg/core/task"
	"github.com/stevelan1995/task-engine/pkg/core/workflow"
	"github.com/stevelan1995/task-engine/pkg/storage"
)

// WorkflowDefinition 工作流定义（封装配置文件加载结果）
type WorkflowDefinition struct {
	ID         string                 // 工作流ID
	Config     *config.WorkflowConfig // 工作流配置
	SourcePath string                 // 配置文件路径
	Workflow   *workflow.Workflow     // 转换后的Workflow对象
}

// Engine 调度引擎核心结构体（对外导出）
type Engine struct {
	executor             *executor.Executor
	workflowRepo         storage.WorkflowRepository
	workflowInstanceRepo storage.WorkflowInstanceRepository
	taskRepo             storage.TaskRepository
	registry             *task.FunctionRegistry
	cfg                  *config.EngineConfig // 框架配置
	jobRegistry          sync.Map             // Job函数注册表（funcKey -> function）
	callbackRegistry     sync.Map             // Callback函数注册表（funcKey -> function）
	serviceRegistry      sync.Map             // 服务依赖注册表（serviceKey -> service）
	running              bool
	MaxConcurrency       int
	Timeout              int
	managers             map[string]*WorkflowInstanceManager    // WorkflowInstance ID -> Manager映射
	controllers          map[string]workflow.WorkflowController // WorkflowInstance ID -> Controller映射
	mu                   sync.RWMutex
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

	return &Engine{
		executor:             exec,
		workflowRepo:         workflowRepo,
		workflowInstanceRepo: workflowInstanceRepo,
		taskRepo:             taskRepo,
		registry:             registry,
		MaxConcurrency:       maxConcurrency,
		Timeout:              timeout,
		running:              false,
		managers:             make(map[string]*WorkflowInstanceManager),
		controllers:          make(map[string]workflow.WorkflowController),
	}, nil
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

	// 恢复未完成的WorkflowInstance（文档1.2节要求）
	if err := e.restoreUnfinishedInstances(ctx); err != nil {
		log.Printf("恢复未完成实例失败: %v", err)
		// 不阻止启动，仅记录日志
	}

	return nil
}

// GetRegistry 获取函数注册中心（对外导出，用于测试和函数注册）
func (e *Engine) GetRegistry() *task.FunctionRegistry {
	return e.registry
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
	manager *WorkflowInstanceManager
}

// AddSubTask 添加子任务（对外导出）
func (i *InstanceManagerInterface) AddSubTask(subTask workflow.Task, parentTaskID string) error {
	return i.manager.AddSubTask(subTask, parentTaskID)
}

// restoreUnfinishedInstances 恢复未完成的WorkflowInstance（内部方法）
// 从数据库加载所有状态为Running或Paused的WorkflowInstance，并恢复执行
func (e *Engine) restoreUnfinishedInstances(ctx context.Context) error {
	// 查询所有Running状态的实例
	runningInstances, err := e.workflowInstanceRepo.ListByStatus(ctx, "Running")
	if err != nil {
		return fmt.Errorf("查询Running状态的WorkflowInstance失败: %w", err)
	}

	// 查询所有Paused状态的实例
	pausedInstances, err := e.workflowInstanceRepo.ListByStatus(ctx, "Paused")
	if err != nil {
		return fmt.Errorf("查询Paused状态的WorkflowInstance失败: %w", err)
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
	wf, err := e.workflowRepo.GetByID(ctx, instance.WorkflowID)
	if err != nil {
		return fmt.Errorf("加载Workflow模板失败: %w", err)
	}
	if wf == nil {
		return fmt.Errorf("Workflow模板不存在: %s", instance.WorkflowID)
	}

	// 创建WorkflowInstanceManager（会自动从断点数据恢复）
	manager, err := NewWorkflowInstanceManager(
		instance,
		wf,
		e.executor,
		e.taskRepo,
		e.workflowInstanceRepo,
		e.registry,
	)
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

	// 2. 通知所有WorkflowInstance准备关闭（发送Terminate信号）
	e.mu.RLock()
	instances := make([]*WorkflowInstanceManager, 0, len(e.managers))
	for _, manager := range e.managers {
		instances = append(instances, manager)
	}
	e.mu.RUnlock()

	// 发送Terminate信号到所有实例
	for _, manager := range instances {
		select {
		case manager.GetControlSignalChannel() <- workflow.SignalTerminate:
		default:
			// channel已满，记录日志
			log.Printf("WorkflowInstance %s 控制信号channel已满，跳过", manager.instance.ID)
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
		manager.mu.RLock()
		status := manager.instance.Status
		manager.mu.RUnlock()

		if status == "Running" {
			// 记录断点数据
			breakpoint := manager.createBreakpoint()
			if err := e.workflowInstanceRepo.UpdateBreakpoint(ctx, manager.instance.ID, breakpoint); err != nil {
				log.Printf("保存WorkflowInstance %s 断点数据失败: %v", manager.instance.ID, err)
			}
			// 更新状态为Paused（而不是Terminated，便于恢复）
			if err := e.workflowInstanceRepo.UpdateStatus(ctx, manager.instance.ID, "Paused"); err != nil {
				log.Printf("更新WorkflowInstance %s 状态失败: %v", manager.instance.ID, err)
			}
		}
	}

	// 5. 关闭Executor
	e.executor.Shutdown()

	// 6. 清理内存映射
	e.mu.Lock()
	e.managers = make(map[string]*WorkflowInstanceManager)
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
	if err := e.workflowRepo.Save(ctx, wf); err != nil {
		return err
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

	// 创建WorkflowInstance
	instance := workflow.NewWorkflowInstance(wf.GetID())
	instance.Status = "Ready"

	// 持久化WorkflowInstance
	if err := e.workflowInstanceRepo.Save(ctx, instance); err != nil {
		return nil, fmt.Errorf("保存WorkflowInstance失败: %w", err)
	}

	// 在事务中同时保存Workflow模板和所有预定义的Task
	if err := e.workflowRepo.SaveWithTasks(ctx, wf, instance.ID, e.taskRepo, e.registry); err != nil {
		return nil, fmt.Errorf("保存Workflow和Task失败: %w", err)
	}

	// 创建WorkflowInstanceManager
	manager, err := NewWorkflowInstanceManager(
		instance,
		wf,
		e.executor,
		e.taskRepo,
		e.workflowInstanceRepo,
		e.registry,
	)
	if err != nil {
		return nil, fmt.Errorf("创建WorkflowInstanceManager失败: %w", err)
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
	manager.GetControlSignalChannel() <- workflow.SignalPause

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
		instance, err := e.workflowInstanceRepo.GetByID(ctx, instanceID)
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
		wf, err := e.workflowRepo.GetByID(ctx, instance.WorkflowID)
		if err != nil {
			return fmt.Errorf("加载Workflow模板失败: %w", err)
		}
		if wf == nil {
			return logError("workflow_not_found", "Workflow模板不存在")
		}

		// 重新创建Manager并恢复
		manager, err = NewWorkflowInstanceManager(
			instance,
			wf,
			e.executor,
			e.taskRepo,
			e.workflowInstanceRepo,
			e.registry,
		)
		if err != nil {
			return fmt.Errorf("恢复WorkflowInstanceManager失败: %w", err)
		}

		e.mu.Lock()
		e.managers[instanceID] = manager
		e.mu.Unlock()
	}

	// 发送恢复信号
	manager.GetControlSignalChannel() <- workflow.SignalResume

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
	if exists {
		manager.mu.RLock()
		currentStatus = manager.instance.Status
		manager.mu.RUnlock()
	} else {
		// 从数据库加载
		instance, err := e.workflowInstanceRepo.GetByID(ctx, instanceID)
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
		instance, err := e.workflowInstanceRepo.GetByID(ctx, instanceID)
		if err != nil {
			return fmt.Errorf("查询WorkflowInstance失败: %w", err)
		}
		instance.Status = "Terminated"
		instance.ErrorMessage = reason
		if err := e.workflowInstanceRepo.UpdateStatus(ctx, instanceID, "Terminated"); err != nil {
			return fmt.Errorf("更新WorkflowInstance状态失败: %w", err)
		}

		log.Printf("✅ WorkflowInstance %s 已终止，原因: %s", instanceID, reason)
		return nil
	}

	// 发送终止信号
	manager.GetControlSignalChannel() <- workflow.SignalTerminate

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
		manager.mu.RLock()
		status := manager.instance.Status
		manager.mu.RUnlock()
		return status, nil
	}

	// 从数据库加载
	instance, err := e.workflowInstanceRepo.GetByID(ctx, instanceID)
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
func (e *Engine) forwardStatusUpdates(instanceID string, manager *WorkflowInstanceManager, controller workflow.WorkflowController) {
	// 获取Manager的状态更新通道
	managerStatusChan := manager.GetStatusUpdateChannel()

	// 转发状态更新到Controller
	for {
		select {
		case status := <-managerStatusChan:
			// 通过Controller的UpdateStatus方法更新状态
			// UpdateStatus现在是接口方法，可以直接调用
			controller.UpdateStatus(status)
		case <-manager.ctx.Done():
			// Manager已关闭，退出转发协程
			return
		}
	}
}

// 内部辅助函数（小写，不导出）
func logError(code, msg string) error {
	return fmt.Errorf("%s: %s", code, msg)
}
