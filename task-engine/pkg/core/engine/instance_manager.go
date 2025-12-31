package engine

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/stevelan1995/task-engine/pkg/core/dag"
	"github.com/stevelan1995/task-engine/pkg/core/executor"
	"github.com/stevelan1995/task-engine/pkg/core/task"
	"github.com/stevelan1995/task-engine/pkg/core/workflow"
	"github.com/stevelan1995/task-engine/pkg/storage"
)

// WorkflowInstanceManager 管理单个WorkflowInstance的运行时状态（内部结构）
type WorkflowInstanceManager struct {
	instance             *workflow.WorkflowInstance
	workflow             *workflow.Workflow
	dag                  *dag.DAG
	processedNodes       sync.Map               // 已处理的Task ID -> bool
	candidateNodes       sync.Map               // 候选Task ID -> workflow.Task
	contextData          map[string]interface{} // Task间传递的数据
	controlSignalChan    chan workflow.ControlSignal
	statusUpdateChan     chan string
	mu                   sync.RWMutex
	ctx                  context.Context
	cancel               context.CancelFunc
	executor             *executor.Executor
	taskRepo             storage.TaskRepository
	workflowInstanceRepo storage.WorkflowInstanceRepository
	registry             *task.FunctionRegistry
	wg                   sync.WaitGroup // 用于等待所有协程完成
}

// NewWorkflowInstanceManager 创建WorkflowInstanceManager（内部方法）
func NewWorkflowInstanceManager(
	instance *workflow.WorkflowInstance,
	wf *workflow.Workflow,
	exec *executor.Executor,
	taskRepo storage.TaskRepository,
	workflowInstanceRepo storage.WorkflowInstanceRepository,
	registry *task.FunctionRegistry,
) (*WorkflowInstanceManager, error) {
	// 构建DAG
	dagInstance, err := dag.BuildDAG(wf.GetTasks(), wf.GetDependencies())
	if err != nil {
		return nil, err
	}

	// 检测循环依赖
	if err := dagInstance.DetectCycle(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	manager := &WorkflowInstanceManager{
		instance:             instance,
		workflow:             wf,
		dag:                  dagInstance,
		contextData:          make(map[string]interface{}),
		controlSignalChan:    make(chan workflow.ControlSignal, 10),
		statusUpdateChan:     make(chan string, 10),
		ctx:                  ctx,
		cancel:               cancel,
		executor:             exec,
		taskRepo:             taskRepo,
		workflowInstanceRepo: workflowInstanceRepo,
		registry:             registry,
	}

	// 初始化candidateNodes（根节点，入度为0的Task）
	readyTasks := dagInstance.GetReadyTasks()
	for _, taskID := range readyTasks {
		if t, exists := wf.GetTasks()[taskID]; exists {
			manager.candidateNodes.Store(taskID, t)
		}
	}

	return manager, nil
}

// Start 启动WorkflowInstance执行（内部方法）
func (m *WorkflowInstanceManager) Start() {
	// 更新状态为Running
	m.mu.Lock()
	m.instance.Status = "Running"
	m.instance.StartTime = time.Now()
	m.mu.Unlock()

	// 持久化状态
	ctx := context.Background()
	if err := m.workflowInstanceRepo.UpdateStatus(ctx, m.instance.ID, "Running"); err != nil {
		log.Printf("更新WorkflowInstance状态失败: %v", err)
	}

	// 发送状态更新通知（重要：让Controller知道状态已变为Running）
	select {
	case m.statusUpdateChan <- "Running":
		// 状态更新已发送
	default:
		// 通道已满，记录警告（但不应发生，因为状态更新通道有缓冲）
		log.Printf("警告: WorkflowInstance %s 状态更新通道已满，状态更新可能丢失", m.instance.ID)
	}

	// 启动任务提交协程
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.taskSubmissionGoroutine()
	}()

	// 启动控制信号处理协程
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.controlSignalGoroutine()
	}()
}

// taskSubmissionGoroutine 任务提交协程（Goroutine 1）
func (m *WorkflowInstanceManager) taskSubmissionGoroutine() {
	for {
		select {
		case <-m.ctx.Done():
			log.Printf("WorkflowInstance %s: 任务提交协程退出", m.instance.ID)
			return
		default:
			// 检查控制信号（非阻塞）
			select {
			case signal := <-m.controlSignalChan:
				if signal == workflow.SignalPause || signal == workflow.SignalTerminate {
					log.Printf("WorkflowInstance %s: 收到 %v 信号，退出任务提交协程", m.instance.ID, signal)
					return
				}
			default:
			}

			// 获取可执行任务
			availableTasks := m.getAvailableTasks()
			if len(availableTasks) == 0 {
				// 检查是否所有任务都已完成
				// 注意：需要等待一段时间，让Handler有机会添加子任务
				// 因为Handler是在goroutine中异步执行的
				time.Sleep(100 * time.Millisecond) // 等待Handler执行完成

				// 再次检查是否有可执行任务（可能在等待期间添加了子任务）
				availableTasks = m.getAvailableTasks()
				if len(availableTasks) > 0 {
					// 有新任务可执行，继续处理
					continue
				}

				// 再次检查是否所有任务都已完成
				if m.isAllTasksCompleted() {
					m.mu.Lock()
					m.instance.Status = "Success"
					now := time.Now()
					m.instance.EndTime = &now
					m.mu.Unlock()

					ctx := context.Background()
					m.workflowInstanceRepo.UpdateStatus(ctx, m.instance.ID, "Success")

					// 发送状态更新通知（重要：让Controller知道workflow已完成）
					select {
					case m.statusUpdateChan <- "Success":
						// 状态更新已发送
					default:
						// 通道已满，记录警告（但不应发生，因为状态更新通道有缓冲）
						log.Printf("警告: WorkflowInstance %s 状态更新通道已满，状态更新可能丢失", m.instance.ID)
					}

					log.Printf("WorkflowInstance %s: 所有任务已完成", m.instance.ID)
					return
				}
				// 短暂休眠，避免CPU占用过高
				time.Sleep(10 * time.Millisecond)
				continue
			}

			// 提交任务到Executor
			for _, t := range availableTasks {
				taskID := t.GetID()
				// 标记为已处理
				m.processedNodes.Store(taskID, true)
				m.candidateNodes.Delete(taskID)

				// 通过JobFuncName从registry获取JobFuncID
				jobFuncID := ""
				if m.registry != nil {
					jobFuncID = m.registry.GetIDByName(t.GetJobFuncName())
				}

				// 创建task.Task实例（用于Executor）
				// 获取参数并转换为map[string]any
				paramsAny := make(map[string]any)
				for k, v := range t.GetParams() {
					paramsAny[k] = v
				}

				// 转换为map[string]string用于NewTask
				paramsStr := make(map[string]string)
				for k, v := range t.GetParams() {
					switch val := v.(type) {
					case string:
						paramsStr[k] = val
					case nil:
						paramsStr[k] = ""
					default:
						paramsStr[k] = fmt.Sprintf("%v", val)
					}
				}

				taskObj := task.NewTask(t.GetName(), "", jobFuncID, paramsAny, paramsStr)
				taskObj.ID = taskID // 使用已有的ID
				taskObj.JobFuncName = t.GetJobFuncName()
				taskObj.TimeoutSeconds = 30 // 默认值
				taskObj.RetryCount = 0
				taskObj.Status = task.TaskStatusPending

				// 创建storage.TaskInstance并保存到数据库
				taskInstance := &storage.TaskInstance{
					ID:                 taskID,
					Name:               t.GetName(),
					WorkflowInstanceID: m.instance.ID,
					JobFuncID:          jobFuncID,
					JobFuncName:        t.GetJobFuncName(),
					Params:             t.GetParams(),
					Status:             "Pending",
					TimeoutSeconds:     30,
					RetryCount:         0,
					CreateTime:         time.Now(),
				}

				ctx := context.Background()
				if err := m.taskRepo.Save(ctx, taskInstance); err != nil {
					log.Printf("保存Task实例失败: %v", err)
					continue
				}

				// 创建executor.PendingTask
				pendingTask := &executor.PendingTask{
					Task:       taskObj,
					WorkflowID: m.instance.WorkflowID,
					InstanceID: m.instance.ID,
					Domain:     "",
					MaxRetries: 0,
					OnComplete: m.createTaskCompleteHandler(taskID),
					OnError:    m.createTaskErrorHandler(taskID),
				}

				// 提交到Executor
				if err := m.executor.SubmitTask(pendingTask); err != nil {
					log.Printf("提交Task到Executor失败: %v", err)
					continue
				}

				// 更新Task状态为Pending（已在Save中设置，这里确保一致性）
				m.taskRepo.UpdateStatus(ctx, taskID, "Pending")
			}
		}
	}
}

// controlSignalGoroutine 控制信号处理协程（Goroutine 2）
func (m *WorkflowInstanceManager) controlSignalGoroutine() {
	for {
		select {
		case <-m.ctx.Done():
			log.Printf("WorkflowInstance %s: 控制信号处理协程退出", m.instance.ID)
			return
		case signal := <-m.controlSignalChan:
			switch signal {
			case workflow.SignalPause:
				m.handlePause()
			case workflow.SignalResume:
				m.handleResume()
			case workflow.SignalTerminate:
				m.handleTerminate()
			}
		}
	}
}

// handlePause 处理暂停信号
func (m *WorkflowInstanceManager) handlePause() {
	m.mu.Lock()
	m.instance.Status = "Paused"
	m.mu.Unlock()

	// 记录断点数据
	breakpoint := m.createBreakpoint()
	ctx := context.Background()
	m.workflowInstanceRepo.UpdateBreakpoint(ctx, m.instance.ID, breakpoint)
	m.workflowInstanceRepo.UpdateStatus(ctx, m.instance.ID, "Paused")

	// 发送状态更新通知（非阻塞）
	select {
	case m.statusUpdateChan <- "Paused":
	default:
		// 通道已满，忽略
	}

	log.Printf("WorkflowInstance %s: 已暂停", m.instance.ID)
}

// handleResume 处理恢复信号
func (m *WorkflowInstanceManager) handleResume() {
	m.mu.Lock()
	m.instance.Status = "Running"
	m.mu.Unlock()

	ctx := context.Background()
	m.workflowInstanceRepo.UpdateStatus(ctx, m.instance.ID, "Running")

	// 重新启动任务提交协程
	go m.taskSubmissionGoroutine()

	// 发送状态更新通知（非阻塞）
	select {
	case m.statusUpdateChan <- "Running":
	default:
		// 通道已满，忽略
	}

	log.Printf("WorkflowInstance %s: 已恢复", m.instance.ID)
}

// handleTerminate 处理终止信号
func (m *WorkflowInstanceManager) handleTerminate() {
	m.mu.Lock()
	m.instance.Status = "Terminated"
	m.instance.ErrorMessage = "用户终止"
	now := time.Now()
	m.instance.EndTime = &now
	m.mu.Unlock()

	ctx := context.Background()
	m.workflowInstanceRepo.UpdateStatus(ctx, m.instance.ID, "Terminated")

	// 发送状态更新通知（非阻塞）
	select {
	case m.statusUpdateChan <- "Terminated":
	default:
		// 通道已满，忽略
	}

	// 取消context，停止所有协程
	m.cancel()

	log.Printf("WorkflowInstance %s: 已终止", m.instance.ID)
}

// getAvailableTasks 获取可执行的任务列表
func (m *WorkflowInstanceManager) getAvailableTasks() []workflow.Task {
	var available []workflow.Task

	m.candidateNodes.Range(func(key, value interface{}) bool {
		taskID := key.(string)
		t := value.(workflow.Task)

		// 检查是否已处理
		if _, processed := m.processedNodes.Load(taskID); processed {
			return true // 继续下一个
		}

		// 检查所有父节点是否都已处理
		deps := t.GetDependencies()
		allDepsProcessed := true
		for _, depName := range deps {
			// 通过名称找到Task ID
			depTaskID := m.findTaskIDByName(depName)
			if depTaskID == "" {
				allDepsProcessed = false
				break
			}
			if _, processed := m.processedNodes.Load(depTaskID); !processed {
				allDepsProcessed = false
				break
			}
		}

		if allDepsProcessed {
			available = append(available, t)
		}

		return true
	})

	return available
}

// isAllTasksCompleted 检查是否所有任务都已完成
func (m *WorkflowInstanceManager) isAllTasksCompleted() bool {
	// 注意：m.workflow.GetTasks() 可能包含动态添加的子任务，所以需要实时获取
	totalTasks := len(m.workflow.GetTasks())
	processedCount := 0
	m.processedNodes.Range(func(key, value interface{}) bool {
		processedCount++
		return true
	})

	// 如果已处理的任务数小于总任务数，说明还有未完成的任务
	if processedCount < totalTasks {
		return false
	}

	// 还需要检查是否有任务在候选队列中但未处理
	hasUnprocessedCandidate := false
	m.candidateNodes.Range(func(key, value interface{}) bool {
		taskID := key.(string)
		if _, processed := m.processedNodes.Load(taskID); !processed {
			hasUnprocessedCandidate = true
			return false // 停止遍历
		}
		return true
	})

	// 如果有未处理的候选任务，说明还有任务未完成
	if hasUnprocessedCandidate {
		return false
	}

	return true
}

// findTaskIDByName 通过Task名称查找Task ID
func (m *WorkflowInstanceManager) findTaskIDByName(name string) string {
	for taskID, t := range m.workflow.GetTasks() {
		if t.GetName() == name {
			return taskID
		}
	}
	return ""
}

// createBreakpoint 创建断点数据
func (m *WorkflowInstanceManager) createBreakpoint() *workflow.BreakpointData {
	completedTaskNames := make([]string, 0)
	m.processedNodes.Range(func(key, value interface{}) bool {
		taskID := key.(string)
		if t, exists := m.workflow.GetTasks()[taskID]; exists {
			completedTaskNames = append(completedTaskNames, t.GetName())
		}
		return true
	})

	// TODO: 获取当前运行中的Task名称（需要从Executor查询）
	runningTaskNames := make([]string, 0)

	// DAG快照（简化处理）
	dagSnapshot := make(map[string]interface{})
	dagSnapshot["nodes"] = m.dag.GetOrder() // 使用 go-dag 的 GetOrder 方法获取节点数

	return &workflow.BreakpointData{
		CompletedTaskNames: completedTaskNames,
		RunningTaskNames:   runningTaskNames,
		DAGSnapshot:        dagSnapshot,
		ContextData:        m.contextData,
		LastUpdateTime:     time.Now(),
	}
}

// RestoreFromBreakpoint 从断点数据恢复WorkflowInstance状态（内部方法）
func (m *WorkflowInstanceManager) RestoreFromBreakpoint(breakpoint *workflow.BreakpointData) error {
	if breakpoint == nil {
		return nil
	}

	// 1. 恢复已完成的Task列表
	m.processedNodes = sync.Map{}
	for _, taskName := range breakpoint.CompletedTaskNames {
		taskID := m.findTaskIDByName(taskName)
		if taskID != "" {
			m.processedNodes.Store(taskID, true)
		}
	}

	// 2. 恢复上下文数据
	if breakpoint.ContextData != nil {
		m.contextData = breakpoint.ContextData
	} else {
		m.contextData = make(map[string]interface{})
	}

	// 3. 重新计算候选节点（基于已完成的Task）
	m.candidateNodes = sync.Map{}
	readyTasks := m.dag.GetReadyTasks()
	for _, taskID := range readyTasks {
		// 检查是否已处理
		if _, processed := m.processedNodes.Load(taskID); !processed {
			// 检查所有父节点是否都已处理
			parents, err := m.dag.GetParents(taskID)
			if err == nil {
				allParentsProcessed := true
				for _, parentID := range parents {
					if _, processed := m.processedNodes.Load(parentID); !processed {
						allParentsProcessed = false
						break
					}
				}
				if allParentsProcessed {
					if t, exists := m.workflow.GetTasks()[taskID]; exists {
						m.candidateNodes.Store(taskID, t)
					}
				}
			}
		}
	}

	// 对于所有未完成的Task，检查其依赖关系，如果依赖已完成，加入候选队列
	for taskID, t := range m.workflow.GetTasks() {
		// 如果已处理，跳过
		if _, processed := m.processedNodes.Load(taskID); processed {
			continue
		}

		// 检查是否已在候选队列
		if _, exists := m.candidateNodes.Load(taskID); exists {
			continue
		}

		// 检查所有依赖是否都已处理
		deps := t.GetDependencies()
		allDepsProcessed := true
		for _, depName := range deps {
			depTaskID := m.findTaskIDByName(depName)
			if depTaskID == "" {
				allDepsProcessed = false
				break
			}
			if _, processed := m.processedNodes.Load(depTaskID); !processed {
				allDepsProcessed = false
				break
			}
		}

		if allDepsProcessed {
			m.candidateNodes.Store(taskID, t)
		}
	}

	// 4. 对于所有未完成的Task，检查其依赖关系，如果依赖已完成，加入候选队列
	for taskID, t := range m.workflow.GetTasks() {
		// 如果已处理，跳过
		if _, processed := m.processedNodes.Load(taskID); processed {
			continue
		}

		// 检查是否已在候选队列
		if _, exists := m.candidateNodes.Load(taskID); exists {
			continue
		}

		// 检查所有依赖是否都已处理
		deps := t.GetDependencies()
		allDepsProcessed := true
		for _, depName := range deps {
			depTaskID := m.findTaskIDByName(depName)
			if depTaskID == "" {
				allDepsProcessed = false
				break
			}
			if _, processed := m.processedNodes.Load(depTaskID); !processed {
				allDepsProcessed = false
				break
			}
		}

		if allDepsProcessed {
			m.candidateNodes.Store(taskID, t)
		}
	}

	return nil
}

// createTaskCompleteHandler 创建任务完成处理器
func (m *WorkflowInstanceManager) createTaskCompleteHandler(taskID string) func(*executor.TaskResult) {
	return func(result *executor.TaskResult) {
		ctx := context.Background()
		m.taskRepo.UpdateStatus(ctx, taskID, "Success")

		// 执行Task的状态Handler（Success状态）
		if m.registry != nil {
			// 从Workflow中获取Task配置（包含StatusHandlers）
			workflowTask, exists := m.workflow.GetTasks()[taskID]
			if !exists {
				return
			}

			// 从数据库加载Task实例以获取当前状态
			taskInstance, err := m.taskRepo.GetByID(ctx, taskID)
			if err != nil {
				log.Printf("加载Task实例失败: %v", err)
				return
			}

			// 尝试从workflow.Task获取StatusHandlers
			// 注意：workflow.Task是接口，需要类型断言或通过其他方式获取
			// 这里简化处理，假设StatusHandlers在创建Task时已配置
			// 实际应该从Task定义中获取StatusHandlers配置
			var statusHandlers map[string]string
			if taskObj, ok := workflowTask.(*task.Task); ok {
				statusHandlers = taskObj.StatusHandlers
			}

			// 创建task.Task实例用于handler调用
			taskObj := &task.Task{
				ID:             taskInstance.ID,
				Name:           taskInstance.Name,
				Description:    workflowTask.GetName(), // 使用workflow中的描述
				Params:         taskInstance.Params,
				Status:         taskInstance.Status,
				StatusHandlers: statusHandlers,
				JobFuncID:      taskInstance.JobFuncID,
				JobFuncName:    taskInstance.JobFuncName,
				TimeoutSeconds: taskInstance.TimeoutSeconds,
				RetryCount:     taskInstance.RetryCount,
				Dependencies:   []string{}, // 从workflowTask获取
			}

			if err := task.ExecuteTaskHandlerWithContext(
				m.registry,
				taskObj,
				task.TaskStatusSuccess,
				m.instance.WorkflowID,
				m.instance.ID,
				result.Data,
				"",
			); err != nil {
				log.Printf("执行Task Handler失败: Task=%s, Status=Success, Error=%v", taskID, err)
			}
		}

		// 更新DAG入度（go-dag 自动管理，这里保留用于兼容性）
		m.dag.UpdateInDegree(taskID)

		// 将下游节点加入候选队列
		node, exists := m.dag.GetNode(taskID)
		if exists {
			for _, nextID := range node.OutEdges {
				if t, exists := m.workflow.GetTasks()[nextID]; exists {
					// 检查是否所有父节点都已处理
					allDepsProcessed := true
					for _, depName := range t.GetDependencies() {
						depTaskID := m.findTaskIDByName(depName)
						if depTaskID == "" {
							allDepsProcessed = false
							break
						}
						if _, processed := m.processedNodes.Load(depTaskID); !processed {
							allDepsProcessed = false
							break
						}
					}
					if allDepsProcessed {
						m.candidateNodes.Store(nextID, t)
					}
				}
			}
		}

		// 保存结果数据到上下文
		if result.Data != nil {
			m.contextData[taskID] = result.Data
		}
	}
}

// createTaskErrorHandler 创建任务错误处理器
func (m *WorkflowInstanceManager) createTaskErrorHandler(taskID string) func(error) {
	return func(err error) {
		ctx := context.Background()
		status := "Failed"
		m.taskRepo.UpdateStatusWithError(ctx, taskID, status, err.Error())

		// 执行Task的状态Handler（Failed状态）
		if m.registry != nil {
			// 从Workflow中获取Task配置（包含StatusHandlers）
			workflowTask, exists := m.workflow.GetTasks()[taskID]
			if !exists {
				return
			}

			// 从数据库加载Task实例以获取当前状态
			taskInstance, loadErr := m.taskRepo.GetByID(ctx, taskID)
			if loadErr != nil {
				log.Printf("加载Task实例失败: %v", loadErr)
				return
			}

			// 尝试从workflow.Task获取StatusHandlers
			var statusHandlers map[string]string
			if taskObj, ok := workflowTask.(*task.Task); ok {
				statusHandlers = taskObj.StatusHandlers
			}

			// 创建task.Task实例用于handler调用
			taskObj := &task.Task{
				ID:             taskInstance.ID,
				Name:           taskInstance.Name,
				Description:    workflowTask.GetName(),
				Params:         taskInstance.Params,
				Status:         taskInstance.Status,
				StatusHandlers: statusHandlers,
				JobFuncID:      taskInstance.JobFuncID,
				JobFuncName:    taskInstance.JobFuncName,
				TimeoutSeconds: taskInstance.TimeoutSeconds,
				RetryCount:     taskInstance.RetryCount,
				Dependencies:   []string{},
			}

			if handlerErr := task.ExecuteTaskHandlerWithContext(
				m.registry,
				taskObj,
				task.TaskStatusFailed,
				m.instance.WorkflowID,
				m.instance.ID,
				nil,
				err.Error(),
			); handlerErr != nil {
				log.Printf("执行Task Handler失败: Task=%s, Status=Failed, Error=%v", taskID, handlerErr)
			}
		}

		// 标记WorkflowInstance为Failed
		m.mu.Lock()
		m.instance.Status = "Failed"
		m.instance.ErrorMessage = err.Error()
		now := time.Now()
		m.instance.EndTime = &now
		m.mu.Unlock()

		m.workflowInstanceRepo.UpdateStatus(ctx, m.instance.ID, "Failed")
	}
}

// GetControlSignalChannel 获取控制信号通道（内部方法）
func (m *WorkflowInstanceManager) GetControlSignalChannel() chan<- workflow.ControlSignal {
	return m.controlSignalChan
}

// GetStatusUpdateChannel 获取状态更新通道（内部方法）
// 用于Engine转发状态更新到Controller
func (m *WorkflowInstanceManager) GetStatusUpdateChannel() <-chan string {
	return m.statusUpdateChan
}

// AddSubTask 动态添加子任务到WorkflowInstance（内部方法）
// subTask: 动态生成的子Task
// parentTaskID: 父Task ID
func (m *WorkflowInstanceManager) AddSubTask(subTask workflow.Task, parentTaskID string) error {
	if subTask == nil {
		return fmt.Errorf("子Task不能为空")
	}
	if subTask.GetID() == "" {
		return fmt.Errorf("子Task ID不能为空")
	}
	if subTask.GetName() == "" {
		return fmt.Errorf("子Task名称不能为空")
	}

	// 检查父Task是否存在
	if _, exists := m.workflow.GetTasks()[parentTaskID]; !exists {
		return fmt.Errorf("父Task %s 不存在", parentTaskID)
	}

	// 检查子Task ID是否重复
	if _, exists := m.workflow.GetTasks()[subTask.GetID()]; exists {
		return fmt.Errorf("子Task ID %s 已存在", subTask.GetID())
	}

	// 检查子Task名称是否唯一
	for taskID, t := range m.workflow.GetTasks() {
		if t.GetName() == subTask.GetName() && taskID != subTask.GetID() {
			return fmt.Errorf("Task名称 %s 已存在", subTask.GetName())
		}
	}

	// 1. 将子Task添加到Workflow的Tasks映射中
	m.workflow.Tasks[subTask.GetID()] = subTask

	// 2. 更新Workflow的Dependencies映射（子任务依赖父任务）
	if m.workflow.Dependencies == nil {
		m.workflow.Dependencies = make(map[string][]string)
	}
	// 检查是否已存在该依赖
	found := false
	for _, depID := range m.workflow.Dependencies[subTask.GetID()] {
		if depID == parentTaskID {
			found = true
			break
		}
	}
	if !found {
		m.workflow.Dependencies[subTask.GetID()] = append(m.workflow.Dependencies[subTask.GetID()], parentTaskID)
	}

	// 3. 更新DAG，添加子任务节点和依赖关系
	if err := m.dag.AddNode(subTask.GetID(), subTask.GetName(), subTask, []string{parentTaskID}); err != nil {
		// 如果DAG添加失败，回滚Workflow的更改
		delete(m.workflow.Tasks, subTask.GetID())
		delete(m.workflow.Dependencies, subTask.GetID())
		return fmt.Errorf("添加子任务到DAG失败: %w", err)
	}

	// 4. 检查子任务的依赖是否已满足，如果满足则加入候选队列
	allDepsProcessed := true
	subTaskDeps := subTask.GetDependencies()
	for _, depName := range subTaskDeps {
		depTaskID := m.findTaskIDByName(depName)
		if depTaskID == "" {
			allDepsProcessed = false
			break
		}
		// 检查依赖是否已处理（通过processedNodes）
		if _, processed := m.processedNodes.Load(depTaskID); !processed {
			allDepsProcessed = false
			break
		}
	}

	// 如果子任务的依赖已满足，加入候选队列
	if allDepsProcessed {
		m.candidateNodes.Store(subTask.GetID(), subTask)
		log.Printf("WorkflowInstance %s: 子任务 %s 已添加，依赖已满足，加入候选队列", m.instance.ID, subTask.GetName())
	} else {
		log.Printf("WorkflowInstance %s: 子任务 %s 已添加，等待依赖满足", m.instance.ID, subTask.GetName())
	}

	return nil
}

// Shutdown 优雅关闭WorkflowInstanceManager（内部方法）
// 取消context，等待所有协程完成
func (m *WorkflowInstanceManager) Shutdown() {
	// 取消context，通知所有协程退出
	m.cancel()

	// 等待所有协程完成（最多等待30秒）
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// 所有协程已完成
		log.Printf("WorkflowInstance %s: 所有协程已退出", m.instance.ID)
	case <-time.After(30 * time.Second):
		// 超时，记录日志
		log.Printf("WorkflowInstance %s: 等待协程退出超时", m.instance.ID)
	}
}
