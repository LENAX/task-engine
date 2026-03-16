package executor

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/LENAX/task-engine/pkg/core/task"
)

// executorImpl 执行器实现（内部实现）
type executorImpl struct {
	mu          sync.RWMutex
	maxWorkers  int                    // 全局最大并发数
	workerPool  chan struct{}          // 全局Worker池
	domainPools map[string]*domainPool // 业务域子池
	taskQueue   chan *PendingTask      // 待调度任务队列
	wg          sync.WaitGroup
	running     bool
	shutdown    chan struct{}
	registry    task.FunctionRegistry // Job函数注册中心（接口）
}

// domainPool 业务域子池（内部结构）
type domainPool struct {
	maxSize    int           // 最大并发数
	current    int           // 当前运行数
	workerPool chan struct{} // Worker池
	mu         sync.RWMutex
}

const (
	maxGlobalWorkers = 1000  // 全局最大并发数上限
	defaultQueueSize = 10000 // 默认任务队列大小（支持大型workflow）
	maxLogValueLen   = 200   // 日志中结果/错误字符串最大长度，超出则截断
)

// NewExecutor 创建执行器实例（对外导出的工厂方法，engine包会调用）
func NewExecutor(maxWorkers int) (Executor, error) {
	if maxWorkers <= 0 {
		maxWorkers = 10 // 默认值
	}
	if maxWorkers > maxGlobalWorkers {
		return nil, fmt.Errorf("最大并发数不能超过 %d", maxGlobalWorkers)
	}

	exec := &executorImpl{
		maxWorkers:  maxWorkers,
		workerPool:  make(chan struct{}, maxWorkers),
		domainPools: make(map[string]*domainPool),
		taskQueue:   make(chan *PendingTask, defaultQueueSize),
		running:     false,
		shutdown:    make(chan struct{}),
	}

	// 启动任务调度器
	go exec.scheduler()

	return exec, nil
}

// truncateLogValue 将任意值格式化为字符串，超过 maxLogValueLen 则截断并追加 "..."
func truncateLogValue(v any) string {
	s := fmt.Sprint(v)
	if len(s) <= maxLogValueLen {
		return s
	}
	return s[:maxLogValueLen-3] + "..."
}

// Start 启动执行器（对外导出）
func (e *executorImpl) Start() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running {
		return
	}
	e.running = true
	log.Println("✅ 执行器已启动")
}

// Shutdown 关闭执行器（对外导出）
func (e *executorImpl) Shutdown() error {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return nil
	}
	e.running = false
	close(e.shutdown)
	e.mu.Unlock()

	// 关闭任务队列
	close(e.taskQueue)

	// 等待所有任务完成（最多等待30秒）
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("Executor: 所有任务已完成")
	case <-ctx.Done():
		log.Println("Executor: 关闭超时，强制终止")
	}

	log.Println("✅ 执行器已关闭")
	return nil
}

// SetPoolSize 动态调整Executor的全局并发池大小（对外导出）
func (e *executorImpl) SetPoolSize(maxSize int) error {
	if maxSize <= 0 {
		return fmt.Errorf("并发池大小必须大于0")
	}
	if maxSize > maxGlobalWorkers {
		return fmt.Errorf("并发池大小不能超过 %d", maxGlobalWorkers)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// 检查是否超过CPU核心数的2倍（建议值，但允许更大）
	maxCPUCores := runtime.NumCPU() * 2
	if maxSize > maxCPUCores {
		// 警告但不阻止（允许用户设置更大的值）
		log.Printf("警告: 并发池大小（%d）超过CPU核心数的2倍（%d），可能影响性能", maxSize, maxCPUCores)
	}

	oldSize := e.maxWorkers
	e.maxWorkers = maxSize

	// 调整全局Worker池大小
	if maxSize > oldSize {
		// 扩大池
		newPool := make(chan struct{}, maxSize)
		// 将旧的token转移到新池（如果有空闲的）
		for i := 0; i < oldSize && len(e.workerPool) > 0; i++ {
			select {
			case <-e.workerPool:
				select {
				case newPool <- struct{}{}:
				default:
				}
			default:
			}
		}
		e.workerPool = newPool
	} else {
		// 缩小池（等待当前任务完成，新任务会使用新大小）
		newPool := make(chan struct{}, maxSize)
		e.workerPool = newPool
	}

	return nil
}

// SetDomainPoolSize 动态调整指定业务域的子池大小（对外导出）
func (e *executorImpl) SetDomainPoolSize(domain string, size int) error {
	if size <= 0 {
		return fmt.Errorf("子池大小必须大于0")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// 检查子池大小总和是否超过全局最大并发数
	totalDomainSize := 0
	for _, pool := range e.domainPools {
		if pool.maxSize > 0 {
			totalDomainSize += pool.maxSize
		}
	}
	// 减去当前域的大小（如果存在）
	if existingPool, exists := e.domainPools[domain]; exists {
		totalDomainSize -= existingPool.maxSize
	}
	totalDomainSize += size

	if totalDomainSize > e.maxWorkers {
		return fmt.Errorf("业务域子池大小总和（%d）超过全局最大并发数（%d）", totalDomainSize, e.maxWorkers)
	}

	// 创建或更新业务域子池
	if pool, exists := e.domainPools[domain]; exists {
		pool.mu.Lock()
		pool.maxSize = size
		pool.workerPool = make(chan struct{}, size)
		pool.mu.Unlock()
	} else {
		e.domainPools[domain] = &domainPool{
			maxSize:    size,
			current:    0,
			workerPool: make(chan struct{}, size),
		}
	}

	return nil
}

// GetDomainPoolStatus 查询指定业务域子池的状态（对外导出）
func (e *executorImpl) GetDomainPoolStatus(domain string) (int, int, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	pool, exists := e.domainPools[domain]
	if !exists {
		return 0, 0, fmt.Errorf("业务域 %s 不存在", domain)
	}

	pool.mu.RLock()
	defer pool.mu.RUnlock()

	// 当前可用数 = 最大并发数 - 当前运行数
	available := pool.maxSize - pool.current
	if available < 0 {
		available = 0
	}

	return available, pool.maxSize, nil
}

// SetRegistry 设置Job函数注册中心（对外导出）
func (e *executorImpl) SetRegistry(registry task.FunctionRegistry) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.registry = registry
}

// SubmitTask 将待调度Task提交至Executor的任务队列（对外导出）
// 如果队列已满，会阻塞等待直到有空间或Executor关闭
func (e *executorImpl) SubmitTask(pendingTask *PendingTask) error {
	if pendingTask == nil {
		return fmt.Errorf("任务不能为空")
	}
	if pendingTask.Task == nil {
		return fmt.Errorf("Task实例不能为空")
	}

	e.mu.RLock()
	running := e.running
	// queueLen := len(e.taskQueue) // 相关debuglog已去除
	e.mu.RUnlock()

	// agentlog已清理

	if !running {
		return fmt.Errorf("Executor未运行")
	}

	// 提交到任务队列（阻塞等待，直到有空间或Executor关闭）
	select {
	case e.taskQueue <- pendingTask:
		// agentlog已清理
		return nil
	case <-e.shutdown:
		return fmt.Errorf("Executor已关闭")
	}
}

// scheduler 任务调度器（内部方法）
func (e *executorImpl) scheduler() {
	for {
		select {
		case pendingTask, ok := <-e.taskQueue:
			if !ok {
				// 任务队列已关闭
				return
			}
			// agentlog已清理
			// 分配任务到Worker
			e.dispatchTask(pendingTask)
		case <-e.shutdown:
			return
		}
	}
}

// dispatchTask 分配任务到Worker（内部方法）
func (e *executorImpl) dispatchTask(pendingTask *PendingTask) {
	// agentlog已清理
	// 如果有业务域，使用业务域子池
	if pendingTask.Domain != "" {
		e.mu.RLock()
		pool, exists := e.domainPools[pendingTask.Domain]
		e.mu.RUnlock()

		if exists {
			// 尝试获取业务域子池的token
			select {
			case pool.workerPool <- struct{}{}:
				pool.mu.Lock()
				pool.current++
				pool.mu.Unlock()
				// agentlog已清理
				e.wg.Add(1)
				go e.executeTask(pendingTask, pool)
				return
			default:
				// 业务域子池已满，回退到全局池
				// agentlog已清理
			}
		}
	}

	// 使用全局Worker池
	// 注意：这里使用阻塞方式，如果workerPool满了，会一直等待
	// 这可能导致任务无法及时执行，但可以确保任务最终会被执行
	select {
	case e.workerPool <- struct{}{}:
		// agentlog已清理
		e.wg.Add(1)
		go e.executeTask(pendingTask, nil)
	case <-e.shutdown:
		// Executor已关闭，通知任务失败
		err := fmt.Errorf("Executor已关闭")
		// 发送状态事件到 channel（如果提供）
		if pendingTask.StatusChan != nil {
			t := pendingTask.Task
			isTemplate := false
			isSubTask := false
			if t != nil {
				if taskWithFlags, ok := t.(interface {
					IsTemplate() bool
					IsSubTask() bool
				}); ok {
					isTemplate = taskWithFlags.IsTemplate()
					isSubTask = taskWithFlags.IsSubTask()
				}
			}
			event := &TaskStatusEvent{
				TaskID:     t.GetID(),
				Status:     "Failed",
				Error:      err,
				IsTemplate: isTemplate,
				IsSubTask:  isSubTask,
				Timestamp:  time.Now(),
				Duration:   0,
			}
			select {
			case pendingTask.StatusChan <- event:
			default:
				log.Printf("警告: TaskStatusEvent channel 已满，事件可能丢失: TaskID=%s", t.GetID())
			}
		}
		// 调用错误回调（如果提供）
		if pendingTask.OnError != nil {
			pendingTask.OnError(err)
		}
	}
}

// executeTask 执行Task（内部方法）
func (e *executorImpl) executeTask(pendingTask *PendingTask, domainPool *domainPool) {
	defer func() {
		// 释放Worker池token
		if domainPool != nil {
			domainPool.mu.Lock()
			domainPool.current--
			domainPool.mu.Unlock()
			<-domainPool.workerPool
		} else {
			<-e.workerPool
		}
		e.wg.Done()
	}()

	startTime := time.Now()
	t := pendingTask.Task

	// 更新Task状态为Running
	t.SetStatus("RUNNING")

	// 如果没有注册中心，无法执行
	if e.registry == nil {
		result := &TaskResult{
			TaskID:   t.GetID(),
			Status:   "Failed",
			Error:    fmt.Errorf("Job函数注册中心未配置"),
			Duration: time.Since(startTime).Milliseconds(),
		}
		// 发送状态事件到 channel（如果提供）
		e.sendStatusEvent(pendingTask, result)
		// 调用错误回调（如果提供）
		if pendingTask.OnError != nil {
			pendingTask.OnError(result.Error)
		}
		return
	}

	// 获取Job函数
	jobFunc := e.registry.GetByName(t.GetJobFuncName())
	var funcID string
	if jobFunc == nil {
		// 尝试通过JobFuncID获取
		jobFunc = e.registry.Get(t.GetJobFuncID())
		funcID = t.GetJobFuncID()
	} else {
		// 通过名称获取到函数，查找对应的ID
		funcID = e.registry.GetIDByName(t.GetJobFuncName())
		if funcID == "" {
			funcID = t.GetJobFuncName()
		}
	}
	if jobFunc == nil {
		log.Printf("❌ [Task执行失败] TaskID=%s, TaskName=%s, 原因: Job函数 %s 未找到", t.GetID(), t.GetName(), t.GetJobFuncName())
		result := &TaskResult{
			TaskID:   t.GetID(),
			Status:   "Failed",
			Error:    fmt.Errorf("Job函数 %s 未找到", t.GetJobFuncName()),
			Duration: time.Since(startTime).Milliseconds(),
		}
		// 发送状态事件到 channel（如果提供）
		e.sendStatusEvent(pendingTask, result)
		// 调用错误回调（如果提供）
		if pendingTask.OnError != nil {
			pendingTask.OnError(result.Error)
		}
		return
	}

	// 获取参数用于日志打印
	paramsForLog := t.GetParams()
	// 打印函数执行开始日志
	log.Printf("🚀 [开始执行函数] TaskID=%s, TaskName=%s, JobFuncName=%s, JobFuncID=%s, 参数=%s",
		t.GetID(), t.GetName(), t.GetJobFuncName(), funcID, truncateLogValue(paramsForLog))

	// 创建执行上下文
	ctx := context.Background()
	timeoutSeconds := t.GetTimeoutSeconds()
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30 // 默认30秒
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	// 注入依赖到 context（如果 registry 支持依赖注入）
	if e.registry != nil {
		ctx = e.registry.WithDependencies(ctx)
	}

	// 获取参数用于 TaskContext
	paramsMap := t.GetParams()

	// 创建TaskContext
	taskCtx := task.NewTaskContext(
		ctx,
		t.GetID(),
		t.GetName(),
		pendingTask.WorkflowID,
		pendingTask.InstanceID,
		paramsMap,
	)

	// 设置引擎组件引用（用于 Job Function 访问引擎能力）
	if e.registry != nil {
		taskCtx.SetRegistry(e.registry)
	}
	if pendingTask.InstanceManager != nil {
		taskCtx.SetInstanceManager(pendingTask.InstanceManager)
	}

	// 执行Job函数
	log.Printf("📞 [调用函数] TaskID=%s, TaskName=%s, JobFuncName=%s, 开始执行...", t.GetID(), t.GetName(), t.GetJobFuncName())
	stateCh := jobFunc(taskCtx)

	// 监听执行结果
	select {
	case state := <-stateCh:
		duration := time.Since(startTime).Milliseconds()
		result := &TaskResult{
			TaskID:   t.GetID(),
			Status:   state.Status,
			Data:     state.Data,
			Error:    state.Error,
			Duration: duration,
		}

		if task.IsSuccessStatus(state.Status) {
			t.SetStatus("SUCCESS")
			log.Printf("✅ [函数执行成功] TaskID=%s, TaskName=%s, JobFuncName=%s, 耗时=%dms, 结果=%s",
				t.GetID(), t.GetName(), t.GetJobFuncName(), duration, truncateLogValue(state.Data))
			// 发送状态事件到 channel（如果提供）
			e.sendStatusEvent(pendingTask, result)
			// 调用完成回调（如果提供）
			if pendingTask.OnComplete != nil {
				pendingTask.OnComplete(result)
			}
		} else {
			t.SetStatus("FAILED")
			log.Printf("❌ [函数执行失败] TaskID=%s, TaskName=%s, JobFuncName=%s, 耗时=%dms, 错误=%s",
				t.GetID(), t.GetName(), t.GetJobFuncName(), duration, truncateLogValue(state.Error))
			// 检查是否需要重试
			if pendingTask.RetryCount < pendingTask.MaxRetries {
				// 重试：计算重试间隔（1s、2s、4s...）
				retryDelay := time.Duration(1<<uint(pendingTask.RetryCount)) * time.Second
				log.Printf("🔄 [准备重试] TaskID=%s, TaskName=%s, 当前重试次数=%d, 延迟=%v",
					t.GetID(), t.GetName(), pendingTask.RetryCount, retryDelay)
				time.Sleep(retryDelay)
				// 重新提交任务
				pendingTask.RetryCount++
				e.SubmitTask(pendingTask)
			} else {
				// 发送状态事件到 channel（如果提供）
				e.sendStatusEvent(pendingTask, result)
				// 调用错误回调（如果提供）
				if pendingTask.OnError != nil {
					pendingTask.OnError(state.Error)
				}
			}
		}
	case <-ctx.Done():
		// 超时
		duration := time.Since(startTime).Milliseconds()
		t.SetStatus("TIMEOUT")
		log.Printf("⏱️  [函数执行超时] TaskID=%s, TaskName=%s, JobFuncName=%s, 超时时间=%ds, 耗时=%dms",
			t.GetID(), t.GetName(), t.GetJobFuncName(), timeoutSeconds, duration)
		result := &TaskResult{
			TaskID:   t.GetID(),
			Status:   "TimeoutFailed",
			Error:    fmt.Errorf("任务执行超时（%d秒）", timeoutSeconds),
			Duration: duration,
		}
		// 发送状态事件到 channel（如果提供）
		e.sendStatusEvent(pendingTask, result)
		// 调用错误回调（如果提供）
		if pendingTask.OnError != nil {
			pendingTask.OnError(result.Error)
		}
	}
}

// sendStatusEvent 发送任务状态事件到 channel（内部方法）
// 如果 PendingTask 提供了 StatusChan，则将任务结果转换为事件并发送
func (e *executorImpl) sendStatusEvent(pendingTask *PendingTask, result *TaskResult) {
	if pendingTask.StatusChan == nil {
		return
	}

	// 确定状态字符串（大小写不敏感）
	status := result.Status
	if task.IsTimeoutStatus(status) || strings.EqualFold(status, "TimeoutFailed") {
		status = "Timeout"
	}

	// 从 Task 中获取额外信息
	t := pendingTask.Task
	isTemplate := false
	isSubTask := false
	if t != nil {
		// 尝试获取 IsTemplate 和 IsSubTask 信息
		// 注意：workflow.Task 接口可能没有这些方法，需要类型断言
		if taskWithFlags, ok := t.(interface {
			IsTemplate() bool
			IsSubTask() bool
		}); ok {
			isTemplate = taskWithFlags.IsTemplate()
			isSubTask = taskWithFlags.IsSubTask()
		}
	}

	// 构建事件
	event := &TaskStatusEvent{
		TaskID:     result.TaskID,
		Status:     status,
		Result:     result.Data,
		Error:      result.Error,
		IsTemplate: isTemplate,
		IsSubTask:  isSubTask,
		Timestamp:  time.Now(),
		Duration:   result.Duration,
	}

	// 非阻塞发送（避免阻塞 executor）
	select {
	case pendingTask.StatusChan <- event:
		// 成功发送
	default:
		// channel 已满，记录警告但不阻塞
		log.Printf("警告: TaskStatusEvent channel 已满，事件可能丢失: TaskID=%s", result.TaskID)
	}
}
