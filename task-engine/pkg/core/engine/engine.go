package engine

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/stevelan1995/task-engine/pkg/core/executor"
	"github.com/stevelan1995/task-engine/pkg/core/task"
	"github.com/stevelan1995/task-engine/pkg/core/workflow"
	"github.com/stevelan1995/task-engine/pkg/storage"
)

// Engine 调度引擎核心结构体（对外导出）
type Engine struct {
	executor             *executor.Executor
	workflowRepo         storage.WorkflowRepository
	workflowInstanceRepo storage.WorkflowInstanceRepository
	taskRepo             storage.TaskRepository
	registry             *task.FunctionRegistry
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
	exec, err := executor.NewExecutor(maxConcurrency)
	if err != nil {
		return nil, err
	}

	// 创建FunctionRegistry
	// 暂时不传入repo，后续可以完善
	registry := task.NewFunctionRegistry(nil, nil)

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
	log.Println("✅ 量化任务引擎已启动")

	// 恢复未完成的WorkflowInstance（文档1.2节要求）
	if err := e.restoreUnfinishedInstances(ctx); err != nil {
		log.Printf("恢复未完成实例失败: %v", err)
		// 不阻止启动，仅记录日志
	}

	return nil
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
	// 注意：这里简化处理，实际应该使用WaitGroup等待所有协程完成
	// 由于WorkflowInstanceManager内部没有WaitGroup，这里先等待一段时间
	time.Sleep(2 * time.Second)

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

	log.Println("✅ 量化任务引擎已停止")
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

	// 持久化Workflow模板
	if err := e.workflowRepo.Save(ctx, wf); err != nil {
		return nil, fmt.Errorf("保存Workflow模板失败: %w", err)
	}

	// 创建WorkflowInstance
	instance := workflow.NewWorkflowInstance(wf.GetID())
	instance.Status = "Ready"

	// 持久化WorkflowInstance
	if err := e.workflowInstanceRepo.Save(ctx, instance); err != nil {
		return nil, fmt.Errorf("保存WorkflowInstance失败: %w", err)
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
