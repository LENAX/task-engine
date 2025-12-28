package executor

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/stevelan1995/task-engine/pkg/core/task"
)

// Executor 执行器核心结构体（对外导出）
type Executor struct {
	mu          sync.RWMutex
	maxWorkers  int                    // 全局最大并发数
	workerPool  chan struct{}          // 全局Worker池
	domainPools map[string]*domainPool // 业务域子池
	taskQueue   chan *PendingTask      // 待调度任务队列
	wg          sync.WaitGroup
	running     bool
	shutdown    chan struct{}
	registry    *task.JobFunctionRegistry // Job函数注册中心
}

// domainPool 业务域子池（内部结构）
type domainPool struct {
	maxSize    int           // 最大并发数
	current    int           // 当前运行数
	workerPool chan struct{} // Worker池
	mu         sync.RWMutex
}

const (
	maxGlobalWorkers = 1000 // 全局最大并发数上限
	defaultQueueSize = 1000 // 默认任务队列大小
)

// NewExecutor 创建执行器实例（对外导出的工厂方法，engine包会调用）
func NewExecutor(maxWorkers int) (*Executor, error) {
	if maxWorkers <= 0 {
		maxWorkers = 10 // 默认值
	}
	if maxWorkers > maxGlobalWorkers {
		return nil, fmt.Errorf("最大并发数不能超过 %d", maxGlobalWorkers)
	}

	exec := &Executor{
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

// Start 启动执行器（对外导出）
func (e *Executor) Start() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running {
		return
	}
	e.running = true
	log.Println("✅ 执行器已启动")
}

// Shutdown 关闭执行器（对外导出）
func (e *Executor) Shutdown() error {
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
func (e *Executor) SetPoolSize(maxSize int) error {
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
func (e *Executor) SetDomainPoolSize(domain string, size int) error {
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
func (e *Executor) GetDomainPoolStatus(domain string) (int, int, error) {
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
func (e *Executor) SetRegistry(registry *task.JobFunctionRegistry) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.registry = registry
}

// SubmitTask 将待调度Task提交至Executor的任务队列（对外导出）
func (e *Executor) SubmitTask(pendingTask *PendingTask) error {
	if pendingTask == nil {
		return fmt.Errorf("任务不能为空")
	}
	if pendingTask.Task == nil {
		return fmt.Errorf("Task实例不能为空")
	}

	e.mu.RLock()
	if !e.running {
		e.mu.RUnlock()
		return fmt.Errorf("Executor未运行")
	}
	e.mu.RUnlock()

	// 提交到任务队列
	select {
	case e.taskQueue <- pendingTask:
		return nil
	case <-e.shutdown:
		return fmt.Errorf("Executor已关闭")
	default:
		return fmt.Errorf("任务队列已满")
	}
}

// scheduler 任务调度器（内部方法）
func (e *Executor) scheduler() {
	for {
		select {
		case pendingTask, ok := <-e.taskQueue:
			if !ok {
				// 任务队列已关闭
				return
			}
			// 分配任务到Worker
			e.dispatchTask(pendingTask)
		case <-e.shutdown:
			return
		}
	}
}

// dispatchTask 分配任务到Worker（内部方法）
func (e *Executor) dispatchTask(pendingTask *PendingTask) {
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
				e.wg.Add(1)
				go e.executeTask(pendingTask, pool)
				return
			default:
				// 业务域子池已满，回退到全局池
			}
		}
	}

	// 使用全局Worker池
	select {
	case e.workerPool <- struct{}{}:
		e.wg.Add(1)
		go e.executeTask(pendingTask, nil)
	case <-e.shutdown:
		// Executor已关闭，通知任务失败
		if pendingTask.OnError != nil {
			pendingTask.OnError(fmt.Errorf("Executor已关闭"))
		}
	}
}

// executeTask 执行Task（内部方法）
func (e *Executor) executeTask(pendingTask *PendingTask, domainPool *domainPool) {
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
	t.Status = task.TaskStatusRunning

	// 如果没有注册中心，无法执行
	if e.registry == nil {
		result := &TaskResult{
			TaskID:   t.ID,
			Status:   "Failed",
			Error:    fmt.Errorf("Job函数注册中心未配置"),
			Duration: time.Since(startTime).Milliseconds(),
		}
		if pendingTask.OnError != nil {
			pendingTask.OnError(result.Error)
		}
		return
	}

	// 获取Job函数
	jobFunc := e.registry.GetByName(t.JobFuncName)
	if jobFunc == nil {
		// 尝试通过JobFuncID获取
		jobFunc = e.registry.Get(t.JobFuncID)
	}
	if jobFunc == nil {
		result := &TaskResult{
			TaskID:   t.ID,
			Status:   "Failed",
			Error:    fmt.Errorf("Job函数 %s 未找到", t.JobFuncName),
			Duration: time.Since(startTime).Milliseconds(),
		}
		if pendingTask.OnError != nil {
			pendingTask.OnError(result.Error)
		}
		return
	}

	// 创建执行上下文
	ctx := context.Background()
	timeoutSeconds := t.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30 // 默认30秒
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	// Task.Params已经是map[string]string，直接使用
	params := t.Params
	if params == nil {
		params = make(map[string]string)
	}

	// 执行Job函数
	stateCh := jobFunc(ctx, params)

	// 监听执行结果
	select {
	case state := <-stateCh:
		duration := time.Since(startTime).Milliseconds()
		result := &TaskResult{
			TaskID:   t.ID,
			Status:   state.Status,
			Data:     state.Data,
			Error:    state.Error,
			Duration: duration,
		}

		if state.Status == "Success" {
			t.Status = task.TaskStatusSuccess
			if pendingTask.OnComplete != nil {
				pendingTask.OnComplete(result)
			}
		} else {
			t.Status = task.TaskStatusFailed
			// 检查是否需要重试
			if pendingTask.RetryCount < pendingTask.MaxRetries {
				// 重试：计算重试间隔（1s、2s、4s...）
				retryDelay := time.Duration(1<<uint(pendingTask.RetryCount)) * time.Second
				time.Sleep(retryDelay)
				// 重新提交任务
				pendingTask.RetryCount++
				e.SubmitTask(pendingTask)
			} else {
				if pendingTask.OnError != nil {
					pendingTask.OnError(state.Error)
				}
			}
		}
	case <-ctx.Done():
		// 超时
		duration := time.Since(startTime).Milliseconds()
		t.Status = task.TaskStatusTimeout
		result := &TaskResult{
			TaskID:   t.ID,
			Status:   "TimeoutFailed",
			Error:    fmt.Errorf("任务执行超时（%d秒）", timeoutSeconds),
			Duration: duration,
		}
		if pendingTask.OnError != nil {
			pendingTask.OnError(result.Error)
		}
	}
}
