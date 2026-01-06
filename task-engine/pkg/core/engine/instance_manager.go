package engine

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/stevelan1995/task-engine/pkg/core/cache"
	"github.com/stevelan1995/task-engine/pkg/core/dag"
	"github.com/stevelan1995/task-engine/pkg/core/executor"
	"github.com/stevelan1995/task-engine/pkg/core/task"
	"github.com/stevelan1995/task-engine/pkg/core/types"
	"github.com/stevelan1995/task-engine/pkg/core/workflow"
	"github.com/stevelan1995/task-engine/pkg/storage"
)

// parentSubTaskStats 父任务的子任务统计信息（内部结构）
type parentSubTaskStats struct {
	successCount   int          // 成功子任务数
	totalCount     int          // 总子任务数
	completedCount int          // 已完成子任务数（包括成功和失败）
	mu             sync.RWMutex // 保护统计信息
}

// WorkflowInstanceManager 管理单个WorkflowInstance的运行时状态（内部结构）
type WorkflowInstanceManager struct {
	instance             *workflow.WorkflowInstance
	workflow             *workflow.Workflow
	dag                  *dag.DAG
	processedNodes       sync.Map     // 已处理的Task ID -> bool
	readyTasksSet        sync.Map     // 就绪任务集合（taskID -> workflow.Task），O(1)访问
	readyTasksMu         sync.RWMutex // 保护readyTasksSet的批量操作和复合操作
	contextData          sync.Map     // Task间传递的数据
	parentSubTaskStats   sync.Map     // 父任务的子任务统计信息（parentTaskID -> *parentSubTaskStats），优化性能
	controlSignalChan    chan workflow.ControlSignal
	statusUpdateChan     chan string
	mu                   sync.RWMutex
	ctx                  context.Context
	cancel               context.CancelFunc
	executor             *executor.Executor
	taskRepo             storage.TaskRepository
	workflowInstanceRepo storage.WorkflowInstanceRepository
	registry             *task.FunctionRegistry
	resultCache          cache.ResultCache // 结果缓存
	wg                   sync.WaitGroup    // 用于等待所有协程完成
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
	// 创建默认的内存缓存（如果未提供）
	resultCache := cache.NewMemoryResultCache()
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
		contextData:          sync.Map{},
		controlSignalChan:    make(chan workflow.ControlSignal, 10),
		statusUpdateChan:     make(chan string, 10),
		ctx:                  ctx,
		cancel:               cancel,
		executor:             exec,
		taskRepo:             taskRepo,
		workflowInstanceRepo: workflowInstanceRepo,
		registry:             registry,
		resultCache:          resultCache,
	}

	// 初始化readyTasksSet（根节点，入度为0的Task）
	manager.initReadyTasksSet()

	// 验证初始化：检查所有任务是否都被正确添加到 readyTasksSet
	totalTasks := len(wf.GetTasks())
	readyTasks := dagInstance.GetReadyTasks()
	readyCount := 0
	manager.readyTasksSet.Range(func(key, value interface{}) bool {
		readyCount++
		return true
	})

	log.Printf("✅ WorkflowInstance %s: 初始化完成，总任务数: %d, 就绪任务数: %d, 已添加到 readyTasksSet: %d",
		instance.ID, totalTasks, len(readyTasks), readyCount)

	return manager, nil
}

// Start 启动WorkflowInstance执行（公共方法，实现接口）
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
				// 优化：减少recoverPendingTasks的调用频率
				// 使用更长的等待时间，避免频繁查询数据库
				// 检查是否所有任务都已完成
				// 注意：需要等待一段时间，让Handler有机会添加子任务
				// 因为Handler是在goroutine中异步执行的
				time.Sleep(500 * time.Millisecond) // 增加等待时间，减少数据库查询频率

				// 再次检查是否有可执行任务（可能在等待期间添加了子任务）
				availableTasks = m.getAvailableTasks()
				if len(availableTasks) > 0 {
					// 有新任务可执行，继续处理
					continue
				}

				// 优化：只在必要时调用recoverPendingTasks（例如：长时间没有新任务时）
				// 主要用于系统恢复场景：系统崩溃重启后，需要恢复未完成的任务
				// 或者任务提交失败后需要重试的场景
				// 注意：这个调用比较昂贵，所以只在没有可用任务且可能还有未完成任务时才调用
				if !m.isAllTasksCompleted() {
					// 可能还有未完成的任务，尝试恢复
					log.Printf("WorkflowInstance %s: 可能还有未完成的任务，尝试恢复", m.instance.ID)
					m.recoverPendingTasks()
					// 恢复后再次检查
					availableTasks = m.getAvailableTasks()
					if len(availableTasks) > 0 {
						continue
					}
				}

				// 再次检查是否所有任务都已完成
				if m.isAllTasksCompleted() {
					m.mu.Lock()
					m.instance.Status = "Success"
					now := time.Now()
					m.instance.EndTime = &now
					m.mu.Unlock()

					ctx := context.Background()
					// 批量保存所有任务状态
					m.saveAllTaskStatuses(ctx)
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
				taskName := t.GetName()

				// 再次检查任务是否已被处理（防止并发问题：任务在执行过程中被标记为已处理）
				if _, processed := m.processedNodes.Load(taskID); processed {
					// 任务已被处理，从就绪任务集合中删除并跳过
					m.readyTasksSet.Delete(taskID)
					continue
				}

				// 检查依赖任务是否有失败的，如果有则跳过当前任务
				if failedDep := m.checkDependencyFailed(t); failedDep != "" {
					log.Printf("⚠️ WorkflowInstance %s: 任务 %s (%s) 的依赖任务 %s 已失败，跳过执行并标记为失败",
						m.instance.ID, taskID, taskName, failedDep)

					// 标记当前任务为失败
					t.SetStatus(task.TaskStatusFailed)
					m.processedNodes.Store(taskID, true)
					m.readyTasksSet.Delete(taskID)

					// 保存错误信息
					errorKey := fmt.Sprintf("%s:error", taskID)
					m.contextData.Store(errorKey, fmt.Sprintf("依赖任务 %s 执行失败，跳过当前任务", failedDep))

					// 检查下游任务是否可以就绪（虽然当前任务失败，但下游任务可能需要处理）
					m.onTaskCompleted(taskID)
					continue
				}

				// 检查是否为模板任务（模板任务不执行，仅用于生成子任务）
				if t.IsTemplate() {
					log.Printf("📋 WorkflowInstance %s: Task %s (%s) 是模板任务，跳过执行，标记为已处理",
						m.instance.ID, taskID, taskName)
					// 模板任务标记为已处理，但不执行
					// 设置状态为Success，表示模板任务已"完成"（虽然不执行，但依赖关系已满足）
					t.SetStatus(task.TaskStatusSuccess)
					m.processedNodes.Store(taskID, true)
					m.readyTasksSet.Delete(taskID)
					// 检查下游任务是否可以就绪（模板任务虽然不执行，但依赖关系仍然有效）
					m.onTaskCompleted(taskID)
					continue
				}

				// 先不标记为已处理，等成功提交后再标记
				// 这样如果提交失败，任务还在 candidateNodes 中，可以重试

				// 使用IsSubTask()判断是否是动态生成的子任务（不保存到数据库，跳过执行）
				// if t.IsSubTask() {
				// 	log.Printf("⚠️ WorkflowInstance %s: Task %s (%s) 是动态生成的子任务，跳过执行",
				// 		m.instance.ID, taskID, taskName)
				// 	// 从candidateNodes中删除，避免重复检查
				// 	m.candidateNodes.Delete(taskID)
				// 	continue
				// }

				// 从缓存获取上游任务结果并注入参数
				if m.resultCache != nil {
					m.injectCachedResults(t, taskID)
				}

				// 通过JobFuncName从registry获取JobFuncID（如果还没有设置）
				if t.GetJobFuncID() == "" && m.registry != nil {
					t.SetJobFuncID(m.registry.GetIDByName(t.GetJobFuncName()))
				}

				// 确保状态为Pending
				t.SetStatus(task.TaskStatusPending)

				// 创建executor.PendingTask
				// 现在可以直接使用 workflow.Task 接口，不需要类型断言
				pendingTask := &executor.PendingTask{
					Task:       t,
					WorkflowID: m.instance.WorkflowID,
					InstanceID: m.instance.ID,
					Domain:     "",
					MaxRetries: 0,
					OnComplete: m.createTaskCompleteHandler(taskID),
					OnError:    m.createTaskErrorHandler(taskID),
				}

				// 提交到Executor
				if err := m.executor.SubmitTask(pendingTask); err != nil {
					log.Printf("❌ WorkflowInstance %s: 提交Task到Executor失败: TaskID=%s, TaskName=%s, Error=%v",
						m.instance.ID, taskID, taskName, err)
					// 提交失败，需要回滚：将任务重新添加到就绪任务集合，以便重试
					// 注意：任务已经保存到数据库，但还没有被标记为已处理
					// 不更新数据库状态，只在instance完成/保存breakpoint/被取消时批量保存
					m.readyTasksSet.Store(taskID, t)
					continue
				}

				// 提交成功，从就绪任务集合中删除（但不标记为已处理，等任务真正完成后再标记）
				// 注意：任务被提交到Executor后，会在异步执行完成后通过OnComplete/OnError回调更新状态
				// 我们不应该在这里标记为已处理，因为任务可能还在Executor队列中等待执行
				// 不更新数据库状态，只在instance完成/保存breakpoint/被取消时批量保存
				m.readyTasksSet.Delete(taskID)
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

	ctx := context.Background()
	// 批量保存所有任务状态
	m.saveAllTaskStatuses(ctx)

	// 记录断点数据
	breakpointValue := m.CreateBreakpoint()
	// 类型转换：从 interface{} 转换为 *workflow.BreakpointData
	breakpoint, ok := breakpointValue.(*workflow.BreakpointData)
	if !ok {
		log.Printf("WorkflowInstance %s 断点数据类型转换失败", m.instance.ID)
		return
	}
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
	// 批量保存所有任务状态
	m.saveAllTaskStatuses(ctx)
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

// initReadyTasksSet 初始化就绪任务集合（内部方法）
func (m *WorkflowInstanceManager) initReadyTasksSet() {
	m.readyTasksMu.Lock()
	defer m.readyTasksMu.Unlock()

	readyTaskIDs := m.dag.GetReadyTasks()
	for _, taskID := range readyTaskIDs {
		if task, exists := m.workflow.GetTasks()[taskID]; exists {
			m.readyTasksSet.Store(taskID, task)
		}
	}
}

// checkAndAddToReady 检查并添加任务到就绪集合（复合操作，需要锁保护）
func (m *WorkflowInstanceManager) checkAndAddToReady(childID string) {
	m.readyTasksMu.Lock()
	defer m.readyTasksMu.Unlock()

	// 双重检查：在锁内再次检查是否已存在
	if _, exists := m.readyTasksSet.Load(childID); exists {
		return
	}

	// 检查所有父节点是否都已完成
	parents, err := m.dag.GetParents(childID)
	if err != nil {
		return
	}

	allParentsProcessed := true
	for _, parentID := range parents {
		if _, processed := m.processedNodes.Load(parentID); !processed {
			allParentsProcessed = false
			break
		}
	}

	// 如果所有父节点都已完成，添加到就绪集合
	if allParentsProcessed {
		if task, exists := m.workflow.GetTasks()[childID]; exists {
			m.readyTasksSet.Store(childID, task)
		}
	}
}

// initParentSubTaskStats 初始化父任务的子任务统计信息（内部方法）
func (m *WorkflowInstanceManager) initParentSubTaskStats(parentTaskID string) {
	statsValue, _ := m.parentSubTaskStats.LoadOrStore(parentTaskID, &parentSubTaskStats{
		successCount:   0,
		totalCount:     0,
		completedCount: 0,
	})
	stats := statsValue.(*parentSubTaskStats)
	stats.mu.Lock()
	defer stats.mu.Unlock()
	// 如果已经初始化过，不重复初始化
	if stats.totalCount > 0 {
		return
	}
}

// incrementParentSubTaskTotal 增加父任务的子任务总数（内部方法）
func (m *WorkflowInstanceManager) incrementParentSubTaskTotal(parentTaskID string) {
	statsValue, _ := m.parentSubTaskStats.LoadOrStore(parentTaskID, &parentSubTaskStats{
		successCount:   0,
		totalCount:     0,
		completedCount: 0,
	})
	stats := statsValue.(*parentSubTaskStats)
	stats.mu.Lock()
	defer stats.mu.Unlock()
	stats.totalCount++
}

// updateParentSubTaskStats 更新父任务的子任务统计信息（内部方法）
func (m *WorkflowInstanceManager) updateParentSubTaskStats(subTaskID string, isSuccess bool) {
	// 获取子任务的父任务
	parents, err := m.dag.GetParents(subTaskID)
	if err != nil || len(parents) == 0 {
		return
	}

	// 子任务通常只有一个父任务
	parentTaskID := parents[0]

	statsValue, exists := m.parentSubTaskStats.Load(parentTaskID)
	if !exists {
		// 如果不存在，初始化（这种情况不应该发生，但为了安全）
		m.initParentSubTaskStats(parentTaskID)
		statsValue, _ = m.parentSubTaskStats.Load(parentTaskID)
		if statsValue == nil {
			return
		}
	}

	stats := statsValue.(*parentSubTaskStats)
	stats.mu.Lock()
	defer stats.mu.Unlock()

	// 更新统计信息
	stats.completedCount++
	if isSuccess {
		stats.successCount++
	}
}

// checkParentTaskStatus 检查父任务是否应该成功（根据SubTaskErrorTolerance）（内部方法，优化版：使用Map统计）
func (m *WorkflowInstanceManager) checkParentTaskStatus(parentTaskID string) {
	// 从Map中获取父任务的子任务统计信息
	statsValue, exists := m.parentSubTaskStats.Load(parentTaskID)
	if !exists {
		// 如果不存在统计信息，说明没有子任务，不需要检查
		return
	}

	stats := statsValue.(*parentSubTaskStats)
	stats.mu.RLock()
	successCount := stats.successCount
	totalCount := stats.totalCount
	completedCount := stats.completedCount
	stats.mu.RUnlock()

	if totalCount == 0 {
		// 没有子任务，不需要检查
		return
	}

	// 检查是否所有子任务都已完成
	if completedCount < totalCount {
		// 还有子任务未完成，不处理
		return
	}

	// 计算失败率
	failedCount := totalCount - successCount
	failureRate := float64(failedCount) / float64(totalCount)
	tolerance := m.workflow.GetSubTaskErrorTolerance()

	// 判断父任务是否应该成功
	parentTask, exists := m.workflow.GetTasks()[parentTaskID]
	if !exists {
		return
	}

	currentStatus := parentTask.GetStatus()
	// 如果父任务已经成功或失败，不再更新
	if currentStatus == task.TaskStatusSuccess || currentStatus == task.TaskStatusFailed {
		return
	}

	// 根据失败率和容忍度判断
	if failureRate <= tolerance {
		// 失败率小于等于容忍度，父任务成功
		parentTask.SetStatus(task.TaskStatusSuccess)
		// 标记父任务为已处理
		m.processedNodes.Store(parentTaskID, true)
		log.Printf("✅ WorkflowInstance %s: 父任务 %s 成功（子任务成功数: %d/%d, 失败率: %.2f, 容忍度: %.2f）",
			m.instance.ID, parentTask.GetName(), successCount, totalCount, failureRate, tolerance)

		// 更新就绪任务集合：检查父任务的下游任务是否可以就绪
		m.onTaskCompleted(parentTaskID)
	} else {
		// 失败率超过容忍度，父任务失败
		parentTask.SetStatus(task.TaskStatusFailed)
		// 标记父任务为已处理
		m.processedNodes.Store(parentTaskID, true)
		log.Printf("❌ WorkflowInstance %s: 父任务 %s 失败（子任务成功数: %d/%d, 失败率: %.2f, 容忍度: %.2f）",
			m.instance.ID, parentTask.GetName(), successCount, totalCount, failureRate, tolerance)

		// 更新就绪任务集合：检查父任务的下游任务是否可以就绪（即使父任务失败，下游任务也可能需要处理）
		m.onTaskCompleted(parentTaskID)
	}
}

// onTaskCompleted 任务完成时更新就绪集合（内部方法）
func (m *WorkflowInstanceManager) onTaskCompleted(taskID string) {
	// 从就绪集合中删除已完成的任务
	m.readyTasksSet.Delete(taskID)

	// 如果当前任务是子任务，检查父任务状态
	if task, exists := m.workflow.GetTasks()[taskID]; exists && task.IsSubTask() {
		// 获取父任务ID（通过DAG的GetParents）
		parents, err := m.dag.GetParents(taskID)
		if err == nil && len(parents) > 0 {
			// 子任务通常只有一个父任务
			parentTaskID := parents[0]
			m.checkParentTaskStatus(parentTaskID)
		}
	}

	// 检查下游任务是否可以就绪（包括普通下游任务和子任务）
	children, err := m.dag.GetChildren(taskID)
	if err != nil {
		return
	}

	for _, childID := range children {
		m.checkAndAddToReady(childID)
	}
}

// checkDependencyFailed 检查任务的依赖是否有失败的
// 返回失败的依赖任务名称，如果没有失败的依赖则返回空字符串
func (m *WorkflowInstanceManager) checkDependencyFailed(t workflow.Task) string {
	deps := t.GetDependencies()
	for _, depName := range deps {
		depTaskID, exists := m.workflow.GetTaskIDByName(depName)
		if !exists {
			continue
		}

		// 检查依赖任务是否失败
		if depTask, exists := m.workflow.GetTasks()[depTaskID]; exists {
			if depTask.GetStatus() == task.TaskStatusFailed {
				return depName
			}
		}

		// 也检查 contextData 中的错误信息（用于处理状态未及时更新的情况）
		errorKey := fmt.Sprintf("%s:error", depTaskID)
		if _, hasError := m.contextData.Load(errorKey); hasError {
			return depName
		}
	}
	return ""
}

// getAvailableTasks 获取可执行的任务列表（优化版：使用 readyTasksSet，O(1)访问）
func (m *WorkflowInstanceManager) getAvailableTasks() []workflow.Task {
	var available []workflow.Task

	// 使用读锁保护 Range 操作，确保遍历时的一致性
	m.readyTasksMu.RLock()
	m.readyTasksSet.Range(func(key, value interface{}) bool {
		taskID := key.(string)
		task := value.(workflow.Task)

		// 检查是否已处理 - O(1)
		if _, processed := m.processedNodes.Load(taskID); processed {
			// 跳过已处理的任务，稍后删除
			return true
		}

		// 参数校验 - O(D)，业务逻辑无法避免
		if err := m.validateAndMapParams(task, taskID); err != nil {
			log.Printf("参数校验失败: TaskID=%s, Error=%v", taskID, err)
			return true
		}

		available = append(available, task)
		return true
	})
	m.readyTasksMu.RUnlock()

	// 在锁外删除已处理的任务（避免在 Range 中修改）
	for _, task := range available {
		taskID := task.GetID()
		if _, processed := m.processedNodes.Load(taskID); processed {
			m.readyTasksSet.Delete(taskID)
		}
	}

	return available
}

// validateAndMapParams 校验参数并执行resultMapping（内部方法）
func (m *WorkflowInstanceManager) validateAndMapParams(t workflow.Task, taskID string) error {
	// 使用接口方法获取RequiredParams和ResultMapping，无需类型断言
	requiredParams := t.GetRequiredParams()
	resultMapping := t.GetResultMapping()

	// 1. 检查必需参数
	if len(requiredParams) > 0 {
		// 获取上游任务的结果
		deps := t.GetDependencies()
		allParamsFound := true
		missingParams := make([]string, 0)

		for _, requiredParam := range requiredParams {
			found := false
			// 首先检查当前任务的参数中是否已有
			if t.GetParams()[requiredParam] != nil {
				found = true
			} else {
				// 从上游任务结果中查找
				for _, depName := range deps {
					depTaskID, exists := m.workflow.GetTaskIDByName(depName)
					if !exists {
						continue
					}
					// 从contextData获取上游任务结果
					if upstreamResultValue, exists := m.contextData.Load(depTaskID); exists {
						if upstreamResult, ok := upstreamResultValue.(map[string]interface{}); ok {
							if _, hasKey := upstreamResult[requiredParam]; hasKey {
								found = true
								break
							}
						}
					}
				}
			}

			if !found {
				allParamsFound = false
				missingParams = append(missingParams, requiredParam)
			}
		}

		if !allParamsFound {
			return fmt.Errorf("缺少必需参数: %v", missingParams)
		}
	}

	// 2. 执行resultMapping（从上游结果映射到当前任务参数）
	if len(resultMapping) > 0 {
		deps := t.GetDependencies()
		for targetParam, sourceField := range resultMapping {
			// 从上游任务结果中查找sourceField
			for _, depName := range deps {
				depTaskID, exists := m.workflow.GetTaskIDByName(depName)
				if !exists {
					continue
				}
				// 从contextData获取上游任务结果
				if upstreamResultValue, exists := m.contextData.Load(depTaskID); exists {
					if upstreamResult, ok := upstreamResultValue.(map[string]interface{}); ok {
						if sourceValue, hasKey := upstreamResult[sourceField]; hasKey {
							// 动态注入参数到任务
							// 注意：这里需要更新任务的Params，但workflow.Task是接口，无法直接修改
							// 实际参数注入应该在任务执行时通过contextData传递
							log.Printf("📝 [参数映射] TaskID=%s, 从上游任务 %s 映射字段 %s -> %s, 值=%v", taskID, depTaskID, sourceField, targetParam, sourceValue)
							// 将映射的参数保存到contextData，供任务执行时使用
							// 使用特殊的key格式：taskID:paramName
							paramKey := fmt.Sprintf("%s:%s", taskID, targetParam)
							m.contextData.Store(paramKey, sourceValue)
							break
						}
					}
				}
			}
		}
	}

	return nil
}

// injectCachedResults 从缓存获取上游任务结果并注入参数（内部方法）
// 处理ResultMapping规则：如果存在映射关系，按照映射规则注入；否则检查字段一致性并警告
func (m *WorkflowInstanceManager) injectCachedResults(t workflow.Task, taskID string) {
	if m.resultCache == nil {
		return
	}

	// 使用接口方法获取ResultMapping和RequiredParams，无需类型断言
	resultMapping := t.GetResultMapping()
	requiredParams := t.GetRequiredParams()
	hasResultMapping := len(resultMapping) > 0

	deps := t.GetDependencies()
	for _, depName := range deps {
		depTaskID, exists := m.workflow.GetTaskIDByName(depName)
		if !exists {
			log.Printf("⚠️ WorkflowInstance %s: Task %s 依赖的任务名称 %s 不存在", m.instance.ID, taskID, depName)
			continue
		}

		// 尝试从缓存获取
		cachedResult, found := m.resultCache.Get(depTaskID)
		if !found {
			continue
		}

		// 将缓存结果转换为map[string]interface{}格式
		upstreamResult, ok := cachedResult.(map[string]interface{})
		if !ok {
			// 如果结果不是map类型，直接注入整个结果（向后兼容）
			cacheKey := fmt.Sprintf("_cached_%s", depTaskID)
			if _, exists := t.GetParam(cacheKey); !exists {
				t.SetParam(cacheKey, cachedResult)
				log.Printf("📦 [缓存命中] TaskID=%s, 从缓存获取上游任务 %s 的结果（非map类型）", taskID, depTaskID)
			}
			continue
		}

		// 如果有ResultMapping配置，按照映射规则注入
		if hasResultMapping {
			mappedCount := 0
			missingFields := make([]string, 0)

			for targetParam, sourceField := range resultMapping {
				// 检查上游结果中是否存在sourceField
				if sourceValue, hasKey := upstreamResult[sourceField]; hasKey {
					// 按照映射规则注入参数
					if _, exists := t.GetParam(targetParam); !exists {
						t.SetParam(targetParam, sourceValue)
						mappedCount++
						log.Printf("📦 [缓存映射] TaskID=%s, 从上游任务 %s 映射字段 %s -> %s, 值=%v", taskID, depTaskID, sourceField, targetParam, sourceValue)
					}
				} else {
					// 找不到映射的字段，记录警告
					missingFields = append(missingFields, fmt.Sprintf("%s(映射到%s)", sourceField, targetParam))
				}
			}

			// 如果有找不到的映射字段，发出警告
			if len(missingFields) > 0 {
				log.Printf("⚠️ WorkflowInstance %s: Task %s 的ResultMapping中指定的上游字段在上游任务 %s 的结果中不存在: %v", m.instance.ID, taskID, depTaskID, missingFields)
			}

			// 如果所有映射都成功，记录日志
			if mappedCount > 0 && len(missingFields) == 0 {
				log.Printf("✅ [缓存映射完成] TaskID=%s, 从上游任务 %s 成功映射 %d 个字段", taskID, depTaskID, mappedCount)
			}
		} else {
			// 没有ResultMapping配置，检查是否有依赖但找不到对应字段的情况
			// 这里可以检查RequiredParams，如果存在必需参数但上游结果中没有对应字段，发出警告
			if len(requiredParams) > 0 {
				missingRequiredFields := make([]string, 0)
				for _, requiredParam := range requiredParams {
					// 检查当前任务参数中是否已有
					if t.GetParams()[requiredParam] != nil {
						continue
					}
					// 检查上游结果中是否有该字段
					if _, hasKey := upstreamResult[requiredParam]; !hasKey {
						missingRequiredFields = append(missingRequiredFields, requiredParam)
					}
				}
				if len(missingRequiredFields) > 0 {
					log.Printf("⚠️ WorkflowInstance %s: Task %s 的必需参数在上游任务 %s 的结果中不存在: %v (建议配置ResultMapping)", m.instance.ID, taskID, depTaskID, missingRequiredFields)
				}
			}

			// 向后兼容：如果没有ResultMapping，注入整个结果（使用特殊前缀）
			cacheKey := fmt.Sprintf("_cached_%s", depTaskID)
			if _, exists := t.GetParam(cacheKey); !exists {
				t.SetParam(cacheKey, cachedResult)
				log.Printf("📦 [缓存命中] TaskID=%s, 从缓存获取上游任务 %s 的结果", taskID, depTaskID)
			}
		}
	}
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

	// 还需要检查是否有任务在就绪任务集合中但未处理
	hasUnprocessedReady := false
	m.readyTasksSet.Range(func(key, value interface{}) bool {
		taskID := key.(string)
		if _, processed := m.processedNodes.Load(taskID); !processed {
			hasUnprocessedReady = true
			return false // 停止遍历
		}
		return true
	})

	// 如果有未处理的就绪任务，说明还有任务未完成
	if hasUnprocessedReady {
		return false
	}

	return true
}

// recoverPendingTasks 恢复那些在数据库中但不在candidateNodes中的Pending/Failed任务
// 主要用于以下场景：
// 1. 系统崩溃恢复：重启后恢复WorkflowInstance时，需要恢复未完成的任务
// 2. 任务提交失败恢复：任务已保存到数据库但提交到Executor失败，需要重试
// 3. 状态不一致恢复：processedNodes和数据库状态不一致时的恢复
// 注意：只恢复预定义的任务（在workflow中存在的任务），动态生成的子任务不在数据库中，不需要恢复
func (m *WorkflowInstanceManager) recoverPendingTasks() {
	ctx := context.Background()
	taskInstances, err := m.taskRepo.GetByWorkflowInstanceID(ctx, m.instance.ID)
	if err != nil {
		log.Printf("⚠️ WorkflowInstance %s: 查询任务实例失败: %v", m.instance.ID, err)
		return
	}

	pendingCount := 0
	recoveredCount := 0
	skippedProcessed := 0
	skippedInQueue := 0
	skippedNotInWorkflow := 0
	skippedDepsNotMet := 0

	for _, ti := range taskInstances {
		// 处理Pending或Failed状态的任务（Failed可能是提交失败后需要重试的）
		if ti.Status != "Pending" && ti.Status != "Failed" {
			continue
		}

		pendingCount++
		taskID := ti.ID

		// 检查是否已处理
		// 优化：减少数据库查询，只在真正需要时才查询
		// 如果任务在processedNodes中，通常说明任务已经被处理或正在处理中
		// 只有在状态是Pending且被标记为已处理时，才需要进一步检查（这种情况很少见）
		if _, processed := m.processedNodes.Load(taskID); processed {
			// 如果状态不是Pending，说明任务已经完成或失败，正常情况，跳过
			if ti.Status != "Pending" {
				skippedProcessed++
				continue
			}
			// 状态是Pending但被标记为已处理，这是异常情况，但为了性能，我们直接跳过
			// 因为这种情况很少见，而且频繁查询数据库会导致性能问题
			// 如果真的需要恢复，可以通过其他机制（如定期批量检查）来处理
			skippedProcessed++
			continue
		}

		// 检查是否已在就绪任务集合
		if _, exists := m.readyTasksSet.Load(taskID); exists {
			skippedInQueue++
			continue
		}

		// 从workflow中获取任务定义
		// 注意：只恢复预定义的任务，动态生成的子任务不在数据库中，所以这里应该总是能找到
		t, exists := m.workflow.GetTasks()[taskID]
		if !exists {
			// 任务不在workflow中，这不应该发生（因为所有预定义任务都在workflow中）
			// 可能是数据不一致或动态任务（动态任务不应该在数据库中）
			skippedNotInWorkflow++
			log.Printf("⚠️ WorkflowInstance %s: Pending任务 %s (%s) 不在Workflow中，跳过恢复（可能是数据不一致）",
				m.instance.ID, taskID, ti.Name)
			continue
		}

		// 检查所有依赖是否都已处理
		deps := t.GetDependencies()
		allDepsProcessed := true
		missingDeps := make([]string, 0)
		for _, depName := range deps {
			depTaskID, exists := m.workflow.GetTaskIDByName(depName)
			if !exists {
				allDepsProcessed = false
				missingDeps = append(missingDeps, fmt.Sprintf("%s(未找到)", depName))
				break
			}
			if _, processed := m.processedNodes.Load(depTaskID); !processed {
				allDepsProcessed = false
				missingDeps = append(missingDeps, fmt.Sprintf("%s(未完成)", depName))
			}
		}

		// 如果依赖已满足，添加到候选队列
		if allDepsProcessed {
			// 再次检查任务是否已被处理（防止并发问题）
			if _, processed := m.processedNodes.Load(taskID); processed {
				skippedProcessed++
				log.Printf("⚠️ WorkflowInstance %s: 任务 %s (%s) 在恢复过程中被标记为已处理，跳过", m.instance.ID, taskID, ti.Name)
				continue
			}
			// 再次检查任务是否已在就绪任务集合（防止并发问题）
			if _, exists := m.readyTasksSet.Load(taskID); exists {
				skippedInQueue++
				log.Printf("⚠️ WorkflowInstance %s: 任务 %s (%s) 在恢复过程中被添加到就绪任务集合，跳过", m.instance.ID, taskID, ti.Name)
				continue
			}
			// 添加到就绪任务集合
			m.readyTasksSet.Store(taskID, t)
			recoveredCount++
			// 如果任务状态是Failed，重置为Pending以便重试
			if ti.Status == "Failed" {
				_ = m.taskRepo.UpdateStatus(ctx, taskID, "Pending")
				log.Printf("✅ WorkflowInstance %s: 恢复Failed任务 %s (%s) 到就绪任务集合并重置为Pending", m.instance.ID, taskID, ti.Name)
			} else {
				log.Printf("✅ WorkflowInstance %s: 恢复Pending任务 %s (%s) 到就绪任务集合", m.instance.ID, taskID, ti.Name)
			}
		} else {
			skippedDepsNotMet++
			log.Printf("⚠️ WorkflowInstance %s: %s任务 %s (%s) 依赖未满足: %v，跳过恢复",
				m.instance.ID, ti.Status, taskID, ti.Name, missingDeps)
		}
	}

	if pendingCount > 0 {
		log.Printf("📊 WorkflowInstance %s: recoverPendingTasks统计 - Pending/Failed任务总数: %d, 已恢复: %d, 已处理: %d, 已在队列: %d, 不在Workflow: %d, 依赖未满足: %d",
			m.instance.ID, pendingCount, recoveredCount, skippedProcessed, skippedInQueue, skippedNotInWorkflow, skippedDepsNotMet)
	}
}

// saveAllTaskStatuses 批量保存所有任务状态到数据库（只保存预定义任务，跳过动态任务）
func (m *WorkflowInstanceManager) saveAllTaskStatuses(ctx context.Context) {
	// 获取所有任务（包括动态任务）
	allTasks := m.workflow.GetTasks()
	savedCount := 0
	skippedCount := 0

	// 从数据库获取所有任务实例，建立映射
	taskInstances, err := m.taskRepo.GetByWorkflowInstanceID(ctx, m.instance.ID)
	if err != nil {
		log.Printf("⚠️ WorkflowInstance %s: 查询任务实例失败: %v", m.instance.ID, err)
		return
	}
	taskInstanceMap := make(map[string]*storage.TaskInstance)
	for _, ti := range taskInstances {
		taskInstanceMap[ti.ID] = ti
	}

	for taskID, workflowTask := range allTasks {
		// 使用IsSubTask()判断是否是动态生成的子任务（不保存到数据库）
		if workflowTask.IsSubTask() {
			// 动态生成的子任务，跳过保存
			skippedCount++
			continue
		}

		// 检查任务是否在数据库中（预定义任务）
		existingTask, exists := taskInstanceMap[taskID]
		if !exists {
			// 如果任务不存在于数据库，可能是数据不一致，记录日志但跳过
			log.Printf("⚠️ WorkflowInstance %s: Task %s 不在数据库中，跳过保存", m.instance.ID, taskID)
			skippedCount++
			continue
		}

		// 获取任务当前状态（从workflow.Task获取）
		currentStatus := workflowTask.GetStatus()
		if currentStatus == "" {
			// 如果workflow.Task没有状态，检查是否已处理
			if _, processed := m.processedNodes.Load(taskID); processed {
				// 已处理，检查是否有错误信息（从contextData获取，使用特殊key）
				errorKey := fmt.Sprintf("%s:error", taskID)
				if _, hasError := m.contextData.Load(errorKey); hasError {
					// 有错误信息，状态为Failed
					currentStatus = "Failed"
				} else {
					// 没有错误信息，状态为Success
					currentStatus = "Success"
				}
			} else {
				// 未处理，保持数据库中的状态
				continue
			}
		}

		// 如果状态没有变化，跳过
		if existingTask.Status == currentStatus {
			continue
		}

		// 更新任务状态
		var updateErr error
		if currentStatus == "Failed" {
			// 如果是失败状态，尝试获取错误信息
			errorKey := fmt.Sprintf("%s:error", taskID)
			errorMsg := ""
			if errorValue, hasError := m.contextData.Load(errorKey); hasError {
				if errStr, ok := errorValue.(string); ok {
					errorMsg = errStr
				}
			}
			updateErr = m.taskRepo.UpdateStatusWithError(ctx, taskID, currentStatus, errorMsg)
		} else {
			updateErr = m.taskRepo.UpdateStatus(ctx, taskID, currentStatus)
		}

		if updateErr != nil {
			log.Printf("⚠️ WorkflowInstance %s: 更新任务状态失败: TaskID=%s, Status=%s, Error=%v",
				m.instance.ID, taskID, currentStatus, updateErr)
		} else {
			savedCount++
		}
	}

	if savedCount > 0 || skippedCount > 0 {
		log.Printf("📊 WorkflowInstance %s: 批量保存任务状态完成 - 已保存: %d, 跳过动态任务: %d",
			m.instance.ID, savedCount, skippedCount)
	}
}

// CreateBreakpoint 创建断点数据（公共方法，实现接口）
func (m *WorkflowInstanceManager) CreateBreakpoint() interface{} {
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

	// 将 sync.Map 转换为 map[string]interface{} 用于序列化
	contextDataMap := make(map[string]interface{})
	m.contextData.Range(func(key, value interface{}) bool {
		if keyStr, ok := key.(string); ok {
			contextDataMap[keyStr] = value
		}
		return true
	})

	return &workflow.BreakpointData{
		CompletedTaskNames: completedTaskNames,
		RunningTaskNames:   runningTaskNames,
		DAGSnapshot:        dagSnapshot,
		ContextData:        contextDataMap,
		LastUpdateTime:     time.Now(),
	}
}

// GetControlSignalChannelTyped 获取控制信号通道（类型化版本，供内部使用）
func (m *WorkflowInstanceManager) GetControlSignalChannelTyped() chan<- workflow.ControlSignal {
	return m.controlSignalChan
}

// RestoreFromBreakpoint 从断点数据恢复WorkflowInstance状态（公共方法，实现接口）
func (m *WorkflowInstanceManager) RestoreFromBreakpoint(breakpoint interface{}) error {
	if breakpoint == nil {
		return nil
	}

	// 类型转换：从 interface{} 转换为 *workflow.BreakpointData
	bp, ok := breakpoint.(*workflow.BreakpointData)
	if !ok {
		return fmt.Errorf("断点数据类型错误，期望 *workflow.BreakpointData")
	}

	// 1. 恢复已完成的Task列表
	m.processedNodes = sync.Map{}
	for _, taskName := range bp.CompletedTaskNames {
		if taskID, exists := m.workflow.GetTaskIDByName(taskName); exists {
			m.processedNodes.Store(taskID, true)
		}
	}

	// 2. 恢复上下文数据
	m.contextData = sync.Map{}
	if bp.ContextData != nil {
		for k, v := range bp.ContextData {
			m.contextData.Store(k, v)
		}
	}

	// 3. 重新初始化就绪任务集合（基于已完成的Task）
	// 清空现有的就绪任务集合
	m.readyTasksMu.Lock()
	m.readyTasksSet = sync.Map{}
	m.readyTasksMu.Unlock()

	// 使用 DAG 获取当前就绪的任务（基于已完成的节点，DAG 会自动计算入度）
	readyTasks := m.dag.GetReadyTasks()
	for _, taskID := range readyTasks {
		// 检查是否已处理
		if _, processed := m.processedNodes.Load(taskID); !processed {
			// 检查所有父节点是否都已处理（双重验证，确保恢复的正确性）
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
						m.readyTasksSet.Store(taskID, t)
					}
				}
			}
		}
	}

	return nil
}

// createTaskCompleteHandler 创建任务完成处理器
func (m *WorkflowInstanceManager) createTaskCompleteHandler(taskID string) func(*executor.TaskResult) {
	return func(result *executor.TaskResult) {
		// 更新workflow.Task的状态为Success
		if workflowTask, exists := m.workflow.GetTasks()[taskID]; exists {
			workflowTask.SetStatus(task.TaskStatusSuccess)
		}

		// 标记任务为已处理（不更新数据库，只在instance完成/保存breakpoint/被取消时批量保存）
		m.processedNodes.Store(taskID, true)

		// 执行Task的状态Handler（Success状态）
		if m.registry != nil {
			// 从Workflow中获取Task配置（包含StatusHandlers）
			workflowTask, exists := m.workflow.GetTasks()[taskID]
			if !exists {
				return
			}

			// 优化：直接使用workflowTask的信息，避免数据库查询
			// workflowTask已经包含了所有需要的信息（ID, Name, JobFuncID, Params等）
			statusHandlers := workflowTask.GetStatusHandlers()

			// 创建task.Task实例用于handler调用
			// 使用workflowTask的信息，而不是从数据库加载
			taskObj := task.NewTask(workflowTask.GetName(), workflowTask.GetDescription(), workflowTask.GetJobFuncID(), workflowTask.GetParams(), statusHandlers)
			taskObj.SetID(workflowTask.GetID())
			taskObj.SetJobFuncName(workflowTask.GetJobFuncName())
			taskObj.SetTimeoutSeconds(workflowTask.GetTimeoutSeconds())
			taskObj.SetRetryCount(workflowTask.GetRetryCount())
			taskObj.SetDependencies(workflowTask.GetDependencies())
			taskObj.SetStatus(task.TaskStatusSuccess) // 使用当前状态（Success）

			// 在调用Handler之前，将Manager接口注入到registry的依赖中
			// 这样Handler可以直接通过ctx.GetDependency("InstanceManager")获取Manager，而不需要Engine
			managerInterface := &InstanceManagerInterface{
				manager: m,
			}
			_ = m.registry.RegisterDependencyWithKey("InstanceManager", managerInterface)

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
		// 注意：DAG 的入度是自动管理的，当任务完成时，下游节点的入度会自动更新
		// m.dag.UpdateInDegree(taskID)

		// 如果当前任务是子任务，更新父任务的子任务统计信息
		if workflowTask, exists := m.workflow.GetTasks()[taskID]; exists && workflowTask.IsSubTask() {
			m.updateParentSubTaskStats(taskID, true) // true表示成功
		}

		// 更新就绪任务集合：从集合中删除已完成的任务，并检查下游任务是否可以就绪
		m.onTaskCompleted(taskID)

		// 保存结果数据到上下文
		if result.Data != nil {
			m.contextData.Store(taskID, result.Data)
			// 缓存结果（TTL默认1小时）
			if m.resultCache != nil {
				ttl := 1 * time.Hour
				if err := m.resultCache.Set(taskID, result.Data, ttl); err != nil {
					log.Printf("缓存任务结果失败: TaskID=%s, Error=%v", taskID, err)
				} else {
					log.Printf("✅ [缓存保存] TaskID=%s, 结果已缓存", taskID)
				}
			}
		}
	}
}

// createTaskErrorHandler 创建任务错误处理器
func (m *WorkflowInstanceManager) createTaskErrorHandler(taskID string) func(error) {
	return func(err error) {
		ctx := context.Background()

		// 保存错误信息到contextData（用于批量保存时获取错误信息）
		errorKey := fmt.Sprintf("%s:error", taskID)
		m.contextData.Store(errorKey, err.Error())

		// 更新workflow.Task的状态为Failed
		if workflowTask, exists := m.workflow.GetTasks()[taskID]; exists {
			workflowTask.SetStatus(task.TaskStatusFailed)
		}

		// 标记任务为已处理（不更新数据库，只在instance完成/保存breakpoint/被取消时批量保存）
		m.processedNodes.Store(taskID, true)

		// 执行Task的状态Handler（Failed状态）
		if m.registry != nil {
			// 从Workflow中获取Task配置（包含StatusHandlers）
			workflowTask, exists := m.workflow.GetTasks()[taskID]
			if !exists {
				return
			}

			// 优化：直接使用workflowTask的信息，避免数据库查询
			// workflowTask已经包含了所有需要的信息（ID, Name, JobFuncID, Params等）
			statusHandlers := workflowTask.GetStatusHandlers()

			// 创建task.Task实例用于handler调用
			// 使用workflowTask的信息，而不是从数据库加载
			taskObj := task.NewTask(workflowTask.GetName(), workflowTask.GetDescription(), workflowTask.GetJobFuncID(), workflowTask.GetParams(), statusHandlers)
			taskObj.SetID(workflowTask.GetID())
			taskObj.SetJobFuncName(workflowTask.GetJobFuncName())
			taskObj.SetTimeoutSeconds(workflowTask.GetTimeoutSeconds())
			taskObj.SetRetryCount(workflowTask.GetRetryCount())
			taskObj.SetDependencies(workflowTask.GetDependencies())
			taskObj.SetStatus(task.TaskStatusFailed) // 使用当前状态（Failed）

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

		// 如果当前任务是子任务，更新父任务的子任务统计信息
		if workflowTask, exists := m.workflow.GetTasks()[taskID]; exists && workflowTask.IsSubTask() {
			m.updateParentSubTaskStats(taskID, false) // false表示失败
		}

		// 更新就绪任务集合：从集合中删除已失败的任务，并检查下游任务是否可以就绪
		// 注意：失败的任务也会标记为已处理，但下游任务仍然可以继续执行（如果依赖已满足）
		m.onTaskCompleted(taskID)

		// 标记WorkflowInstance为Failed
		m.mu.Lock()
		m.instance.Status = "Failed"
		m.instance.ErrorMessage = err.Error()
		now := time.Now()
		m.instance.EndTime = &now
		m.mu.Unlock()

		// 批量保存所有任务状态（包括失败的任务）
		m.saveAllTaskStatuses(ctx)
		m.workflowInstanceRepo.UpdateStatus(ctx, m.instance.ID, "Failed")
	}
}

// GetControlSignalChannel 获取控制信号通道（公共方法，实现接口）
func (m *WorkflowInstanceManager) GetControlSignalChannel() interface{} {
	return m.controlSignalChan
}

// GetStatusUpdateChannel 获取状态更新通道（公共方法，实现接口）
// 用于Engine转发状态更新到Controller
func (m *WorkflowInstanceManager) GetStatusUpdateChannel() <-chan string {
	return m.statusUpdateChan
}

// AddSubTask 动态添加子任务到WorkflowInstance（公共方法，实现接口）
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

	// 如果子任务是*task.Task类型，设置isSubTask标志
	subTask.SetSubTask(true)

	// 使用Workflow的AddSubTask方法（线程安全）
	if err := m.workflow.AddSubTask(subTask, parentTaskID); err != nil {
		return err
	}

	// 3. 更新DAG依赖关系（重构：父任务-子任务-下游任务）
	// 获取父任务的所有下游任务
	parentNode, exists := m.dag.GetNode(parentTaskID)
	if exists {
		downstreamTaskIDs := parentNode.OutEdges
		if len(downstreamTaskIDs) > 0 {
			// 需要重构依赖关系：
			// 1. 删除父任务到下游任务的直接依赖（在Workflow.Dependencies中）
			// 2. 添加子任务到下游任务的依赖
			for _, downstreamID := range downstreamTaskIDs {
				// 从Workflow.Dependencies中删除父任务到下游任务的依赖
				depsValue, exists := m.workflow.Dependencies.Load(downstreamID)
				if exists {
					deps := depsValue.([]string)
					newDeps := make([]string, 0, len(deps))
					for _, depID := range deps {
						if depID != parentTaskID {
							newDeps = append(newDeps, depID)
						}
					}
					m.workflow.Dependencies.Store(downstreamID, newDeps)
				}
				// 添加子任务到下游任务的依赖（在Workflow.Dependencies中）
				depsValue2, _ := m.workflow.Dependencies.LoadOrStore(downstreamID, make([]string, 0))
				deps := depsValue2.([]string)
				// 检查是否已存在
				found := false
				for _, depID := range deps {
					if depID == subTask.GetID() {
						found = true
						break
					}
				}
				if !found {
					// 创建新的依赖列表（避免并发修改）
					newDeps := make([]string, len(deps), len(deps)+1)
					copy(newDeps, deps)
					newDeps = append(newDeps, subTask.GetID())
					m.workflow.Dependencies.Store(downstreamID, newDeps)
				}
			}
		}
	}

	// 4. 更新DAG，添加子任务节点和依赖关系
	// 注意：由于go-dag是只读的，我们需要重新构建DAG
	// 但为了性能，我们只添加新节点，依赖关系通过Workflow.Dependencies管理
	if err := m.dag.AddNode(subTask.GetID(), subTask.GetName(), subTask, []string{parentTaskID}); err != nil {
		// 如果DAG添加失败，回滚Workflow的更改
		m.workflow.Tasks.Delete(subTask.GetID())
		m.workflow.Dependencies.Delete(subTask.GetID())
		// 回滚下游任务的依赖关系
		if exists {
			for _, downstreamID := range parentNode.OutEdges {
				// 恢复父任务到下游任务的依赖
				depsValue, _ := m.workflow.Dependencies.LoadOrStore(downstreamID, make([]string, 0))
				deps := depsValue.([]string)
				// 检查是否已存在
				found := false
				for _, depID := range deps {
					if depID == parentTaskID {
						found = true
						break
					}
				}
				if !found {
					// 创建新的依赖列表（避免并发修改）
					newDeps := make([]string, len(deps), len(deps)+1)
					copy(newDeps, deps)
					newDeps = append(newDeps, parentTaskID)
					m.workflow.Dependencies.Store(downstreamID, newDeps)
				}
				// 删除子任务到下游任务的依赖
				depsValue, _ = m.workflow.Dependencies.Load(downstreamID)
				deps = depsValue.([]string)
				newDeps := make([]string, 0, len(deps))
				for _, depID := range deps {
					if depID != subTask.GetID() {
						newDeps = append(newDeps, depID)
					}
				}
				m.workflow.Dependencies.Store(downstreamID, newDeps)
			}
		}
		return fmt.Errorf("添加子任务到DAG失败: %w", err)
	}

	// 5. 由于go-dag是只读的，我们需要重新构建DAG以反映新的依赖关系
	// 但为了性能，我们只在必要时重新构建
	// 这里我们通过更新Workflow.Dependencies来管理依赖关系，DAG会在下次需要时重新构建

	// 增加父任务的子任务总数
	m.incrementParentSubTaskTotal(parentTaskID)

	// 4. 检查子任务的依赖是否已满足，如果满足则加入候选队列
	// 子任务通过AddSubTask添加，其依赖关系存储在Workflow.Dependencies中
	// 需要检查父任务和子任务通过GetDependencies()声明的其他依赖是否都已完成
	allDepsProcessed := true

	// 首先检查父任务是否已完成（子任务必须依赖父任务）
	if _, processed := m.processedNodes.Load(parentTaskID); !processed {
		allDepsProcessed = false
	}

	// 然后检查子任务通过GetDependencies()声明的其他依赖（如果有）
	if allDepsProcessed {
		subTaskDeps := subTask.GetDependencies()
		for _, depName := range subTaskDeps {
			depTaskID, exists := m.workflow.GetTaskIDByName(depName)
			if !exists {
				allDepsProcessed = false
				break
			}
			// 检查依赖是否已处理（通过processedNodes）
			if _, processed := m.processedNodes.Load(depTaskID); !processed {
				allDepsProcessed = false
				break
			}
		}
	}

	// 如果子任务的依赖已满足，加入就绪任务集合
	if allDepsProcessed {
		m.readyTasksSet.Store(subTask.GetID(), subTask)
		log.Printf("WorkflowInstance %s: 子任务 %s 已添加，依赖已满足，加入就绪任务集合", m.instance.ID, subTask.GetName())
	} else {
		log.Printf("WorkflowInstance %s: 子任务 %s 已添加，等待依赖满足（父任务: %s）", m.instance.ID, subTask.GetName(), parentTaskID)
	}

	return nil
}

// AtomicAddSubTasks 原子性地添加多个子任务到WorkflowInstance（公共方法，实现接口）
// 保证要么全部成功，要么全部失败（回滚）
func (m *WorkflowInstanceManager) AtomicAddSubTasks(subTasks []types.Task, parentTaskID string) error {
	if len(subTasks) == 0 {
		return nil // 空列表，直接返回成功
	}

	// 验证所有子任务
	for i, subTask := range subTasks {
		if subTask == nil {
			return fmt.Errorf("子任务[%d]不能为空", i)
		}
		if subTask.GetID() == "" {
			return fmt.Errorf("子任务[%d] ID不能为空", i)
		}
		if subTask.GetName() == "" {
			return fmt.Errorf("子任务[%d]名称不能为空", i)
		}
	}

	// 类型转换：types.Task -> workflow.Task
	workflowTasks := make([]workflow.Task, 0, len(subTasks))
	for _, subTask := range subTasks {
		workflowTask, ok := subTask.(workflow.Task)
		if !ok {
			return fmt.Errorf("子任务类型转换失败: TaskID=%s", subTask.GetID())
		}
		workflowTasks = append(workflowTasks, workflowTask)
	}

	// 记录已添加的子任务，用于回滚
	addedSubTasks := make([]workflow.Task, 0, len(workflowTasks))
	addedToDAG := make([]string, 0, len(workflowTasks)) // 记录已添加到DAG的任务ID

	// 第一步：添加所有子任务到Workflow（如果失败，回滚）
	for _, subTask := range workflowTasks {
		// 设置isSubTask标志
		subTask.SetSubTask(true)

		// 使用Workflow的AddSubTask方法（线程安全）
		if err := m.workflow.AddSubTask(subTask, parentTaskID); err != nil {
			// 回滚已添加的子任务
			for _, addedTask := range addedSubTasks {
				m.workflow.Tasks.Delete(addedTask.GetID())
				m.workflow.Dependencies.Delete(addedTask.GetID())
				// 从TaskNameIndex中删除
				if taskName := addedTask.GetName(); taskName != "" {
					m.workflow.TaskNameIndex.Delete(taskName)
				}
			}
			return fmt.Errorf("添加子任务到Workflow失败: %w", err)
		}
		addedSubTasks = append(addedSubTasks, subTask)
	}

	// 第二步：更新DAG依赖关系（重构：父任务-子任务-下游任务）
	parentNode, exists := m.dag.GetNode(parentTaskID)
	if exists {
		downstreamTaskIDs := parentNode.OutEdges
		if len(downstreamTaskIDs) > 0 {
			// 需要重构依赖关系：
			// 1. 删除父任务到下游任务的直接依赖（在Workflow.Dependencies中）
			// 2. 添加所有子任务到下游任务的依赖
			for _, downstreamID := range downstreamTaskIDs {
				// 从Workflow.Dependencies中删除父任务到下游任务的依赖
				depsValue, exists := m.workflow.Dependencies.Load(downstreamID)
				if exists {
					deps := depsValue.([]string)
					newDeps := make([]string, 0, len(deps))
					for _, depID := range deps {
						if depID != parentTaskID {
							newDeps = append(newDeps, depID)
						}
					}
					m.workflow.Dependencies.Store(downstreamID, newDeps)
				}

				// 添加所有子任务到下游任务的依赖（在Workflow.Dependencies中）
				depsValue2, _ := m.workflow.Dependencies.LoadOrStore(downstreamID, make([]string, 0))
				deps := depsValue2.([]string)
				existingDeps := make(map[string]bool)
				for _, depID := range deps {
					existingDeps[depID] = true
				}

				// 添加所有子任务ID到依赖列表（如果不存在）
				newDeps := make([]string, 0, len(deps)+len(workflowTasks))
				copy(newDeps, deps)
				for _, subTask := range workflowTasks {
					subTaskID := subTask.GetID()
					if !existingDeps[subTaskID] {
						newDeps = append(newDeps, subTaskID)
						existingDeps[subTaskID] = true
					}
				}
				m.workflow.Dependencies.Store(downstreamID, newDeps)
			}
		}
	}

	// 第三步：更新DAG，添加所有子任务节点和依赖关系
	// 如果任何子任务添加失败，回滚所有已添加的子任务
	for _, subTask := range workflowTasks {
		if err := m.dag.AddNode(subTask.GetID(), subTask.GetName(), subTask, []string{parentTaskID}); err != nil {
			// 回滚DAG：删除已添加到DAG的节点
			for _, addedTaskID := range addedToDAG {
				// 注意：DAG可能不支持删除节点，这里我们只回滚Workflow的更改
				_ = addedTaskID
			}

			// 回滚Workflow的更改
			for _, addedTask := range addedSubTasks {
				m.workflow.Tasks.Delete(addedTask.GetID())
				m.workflow.Dependencies.Delete(addedTask.GetID())
				// 从TaskNameIndex中删除
				if taskName := addedTask.GetName(); taskName != "" {
					m.workflow.TaskNameIndex.Delete(taskName)
				}
			}

			// 回滚下游任务的依赖关系
			if exists {
				for _, downstreamID := range parentNode.OutEdges {
					// 恢复父任务到下游任务的依赖
					depsValue, _ := m.workflow.Dependencies.LoadOrStore(downstreamID, make([]string, 0))
					deps := depsValue.([]string)
					found := false
					for _, depID := range deps {
						if depID == parentTaskID {
							found = true
							break
						}
					}
					if !found {
						newDeps := make([]string, len(deps), len(deps)+1)
						copy(newDeps, deps)
						newDeps = append(newDeps, parentTaskID)
						m.workflow.Dependencies.Store(downstreamID, newDeps)
					}

					// 删除所有子任务到下游任务的依赖
					depsValue, _ = m.workflow.Dependencies.Load(downstreamID)
					deps = depsValue.([]string)
					newDeps := make([]string, 0, len(deps))
					for _, depID := range deps {
						isSubTaskID := false
						for _, subTask := range workflowTasks {
							if depID == subTask.GetID() {
								isSubTaskID = true
								break
							}
						}
						if !isSubTaskID {
							newDeps = append(newDeps, depID)
						}
					}
					m.workflow.Dependencies.Store(downstreamID, newDeps)
				}
			}

			return fmt.Errorf("添加子任务 %s 到DAG失败: %w", subTask.GetID(), err)
		}
		addedToDAG = append(addedToDAG, subTask.GetID())
	}

	// 第四步：增加父任务的子任务总数（每个子任务都增加一次）
	for range workflowTasks {
		m.incrementParentSubTaskTotal(parentTaskID)
	}

	// 第五步：检查每个子任务的依赖是否已满足，如果满足则加入就绪任务集合
	allDepsProcessed := true
	if _, processed := m.processedNodes.Load(parentTaskID); !processed {
		allDepsProcessed = false
	}

	// 检查子任务通过GetDependencies()声明的其他依赖（如果有）
	if allDepsProcessed {
		for _, subTask := range workflowTasks {
			subTaskDeps := subTask.GetDependencies()
			for _, depName := range subTaskDeps {
				depTaskID, exists := m.workflow.GetTaskIDByName(depName)
				if !exists {
					allDepsProcessed = false
					break
				}
				if _, processed := m.processedNodes.Load(depTaskID); !processed {
					allDepsProcessed = false
					break
				}
			}
			if !allDepsProcessed {
				break
			}
		}
	}

	// 如果所有子任务的依赖都已满足，批量加入就绪任务集合
	if allDepsProcessed {
		for _, subTask := range workflowTasks {
			m.readyTasksSet.Store(subTask.GetID(), subTask)
		}
		log.Printf("WorkflowInstance %s: 批量添加 %d 个子任务，依赖已满足，已加入就绪任务集合", m.instance.ID, len(workflowTasks))
	} else {
		log.Printf("WorkflowInstance %s: 批量添加 %d 个子任务，等待依赖满足（父任务: %s）", m.instance.ID, len(workflowTasks), parentTaskID)
	}

	return nil
}

// Shutdown 优雅关闭WorkflowInstanceManager（公共方法，实现接口）
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

// GetInstanceID 获取WorkflowInstance ID（公共方法，实现接口）
func (m *WorkflowInstanceManager) GetInstanceID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.instance.ID
}

// GetStatus 获取WorkflowInstance状态（公共方法，实现接口）
func (m *WorkflowInstanceManager) GetStatus() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.instance.Status
}

// Context 获取context（公共方法，实现接口）
func (m *WorkflowInstanceManager) Context() context.Context {
	return m.ctx
}
