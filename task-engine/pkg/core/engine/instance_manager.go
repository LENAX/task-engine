package engine

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/stevelan1995/task-engine/pkg/core/cache"
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
	processedNodes       sync.Map // 已处理的Task ID -> bool
	candidateNodes       sync.Map // 候选Task ID -> workflow.Task
	contextData          sync.Map // Task间传递的数据
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

	// 初始化candidateNodes（根节点，入度为0的Task）
	readyTasks := dagInstance.GetReadyTasks()
	addedCount := 0
	for _, taskID := range readyTasks {
		if t, exists := wf.GetTasks()[taskID]; exists {
			manager.candidateNodes.Store(taskID, t)
			addedCount++
		} else {
			log.Printf("⚠️ WorkflowInstance %s: 初始化时发现任务 %s 在DAG中但不在Workflow中", instance.ID, taskID)
		}
	}

	// 验证初始化：检查所有任务是否都被正确添加到 candidateNodes
	totalTasks := len(wf.GetTasks())
	missingTasks := make([]string, 0)

	// 检查所有没有依赖的任务是否都被添加到 candidateNodes
	for taskID, t := range wf.GetTasks() {
		deps := t.GetDependencies()
		// 如果没有依赖，应该是根节点，应该被添加到 candidateNodes
		if len(deps) == 0 {
			if _, exists := manager.candidateNodes.Load(taskID); !exists {
				missingTasks = append(missingTasks, fmt.Sprintf("%s (%s)", taskID, t.GetName()))
			}
		}
	}

	if len(missingTasks) > 0 {
		log.Printf("⚠️ WorkflowInstance %s: 初始化验证失败，发现 %d 个根节点任务未被添加到 candidateNodes: %v",
			instance.ID, len(missingTasks), missingTasks)
		// 尝试恢复这些任务
		for _, taskID := range readyTasks {
			if t, exists := wf.GetTasks()[taskID]; exists {
				// 检查是否真的不在 candidateNodes 中
				if _, exists := manager.candidateNodes.Load(taskID); !exists {
					manager.candidateNodes.Store(taskID, t)
					log.Printf("✅ WorkflowInstance %s: 恢复任务 %s (%s) 到 candidateNodes", instance.ID, taskID, t.GetName())
				}
			}
		}
		// 再次检查所有没有依赖的任务
		for taskID, t := range wf.GetTasks() {
			deps := t.GetDependencies()
			if len(deps) == 0 {
				if _, exists := manager.candidateNodes.Load(taskID); !exists {
					manager.candidateNodes.Store(taskID, t)
					log.Printf("✅ WorkflowInstance %s: 补充添加任务 %s (%s) 到 candidateNodes", instance.ID, taskID, t.GetName())
				}
			}
		}
	}

	log.Printf("✅ WorkflowInstance %s: 初始化完成，总任务数: %d, 就绪任务数: %d, 已添加到 candidateNodes: %d",
		instance.ID, totalTasks, len(readyTasks), addedCount)

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
				// 检查是否有任务在数据库中但不在candidateNodes中
				// 这可能是由于初始化时的问题或任务被提前创建到数据库
				m.recoverPendingTasks()

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
				taskName := t.GetName()

				// 再次检查任务是否已被处理（防止并发问题：任务在执行过程中被标记为已处理）
				if _, processed := m.processedNodes.Load(taskID); processed {
					// 任务已被处理，从candidateNodes中删除并跳过
					m.candidateNodes.Delete(taskID)
					// 减少日志写入频率，只在必要时记录
					// #region agent log
					// logFile, _ := os.OpenFile("/Users/stevelan/Desktop/projects/task-engine/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
					// if logFile != nil {
					// 	fmt.Fprintf(logFile, `{"timestamp":%d,"location":"instance_manager.go:245","message":"任务提交前发现已处理，跳过","data":{"instanceID":"%s","taskID":"%s","taskName":"%s"},"sessionId":"debug-session","runId":"run1","hypothesisId":"F"}`+"\n", time.Now().UnixMilli(), m.instance.ID, taskID, taskName)
					// 	logFile.Close()
					// }
					// #endregion
					continue
				}

				// 先不标记为已处理，等成功提交后再标记
				// 这样如果提交失败，任务还在 candidateNodes 中，可以重试

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

				// 从缓存获取上游任务结果并注入参数
				if m.resultCache != nil {
					deps := t.GetDependencies()
					for _, depName := range deps {
						depTaskID := m.findTaskIDByName(depName)
						if depTaskID == "" {
							continue
						}
						// 尝试从缓存获取
						if cachedResult, found := m.resultCache.Get(depTaskID); found {
							// 将缓存结果注入到参数中（使用特殊前缀）
							paramsAny[fmt.Sprintf("_cached_%s", depTaskID)] = cachedResult
							log.Printf("📦 [缓存命中] TaskID=%s, 从缓存获取上游任务 %s 的结果", taskID, depTaskID)
						}
					}
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
				taskObj.SetStatus(task.TaskStatusPending)

				// 检查Task是否已存在于数据库（预定义的Task已在SubmitWorkflow时保存）
				ctx := context.Background()
				existingTask, err := m.taskRepo.GetByID(ctx, taskID)
				if err != nil {
					log.Printf("⚠️ WorkflowInstance %s: 查询Task %s 失败: %v", m.instance.ID, taskID, err)
					// 查询失败，跳过该任务
					continue
				}

				// 如果Task不存在，说明是动态生成的子任务，不保存（根据业务需求）
				if existingTask == nil {
					log.Printf("⚠️ WorkflowInstance %s: Task %s (%s) 不存在于数据库，可能是动态生成的子任务，跳过执行",
						m.instance.ID, taskID, taskName)
					// 从candidateNodes中删除，避免重复检查
					m.candidateNodes.Delete(taskID)
					continue
				}

				// Task已存在（预定义的Task），使用数据库中的信息更新taskObj
				if existingTask.TimeoutSeconds > 0 {
					taskObj.TimeoutSeconds = existingTask.TimeoutSeconds
				}
				if existingTask.JobFuncID != "" {
					taskObj.JobFuncID = existingTask.JobFuncID
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
				// #region agent log
				logFile, _ := os.OpenFile("/Users/stevelan/Desktop/projects/task-engine/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if logFile != nil {
					fmt.Fprintf(logFile, `{"timestamp":%d,"location":"instance_manager.go:331","message":"提交任务到Executor前","data":{"instanceID":"%s","taskID":"%s","taskName":"%s"},"sessionId":"debug-session","runId":"run1","hypothesisId":"A"}`+"\n", time.Now().UnixMilli(), m.instance.ID, taskID, taskName)
					logFile.Close()
				}
				// #endregion
				if err := m.executor.SubmitTask(pendingTask); err != nil {
					// #region agent log
					logFile, _ := os.OpenFile("/Users/stevelan/Desktop/projects/task-engine/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
					if logFile != nil {
						fmt.Fprintf(logFile, `{"timestamp":%d,"location":"instance_manager.go:332","message":"提交任务到Executor失败","data":{"instanceID":"%s","taskID":"%s","taskName":"%s","error":"%v"},"sessionId":"debug-session","runId":"run1","hypothesisId":"E"}`+"\n", time.Now().UnixMilli(), m.instance.ID, taskID, taskName, err)
						logFile.Close()
					}
					// #endregion
					log.Printf("❌ WorkflowInstance %s: 提交Task到Executor失败: TaskID=%s, TaskName=%s, Error=%v",
						m.instance.ID, taskID, taskName, err)
					// 提交失败，需要回滚：将任务重新添加到 candidateNodes，以便重试
					// 注意：任务已经保存到数据库，但还没有被标记为已处理
					m.candidateNodes.Store(taskID, t)
					// 更新数据库中的任务状态为失败
					errorMsg := fmt.Sprintf("提交到Executor失败: %v", err)
					_ = m.taskRepo.UpdateStatusWithError(ctx, taskID, "Failed", errorMsg)
					continue
				}

				// #region agent log
				logFile, _ = os.OpenFile("/Users/stevelan/Desktop/projects/task-engine/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if logFile != nil {
					fmt.Fprintf(logFile, `{"timestamp":%d,"location":"instance_manager.go:344","message":"提交任务到Executor成功","data":{"instanceID":"%s","taskID":"%s","taskName":"%s"},"sessionId":"debug-session","runId":"run1","hypothesisId":"A"}`+"\n", time.Now().UnixMilli(), m.instance.ID, taskID, taskName)
					logFile.Close()
				}
				// #endregion
				// 提交成功，从 candidateNodes 中删除（但不标记为已处理，等任务真正完成后再标记）
				// 注意：任务被提交到Executor后，会在异步执行完成后通过OnComplete/OnError回调更新状态
				// 我们不应该在这里标记为已处理，因为任务可能还在Executor队列中等待执行
				m.candidateNodes.Delete(taskID)

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
			// 如果任务已处理，从candidateNodes中删除（防止重复提交）
			m.candidateNodes.Delete(taskID)
			// 减少日志写入频率，只在必要时记录
			// #region agent log
			// logFile, _ := os.OpenFile("/Users/stevelan/Desktop/projects/task-engine/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			// if logFile != nil {
			// 	fmt.Fprintf(logFile, `{"timestamp":%d,"location":"instance_manager.go:476","message":"从candidateNodes删除已处理的任务","data":{"instanceID":"%s","taskID":"%s","taskName":"%s"},"sessionId":"debug-session","runId":"run1","hypothesisId":"F"}`+"\n", time.Now().UnixMilli(), m.instance.ID, taskID, t.GetName())
			// 	logFile.Close()
			// }
			// #endregion
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
			// 执行参数校验和resultMapping
			if err := m.validateAndMapParams(t, taskID); err != nil {
				log.Printf("参数校验失败: TaskID=%s, Error=%v", taskID, err)
				// 参数校验失败，跳过该任务
				return true
			}
			available = append(available, t)
		}

		return true
	})

	return available
}

// validateAndMapParams 校验参数并执行resultMapping（内部方法）
func (m *WorkflowInstanceManager) validateAndMapParams(t workflow.Task, taskID string) error {
	// 尝试获取Task对象以访问RequiredParams和ResultMapping
	taskObj, ok := t.(*task.Task)
	if !ok {
		// 如果不是task.Task类型，跳过校验
		return nil
	}

	// 1. 检查必需参数
	if len(taskObj.RequiredParams) > 0 {
		// 获取上游任务的结果
		deps := t.GetDependencies()
		allParamsFound := true
		missingParams := make([]string, 0)

		for _, requiredParam := range taskObj.RequiredParams {
			found := false
			// 首先检查当前任务的参数中是否已有
			if t.GetParams()[requiredParam] != nil {
				found = true
			} else {
				// 从上游任务结果中查找
				for _, depName := range deps {
					depTaskID := m.findTaskIDByName(depName)
					if depTaskID == "" {
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
	if len(taskObj.ResultMapping) > 0 {
		deps := t.GetDependencies()
		for targetParam, sourceField := range taskObj.ResultMapping {
			// 从上游任务结果中查找sourceField
			for _, depName := range deps {
				depTaskID := m.findTaskIDByName(depName)
				if depTaskID == "" {
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

	// 额外检查：从数据库查询实际的任务状态，确保所有任务都已完成
	// 这对于大型工作流很重要，因为可能存在任务还没有被提交到执行队列的情况
	ctx := context.Background()
	taskInstances, err := m.taskRepo.GetByWorkflowInstanceID(ctx, m.instance.ID)
	if err == nil {
		// 检查是否有待处理或运行中的任务
		for _, ti := range taskInstances {
			if ti.Status == "Pending" || ti.Status == "Running" {
				log.Printf("WorkflowInstance %s: 发现任务 %s 状态为 %s，尚未完成", m.instance.ID, ti.ID, ti.Status)
				return false
			}
		}
		// 检查任务数量是否匹配（可能有些任务还没有被创建到数据库）
		if len(taskInstances) < totalTasks {
			log.Printf("WorkflowInstance %s: 数据库中的任务数 (%d) 少于工作流中的任务数 (%d)，可能还有任务未创建", m.instance.ID, len(taskInstances), totalTasks)
			return false
		}
	}

	return true
}

// recoverPendingTasks 恢复那些在数据库中但不在candidateNodes中的Pending任务
// 这可能是由于初始化时的问题或任务被提前创建到数据库
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
	clearedProcessedNodes := 0

	for _, ti := range taskInstances {
		// 处理Pending或Failed状态的任务（Failed可能是提交失败后需要重试的）
		if ti.Status != "Pending" && ti.Status != "Failed" {
			continue
		}

		pendingCount++
		taskID := ti.ID

		// 检查是否已处理
		// 注意：如果任务在processedNodes中但状态还是Pending，说明任务被提交了但可能还没执行完成
		// 这种情况下，我们应该检查任务是否真的在执行中（状态为Running），如果不是，应该恢复它
		if _, processed := m.processedNodes.Load(taskID); processed {
			// 如果任务被标记为已处理，但状态还是Pending，说明可能有问题
			// 检查任务是否真的在执行中
			if ti.Status == "Pending" {
				// 任务被标记为已处理但状态还是Pending，可能是：
				// 1. 任务执行完成，OnComplete回调被调用，标记为已处理，但数据库更新失败或延迟
				// 2. 任务被错误地标记为已处理
				// 3. 任务正在执行中，但状态还没更新为Running
				// 为了安全，我们检查任务是否真的在执行中（通过查询数据库的最新状态）
				// 如果任务确实还在Pending，说明可能有问题，需要重新检查
				latestTask, err := m.taskRepo.GetByID(ctx, taskID)
				if err == nil {
					if latestTask.Status == "Running" {
						// 任务正在执行中，正常情况
						skippedProcessed++
						continue
					} else if latestTask.Status == "Success" || latestTask.Status == "Failed" {
						// 任务已完成，但processedNodes标记和数据库状态不一致
						// 这种情况不应该发生，但为了安全，我们跳过
						skippedProcessed++
						continue
					} else if latestTask.Status == "Pending" {
						// 任务确实还在Pending，但被标记为已处理，这是异常情况
						// 可能是OnComplete回调被调用但数据库更新失败
						// 或者任务被错误地标记为已处理
						// 为了恢复，我们清除processedNodes标记，让任务可以被恢复
						log.Printf("⚠️ WorkflowInstance %s: 任务 %s (%s) 在processedNodes中但状态为Pending，清除processedNodes标记以便恢复",
							m.instance.ID, taskID, ti.Name)
						m.processedNodes.Delete(taskID)
						clearedProcessedNodes++
						// 不continue，继续处理这个任务
					}
				} else {
					// 查询失败，为了安全，跳过
					skippedProcessed++
					continue
				}
			} else {
				// 状态不是Pending，正常情况
				skippedProcessed++
				continue
			}
		}

		// 检查是否已在候选队列
		if _, exists := m.candidateNodes.Load(taskID); exists {
			skippedInQueue++
			continue
		}

		// 从workflow中获取任务定义
		t, exists := m.workflow.GetTasks()[taskID]
		if !exists {
			// 任务不在workflow中，记录详细信息
			skippedNotInWorkflow++
			log.Printf("⚠️ WorkflowInstance %s: Pending任务 %s (%s) 不在Workflow中，无法恢复",
				m.instance.ID, taskID, ti.Name)
			continue
		}

		// 检查所有依赖是否都已处理
		deps := t.GetDependencies()
		allDepsProcessed := true
		missingDeps := make([]string, 0)
		for _, depName := range deps {
			depTaskID := m.findTaskIDByName(depName)
			if depTaskID == "" {
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
			// 再次检查任务是否已在候选队列（防止并发问题）
			if _, exists := m.candidateNodes.Load(taskID); exists {
				skippedInQueue++
				log.Printf("⚠️ WorkflowInstance %s: 任务 %s (%s) 在恢复过程中被添加到候选队列，跳过", m.instance.ID, taskID, ti.Name)
				continue
			}
			m.candidateNodes.Store(taskID, t)
			recoveredCount++
			// 如果任务状态是Failed，重置为Pending以便重试
			if ti.Status == "Failed" {
				_ = m.taskRepo.UpdateStatus(ctx, taskID, "Pending")
				log.Printf("✅ WorkflowInstance %s: 恢复Failed任务 %s (%s) 到候选队列并重置为Pending", m.instance.ID, taskID, ti.Name)
			} else {
				log.Printf("✅ WorkflowInstance %s: 恢复Pending任务 %s (%s) 到候选队列", m.instance.ID, taskID, ti.Name)
			}
		} else {
			skippedDepsNotMet++
			log.Printf("⚠️ WorkflowInstance %s: %s任务 %s (%s) 依赖未满足: %v，跳过恢复",
				m.instance.ID, ti.Status, taskID, ti.Name, missingDeps)
		}
	}

	if pendingCount > 0 {
		log.Printf("📊 WorkflowInstance %s: recoverPendingTasks统计 - Pending/Failed任务总数: %d, 已恢复: %d, 已处理: %d, 已在队列: %d, 不在Workflow: %d, 依赖未满足: %d, 清除processedNodes: %d",
			m.instance.ID, pendingCount, recoveredCount, skippedProcessed, skippedInQueue, skippedNotInWorkflow, skippedDepsNotMet, clearedProcessedNodes)
	}
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
	m.contextData = sync.Map{}
	if breakpoint.ContextData != nil {
		for k, v := range breakpoint.ContextData {
			m.contextData.Store(k, v)
		}
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
		// #region agent log
		logFile, _ := os.OpenFile("/Users/stevelan/Desktop/projects/task-engine/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if logFile != nil {
			fmt.Fprintf(logFile, `{"timestamp":%d,"location":"instance_manager.go:890","message":"OnComplete回调被调用","data":{"instanceID":"%s","taskID":"%s","status":"%s"},"sessionId":"debug-session","runId":"run1","hypothesisId":"C"}`+"\n", time.Now().UnixMilli(), m.instance.ID, taskID, result.Status)
			logFile.Close()
		}
		// #endregion
		ctx := context.Background()
		// #region agent log
		logFile, _ = os.OpenFile("/Users/stevelan/Desktop/projects/task-engine/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if logFile != nil {
			fmt.Fprintf(logFile, `{"timestamp":%d,"location":"instance_manager.go:892","message":"更新数据库状态为Success前","data":{"instanceID":"%s","taskID":"%s"},"sessionId":"debug-session","runId":"run1","hypothesisId":"D"}`+"\n", time.Now().UnixMilli(), m.instance.ID, taskID)
			logFile.Close()
		}
		// #endregion
		// 重试更新数据库状态（处理SQLite并发锁定问题）
		updateSuccess := false
		maxRetries := 5
		retryDelay := 10 * time.Millisecond
		for i := 0; i < maxRetries; i++ {
			if err := m.taskRepo.UpdateStatus(ctx, taskID, "Success"); err != nil {
				// 检查是否是数据库锁定错误
				if i < maxRetries-1 && (err.Error() == "更新Task状态失败: database is locked" ||
					err.Error() == "database is locked") {
					// 数据库锁定，等待后重试
					time.Sleep(retryDelay)
					retryDelay *= 2 // 指数退避
					continue
				}
				// 其他错误或重试次数用完，记录日志
				// #region agent log
				logFile, _ = os.OpenFile("/Users/stevelan/Desktop/projects/task-engine/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if logFile != nil {
					fmt.Fprintf(logFile, `{"timestamp":%d,"location":"instance_manager.go:996","message":"更新数据库状态失败","data":{"instanceID":"%s","taskID":"%s","error":"%v","retryCount":%d},"sessionId":"debug-session","runId":"run1","hypothesisId":"D"}`+"\n", time.Now().UnixMilli(), m.instance.ID, taskID, err, i+1)
					logFile.Close()
				}
				// #endregion
				log.Printf("❌ WorkflowInstance %s: 更新任务状态失败: TaskID=%s, Error=%v, 重试次数=%d", m.instance.ID, taskID, err, i+1)
				break
			} else {
				updateSuccess = true
				break
			}
		}

		if updateSuccess {
			// #region agent log
			logFile, _ = os.OpenFile("/Users/stevelan/Desktop/projects/task-engine/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if logFile != nil {
				fmt.Fprintf(logFile, `{"timestamp":%d,"location":"instance_manager.go:892","message":"更新数据库状态为Success成功","data":{"instanceID":"%s","taskID":"%s"},"sessionId":"debug-session","runId":"run1","hypothesisId":"D"}`+"\n", time.Now().UnixMilli(), m.instance.ID, taskID)
				logFile.Close()
			}
			// #endregion
		}

		// 任务真正完成时，才标记为已处理
		// 注意：只有在数据库更新成功时才标记为已处理
		// 如果数据库更新失败，不标记为已处理，让recoverPendingTasks可以恢复这个任务
		if updateSuccess {
			// #region agent log
			logFile, _ = os.OpenFile("/Users/stevelan/Desktop/projects/task-engine/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if logFile != nil {
				fmt.Fprintf(logFile, `{"timestamp":%d,"location":"instance_manager.go:1045","message":"标记任务为已处理","data":{"instanceID":"%s","taskID":"%s"},"sessionId":"debug-session","runId":"run1","hypothesisId":"C"}`+"\n", time.Now().UnixMilli(), m.instance.ID, taskID)
				logFile.Close()
			}
			// #endregion
			m.processedNodes.Store(taskID, true)
		} else {
			// 数据库更新失败，不标记为已处理，让recoverPendingTasks可以恢复
			// #region agent log
			logFile, _ = os.OpenFile("/Users/stevelan/Desktop/projects/task-engine/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if logFile != nil {
				fmt.Fprintf(logFile, `{"timestamp":%d,"location":"instance_manager.go:1055","message":"数据库更新失败，不标记为已处理","data":{"instanceID":"%s","taskID":"%s"},"sessionId":"debug-session","runId":"run1","hypothesisId":"D"}`+"\n", time.Now().UnixMilli(), m.instance.ID, taskID)
				logFile.Close()
			}
			// #endregion
			log.Printf("⚠️ WorkflowInstance %s: 任务 %s 数据库更新失败，不标记为已处理，等待recoverPendingTasks恢复", m.instance.ID, taskID)
			// 不标记为已处理，让recoverPendingTasks可以恢复这个任务
			return
		}

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
			var statusHandlers map[string][]string
			if taskObj, ok := workflowTask.(*task.Task); ok {
				statusHandlers = taskObj.StatusHandlers
			}

			// 创建task.Task实例用于handler调用
			taskObj := task.NewTask(taskInstance.Name, workflowTask.GetName(), taskInstance.JobFuncID, taskInstance.Params, statusHandlers)
			taskObj.ID = taskInstance.ID
			taskObj.JobFuncName = taskInstance.JobFuncName
			taskObj.TimeoutSeconds = taskInstance.TimeoutSeconds
			taskObj.RetryCount = taskInstance.RetryCount
			taskObj.Dependencies = []string{} // 从workflowTask获取
			taskObj.SetStatus(taskInstance.Status)

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
		m.dag.UpdateInDegree(taskID)

		// 将下游节点加入候选队列
		node, exists := m.dag.GetNode(taskID)
		if exists {
			for _, nextID := range node.OutEdges {
				// 如果下游节点是当前任务自己，说明DAG存在环，这是不应该发生的
				// 因为DAG在构建和动态添加节点时都会检测循环依赖
				if nextID == taskID {
					// 重新检测DAG是否有环，如果确实有环，应该报错
					if err := m.dag.DetectCycle(); err != nil {
						log.Printf("❌ WorkflowInstance %s: 检测到DAG存在循环依赖！任务 %s 的下游节点是自己。错误: %v", m.instance.ID, taskID, err)
						// 记录详细的DAG状态用于调试
						// #region agent log
						logFile, _ := os.OpenFile("/Users/stevelan/Desktop/projects/task-engine/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
						if logFile != nil {
							fmt.Fprintf(logFile, `{"timestamp":%d,"location":"instance_manager.go:1025","message":"检测到DAG循环依赖","data":{"instanceID":"%s","taskID":"%s","error":"%v"},"sessionId":"debug-session","runId":"run1","hypothesisId":"F"}`+"\n", time.Now().UnixMilli(), m.instance.ID, taskID, err)
							logFile.Close()
						}
						// #endregion
						// 不继续处理，避免无限循环
						continue
					} else {
						// 如果DAG检测没有环，但OutEdges包含自己，说明是DAG状态异常
						log.Printf("⚠️ WorkflowInstance %s: 任务 %s 的OutEdges包含自己，但DAG检测无环，可能是DAG状态异常", m.instance.ID, taskID)
						// #region agent log
						logFile, _ := os.OpenFile("/Users/stevelan/Desktop/projects/task-engine/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
						if logFile != nil {
							fmt.Fprintf(logFile, `{"timestamp":%d,"location":"instance_manager.go:1033","message":"DAG状态异常：OutEdges包含自己但无环","data":{"instanceID":"%s","taskID":"%s"},"sessionId":"debug-session","runId":"run1","hypothesisId":"F"}`+"\n", time.Now().UnixMilli(), m.instance.ID, taskID)
							logFile.Close()
						}
						// #endregion
						continue
					}
				}
				// 防止将已完成的任务重新添加到候选队列
				if _, processed := m.processedNodes.Load(nextID); processed {
					continue
				}
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
						// 再次检查任务是否已被处理（防止并发问题）
						if _, processed := m.processedNodes.Load(nextID); !processed {
							m.candidateNodes.Store(nextID, t)
							// #region agent log
							logFile, _ := os.OpenFile("/Users/stevelan/Desktop/projects/task-engine/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
							if logFile != nil {
								fmt.Fprintf(logFile, `{"timestamp":%d,"location":"instance_manager.go:1032","message":"将下游任务添加到candidateNodes","data":{"instanceID":"%s","parentTaskID":"%s","nextTaskID":"%s","nextTaskName":"%s"},"sessionId":"debug-session","runId":"run1","hypothesisId":"F"}`+"\n", time.Now().UnixMilli(), m.instance.ID, taskID, nextID, t.GetName())
								logFile.Close()
							}
							// #endregion
						}
					}
				}
			}
		}

		// 确保已完成的任务从candidateNodes中删除（防止重复提交）
		m.candidateNodes.Delete(taskID)

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
		status := "Failed"
		// 重试更新数据库状态（处理SQLite并发锁定问题）
		updateSuccess := false
		maxRetries := 5
		retryDelay := 10 * time.Millisecond
		for i := 0; i < maxRetries; i++ {
			if updateErr := m.taskRepo.UpdateStatusWithError(ctx, taskID, status, err.Error()); updateErr != nil {
				// 检查是否是数据库锁定错误
				if i < maxRetries-1 && (updateErr.Error() == "更新Task状态和错误信息失败: database is locked" ||
					updateErr.Error() == "database is locked") {
					// 数据库锁定，等待后重试
					time.Sleep(retryDelay)
					retryDelay *= 2 // 指数退避
					continue
				}
				// 其他错误或重试次数用完，记录日志
				log.Printf("❌ WorkflowInstance %s: 更新任务失败状态失败: TaskID=%s, Error=%v, 重试次数=%d", m.instance.ID, taskID, updateErr, i+1)
				break
			} else {
				updateSuccess = true
				break
			}
		}

		// 任务真正完成（失败）时，才标记为已处理
		// 注意：只有在数据库更新成功时才标记为已处理
		if updateSuccess {
			m.processedNodes.Store(taskID, true)
		} else {
			// 数据库更新失败，不标记为已处理，让recoverPendingTasks可以恢复这个任务
			log.Printf("⚠️ WorkflowInstance %s: 任务 %s 失败状态更新失败，不标记为已处理，等待recoverPendingTasks恢复", m.instance.ID, taskID)
			return
		}

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
			var statusHandlers map[string][]string
			if taskObj, ok := workflowTask.(*task.Task); ok {
				statusHandlers = taskObj.StatusHandlers
			}

			// 创建task.Task实例用于handler调用
			taskObj := task.NewTask(taskInstance.Name, workflowTask.GetName(), taskInstance.JobFuncID, taskInstance.Params, statusHandlers)
			taskObj.ID = taskInstance.ID
			taskObj.JobFuncName = taskInstance.JobFuncName
			taskObj.TimeoutSeconds = taskInstance.TimeoutSeconds
			taskObj.RetryCount = taskInstance.RetryCount
			taskObj.Dependencies = []string{}
			taskObj.SetStatus(taskInstance.Status)

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
	}

	// 如果子任务的依赖已满足，加入候选队列
	if allDepsProcessed {
		m.candidateNodes.Store(subTask.GetID(), subTask)
		log.Printf("WorkflowInstance %s: 子任务 %s 已添加，依赖已满足，加入候选队列", m.instance.ID, subTask.GetName())
	} else {
		log.Printf("WorkflowInstance %s: 子任务 %s 已添加，等待依赖满足（父任务: %s）", m.instance.ID, subTask.GetName(), parentTaskID)
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
