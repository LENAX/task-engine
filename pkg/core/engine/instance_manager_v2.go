package engine

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LENAX/task-engine/pkg/core/cache"
	"github.com/LENAX/task-engine/pkg/core/dag"
	"github.com/LENAX/task-engine/pkg/core/executor"
	"github.com/LENAX/task-engine/pkg/core/saga"
	"github.com/LENAX/task-engine/pkg/core/task"
	"github.com/LENAX/task-engine/pkg/core/types"
	"github.com/LENAX/task-engine/pkg/core/workflow"
	"github.com/LENAX/task-engine/pkg/plugin"
	"github.com/LENAX/task-engine/pkg/storage"
)

// TaskStatusEvent 任务状态事件（通过 channel 传递）
type TaskStatusEvent struct {
	TaskID      string      // 任务ID
	Status      string      // Success, Failed, Timeout, subtask_added, ready
	Result      interface{} // 任务结果（Success 时）
	Error       error       // 错误信息（Failed 时）
	IsTemplate  bool        // 是否为模板任务
	IsSubTask   bool        // 是否为子任务
	ParentID    string      // 父任务ID（子任务特有）
	IsProcessed bool        // 是否已处理（避免重复计数）
	Timestamp   time.Time   // 事件时间戳
}

// TaskStatsUpdate 任务统计更新
type TaskStatsUpdate struct {
	Type       string // task_completed, task_failed, task_added
	TaskID     string
	Status     string
	IsTemplate bool
	IsSubTask  bool
}

// AtomicAddSubTasksEvent 原子性子任务添加事件（包含多个子任务）
type AtomicAddSubTasksEvent struct {
	SubTasks  []workflow.Task // 子任务列表
	ParentID  string          // 父任务ID
	Timestamp time.Time       // 事件时间戳
}

// SubTaskTracker 子任务跟踪器（用于结果聚合）
type SubTaskTracker struct {
	SubTaskIDs     []string   // 子任务 ID 列表
	CompletedCount int32      // 已完成数量（atomic）
	FailedCount    int32      // 失败数量（atomic）
	TotalCount     int32      // 总数量
	Results        sync.Map   // subTaskID -> SubTaskResult
	mu             sync.Mutex // 保护 SubTaskIDs 的并发访问
}

// SubTaskResult 子任务结果
type SubTaskResult struct {
	TaskID   string      // 子任务 ID
	TaskName string      // 子任务名称
	Status   string      // 状态：Success, Failed
	Result   interface{} // 结果数据
	Error    string      // 错误信息（失败时）
}

// LeveledTaskQueue 二维任务队列（按拓扑层级组织，使用 map[string]Task）
type LeveledTaskQueue struct {
	queues        []map[string]workflow.Task // []map[string]Task，每个层级一个 map
	currentLevel  int32                      // atomic 操作，当前执行层级
	maxLevel      int                        // 最大层级（初始化时确定）
	mu            sync.RWMutex               // 仅保护队列结构变更（很少使用）
	sizes         []int32                    // atomic，每个层级的待提交任务数量
	runningCounts []int32                    // atomic，每个层级正在执行的任务数量
}

// NewLeveledTaskQueue 创建二维任务队列（使用 map[string]Task）
func NewLeveledTaskQueue(maxLevel int) *LeveledTaskQueue {
	queues := make([]map[string]workflow.Task, maxLevel)
	sizes := make([]int32, maxLevel)
	runningCounts := make([]int32, maxLevel)
	for i := 0; i < maxLevel; i++ {
		queues[i] = make(map[string]workflow.Task)
	}
	return &LeveledTaskQueue{
		queues:        queues,
		currentLevel:  0,
		maxLevel:      maxLevel,
		sizes:         sizes,
		runningCounts: runningCounts,
	}
}

func (q *LeveledTaskQueue) addTask(level int, task workflow.Task) {
	taskID := task.GetID()

	// 检查任务是否已经存在于当前层级的队列中
	if _, exists := q.queues[level][taskID]; exists {
		return // 任务已存在于当前层级，不重复添加
	}

	// 添加到目标层级
	q.queues[level][taskID] = task
	// 关键：在锁内更新 sizes，确保与队列状态一致，避免 IsEmpty 误判
	atomic.AddInt32(&q.sizes[level], 1)
}

// AddTask 添加任务到指定层级（无锁，通过 channel 调用）
func (q *LeveledTaskQueue) AddTask(level int, task workflow.Task) {
	if level < 0 || level >= len(q.queues) {
		return
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	q.addTask(level, task)
}

// AddTasks 批量添加任务到指定层级（无锁，通过 channel 调用）
func (q *LeveledTaskQueue) AddTasks(level int, tasks []workflow.Task) {
	if level < 0 || level >= len(q.queues) || len(tasks) == 0 {
		return
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	for _, task := range tasks {
		q.addTask(level, task)
	}
}

// PopTasks 从指定层级获取并移除任务（批量获取，获取时直接从队列移除）
// 任务被 Pop 后计入 runningCounts，表示正在执行
func (q *LeveledTaskQueue) PopTasks(level int, maxCount int) []workflow.Task {
	if level < 0 || level >= len(q.queues) {
		return nil
	}
	queue := q.queues[level]
	q.mu.Lock()
	defer q.mu.Unlock()

	tasks := make([]workflow.Task, 0, maxCount)
	count := 0
	for taskID, task := range queue {
		if count >= maxCount {
			break
		}
		tasks = append(tasks, task)
		delete(queue, taskID)
		atomic.AddInt32(&q.sizes[level], -1)
		atomic.AddInt32(&q.runningCounts[level], 1) // 标记为正在执行
		count++
	}
	return tasks
}

// RemoveTask 从指定层级移除任务（O(1) 时间复杂度）
func (q *LeveledTaskQueue) RemoveTask(level int, taskID string) {
	if level < 0 || level >= len(q.queues) {
		return
	}
	queue := q.queues[level]
	q.mu.Lock()
	if _, exists := queue[taskID]; exists {
		delete(queue, taskID)
		atomic.AddInt32(&q.sizes[level], -1)
	}
	q.mu.Unlock()
}

// TaskCompleted 标记任务完成，减少 runningCounts
func (q *LeveledTaskQueue) TaskCompleted(level int) {
	if level < 0 || level >= len(q.runningCounts) {
		return
	}
	atomic.AddInt32(&q.runningCounts[level], -1)
}

// IsEmpty 检查指定层级是否为空（只检查待提交队列）
func (q *LeveledTaskQueue) IsEmpty(level int) bool {
	if level < 0 || level >= len(q.queues) {
		return true
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.queues[level]) == 0
}

// IsLevelComplete 检查指定层级是否完全完成（队列为空且无执行中任务）
func (q *LeveledTaskQueue) IsLevelComplete(level int) bool {
	if level < 0 || level >= len(q.queues) {
		return true
	}
	q.mu.Lock()
	isEmpty := len(q.queues[level]) == 0
	q.mu.Unlock()
	runningCount := atomic.LoadInt32(&q.runningCounts[level])
	return isEmpty && runningCount == 0
}

// GetRunningCount 获取指定层级的执行中任务数
func (q *LeveledTaskQueue) GetRunningCount(level int) int32 {
	if level < 0 || level >= len(q.runningCounts) {
		return 0
	}
	return atomic.LoadInt32(&q.runningCounts[level])
}

// GetTaskIDsAtLevel 返回指定层级队列中的任务 ID 列表（用于进度 API 暴露待执行任务）
func (q *LeveledTaskQueue) GetTaskIDsAtLevel(level int) []string {
	if level < 0 || level >= len(q.queues) {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	queue := q.queues[level]
	ids := make([]string, 0, len(queue))
	for taskID := range queue {
		ids = append(ids, taskID)
	}
	return ids
}

// GetCurrentLevel 获取当前层级（atomic 读取）
func (q *LeveledTaskQueue) GetCurrentLevel() int {
	return int(atomic.LoadInt32(&q.currentLevel))
}

// AdvanceLevel 推进层级（atomic 操作）
func (q *LeveledTaskQueue) AdvanceLevel() {
	atomic.AddInt32(&q.currentLevel, 1)
}

// GetMaxLevel 获取最大层级（用于外部访问）
func (q *LeveledTaskQueue) GetMaxLevel() int {
	return q.maxLevel
}

// IsAllTasksCompleted 判断是否所有任务都已完成
// 条件：currentLevel >= len(queues) 且所有队列都为空
func (q *LeveledTaskQueue) IsAllTasksCompleted() (bool, error) {
	currentLevel := q.GetCurrentLevel()
	queueCount := len(q.queues)

	// 如果 currentLevel >= queueCount，说明已经处理完所有层级
	if currentLevel >= queueCount {
		// 检查所有队列是否都为空
		for i := 0; i < queueCount; i++ {
			if !q.IsEmpty(i) {
				// 异常情况：currentLevel >= queueCount 但队列不是空的
				return false, fmt.Errorf("异常：currentLevel (%d) >= queueCount (%d) 但队列 level %d 不为空",
					currentLevel, queueCount, i)
			}
		}
		// 所有队列都为空，任务全部完成
		return true, nil
	}

	// currentLevel < queueCount，还有任务未完成
	return false, nil
}

// TaskStatistics 任务统计（通过 channel 更新，避免锁）
type TaskStatistics struct {
	TotalTasks    int32 // atomic，总任务数
	StaticTasks   int32 // atomic，静态任务数
	SubTasks      int32 // atomic，子任务数
	SuccessTasks  int32 // atomic，成功任务数
	FailedTasks   int32 // atomic，失败任务数
	PendingTasks  int32 // atomic，等待任务数
	TemplateTasks int32 // atomic，模板任务数（当前层级）
}

// Update 更新统计（atomic 操作）
func (s *TaskStatistics) Update(update TaskStatsUpdate) {
	switch update.Type {
	case "task_completed":
		atomic.AddInt32(&s.SuccessTasks, 1)
		atomic.AddInt32(&s.PendingTasks, -1)
	case "task_failed":
		atomic.AddInt32(&s.FailedTasks, 1)
		atomic.AddInt32(&s.PendingTasks, -1)
	case "task_added":
		atomic.AddInt32(&s.TotalTasks, 1)
		atomic.AddInt32(&s.PendingTasks, 1)
		if update.IsSubTask {
			atomic.AddInt32(&s.SubTasks, 1)
		} else {
			atomic.AddInt32(&s.StaticTasks, 1)
		}
		if update.IsTemplate {
			atomic.AddInt32(&s.TemplateTasks, 1)
		}
	}
}

// 验证统计数是否一致：
// 总任务数 = 静态任务数 + 子任务数 = 成功任务数 + 失败任务数 + 等待任务数
func (s *TaskStatistics) Validate() bool {
	total := atomic.LoadInt32(&s.TotalTasks)
	static := atomic.LoadInt32(&s.StaticTasks)
	sub := atomic.LoadInt32(&s.SubTasks)
	success := atomic.LoadInt32(&s.SuccessTasks)
	failed := atomic.LoadInt32(&s.FailedTasks)
	pending := atomic.LoadInt32(&s.PendingTasks)

	// 总数 = 静态任务数 + 子任务数
	match1 := total == (static + sub)
	if !match1 {
		return false
	}

	// 总数 = 成功 + 失败 + 等待
	match2 := total == (success + failed + pending)
	if !match2 {
		return false
	}

	return true
}

// WorkflowInstanceManagerV2 新版WorkflowInstanceManager（基于生产者消费者模型）
type WorkflowInstanceManagerV2 struct {
	// 原有字段
	instance             *workflow.WorkflowInstance
	workflow             *workflow.Workflow
	dag                  dag.DAG
	executor             executor.Executor
	aggregateRepo        storage.WorkflowAggregateRepository // 聚合Repository（优先使用）
	taskRepo             storage.TaskRepository
	workflowInstanceRepo storage.WorkflowInstanceRepository
	registry             task.FunctionRegistry
	resultCache          cache.ResultCache
	ctx                  context.Context
	cancel               context.CancelFunc
	wg                   sync.WaitGroup

	// 新增：通道通信（无锁）
	taskStatusChan     chan TaskStatusEvent        // Executor -> Observer
	queueUpdateChan    chan TaskStatusEvent        // Observer -> QueueManager
	addSubTaskChan     chan AtomicAddSubTasksEvent // AddSubTask -> QueueManager（子任务添加专用通道，支持批量）
	taskSubmissionChan chan []workflow.Task        // QueueManager -> Submission
	taskStatsChan      chan TaskStatsUpdate        // Observer -> Statistics

	// 运行时任务存储（动态添加的子任务，不存储在 workflow 中）
	runtimeTasks sync.Map // taskID -> workflow.Task（子任务存储）

	// 子任务跟踪器（用于结果聚合，parentTaskID -> *SubTaskTracker）
	subTaskTracker sync.Map

	// 新增：队列结构
	taskQueue          *LeveledTaskQueue
	taskStats          *TaskStatistics
	templateTaskCounts []atomic.Int32 // 每层的模板任务数量（初始化时统计）
	templateTaskCount  atomic.Int32   // 当前层级的模板任务计数器（从 templateTaskCounts[currentLevel] 初始化）

	// 任务完成检查优化
	lastCompletionCheck int64 // atomic，上次完成检查的时间戳（纳秒），用于减少检查频率
	// “当前层为空、下一层 pending”诊断日志节流（纳秒），避免 ticker 每次触发都打一条导致刷屏
	lastLevelEmptyDiagnosticLog int64

	contextData    sync.Map // 上下文数据
	processedNodes sync.Map // 已处理任务标记（taskID -> bool）
	runningTaskIDs sync.Map // 正在执行的任务 ID（taskID -> struct{}），用于 GetProgress 暴露未完成任务

	// 层级推进锁（保护层级推进的原子性）
	levelAdvanceMu sync.Mutex // 保护 canAdvanceLevel 检查和 advanceLevel 执行的原子性

	// 控制信号（保留）
	controlSignalChan chan workflow.ControlSignal
	statusUpdateChan  chan string
	mu                sync.RWMutex // 仅保护 instance 状态

	// SAGA事务协调器（可选，接口类型）
	sagaCoordinator saga.Coordinator
	sagaEnabled     bool // 是否启用SAGA

	// 插件管理器（可选，接口类型）
	pluginManager plugin.PluginManager
}

// NewWorkflowInstanceManagerV2 创建WorkflowInstanceManagerV2实例
func NewWorkflowInstanceManagerV2(
	instance *workflow.WorkflowInstance,
	wf *workflow.Workflow,
	exec executor.Executor,
	taskRepo storage.TaskRepository,
	workflowInstanceRepo storage.WorkflowInstanceRepository,
	registry task.FunctionRegistry,
	pluginManager plugin.PluginManager,
) (*WorkflowInstanceManagerV2, error) {
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

	// 计算总任务数（用于设置 channel 容量）
	totalTasks := len(wf.GetTasks())
	channelCapacity := totalTasks * 2 // channel 容量为总任务数量的两倍
	if channelCapacity < 100 {
		channelCapacity = 100 // 最小容量 100，避免过小
	}

	// 检查是否需要启用SAGA（如果有任务配置了补偿函数）
	sagaEnabled := false
	for _, t := range wf.GetTasks() {
		if t.GetCompensationFuncName() != "" {
			sagaEnabled = true
			break
		}
	}

	// 如果启用SAGA，创建协调器
	var sagaCoordinator saga.Coordinator
	if sagaEnabled && registry != nil {
		sagaCoordinator = saga.NewCoordinator(instance.ID, registry)
		log.Printf("WorkflowInstance %s: SAGA事务已启用", instance.ID)
	}

	manager := &WorkflowInstanceManagerV2{
		instance:             instance,
		workflow:             wf,
		dag:                  dagInstance,
		executor:             exec,
		taskRepo:             taskRepo,
		workflowInstanceRepo: workflowInstanceRepo,
		registry:             registry,
		resultCache:          cache.NewMemoryResultCache(),
		ctx:                  ctx,
		cancel:               cancel,

		// 初始化 channel（容量为总任务数量的两倍）
		taskStatusChan:     make(chan TaskStatusEvent, channelCapacity),
		queueUpdateChan:    make(chan TaskStatusEvent, channelCapacity),
		addSubTaskChan:     make(chan AtomicAddSubTasksEvent, channelCapacity), // 子任务添加专用通道，支持批量
		taskSubmissionChan: make(chan []workflow.Task, channelCapacity),
		taskStatsChan:      make(chan TaskStatsUpdate, channelCapacity),

		// 初始化其他字段
		taskStats:         &TaskStatistics{},
		controlSignalChan: make(chan workflow.ControlSignal, 10),
		statusUpdateChan:  make(chan string, 10),
		sagaCoordinator:   sagaCoordinator,
		sagaEnabled:       sagaEnabled,
		pluginManager:     pluginManager,
	}

	log.Printf("WorkflowInstance %s: V2初始化完成，总任务数: %d，Channel 容量: %d",
		instance.ID, totalTasks, channelCapacity)

	// 在初始化时注册 InstanceManagerInterfaceV2 到 registry（只注册一次）
	if registry != nil {
		managerInterface := &InstanceManagerInterfaceV2{
			manager: manager,
		}
		_ = registry.RegisterDependencyWithKey("InstanceManager", managerInterface)
	}

	return manager, nil
}

// NewWorkflowInstanceManagerV2WithAggregate 创建WorkflowInstanceManagerV2实例（使用聚合Repository）
// aggregateRepo: 聚合Repository，优先使用，统一管理事务操作
// taskRepo, workflowInstanceRepo: 兼容旧版Repository，当aggregateRepo为nil时使用
func NewWorkflowInstanceManagerV2WithAggregate(
	instance *workflow.WorkflowInstance,
	wf *workflow.Workflow,
	exec executor.Executor,
	aggregateRepo storage.WorkflowAggregateRepository,
	taskRepo storage.TaskRepository,
	workflowInstanceRepo storage.WorkflowInstanceRepository,
	registry task.FunctionRegistry,
	pluginManager plugin.PluginManager,
) (*WorkflowInstanceManagerV2, error) {
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

	// 计算总任务数（用于设置 channel 容量）
	totalTasks := len(wf.GetTasks())
	channelCapacity := totalTasks * 2
	if channelCapacity < 100 {
		channelCapacity = 100
	}

	// 检查是否需要启用SAGA
	sagaEnabled := false
	for _, t := range wf.GetTasks() {
		if t.GetCompensationFuncName() != "" {
			sagaEnabled = true
			break
		}
	}

	// 如果启用SAGA，创建协调器
	var sagaCoordinator saga.Coordinator
	if sagaEnabled && registry != nil {
		sagaCoordinator = saga.NewCoordinator(instance.ID, registry)
		log.Printf("WorkflowInstance %s: SAGA事务已启用", instance.ID)
	}

	manager := &WorkflowInstanceManagerV2{
		instance:             instance,
		workflow:             wf,
		dag:                  dagInstance,
		executor:             exec,
		aggregateRepo:        aggregateRepo, // 设置聚合Repository
		taskRepo:             taskRepo,
		workflowInstanceRepo: workflowInstanceRepo,
		registry:             registry,
		resultCache:          cache.NewMemoryResultCache(),
		ctx:                  ctx,
		cancel:               cancel,

		taskStatusChan:     make(chan TaskStatusEvent, channelCapacity),
		queueUpdateChan:    make(chan TaskStatusEvent, channelCapacity),
		addSubTaskChan:     make(chan AtomicAddSubTasksEvent, channelCapacity),
		taskSubmissionChan: make(chan []workflow.Task, channelCapacity),
		taskStatsChan:      make(chan TaskStatsUpdate, channelCapacity),

		taskStats:         &TaskStatistics{},
		controlSignalChan: make(chan workflow.ControlSignal, 10),
		statusUpdateChan:  make(chan string, 10),
		sagaCoordinator:   sagaCoordinator,
		sagaEnabled:       sagaEnabled,
		pluginManager:     pluginManager,
	}

	log.Printf("WorkflowInstance %s: V2初始化完成（聚合Repository模式），总任务数: %d，Channel 容量: %d",
		instance.ID, totalTasks, channelCapacity)

	// 在初始化时注册 InstanceManagerInterfaceV2 到 registry（只注册一次）
	if registry != nil {
		managerInterface := &InstanceManagerInterfaceV2{
			manager: manager,
		}
		_ = registry.RegisterDependencyWithKey("InstanceManager", managerInterface)
	}

	return manager, nil
}

// Start 启动WorkflowInstance执行（公共方法，实现接口）
func (m *WorkflowInstanceManagerV2) Start() {
	// 更新状态为Running
	m.mu.Lock()
	m.instance.Status = "Running"
	m.instance.StartTime = time.Now()
	m.mu.Unlock()

	// 持久化状态
	ctx := context.Background()
	if err := m.updateWorkflowInstanceStatus(ctx, m.instance.ID, "Running", ""); err != nil {
		log.Printf("更新WorkflowInstance状态失败: %v", err)
	}

	// 发送状态更新通知
	select {
	case m.statusUpdateChan <- "Running":
	default:
		log.Printf("警告: WorkflowInstance %s 状态更新通道已满", m.instance.ID)
	}

	// 触发Workflow启动插件
	if m.pluginManager != nil {
		pluginData := plugin.PluginData{
			Event:      plugin.EventWorkflowStarted,
			WorkflowID: m.instance.WorkflowID,
			InstanceID: m.instance.ID,
			TaskID:     "",
			TaskName:   "",
			Status:     "Running",
			Error:      nil,
			Data: map[string]interface{}{
				"workflow_name": m.workflow.Name,
			},
		}
		if err := m.pluginManager.Trigger(m.ctx, plugin.EventWorkflowStarted, pluginData); err != nil {
			log.Printf("触发Workflow启动插件失败: InstanceID=%s, Error=%v", m.instance.ID, err)
		}
	}

	// 启动三个核心goroutine
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.taskObserverGoroutine()
	}()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.queueManagerGoroutine()
	}()

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

// taskObserverGoroutine 状态观察器（Goroutine 1）
func (m *WorkflowInstanceManagerV2) taskObserverGoroutine() {

	// 批量处理缓冲区
	batchSize := 10
	batch := make([]TaskStatusEvent, 0, batchSize)
	ticker := time.NewTicker(1 * time.Millisecond) // 批量处理间隔（优化：减少延迟）
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			// 处理剩余批次
			if len(batch) > 0 {
				m.processBatch(batch)
			}
			return

		case event := <-m.taskStatusChan:
			// 添加到批次
			batch = append(batch, event)

			// 批次满了，立即处理
			if len(batch) >= batchSize {
				m.processBatch(batch)
				batch = batch[:0]
			}

		case <-ticker.C:
			// 定时处理批次（避免长时间等待）
			if len(batch) > 0 {
				m.processBatch(batch)
				batch = batch[:0]
			}
		}
	}
}

// processBatch 批量处理事件
func (m *WorkflowInstanceManagerV2) processBatch(batch []TaskStatusEvent) {
	for _, event := range batch {
		// 直接同步更新统计（避免竞态条件）
		statsType := getStatsType(event.Status)
		if statsType != "" {
			m.taskStats.Update(TaskStatsUpdate{
				Type:       statsType,
				TaskID:     event.TaskID,
				Status:     event.Status,
				IsTemplate: event.IsTemplate,
				IsSubTask:  event.IsSubTask,
			})
		}

		// 必须阻塞发送，否则事件丢失会导致 handleTaskCompletion 未调用、TaskCompleted(level) 未执行，
		// runningCount 无法归零、层级无法推进，出现“大量 pending 但无任务在执行”
		select {
		case m.queueUpdateChan <- event:
			// 事件已发送
		case <-m.ctx.Done():
			log.Printf("Context 已取消，停止发送事件: TaskID=%s", event.TaskID)
			return
		}
	}
}

// getStatsType 获取统计类型（状态大小写不敏感）
func getStatsType(status string) string {
	switch {
	case task.IsSuccessStatus(status):
		return "task_completed"
	case task.IsFailedStatus(status), task.IsTimeoutStatus(status):
		return "task_failed"
	default:
		return ""
	}
}

// queueManagerGoroutine 队列管理器（Goroutine 2）
func (m *WorkflowInstanceManagerV2) queueManagerGoroutine() {

	// 初始化：执行拓扑排序、初始化任务队列、按层级添加任务并统计模板任务数量
	m.initTaskQueue()

	// 使用 templateTaskCounts slice 初始化当前 level 的 templateTaskCount（使用 CAS 避免竞态条件）
	currentLevel := m.taskQueue.GetCurrentLevel()
	if currentLevel < len(m.templateTaskCounts) {
		expectedValue := int32(0) // 期望的旧值（初始化为0）
		newValue := m.templateTaskCounts[currentLevel].Load()
		// 使用 CAS 原子性地设置 templateTaskCount
		if m.templateTaskCount.CompareAndSwap(expectedValue, newValue) {
			log.Printf("WorkflowInstance %s: 初始化 Level %d 的 templateTaskCount = %d",
				m.instance.ID, currentLevel, newValue)
		} else {
			// CAS 失败，说明已经被其他 goroutine 初始化，直接加载当前值
			currentValue := m.templateTaskCount.Load()
			log.Printf("WorkflowInstance %s: templateTaskCount 已被初始化，当前值 = %d (Level %d)",
				m.instance.ID, currentValue, currentLevel)
		}
	}

	// 启动统计更新处理goroutine（异步处理，但会在检查完成前同步）
	go func() {
		for {
			select {
			case <-m.ctx.Done():
				return
			case update := <-m.taskStatsChan:
				m.taskStats.Update(update)
			}
		}
	}()

	for {
		select {
		case <-m.ctx.Done():
			// 处理剩余事件
			m.drainQueueUpdateChan()
			return

		case update := <-m.taskStatsChan:
			// 同步处理统计更新（优先处理，确保统计准确）
			m.taskStats.Update(update)
			continue

		case atomicAddSubTasksEvent := <-m.addSubTaskChan:
			// 处理原子性子任务添加事件（从专用 channel 接收）
			// 必须保证模板任务执行的原子性
			m.handleAtomicAddSubTasks(atomicAddSubTasksEvent)
			// 子任务添加后，检查是否可以推进层级
			// 注意：子任务被添加到当前层级，所以当前层级不为空，不会推进层级
			// 但是，如果模板任务计数为0，且当前层级为空，说明所有子任务都已完成，可以推进
			m.tryAdvanceLevel()

		case event := <-m.queueUpdateChan:
			// 根据事件类型处理（状态大小写不敏感）
			norm := task.NormalizeTaskStatus(event.Status)
			switch norm {
			case task.TaskStatusSuccess, task.TaskStatusFailed, task.TaskStatusTimeout:
				m.handleTaskCompletion(event)
			case "ready":
				// 就绪任务事件：任务已在初始化时按层级加入队列
			default:
				// 其他状态忽略
			}

			// 先添加新任务，再检查层级推进
			m.tryAdvanceLevel()

			// 先处理所有待处理的统计更新（确保统计准确）
			m.drainTaskStatsChan()

			// 使用快速检查：从TaskStatistics获取已完成任务数和总任务数
			successCount := atomic.LoadInt32(&m.taskStats.SuccessTasks)
			failedCount := atomic.LoadInt32(&m.taskStats.FailedTasks)
			totalCount := atomic.LoadInt32(&m.taskStats.TotalTasks)
			completed := successCount + failedCount

			// 减少检查频率：每 10ms 最多检查一次，或已完成数达到总任务数时检查（优化：更快响应）
			now := time.Now().UnixNano()
			lastCheck := atomic.LoadInt64(&m.lastCompletionCheck)
			shouldCheck := false

			if completed >= totalCount && totalCount > 0 {
				// 已完成数达到总任务数，必须检查
				shouldCheck = true
			} else if now-lastCheck > 10*int64(time.Millisecond) {
				// 距离上次检查超过 10ms，可以检查（优化：减少延迟）
				shouldCheck = true
			}

			if shouldCheck {
				atomic.StoreInt64(&m.lastCompletionCheck, now)

				// 详细检查：验证队列状态
				allCompleted, err := m.checkAllTasksCompleted(completed, totalCount)
				if err != nil {
					log.Printf("错误: WorkflowInstance %s 任务完成检查异常: %v, completed=%d, total=%d",
						m.instance.ID, err, completed, totalCount)
					continue
				}

				// 调试日志
				if completed >= totalCount && totalCount > 0 {
					currentLevel := m.taskQueue.GetCurrentLevel()
					maxLevel := m.taskQueue.GetMaxLevel()
					log.Printf("调试: WorkflowInstance %s 任务计数检查: completed=%d, total=%d, currentLevel=%d, maxLevel=%d",
						m.instance.ID, completed, totalCount, currentLevel, maxLevel)
					// 检查队列状态
					for i := 0; i < maxLevel; i++ {
						isEmpty := m.taskQueue.IsEmpty(i)
						size := atomic.LoadInt32(&m.taskQueue.sizes[i])
						log.Printf("调试: Level %d: isEmpty=%v, size=%d", i, isEmpty, size)
					}
				}

				if allCompleted {
					// 检查是否有失败的任务
					hasFailedTask := false
					allTasks := m.workflow.GetTasks()
					log.Printf("🔍 [Workflow完成检查] 开始检查失败任务，总任务数: %d", len(allTasks))
					for taskID, wfTask := range allTasks {
						taskStatus := wfTask.GetStatus()
						taskName := wfTask.GetName()
						log.Printf("🔍 [Workflow完成检查] 检查任务: TaskID=%s, TaskName=%s, Status=%s", taskID, taskName, taskStatus)
						if task.IsFailedStatus(taskStatus) {
							log.Printf("🔍 [Workflow完成检查] ✅ 发现失败任务: TaskID=%s, TaskName=%s, Status=%s", taskID, taskName, taskStatus)
							hasFailedTask = true
							break
						}
						// 检查 contextData 中的错误信息
						errorKey := fmt.Sprintf("%s:error", taskID)
						if _, hasError := m.contextData.Load(errorKey); hasError {
							log.Printf("🔍 [Workflow完成检查] ✅ 发现失败任务（通过errorKey）: TaskID=%s, TaskName=%s", taskID, taskName)
							hasFailedTask = true
							break
						}
					}
					// 也检查运行时任务（动态添加的子任务）
					if !hasFailedTask {
						m.runtimeTasks.Range(func(key, value interface{}) bool {
							if wfTask, ok := value.(workflow.Task); ok {
								if task.IsFailedStatus(wfTask.GetStatus()) {
									log.Printf("🔍 [Workflow完成检查] 发现失败运行时任务: TaskID=%s, TaskName=%s", wfTask.GetID(), wfTask.GetName())
									hasFailedTask = true
									return false // 停止遍历
								}
								// 检查 contextData 中的错误信息
								errorKey := fmt.Sprintf("%s:error", wfTask.GetID())
								if _, hasError := m.contextData.Load(errorKey); hasError {
									log.Printf("🔍 [Workflow完成检查] 发现失败运行时任务（通过errorKey）: TaskID=%s, TaskName=%s", wfTask.GetID(), wfTask.GetName())
									hasFailedTask = true
									return false // 停止遍历
								}
							}
							return true
						})
					}

					// 根据是否有失败任务决定最终状态
					finalStatus := "Success"
					if hasFailedTask {
						finalStatus = "Failed"
						m.mu.Lock()
						m.instance.Status = "Failed"
						m.instance.ErrorMessage = "部分任务执行失败"
						m.mu.Unlock()

						// 如果启用了SAGA，触发补偿
						if m.sagaEnabled && m.sagaCoordinator != nil {
							ctx := context.Background()
							if err := m.sagaCoordinator.Compensate(ctx); err != nil {
								log.Printf("⚠️ [SAGA] WorkflowInstance %s, 补偿执行失败: %v", m.instance.ID, err)
							}
						}
					} else {
						m.mu.Lock()
						m.instance.Status = "Success"
						m.mu.Unlock()

						// 如果启用了SAGA，提交事务
						if m.sagaEnabled && m.sagaCoordinator != nil {
							if err := m.sagaCoordinator.Commit(); err != nil {
								log.Printf("⚠️ [SAGA] WorkflowInstance %s, 事务提交失败: %v", m.instance.ID, err)
							}
						}
					}

					m.mu.Lock()
					now := time.Now()
					m.instance.EndTime = &now
					m.mu.Unlock()

					ctx := context.Background()
					m.saveAllTaskStatuses(ctx)
					if err := m.updateWorkflowInstanceStatus(ctx, m.instance.ID, finalStatus, ""); err != nil {
						log.Printf("更新WorkflowInstance状态失败: %v", err)
					}

					// 发送状态更新通知
					select {
					case m.statusUpdateChan <- finalStatus:
					default:
						log.Printf("警告: WorkflowInstance %s 状态更新通道已满", m.instance.ID)
					}

					// 触发Workflow完成/失败插件
					if m.pluginManager != nil {
						var event plugin.TriggerEvent
						if finalStatus == "Success" {
							event = plugin.EventWorkflowCompleted
						} else {
							event = plugin.EventWorkflowFailed
						}
						pluginData := plugin.PluginData{
							Event:      event,
							WorkflowID: m.instance.WorkflowID,
							InstanceID: m.instance.ID,
							TaskID:     "",
							TaskName:   "",
							Status:     finalStatus,
							Error:      nil,
							Data: map[string]interface{}{
								"workflow_name": m.workflow.Name,
								"total_tasks":   totalCount,
								"completed":     completed,
							},
						}
						if finalStatus == "Failed" {
							pluginData.Error = fmt.Errorf("部分任务执行失败")
							pluginData.Data["error"] = "部分任务执行失败"
						}
						if err := m.pluginManager.Trigger(m.ctx, event, pluginData); err != nil {
							log.Printf("触发Workflow %s插件失败: InstanceID=%s, Error=%v", finalStatus, m.instance.ID, err)
						}
					}

					log.Printf("WorkflowInstance %s: 所有任务已完成，最终状态: %s", m.instance.ID, finalStatus)
					return
				}
			}
		}
	}
}

// initTaskQueue 初始化任务队列（使用 DAG 拓扑排序结果）
func (m *WorkflowInstanceManagerV2) initTaskQueue() {
	// 1. 执行拓扑排序
	topoOrder, err := m.dag.TopologicalSort()
	if err != nil {
		log.Printf("WorkflowInstance %s: 拓扑排序失败: %v，创建空队列", m.instance.ID, err)
		// 即使拓扑排序失败，也创建一个空队列，避免后续 nil 指针异常
		maxLevel := 1
		m.taskQueue = NewLeveledTaskQueue(maxLevel)
		m.templateTaskCounts = make([]atomic.Int32, maxLevel)
		return
	}

	// 2. 初始化任务队列（层级数 = 拓扑排序的层级数）
	maxLevel := len(topoOrder.Levels)

	// 处理空 Workflow
	if maxLevel == 0 {
		log.Printf("WorkflowInstance %s: Workflow 为空，创建空队列", m.instance.ID)
		maxLevel = 1
		m.taskQueue = NewLeveledTaskQueue(maxLevel)
		m.templateTaskCounts = make([]atomic.Int32, maxLevel)
		return
	}

	m.taskQueue = NewLeveledTaskQueue(maxLevel)

	// 获取所有任务
	allTasks := m.workflow.GetTasks()

	// 3. 按层级逐层添加任务，并统计该层模板任务的数量
	m.templateTaskCounts = make([]atomic.Int32, maxLevel)

	for level, taskIDs := range topoOrder.Levels {
		templateCount := int32(0)
		for _, taskID := range taskIDs {
			if task, exists := allTasks[taskID]; exists {
				// 添加到对应层级的队列
				m.taskQueue.AddTask(level, task)

				// 记录任务原本的层级（用于重试时使用）
				levelKey := fmt.Sprintf("%s:original_level", taskID)
				m.contextData.Store(levelKey, level)

				// 统计该层的模板任务数量
				if task.IsTemplate() {
					templateCount++
				}
			}
		}
		// 保存该层的模板任务数量
		m.templateTaskCounts[level].Store(templateCount)
	}

	// 4. 初始化任务统计（通过taskStatsChan发送task_added事件）
	for taskID := range allTasks {
		task := allTasks[taskID]
		select {
		case m.taskStatsChan <- TaskStatsUpdate{
			Type:       "task_added",
			TaskID:     taskID,
			IsTemplate: task.IsTemplate(),
			IsSubTask:  task.IsSubTask(),
		}:
		default:
			log.Printf("警告: taskStatsChan 已满，任务统计更新可能丢失: TaskID=%s", taskID)
		}
	}
	atomic.StoreInt64(&m.lastCompletionCheck, time.Now().UnixNano())

	log.Printf("WorkflowInstance %s: 任务队列初始化完成，层级数: %d，总任务数: %d",
		m.instance.ID, maxLevel, len(allTasks))
	for level, _ := range m.templateTaskCounts {
		count := m.templateTaskCounts[level].Load()
		if count > 0 {
			log.Printf("  Level %d: %d 个模板任务", level, count)
		}
	}
}

// handleTaskCompletion 处理任务完成事件
func (m *WorkflowInstanceManagerV2) handleTaskCompletion(event TaskStatusEvent) {
	// 从“正在执行”集合移除，供 GetProgress 暴露未完成任务
	m.runningTaskIDs.Delete(event.TaskID)

	// 标记任务执行完成，减少 runningCounts（无论成功或失败）
	levelKey := fmt.Sprintf("%s:original_level", event.TaskID)
	if levelVal, ok := m.contextData.Load(levelKey); ok {
		if level, ok := levelVal.(int); ok {
			m.taskQueue.TaskCompleted(level)
		}
	}

	// 处理任务失败重试逻辑（状态大小写不敏感）
	if task.IsFailedStatus(event.Status) {
		m.handleTaskFailure(event)
		return
	}

	// 处理任务成功逻辑
	if task.IsSuccessStatus(event.Status) {
		m.handleTaskSuccess(event)
	}
}

// handleTaskSuccess 处理任务成功事件
func (m *WorkflowInstanceManagerV2) handleTaskSuccess(event TaskStatusEvent) {
	// 检查任务是否已经被处理过（避免重复处理）
	// 使用 LoadOrStore 确保原子性，避免并发时重复处理
	if _, loaded := m.processedNodes.LoadOrStore(event.TaskID, true); loaded {
		return // 已经处理过，直接返回
	}

	// 注意：任务完成计数已通过taskStatsChan在processBatch中更新，这里不需要再计数
	// 注意：模板任务计数统一在 handleAtomicAddSubTasks 中处理，这里不处理

	// 保存结果到上下文
	if event.Result != nil {
		m.contextData.Store(event.TaskID, event.Result)

		// 缓存结果
		if m.resultCache != nil {
			ttl := 1 * time.Hour
			_ = m.resultCache.Set(event.TaskID, event.Result, ttl)
		}
	}

	// 如果是子任务，处理子任务完成逻辑（记录结果，触发聚合）
	if event.IsSubTask {
		m.handleSubTaskCompletion(event)
	}

	// 如果是模板任务，检查并批量添加等待中的子任务
	// 这是新设计中的关键步骤：模板任务的 Job Function 执行完毕后，触发等待中的子任务添加
	if event.IsTemplate {
		m.processPendingSubTasks(event.TaskID)
	}
}

// processPendingSubTasks 处理等待中的子任务（模板任务成功后调用）
func (m *WorkflowInstanceManagerV2) processPendingSubTasks(parentTaskID string) {
	subTasksKey := fmt.Sprintf("%s:subtasks", parentTaskID)
	subTasksValue, exists := m.contextData.Load(subTasksKey)
	if !exists {
		return // 没有等待中的子任务
	}

	// 类型检查
	subTasksList, ok := subTasksValue.([]workflow.Task)
	if !ok {
		log.Printf("警告: WorkflowInstance %s: contextData 中的子任务列表类型错误，ParentTaskID=%s", m.instance.ID, parentTaskID)
		m.contextData.Delete(subTasksKey)
		return
	}

	if len(subTasksList) == 0 {
		m.contextData.Delete(subTasksKey)
		return
	}

	// 获取父任务信息
	parentTask, exists := m.workflow.GetTasks()[parentTaskID]
	if !exists {
		log.Printf("警告: WorkflowInstance %s: 父任务不存在，ParentTaskID=%s", m.instance.ID, parentTaskID)
		return
	}

	// 获取目标层级
	currentLevel := m.taskQueue.GetCurrentLevel()
	targetLevel := currentLevel

	// 批量添加子任务到队列
	m.taskQueue.AddTasks(targetLevel, subTasksList)

	// 存储子任务的层级
	for _, subTask := range subTasksList {
		levelKey := fmt.Sprintf("%s:original_level", subTask.GetID())
		m.contextData.Store(levelKey, targetLevel)
	}

	// 清空已收集的子任务列表
	m.contextData.Delete(subTasksKey)

	log.Printf("WorkflowInstance %s: 模板任务 %s 成功后，批量添加 %d 个等待中的子任务到 level %d",
		m.instance.ID, parentTaskID, len(subTasksList), targetLevel)

	// 减少模板任务计数
	if parentTask.IsTemplate() {
		m.decrementTemplateTaskCount(parentTaskID, targetLevel, len(subTasksList))
	}
}

// handleTaskFailure 处理任务失败事件（支持重试）
func (m *WorkflowInstanceManagerV2) handleTaskFailure(event TaskStatusEvent) {
	// 获取任务信息
	task, exists := m.workflow.GetTasks()[event.TaskID]
	if !exists {
		// 可能是运行时任务
		if runtimeTask, ok := m.runtimeTasks.Load(event.TaskID); ok {
			task = runtimeTask.(workflow.Task)
			exists = true
		}
	}
	if !exists {
		log.Printf("警告: 失败的任务不存在: TaskID=%s", event.TaskID)
		return
	}

	// 检查是否可以重试
	retryCount := task.GetRetryCount()
	currentRetries := m.getTaskRetryCount(event.TaskID)

	if currentRetries < retryCount {
		// 优先使用任务原本的层级
		levelKey := fmt.Sprintf("%s:original_level", event.TaskID)
		originalLevel := -1
		if levelValue, exists := m.contextData.Load(levelKey); exists {
			if level, ok := levelValue.(int); ok {
				originalLevel = level
			}
		}

		currentLevel := m.taskQueue.GetCurrentLevel()
		targetLevel := currentLevel

		// 优先使用原层级，但如果原层级 > 当前层级，使用当前层级并记录警告
		if originalLevel >= 0 {
			if originalLevel <= currentLevel {
				targetLevel = originalLevel
			} else {
				// 原层级 > 当前层级，使用当前层级（但记录警告）
				targetLevel = currentLevel
				log.Printf("警告: WorkflowInstance %s: 任务 %s 原层级 %d > 当前层级 %d，使用当前层级 %d 进行重试",
					m.instance.ID, event.TaskID, originalLevel, currentLevel, targetLevel)
			}
		}

		// 重置任务状态
		task.SetStatus("PENDING")

		// 添加到目标层级队列
		m.taskQueue.AddTask(targetLevel, task)

		// 增加重试计数
		m.incrementTaskRetryCount(event.TaskID)

		log.Printf("WorkflowInstance %s: 任务 %s 失败，重试 %d/%d，添加到 level %d (原层级: %d)",
			m.instance.ID, event.TaskID, currentRetries+1, retryCount, targetLevel, originalLevel)

		// 注意：不调用 notifyTaskReady，统一通过 fetchTasksFromQueue 从队列获取任务
	} else {
		// 超过最大重试次数，警告并移除任务
		errorMsg := "未知错误"
		if event.Error != nil {
			errorMsg = event.Error.Error()
		}

		log.Printf("⚠️ 警告: WorkflowInstance %s: 任务 %s (%s) 失败，已达到最大重试次数 %d，将移除任务。错误: %s",
			m.instance.ID, event.TaskID, task.GetName(), retryCount, errorMsg)

		// 从当前层级队列中移除任务
		currentLevel := m.taskQueue.GetCurrentLevel()
		m.taskQueue.RemoveTask(currentLevel, event.TaskID)

		// 保存错误信息到上下文
		errorKey := fmt.Sprintf("%s:error", event.TaskID)
		m.contextData.Store(errorKey, fmt.Sprintf("超过最大重试次数 %d: %s", retryCount, errorMsg))

		// 更新任务状态为最终失败
		task.SetStatus("FAILED")

		// 标记为已处理（最终失败）
		m.processedNodes.Store(event.TaskID, true)

		// 如果是模板任务，减少计数（最终失败）
		if event.IsTemplate {
			m.templateTaskCounts[currentLevel].Add(-1)
		}

		// 执行 Task 的 Failed 状态 Handler（重要：达到最大重试次数时也需要触发）
		m.executeTaskFailedHandler(event.TaskID, task, errorMsg)

		// 如果是子任务，处理子任务失败逻辑（记录结果，触发聚合）
		if event.IsSubTask {
			m.handleSubTaskFailure(event)
		}

		// 注意：任务失败计数已通过taskStatsChan在processBatch中更新，这里不需要再计数
	}
}

// getTaskRetryCount 获取任务的重试次数
func (m *WorkflowInstanceManagerV2) getTaskRetryCount(taskID string) int {
	retryKey := fmt.Sprintf("%s:retry_count", taskID)
	if count, exists := m.contextData.Load(retryKey); exists {
		if retryCount, ok := count.(int); ok {
			return retryCount
		}
	}
	return 0
}

// incrementTaskRetryCount 增加任务的重试次数
func (m *WorkflowInstanceManagerV2) incrementTaskRetryCount(taskID string) {
	retryKey := fmt.Sprintf("%s:retry_count", taskID)
	currentCount := m.getTaskRetryCount(taskID)
	m.contextData.Store(retryKey, currentCount+1)
}

// canAdvanceLevel 判断是否可以推进 currentLevel（内部方法，需要在锁保护下调用）
func (m *WorkflowInstanceManagerV2) canAdvanceLevel() bool {
	currentLevel := m.taskQueue.GetCurrentLevel()

	// 1. 当前 level 必须完全完成（队列为空且无执行中任务）
	// 使用 IsLevelComplete 检查，确保所有任务都已执行完毕
	if !m.taskQueue.IsLevelComplete(currentLevel) {
		runningCount := m.taskQueue.GetRunningCount(currentLevel)
		isEmpty := m.taskQueue.IsEmpty(currentLevel)
		log.Printf("调试: canAdvanceLevel=false，currentLevel=%d，isEmpty=%v，runningCount=%d",
			currentLevel, isEmpty, runningCount)
		return false
	}

	// 2. 没有待处理的模板任务（使用当前层级的 templateTaskCount）
	if m.templateTaskCount.Load() > 0 {
		return false
	}

	// 3. 检查当前层级的所有模板任务的子任务是否都已完成
	if !m.allSubTasksCompleted(currentLevel) {
		log.Printf("调试: canAdvanceLevel=false，currentLevel=%d，子任务未全部完成", currentLevel)
		return false
	}

	// 4. 检查是否有下一层级
	if currentLevel >= m.taskQueue.GetMaxLevel() {
		return false
	}

	return true
}

// advanceLevel 推进 currentLevel（内部方法，需要在锁保护下调用）
func (m *WorkflowInstanceManagerV2) advanceLevel() {
	oldLevel := m.taskQueue.GetCurrentLevel()
	m.taskQueue.AdvanceLevel()
	newLevel := m.taskQueue.GetCurrentLevel()

	// 使用 CAS 从 templateTaskCounts[newLevel] 读取并设置 templateTaskCount（避免竞态条件）
	if newLevel < len(m.templateTaskCounts) {
		// 读取旧值（当前层级的 templateTaskCount，应该为0）
		oldValue := m.templateTaskCount.Load()
		newValue := m.templateTaskCounts[newLevel].Load()

		// 使用 CAS 原子性地更新 templateTaskCount
		// 期望旧值为 oldValue（当前值），新值为 newValue（新层级的模板任务数）
		if m.templateTaskCount.CompareAndSwap(oldValue, newValue) {
			log.Printf("WorkflowInstance %s: currentLevel 从 %d 推进到 %d，templateTaskCount 从 %d 更新为 %d",
				m.instance.ID, oldLevel, newLevel, oldValue, newValue)
		} else {
			// CAS 失败，说明 templateTaskCount 已被其他 goroutine 修改
			// 重新读取当前值并重试
			currentValue := m.templateTaskCount.Load()
			if m.templateTaskCount.CompareAndSwap(currentValue, newValue) {
				log.Printf("WorkflowInstance %s: currentLevel 从 %d 推进到 %d，templateTaskCount 从 %d 更新为 %d (重试成功)",
					m.instance.ID, oldLevel, newLevel, currentValue, newValue)
			} else {
				// 重试失败，记录警告但继续执行
				log.Printf("警告: WorkflowInstance %s: currentLevel 从 %d 推进到 %d，但 templateTaskCount CAS 更新失败，当前值 = %d",
					m.instance.ID, oldLevel, newLevel, m.templateTaskCount.Load())
			}
		}
	} else {
		log.Printf("WorkflowInstance %s: currentLevel 从 %d 推进到 %d (新层级超出范围，templateTaskCount 保持为 %d)",
			m.instance.ID, oldLevel, newLevel, m.templateTaskCount.Load())
	}
}

// tryAdvanceLevel 原子地检查和推进层级
func (m *WorkflowInstanceManagerV2) tryAdvanceLevel() bool {
	m.levelAdvanceMu.Lock()
	defer m.levelAdvanceMu.Unlock()

	if !m.canAdvanceLevel() {
		return false
	}

	m.advanceLevel()
	return true
}

// notifyTaskReady 通知任务就绪
func (m *WorkflowInstanceManagerV2) notifyTaskReady(task workflow.Task) {
	select {
	case m.taskSubmissionChan <- []workflow.Task{task}:
	case <-time.After(5 * time.Second):
		log.Printf("警告: taskSubmissionChan 发送超时，任务可能丢失: TaskID=%s", task.GetID())
	case <-m.ctx.Done():
		log.Printf("Context 已取消，停止发送任务: TaskID=%s", task.GetID())
	}
}

// checkAllTasksCompleted 检查是否所有任务都已完成
func (m *WorkflowInstanceManagerV2) checkAllTasksCompleted(completedCount, totalCount int32) (bool, error) {
	// 先验证计数一致性
	if err := m.validateTaskCounts(); err != nil {
		return false, err
	}

	// 快速检查：已完成数必须 >= 总任务数
	if completedCount < totalCount {
		return false, nil
	}

	// 如果已完成数 >= 总任务数，尝试推进到最终层级（如果还没有）
	// 持续尝试推进层级，直到无法推进或到达最大层级
	maxLevel := m.taskQueue.GetMaxLevel()
	for {
		currentLevel := m.taskQueue.GetCurrentLevel()
		if currentLevel >= maxLevel {
			break
		}
		// 检查当前层级是否为空，如果为空则推进
		// 但是，如果当前层级还有任务（比如动态添加的子任务），不应该推进
		if m.taskQueue.IsEmpty(currentLevel) && m.templateTaskCount.Load() == 0 {
			// 使用 tryAdvanceLevel 原子性地检查和推进
			if !m.tryAdvanceLevel() {
				// 无法推进，退出循环
				break
			}
		} else {
			// 当前层级不为空或还有模板任务，无法推进
			break
		}
	}

	// 详细检查：验证队列状态和层级
	isCompleted, err := m.taskQueue.IsAllTasksCompleted()
	if err != nil {
		log.Printf("WorkflowInstance %s: IsAllTasksCompleted 检查异常: %v", m.instance.ID, err)
		return false, err
	}
	if isCompleted {
		// 检查是否有失败任务（用于日志）
		hasFailed := false
		allTasks := m.workflow.GetTasks()
		for taskID, task := range allTasks {
			if task.GetStatus() == "FAILED" {
				hasFailed = true
				log.Printf("WorkflowInstance %s: 所有任务已完成，但发现失败任务: TaskID=%s, TaskName=%s", m.instance.ID, taskID, task.GetName())
				break
			}
			errorKey := fmt.Sprintf("%s:error", taskID)
			if _, hasError := m.contextData.Load(errorKey); hasError {
				hasFailed = true
				log.Printf("WorkflowInstance %s: 所有任务已完成，但发现失败任务（通过errorKey）: TaskID=%s, TaskName=%s", m.instance.ID, taskID, task.GetName())
				break
			}
		}
		if !hasFailed {
			log.Printf("WorkflowInstance %s: 所有任务已完成，最终状态: Success", m.instance.ID)
		}
	}
	return isCompleted, nil
}

// validateTaskCounts 验证任务计数一致性
func (m *WorkflowInstanceManagerV2) validateTaskCounts() error {
	isValid := m.taskStats.Validate()
	if !isValid {
		return fmt.Errorf("任务统计不一致")
	}
	return nil
}

// drainQueueUpdateChan 处理剩余事件
func (m *WorkflowInstanceManagerV2) drainQueueUpdateChan() {
	timeout := time.After(2 * time.Second)
	for {
		select {
		case event := <-m.queueUpdateChan:
			norm := task.NormalizeTaskStatus(event.Status)
			switch norm {
			case task.TaskStatusSuccess, task.TaskStatusFailed, task.TaskStatusTimeout:
				m.handleTaskCompletion(event)
			case "ready":
				// 就绪任务事件
			}
		case <-timeout:
			log.Printf("WorkflowInstance %s: 处理剩余事件超时", m.instance.ID)
			return
		default:
			return
		}
	}
}

// drainTaskStatsChan 处理所有待处理的统计更新（非阻塞）
func (m *WorkflowInstanceManagerV2) drainTaskStatsChan() {
	for {
		select {
		case update := <-m.taskStatsChan:
			m.taskStats.Update(update)
		default:
			return
		}
	}
}

// taskSubmissionGoroutine 任务提交器（Goroutine 3）
func (m *WorkflowInstanceManagerV2) taskSubmissionGoroutine() {

	maxBatchSize := 10
	batch := make([]workflow.Task, 0, maxBatchSize)
	ticker := time.NewTicker(5 * time.Millisecond) // 批量提交间隔（优化：减少延迟，更快响应）
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			// 提交剩余批次
			if len(batch) > 0 {
				m.submitBatch(batch)
			}
			return

		case tasks := <-m.taskSubmissionChan:
			// 进入提交管道的任务（含动态 notifyTaskReady）统一计入“运行中”
			for _, t := range tasks {
				m.runningTaskIDs.Store(t.GetID(), struct{}{})
			}
			// 添加到批次
			batch = append(batch, tasks...)

			// 批次满了，立即提交
			if len(batch) >= maxBatchSize {
				m.submitBatch(batch)
				batch = batch[:0]
			}

		case <-ticker.C:
			// 定时提交批次
			if len(batch) > 0 {
				m.submitBatch(batch)
				batch = batch[:0]
			}

			// 从队列获取任务（根据 currentLevel）
			m.fetchTasksFromQueue()
		}
	}
}

// fetchTasksFromQueue 从队列获取任务
func (m *WorkflowInstanceManagerV2) fetchTasksFromQueue() {
	// 检查 taskQueue 是否已初始化
	if m.taskQueue == nil {
		return
	}

	currentLevel := m.taskQueue.GetCurrentLevel()
	maxLevel := m.taskQueue.GetMaxLevel()
	pendingAtCurrent := len(m.taskQueue.GetTaskIDsAtLevel(currentLevel))

	// 从 workflow 获取最大并发任务数
	maxConcurrent := m.workflow.GetMaxConcurrentTask()
	if maxConcurrent <= 0 {
		maxConcurrent = 10 // 默认值
	}

	// 从当前层级获取并移除任务
	tasks := m.taskQueue.PopTasks(currentLevel, maxConcurrent)

	if len(tasks) > 0 {
		ids := make([]string, 0, len(tasks))
		for _, t := range tasks {
			ids = append(ids, t.GetID())
		}
		log.Printf("[进度诊断] WorkflowInstance %s: Pop 出队 level=%d maxLevel=%d count=%d task_ids=%v",
			m.instance.ID, currentLevel, maxLevel, len(tasks), ids)
		select {
		case m.taskSubmissionChan <- tasks:
			// 接收方 taskSubmissionGoroutine 会统一计入 runningTaskIDs
		case <-time.After(5 * time.Second):
			log.Printf("警告: taskSubmissionChan 发送超时，任务加回队列: count=%d", len(tasks))
			for _, task := range tasks {
				m.taskQueue.AddTask(currentLevel, task)
			}
		case <-m.ctx.Done():
			log.Printf("Context 已取消，任务加回队列: count=%d", len(tasks))
			for _, task := range tasks {
				m.taskQueue.AddTask(currentLevel, task)
			}
		}
	} else if pendingAtCurrent > 0 {
		// 当前层级队列应有任务但 Pop 返回 0（可能竞态），记一条便于排查
		log.Printf("[进度诊断] WorkflowInstance %s: level=%d 队列应有 %d 个任务但 Pop 返回 0",
			m.instance.ID, currentLevel, pendingAtCurrent)
	} else if currentLevel < maxLevel {
		// 当前层待 Pop 队列为空时，记录本层 running 与下一层 pending，便于排查“为何不推进”
		// 推进条件：当前层队列空 且 当前层 runningCount==0；若 runningCount>0 会一直不推进
		// 节流：同一实例该条诊断最多每 10 秒打一次，避免 ticker 高频触发导致刷屏
		nextPending := len(m.taskQueue.GetTaskIDsAtLevel(currentLevel + 1))
		runningCount := m.taskQueue.GetRunningCount(currentLevel)
		if nextPending > 0 || runningCount > 0 {
			nowNano := time.Now().UnixNano()
			lastNano := atomic.LoadInt64(&m.lastLevelEmptyDiagnosticLog)
			if nowNano-lastNano >= int64(10*time.Second) {
				atomic.StoreInt64(&m.lastLevelEmptyDiagnosticLog, nowNano)
				log.Printf("[进度诊断] WorkflowInstance %s: level=%d 当前层待出队为空，running=%d；下一层 level=%d pending=%d（推进需本层 running=0，未推进则下一层会挂起）",
					m.instance.ID, currentLevel, runningCount, currentLevel+1, nextPending)
			}
		}
	}
}

// checkDependencyFailed 检查任务的依赖是否有失败的
// 返回失败的依赖任务名称，如果没有失败的依赖则返回空字符串
func (m *WorkflowInstanceManagerV2) checkDependencyFailed(t workflow.Task) string {
	deps := t.GetDependencies()
	for _, depName := range deps {
		depTaskID, exists := m.workflow.GetTaskIDByName(depName)
		if !exists {
			continue
		}

		// 检查依赖任务是否失败
		var depTask workflow.Task
		if wfTask, exists := m.workflow.GetTasks()[depTaskID]; exists {
			depTask = wfTask
		} else if runtimeTask, ok := m.runtimeTasks.Load(depTaskID); ok {
			depTask = runtimeTask.(workflow.Task)
		}

		if depTask != nil && task.IsFailedStatus(depTask.GetStatus()) {
			return depName
		}

		// 也检查 contextData 中的错误信息（用于处理状态未及时更新的情况）
		errorKey := fmt.Sprintf("%s:error", depTaskID)
		if _, hasError := m.contextData.Load(errorKey); hasError {
			return depName
		}
	}
	return ""
}

// submitBatch 批量提交任务到 Executor
func (m *WorkflowInstanceManagerV2) submitBatch(batch []workflow.Task) {
	for _, task := range batch {
		taskID := task.GetID()
		taskName := task.GetName()

		// 检查依赖任务是否有失败的，如果有则跳过当前任务
		if failedDep := m.checkDependencyFailed(task); failedDep != "" {
			log.Printf("⚠️ WorkflowInstance %s: 任务 %s (%s) 的依赖任务 %s 已失败，跳过执行并标记为失败",
				m.instance.ID, taskID, taskName, failedDep)

			// 标记当前任务为失败
			task.SetStatus("FAILED")
			m.processedNodes.Store(taskID, true)

			// 保存错误信息
			errorKey := fmt.Sprintf("%s:error", taskID)
			m.contextData.Store(errorKey, fmt.Sprintf("依赖任务 %s 执行失败，跳过当前任务", failedDep))

			// 发送任务失败事件
			m.runningTaskIDs.Delete(taskID) // 未真正提交，从运行中移除
			select {
			case m.taskStatusChan <- TaskStatusEvent{
				TaskID:      taskID,
				Status:      "Failed",
				Error:       fmt.Errorf("依赖任务 %s 执行失败", failedDep),
				IsTemplate:  task.IsTemplate(),
				IsSubTask:   task.IsSubTask(),
				IsProcessed: false,
				Timestamp:   time.Now(),
			}:
			default:
				log.Printf("警告: taskStatusChan 已满，任务失败事件可能丢失: TaskID=%s", taskID)
			}
			continue
		}

		// 模板任务的 Job Function 正常执行（不再跳过）
		// 根据设计文档要求：用户应该把任务生成函数放在 Job Function 中
		// Job Function 可以从 context 引用之前任务的结果，并注入给子任务
		if task.IsTemplate() {
			log.Printf("📋 WorkflowInstance %s: Task %s (%s) 是模板任务，执行 Job Function 生成子任务",
				m.instance.ID, taskID, taskName)
		}

		// 参数校验和结果映射
		if err := m.validateAndMapParams(task, taskID); err != nil {
			log.Printf("参数校验失败: TaskID=%s, Error=%v", taskID, err)
			m.runningTaskIDs.Delete(taskID)
			continue
		}

		// 从缓存获取上游任务结果并注入参数
		if m.resultCache != nil {
			m.injectCachedResults(task, taskID)
		}

		// 通过JobFuncName从registry获取JobFuncID
		if task.GetJobFuncID() == "" && m.registry != nil {
			task.SetJobFuncID(m.registry.GetIDByName(task.GetJobFuncName()))
		}

		// 创建 InstanceManager 接口包装器（用于模板任务在 Job Function 中添加子任务）
		// 注意：InstanceManagerInterfaceV2 已在初始化时注册到 registry，这里只是创建引用用于 PendingTask
		managerInterface := &InstanceManagerInterfaceV2{
			manager: m,
		}

		// 确保状态为Pending
		task.SetStatus("PENDING")

		// 创建 executor.PendingTask（通过 InstanceManager 字段直接传递引用）
		pendingTask := &executor.PendingTask{
			Task:            task,
			WorkflowID:      m.instance.WorkflowID,
			InstanceID:      m.instance.ID,
			Domain:          "",
			MaxRetries:      0,
			OnComplete:      m.createTaskCompleteHandler(taskID),
			OnError:         m.createTaskErrorHandler(taskID),
			InstanceManager: managerInterface,
		}

		// 提交到Executor
		if err := m.executor.SubmitTask(pendingTask); err != nil {
			log.Printf("提交Task到Executor失败: TaskID=%s, Error=%v", taskID, err)

			// 检查重试次数
			retryCount := task.GetRetryCount()
			currentRetries := m.getTaskRetryCount(taskID)

			if currentRetries < retryCount {
				// 可以重试：将任务添加回当前 level 的队列
				m.runningTaskIDs.Delete(taskID)
				currentLevel := m.taskQueue.GetCurrentLevel()
				m.incrementTaskRetryCount(taskID)
				m.taskQueue.AddTask(currentLevel, task)

				log.Printf("WorkflowInstance %s: 任务 %s 提交失败，重试 %d/%d，添加到当前 level %d",
					m.instance.ID, taskID, currentRetries+1, retryCount, currentLevel)
			} else {
				// 超过最大重试次数，警告并移除任务
				m.runningTaskIDs.Delete(taskID)
				log.Printf("⚠️ 警告: WorkflowInstance %s: 任务 %s (%s) 提交失败，已达到最大重试次数 %d，将移除任务。错误: %v",
					m.instance.ID, taskID, task.GetName(), retryCount, err)

				currentLevel := m.taskQueue.GetCurrentLevel()
				m.taskQueue.RemoveTask(currentLevel, taskID)

				errorKey := fmt.Sprintf("%s:error", taskID)
				m.contextData.Store(errorKey, fmt.Sprintf("提交失败，超过最大重试次数 %d: %v", retryCount, err))

				task.SetStatus("FAILED")
				m.processedNodes.Store(taskID, true)

				if task.IsTemplate() {
					m.templateTaskCounts[currentLevel].Add(-1)
				}

				// 注意：任务失败计数已通过taskStatsChan在processBatch中更新，这里不需要再计数
			}
			continue
		}

		// 运行中已在 fetchTasksFromQueue 出队时计入，此处无需重复

		// 更新统计：任务已提交
		select {
		case m.taskStatsChan <- TaskStatsUpdate{
			Type:   "task_submitted",
			TaskID: taskID,
		}:
		default:
		}
	}
}

// validateAndMapParams 校验参数并执行resultMapping
func (m *WorkflowInstanceManagerV2) validateAndMapParams(t workflow.Task, taskID string) error {
	requiredParams := t.GetRequiredParams()
	resultMapping := t.GetResultMapping()

	// 1. 检查必需参数
	if len(requiredParams) > 0 {
		deps := t.GetDependencies()
		allParamsFound := true
		missingParams := make([]string, 0)

		for _, requiredParam := range requiredParams {
			found := false
			if t.GetParams()[requiredParam] != nil {
				found = true
			} else {
				for _, depName := range deps {
					depTaskID, exists := m.workflow.GetTaskIDByName(depName)
					if !exists {
						continue
					}
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

	// 2. 执行resultMapping
	if len(resultMapping) > 0 {
		deps := t.GetDependencies()
		for targetParam, sourceField := range resultMapping {
			for _, depName := range deps {
				depTaskID, exists := m.workflow.GetTaskIDByName(depName)
				if !exists {
					continue
				}
				if upstreamResultValue, exists := m.contextData.Load(depTaskID); exists {
					if upstreamResult, ok := upstreamResultValue.(map[string]interface{}); ok {
						if sourceValue, hasKey := upstreamResult[sourceField]; hasKey {
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

// injectCachedResults 从缓存获取上游任务结果并注入参数
func (m *WorkflowInstanceManagerV2) injectCachedResults(t workflow.Task, taskID string) {
	if m.resultCache == nil {
		return
	}

	resultMapping := t.GetResultMapping()
	requiredParams := t.GetRequiredParams()
	hasResultMapping := len(resultMapping) > 0

	deps := t.GetDependencies()
	for _, depName := range deps {
		depTaskID, exists := m.workflow.GetTaskIDByName(depName)
		if !exists {
			continue
		}

		cachedResult, found := m.resultCache.Get(depTaskID)
		if !found {
			continue
		}

		upstreamResult, ok := cachedResult.(map[string]interface{})
		if !ok {
			// 同时使用 taskID 和 taskName 作为 key（保持向后兼容，同时支持按名称访问）
			cacheKeyByID := fmt.Sprintf("_cached_%s", depTaskID)
			cacheKeyByName := fmt.Sprintf("_cached_%s", depName)
			if _, exists := t.GetParam(cacheKeyByID); !exists {
				t.SetParam(cacheKeyByID, cachedResult)
			}
			if _, exists := t.GetParam(cacheKeyByName); !exists {
				t.SetParam(cacheKeyByName, cachedResult)
			}
			continue
		}

		if hasResultMapping {
			for targetParam, sourceField := range resultMapping {
				if sourceValue, hasKey := upstreamResult[sourceField]; hasKey {
					if _, exists := t.GetParam(targetParam); !exists {
						t.SetParam(targetParam, sourceValue)
					}
				}
			}
		} else {
			if len(requiredParams) > 0 {
				missingRequiredFields := make([]string, 0)
				for _, requiredParam := range requiredParams {
					if t.GetParams()[requiredParam] != nil {
						continue
					}
					if _, hasKey := upstreamResult[requiredParam]; !hasKey {
						missingRequiredFields = append(missingRequiredFields, requiredParam)
					}
				}
				if len(missingRequiredFields) > 0 {
					log.Printf("⚠️ WorkflowInstance %s: Task %s 的必需参数在上游任务 %s 的结果中不存在: %v (建议配置ResultMapping)",
						m.instance.ID, taskID, depTaskID, missingRequiredFields)
				}
			}

			// 同时使用 taskID 和 taskName 作为 key（保持向后兼容，同时支持按名称访问）
			cacheKeyByID := fmt.Sprintf("_cached_%s", depTaskID)
			cacheKeyByName := fmt.Sprintf("_cached_%s", depName)
			if _, exists := t.GetParam(cacheKeyByID); !exists {
				t.SetParam(cacheKeyByID, cachedResult)
			}
			if _, exists := t.GetParam(cacheKeyByName); !exists {
				t.SetParam(cacheKeyByName, cachedResult)
			}
		}
	}
}

// createTaskCompleteHandler 创建任务完成处理器
func (m *WorkflowInstanceManagerV2) createTaskCompleteHandler(taskID string) func(*executor.TaskResult) {
	return func(result *executor.TaskResult) {
		// 检查任务是否已经被处理过（避免重复处理）
		if _, processed := m.processedNodes.Load(taskID); processed {
			return // 已经处理过，直接返回
		}

		// 更新workflow.Task的状态为Success
		if workflowTask, exists := m.workflow.GetTasks()[taskID]; exists {
			workflowTask.SetStatus("SUCCESS")
		} else if runtimeTask, ok := m.runtimeTasks.Load(taskID); ok {
			runtimeTask.(workflow.Task).SetStatus("SUCCESS")
		}

		// 执行Task的状态Handler（Success状态）
		if m.registry != nil {
			var workflowTask workflow.Task
			var exists bool
			if workflowTask, exists = m.workflow.GetTasks()[taskID]; !exists {
				if runtimeTask, ok := m.runtimeTasks.Load(taskID); ok {
					workflowTask = runtimeTask.(workflow.Task)
					exists = true
				}
			}
			if !exists {
				return
			}

			statusHandlers := workflowTask.GetStatusHandlers()
			taskObj := task.NewTask(workflowTask.GetName(), workflowTask.GetDescription(), workflowTask.GetJobFuncID(), workflowTask.GetParams(), statusHandlers)
			taskObj.SetID(workflowTask.GetID())
			taskObj.SetJobFuncName(workflowTask.GetJobFuncName())
			taskObj.SetTimeoutSeconds(workflowTask.GetTimeoutSeconds())
			taskObj.SetRetryCount(workflowTask.GetRetryCount())
			taskObj.SetDependencies(workflowTask.GetDependencies())
			taskObj.SetStatus("SUCCESS")

			// InstanceManagerInterfaceV2 已在初始化时注册到 registry，无需重复注册
			if err := task.ExecuteTaskHandlerWithContext(
				m.registry,
				taskObj,
				"SUCCESS",
				m.instance.WorkflowID,
				m.instance.ID,
				result.Data,
				"",
			); err != nil {
				log.Printf("执行Task Handler失败: Task=%s, Status=Success, Error=%v", taskID, err)
			}
		}

		// 如果启用了SAGA，记录成功步骤
		if m.sagaEnabled && m.sagaCoordinator != nil {
			var workflowTask workflow.Task
			var exists bool
			if workflowTask, exists = m.workflow.GetTasks()[taskID]; !exists {
				if runtimeTask, ok := m.runtimeTasks.Load(taskID); ok {
					workflowTask = runtimeTask.(workflow.Task)
					exists = true
				}
			}
			if exists && workflowTask.GetCompensationFuncName() != "" {
				step := saga.NewTransactionStep(
					taskID,
					workflowTask.GetName(),
					"Success",
					workflowTask.GetCompensationFuncName(),
					workflowTask.GetCompensationFuncID(),
				)
				step.ExecutedAt = time.Now().Unix()
				m.sagaCoordinator.AddStep(step)
				m.sagaCoordinator.MarkStepSuccess(taskID)
			}
		}

		// 触发Task成功插件
		if m.pluginManager != nil {
			var workflowTask workflow.Task
			var exists bool
			if workflowTask, exists = m.workflow.GetTasks()[taskID]; !exists {
				if runtimeTask, ok := m.runtimeTasks.Load(taskID); ok {
					workflowTask = runtimeTask.(workflow.Task)
					exists = true
				}
			}
			if exists {
				pluginData := plugin.PluginData{
					Event:      plugin.EventTaskSuccess,
					WorkflowID: m.instance.WorkflowID,
					InstanceID: m.instance.ID,
					TaskID:     taskID,
					TaskName:   workflowTask.GetName(),
					Status:     "SUCCESS",
					Error:      nil,
					Data: map[string]interface{}{
						"result": result.Data,
					},
				}
				if err := m.pluginManager.Trigger(m.ctx, plugin.EventTaskSuccess, pluginData); err != nil {
					log.Printf("触发Task成功插件失败: TaskID=%s, Error=%v", taskID, err)
				}
			}
		}

		// 发送任务完成事件到taskStatusChan
		isTemplate := false
		isSubTask := false
		parentID := ""
		if workflowTask, exists := m.workflow.GetTasks()[taskID]; exists {
			isTemplate = workflowTask.IsTemplate()
			isSubTask = workflowTask.IsSubTask()
		} else if runtimeTask, ok := m.runtimeTasks.Load(taskID); ok {
			isSubTask = runtimeTask.(workflow.Task).IsSubTask()
		}
		// 获取子任务的父任务ID
		if isSubTask {
			parentKey := fmt.Sprintf("%s:parent_task_id", taskID)
			if parentValue, exists := m.contextData.Load(parentKey); exists {
				parentID = parentValue.(string)
			}
		}

		// 必须阻塞发送，否则完成事件丢失会导致 runningCount 不归零、层级不推进、出现“大量 pending 但无任务执行”
		select {
		case m.taskStatusChan <- TaskStatusEvent{
			TaskID:      taskID,
			Status:      "Success",
			Result:      result.Data,
			IsTemplate:  isTemplate,
			IsSubTask:   isSubTask,
			ParentID:    parentID,
			IsProcessed: false,
			Timestamp:   time.Now(),
		}:
		case <-m.ctx.Done():
			return
		}
	}
}

// executeTaskFailedHandler 执行任务失败的 Handler（用于达到最大重试次数的场景）
// 注意：这个方法不会发送 TaskStatusEvent，因为事件已经在 processBatch 中发送过
func (m *WorkflowInstanceManagerV2) executeTaskFailedHandler(taskID string, workflowTask workflow.Task, errorMsg string) {
	// 执行 Task 的状态 Handler（Failed 状态）
	if m.registry != nil {
		statusHandlers := workflowTask.GetStatusHandlers()
		taskObj := task.NewTask(workflowTask.GetName(), workflowTask.GetDescription(), workflowTask.GetJobFuncID(), workflowTask.GetParams(), statusHandlers)
		taskObj.SetID(workflowTask.GetID())
		taskObj.SetJobFuncName(workflowTask.GetJobFuncName())
		taskObj.SetTimeoutSeconds(workflowTask.GetTimeoutSeconds())
		taskObj.SetRetryCount(workflowTask.GetRetryCount())
		taskObj.SetDependencies(workflowTask.GetDependencies())
		taskObj.SetStatus("FAILED")

		if handlerErr := task.ExecuteTaskHandlerWithContext(
			m.registry,
			taskObj,
			"FAILED",
			m.instance.WorkflowID,
			m.instance.ID,
			nil,
			errorMsg,
		); handlerErr != nil {
			log.Printf("执行Task Handler失败: Task=%s, Status=Failed, Error=%v", taskID, handlerErr)
		}
	}

	// 如果启用了 SAGA，记录失败步骤
	if m.sagaEnabled && m.sagaCoordinator != nil {
		step := saga.NewTransactionStep(
			taskID,
			workflowTask.GetName(),
			"Failed",
			workflowTask.GetCompensationFuncName(),
			workflowTask.GetCompensationFuncID(),
		)
		step.ExecutedAt = time.Now().Unix()
		m.sagaCoordinator.AddStep(step)
		m.sagaCoordinator.MarkStepFailed(taskID)
		log.Printf("🔍 [SAGA] 已记录失败步骤（达到最大重试次数）: TaskID=%s, TaskName=%s", taskID, workflowTask.GetName())
	}
}

// createTaskErrorHandler 创建任务错误处理器
func (m *WorkflowInstanceManagerV2) createTaskErrorHandler(taskID string) func(error) {
	return func(err error) {
		// 保存错误信息到contextData（用于workflow失败判断）
		errorKey := fmt.Sprintf("%s:error", taskID)
		m.contextData.Store(errorKey, err.Error())

		// 更新workflow.Task的状态为Failed
		if workflowTask, exists := m.workflow.GetTasks()[taskID]; exists {
			workflowTask.SetStatus("FAILED")
		} else if runtimeTask, ok := m.runtimeTasks.Load(taskID); ok {
			runtimeTask.(workflow.Task).SetStatus("FAILED")
		}

		// 执行Task的状态Handler（Failed状态）
		if m.registry != nil {
			var workflowTask workflow.Task
			var exists bool
			if workflowTask, exists = m.workflow.GetTasks()[taskID]; !exists {
				if runtimeTask, ok := m.runtimeTasks.Load(taskID); ok {
					workflowTask = runtimeTask.(workflow.Task)
					exists = true
				}
			}
			if !exists {
				return
			}

			statusHandlers := workflowTask.GetStatusHandlers()
			taskObj := task.NewTask(workflowTask.GetName(), workflowTask.GetDescription(), workflowTask.GetJobFuncID(), workflowTask.GetParams(), statusHandlers)
			taskObj.SetID(workflowTask.GetID())
			taskObj.SetJobFuncName(workflowTask.GetJobFuncName())
			taskObj.SetTimeoutSeconds(workflowTask.GetTimeoutSeconds())
			taskObj.SetRetryCount(workflowTask.GetRetryCount())
			taskObj.SetDependencies(workflowTask.GetDependencies())
			taskObj.SetStatus("FAILED")

			if handlerErr := task.ExecuteTaskHandlerWithContext(
				m.registry,
				taskObj,
				"FAILED",
				m.instance.WorkflowID,
				m.instance.ID,
				nil,
				err.Error(),
			); handlerErr != nil {
				log.Printf("执行Task Handler失败: Task=%s, Status=Failed, Error=%v", taskID, handlerErr)
			}
		}

		// 如果启用了SAGA，记录失败步骤
		if m.sagaEnabled && m.sagaCoordinator != nil {
			var workflowTask workflow.Task
			var exists bool
			if workflowTask, exists = m.workflow.GetTasks()[taskID]; !exists {
				if runtimeTask, ok := m.runtimeTasks.Load(taskID); ok {
					workflowTask = runtimeTask.(workflow.Task)
					exists = true
				}
			}
			if exists {
				step := saga.NewTransactionStep(
					taskID,
					workflowTask.GetName(),
					"Failed",
					workflowTask.GetCompensationFuncName(),
					workflowTask.GetCompensationFuncID(),
				)
				step.ExecutedAt = time.Now().Unix()
				m.sagaCoordinator.AddStep(step)
				m.sagaCoordinator.MarkStepFailed(taskID)
				log.Printf("🔍 [SAGA] 已记录失败步骤: TaskID=%s, TaskName=%s", taskID, workflowTask.GetName())
			}
		}

		// 触发Task失败插件
		if m.pluginManager != nil {
			var workflowTask workflow.Task
			var exists bool
			if workflowTask, exists = m.workflow.GetTasks()[taskID]; !exists {
				if runtimeTask, ok := m.runtimeTasks.Load(taskID); ok {
					workflowTask = runtimeTask.(workflow.Task)
					exists = true
				}
			}
			if exists {
				pluginData := plugin.PluginData{
					Event:      plugin.EventTaskFailed,
					WorkflowID: m.instance.WorkflowID,
					InstanceID: m.instance.ID,
					TaskID:     taskID,
					TaskName:   workflowTask.GetName(),
					Status:     "FAILED",
					Error:      err,
					Data: map[string]interface{}{
						"error": err.Error(),
					},
				}
				if triggerErr := m.pluginManager.Trigger(m.ctx, plugin.EventTaskFailed, pluginData); triggerErr != nil {
					log.Printf("触发Task失败插件失败: TaskID=%s, Error=%v", taskID, triggerErr)
				}
			}
		}

		// 发送任务失败事件到taskStatusChan
		isTemplate := false
		isSubTask := false
		parentID := ""
		if workflowTask, exists := m.workflow.GetTasks()[taskID]; exists {
			isTemplate = workflowTask.IsTemplate()
			isSubTask = workflowTask.IsSubTask()
		} else if runtimeTask, ok := m.runtimeTasks.Load(taskID); ok {
			isSubTask = runtimeTask.(workflow.Task).IsSubTask()
		}
		// 获取子任务的父任务ID
		if isSubTask {
			parentKey := fmt.Sprintf("%s:parent_task_id", taskID)
			if parentValue, exists := m.contextData.Load(parentKey); exists {
				parentID = parentValue.(string)
			}
		}

		// 必须阻塞发送，否则失败事件丢失会导致 runningCount 不归零、层级不推进
		select {
		case m.taskStatusChan <- TaskStatusEvent{
			TaskID:      taskID,
			Status:      "Failed",
			Error:       err,
			IsTemplate:  isTemplate,
			IsSubTask:   isSubTask,
			ParentID:    parentID,
			IsProcessed: false,
			Timestamp:   time.Now(),
		}:
		case <-m.ctx.Done():
			return
		}
	}
}

// InstanceManagerInterfaceV2 实现InstanceManager接口的包装器
type InstanceManagerInterfaceV2 struct {
	manager *WorkflowInstanceManagerV2
}

// AddSubTask 添加子任务
func (i *InstanceManagerInterfaceV2) AddSubTask(subTask types.Task, parentTaskID string) error {
	return i.manager.AddSubTask(subTask, parentTaskID)
}

// AtomicAddSubTasks 原子性地添加多个子任务
func (i *InstanceManagerInterfaceV2) AtomicAddSubTasks(subTasks []types.Task, parentTaskID string) error {
	return i.manager.AtomicAddSubTasks(subTasks, parentTaskID)
}

// controlSignalGoroutine 控制信号处理协程
func (m *WorkflowInstanceManagerV2) controlSignalGoroutine() {
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
func (m *WorkflowInstanceManagerV2) handlePause() {
	m.mu.Lock()
	m.instance.Status = "Paused"
	m.mu.Unlock()

	ctx := context.Background()
	m.saveAllTaskStatuses(ctx)

	breakpointValue := m.CreateBreakpoint()
	breakpoint, ok := breakpointValue.(*workflow.BreakpointData)
	if !ok {
		log.Printf("WorkflowInstance %s 断点数据类型转换失败", m.instance.ID)
		return
	}
	if m.workflowInstanceRepo != nil {
		m.workflowInstanceRepo.UpdateBreakpoint(ctx, m.instance.ID, breakpoint)
	}
	if err := m.updateWorkflowInstanceStatus(ctx, m.instance.ID, "Paused", ""); err != nil {
		log.Printf("更新WorkflowInstance状态失败: %v", err)
	}

	// 触发Workflow暂停插件
	if m.pluginManager != nil {
		pluginData := plugin.PluginData{
			Event:      plugin.EventWorkflowPaused,
			WorkflowID: m.instance.WorkflowID,
			InstanceID: m.instance.ID,
			TaskID:     "",
			TaskName:   "",
			Status:     "Paused",
			Error:      nil,
			Data: map[string]interface{}{
				"workflow_name": m.workflow.Name,
			},
		}
		if err := m.pluginManager.Trigger(m.ctx, plugin.EventWorkflowPaused, pluginData); err != nil {
			log.Printf("触发Workflow暂停插件失败: InstanceID=%s, Error=%v", m.instance.ID, err)
		}
	}

	select {
	case m.statusUpdateChan <- "Paused":
	default:
	}

	log.Printf("WorkflowInstance %s: 已暂停", m.instance.ID)
}

// handleResume 处理恢复信号
func (m *WorkflowInstanceManagerV2) handleResume() {
	m.mu.Lock()
	m.instance.Status = "Running"
	m.mu.Unlock()

	ctx := context.Background()
	if err := m.updateWorkflowInstanceStatus(ctx, m.instance.ID, "Running", ""); err != nil {
		log.Printf("更新WorkflowInstance状态失败: %v", err)
	}

	// 重新启动任务提交协程（如果已停止）
	// 注意：V2版本中，goroutine由ctx控制，恢复时不需要重新启动

	select {
	case m.statusUpdateChan <- "Running":
	default:
	}

	// 触发Workflow恢复插件
	if m.pluginManager != nil {
		pluginData := plugin.PluginData{
			Event:      plugin.EventWorkflowResumed,
			WorkflowID: m.instance.WorkflowID,
			InstanceID: m.instance.ID,
			TaskID:     "",
			TaskName:   "",
			Status:     "Running",
			Error:      nil,
			Data: map[string]interface{}{
				"workflow_name": m.workflow.Name,
			},
		}
		if err := m.pluginManager.Trigger(m.ctx, plugin.EventWorkflowResumed, pluginData); err != nil {
			log.Printf("触发Workflow恢复插件失败: InstanceID=%s, Error=%v", m.instance.ID, err)
		}
	}

	log.Printf("WorkflowInstance %s: 已恢复", m.instance.ID)
}

// handleTerminate 处理终止信号
func (m *WorkflowInstanceManagerV2) handleTerminate() {
	m.mu.Lock()
	m.instance.Status = "Terminated"
	m.instance.ErrorMessage = "用户终止"
	now := time.Now()
	m.instance.EndTime = &now
	m.mu.Unlock()

	ctx := context.Background()
	m.saveAllTaskStatuses(ctx)
	if err := m.updateWorkflowInstanceStatus(ctx, m.instance.ID, "Terminated", ""); err != nil {
		log.Printf("更新WorkflowInstance状态失败: %v", err)
	}

	select {
	case m.statusUpdateChan <- "Terminated":
	default:
	}

	// 取消context，停止所有协程
	m.cancel()

	// 触发Workflow终止插件
	if m.pluginManager != nil {
		pluginData := plugin.PluginData{
			Event:      plugin.EventWorkflowTerminated,
			WorkflowID: m.instance.WorkflowID,
			InstanceID: m.instance.ID,
			TaskID:     "",
			TaskName:   "",
			Status:     "Terminated",
			Error:      fmt.Errorf("用户终止"),
			Data: map[string]interface{}{
				"workflow_name": m.workflow.Name,
				"reason":        "用户终止",
			},
		}
		if err := m.pluginManager.Trigger(m.ctx, plugin.EventWorkflowTerminated, pluginData); err != nil {
			log.Printf("触发Workflow终止插件失败: InstanceID=%s, Error=%v", m.instance.ID, err)
		}
	}

	log.Printf("WorkflowInstance %s: 已终止", m.instance.ID)
}

// handleSubTaskAdded 处理子任务添加事件
// handleAtomicAddSubTasks 处理原子性子任务添加事件（关键改进：一次性处理所有子任务）
// 保证模板任务添加子任务的原子性
func (m *WorkflowInstanceManagerV2) handleAtomicAddSubTasks(event AtomicAddSubTasksEvent) {
	subTasks := event.SubTasks
	parentTaskID := event.ParentID

	// 检查父任务是否存在
	parentTask, exists := m.workflow.GetTasks()[parentTaskID]
	if !exists {
		log.Printf("警告: 父任务不存在: ParentID=%s", parentTaskID)
		return
	}

	// 处理空子任务列表：如果模板任务没有生成子任务，需要减少计数
	// 注意：在新设计中，模板任务的成功事件由 createTaskCompleteHandler 发送，这里只减少计数
	if len(subTasks) == 0 {
		if parentTask.IsTemplate() {
			log.Printf("WorkflowInstance %s: 模板任务 %s 没有生成子任务", m.instance.ID, parentTaskID)
			// 减少模板任务计数（没有子任务）
			currentLevel := m.taskQueue.GetCurrentLevel()
			m.decrementTemplateTaskCount(parentTaskID, currentLevel, 0)
		} else {
			log.Printf("警告: WorkflowInstance %s: 收到空的子任务列表，ParentID=%s", m.instance.ID, parentTaskID)
		}
		return
	}

	// 注意：模板任务计数统一在这里处理，不依赖 handleTaskSuccess

	// 防止嵌套模板任务：如果父任务是模板任务，子任务不能是模板任务
	// 条件：recursiveTemplateTask := parent.isTemplate && any(subTask.isTemplate)
	for _, subTask := range subTasks {
		if parentTask.IsTemplate() && subTask.IsTemplate() {
			log.Printf("⚠️ 错误: WorkflowInstance %s: 检测到嵌套模板任务，父任务 %s 是模板任务，子任务 %s 也是模板任务，不允许添加（防止递归模板任务）",
				m.instance.ID, parentTaskID, subTask.GetName())
			return
		}
	}

	// 存储所有子任务到运行时任务，同时记录父任务ID
	for _, subTask := range subTasks {
		m.runtimeTasks.Store(subTask.GetID(), subTask)
		// 存储子任务的父任务ID（用于子任务完成时查找跟踪器）
		parentKey := fmt.Sprintf("%s:parent_task_id", subTask.GetID())
		m.contextData.Store(parentKey, parentTaskID)
	}

	// 初始化或更新子任务跟踪器（用于结果聚合）
	var tracker *SubTaskTracker
	if existingTracker, exists := m.subTaskTracker.Load(parentTaskID); exists {
		// 已存在跟踪器，追加子任务
		tracker = existingTracker.(*SubTaskTracker)
		tracker.mu.Lock()
		for _, subTask := range subTasks {
			tracker.SubTaskIDs = append(tracker.SubTaskIDs, subTask.GetID())
		}
		atomic.AddInt32(&tracker.TotalCount, int32(len(subTasks)))
		tracker.mu.Unlock()
	} else {
		// 创建新的跟踪器
		tracker = &SubTaskTracker{
			SubTaskIDs: make([]string, 0, len(subTasks)),
			TotalCount: int32(len(subTasks)),
		}
		for _, subTask := range subTasks {
			tracker.SubTaskIDs = append(tracker.SubTaskIDs, subTask.GetID())
		}
		m.subTaskTracker.Store(parentTaskID, tracker)
	}
	log.Printf("WorkflowInstance %s: 初始化子任务跟踪器，父任务: %s，子任务数量: %d",
		m.instance.ID, parentTaskID, len(subTasks))

	// 批量更新任务统计（通过taskStatsChan发送task_added事件）
	for _, subTask := range subTasks {
		select {
		case m.taskStatsChan <- TaskStatsUpdate{
			Type:       "task_added",
			TaskID:     subTask.GetID(),
			IsTemplate: false, // 子任务不能是模板任务
			IsSubTask:  true,
		}:
		default:
			log.Printf("警告: taskStatsChan 已满，子任务统计更新可能丢失: TaskID=%s", subTask.GetID())
		}
	}

	// 检查所有子任务的依赖是否已满足
	allDepsProcessed := true

	// 检查父任务是否已完成
	// 注意：在新设计中，不再在这里将模板任务标记为已处理
	// 让 createTaskCompleteHandler 来处理，这样 Success Handler 可以正常执行
	if _, processed := m.processedNodes.Load(parentTaskID); !processed {
		// 对于模板任务，检查状态来判断是否已完成（Job Function 正在执行中的竞态情况）
		if parentTask.IsTemplate() {
			parentStatus := parentTask.GetStatus()
			if task.IsSuccessStatus(parentStatus) {
				// 模板任务状态是 SUCCESS，依赖已满足（但不标记为已处理，让 createTaskCompleteHandler 处理）
				// allDepsProcessed = true (保持为 true)
			} else {
				allDepsProcessed = false
			}
		} else {
			allDepsProcessed = false
		}
	}

	// 检查所有子任务通过GetDependencies()声明的其他依赖
	if allDepsProcessed {
		for _, subTask := range subTasks {
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

	// 获取父任务的原始层级（从contextData中获取，在initTaskQueue中保存）
	parentLevel := -1
	levelKey := fmt.Sprintf("%s:original_level", parentTaskID)
	if levelValue, exists := m.contextData.Load(levelKey); exists {
		if level, ok := levelValue.(int); ok {
			parentLevel = level
		}
	}

	// 确定目标层级
	currentLevel := m.taskQueue.GetCurrentLevel()
	targetLevel := currentLevel
	if parentLevel >= 0 {
		if parentLevel < currentLevel {
			targetLevel = currentLevel
			log.Printf("警告: WorkflowInstance %s: 父任务层级 %d < 当前层级 %d，子任务添加到当前层级 %d",
				m.instance.ID, parentLevel, currentLevel, targetLevel)
		} else if parentLevel == currentLevel {
			targetLevel = parentLevel
		} else {
			targetLevel = currentLevel
		}
	}

	// 如果所有子任务的依赖都已满足，直接批量添加到队列
	// 注意：处理标记已在函数开始时设置（如果是模板任务）
	if allDepsProcessed {
		// 直接批量添加到队列（原子性操作）
		m.taskQueue.AddTasks(targetLevel, subTasks)

		// 存储子任务的层级（用于 TaskCompleted 时减少 runningCounts）
		for _, subTask := range subTasks {
			levelKey := fmt.Sprintf("%s:original_level", subTask.GetID())
			m.contextData.Store(levelKey, targetLevel)
		}

		log.Printf("WorkflowInstance %s: 原子性地批量添加 %d 个子任务到 level %d，依赖已满足", m.instance.ID, len(subTasks), targetLevel)

		// 如果父任务是模板任务，减少模板任务计数
		if parentTask.IsTemplate() {
			m.decrementTemplateTaskCount(parentTaskID, targetLevel, len(subTasks))
		}
	} else {
		// 依赖未满足，存储到contextData等待后续处理
		subTasksKey := fmt.Sprintf("%s:subtasks", parentTaskID)
		var subTasksList []workflow.Task
		if existing, exists := m.contextData.Load(subTasksKey); exists {
			// 添加类型检查，避免 panic
			if list, ok := existing.([]workflow.Task); ok {
				subTasksList = list
			} else {
				log.Printf("警告: WorkflowInstance %s: contextData 中的子任务列表类型错误，重新创建", m.instance.ID)
				subTasksList = make([]workflow.Task, 0)
			}
		} else {
			subTasksList = make([]workflow.Task, 0)
		}
		// 使用 append 创建新切片，避免并发修改问题
		subTasksList = append(subTasksList, subTasks...)
		m.contextData.Store(subTasksKey, subTasksList)
		log.Printf("WorkflowInstance %s: %d 个子任务已添加，等待依赖满足（父任务: %s）", m.instance.ID, len(subTasks), parentTaskID)
		// 注意：在新设计中，模板任务的成功事件由 createTaskCompleteHandler 发送
		// 这里不再设置状态或发送事件
	}

	// 如果父任务是模板任务，处理模板任务逻辑
	// 注意：在新设计中，模板任务的 Job Function 会被正常执行，成功事件由 createTaskCompleteHandler 发送
	// 这里不再发送成功事件，只处理子任务添加后的依赖检查
	if parentTask.IsTemplate() {
		// 如果依赖未满足，尝试批量添加子任务（用于处理后续依赖满足的情况）
		if !allDepsProcessed {
			m.tryBatchAddSubTasks(parentTaskID, parentTask, targetLevel)
		}
	}
}

// decrementTemplateTaskCount 减少模板任务计数（统一方法）
func (m *WorkflowInstanceManagerV2) decrementTemplateTaskCount(parentTaskID string, targetLevel int, subTaskCount int) {
	key := fmt.Sprintf("%s:template_count_decremented", parentTaskID)
	if _, decremented := m.contextData.LoadOrStore(key, true); !decremented {
		// 使用 CAS 原子性地减少 templateTaskCount
		for {
			oldValue := m.templateTaskCount.Load()
			if oldValue <= 0 {
				log.Printf("警告: templateTaskCount <= 0，无法减少，ParentTaskID=%s", parentTaskID)
				break
			}
			newValue := oldValue - 1
			if m.templateTaskCount.CompareAndSwap(oldValue, newValue) {
				// CAS 成功，同时更新 templateTaskCounts[currentLevel]（统一使用 currentLevel）
				currentLevel := m.taskQueue.GetCurrentLevel()
				if currentLevel < len(m.templateTaskCounts) {
					m.templateTaskCounts[currentLevel].Store(newValue)
				}
				log.Printf("WorkflowInstance %s: 模板任务 %s 的所有子任务（%d个）已批量添加到 level %d，templateTaskCount 从 %d 减少到 %d",
					m.instance.ID, parentTaskID, subTaskCount, targetLevel, oldValue, newValue)
				break
			}
			// CAS 失败，重试
		}
	}
}

// tryBatchAddSubTasks 尝试批量添加子任务
// 当依赖满足时，批量添加子任务到队列
func (m *WorkflowInstanceManagerV2) tryBatchAddSubTasks(parentTaskID string, parentTask workflow.Task, targetLevel int) {
	subTasksKey := fmt.Sprintf("%s:subtasks", parentTaskID)
	subTasksValue, exists := m.contextData.Load(subTasksKey)
	if !exists {
		return
	}

	// 添加类型检查，避免 panic
	subTasksList, ok := subTasksValue.([]workflow.Task)
	if !ok {
		log.Printf("警告: WorkflowInstance %s: contextData 中的子任务列表类型错误，ParentTaskID=%s", m.instance.ID, parentTaskID)
		m.contextData.Delete(subTasksKey)
		return
	}

	if len(subTasksList) == 0 {
		return
	}

	// 检查当前层级
	currentLevel := m.taskQueue.GetCurrentLevel()
	if currentLevel != targetLevel {
		// 层级已推进，不需要检查
		return
	}

	// 检查所有子任务的依赖是否已满足
	allDepsProcessed := true
	if _, processed := m.processedNodes.Load(parentTaskID); !processed {
		allDepsProcessed = false
	}

	// 检查所有子任务通过GetDependencies()声明的其他依赖
	if allDepsProcessed {
		for _, subTask := range subTasksList {
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

	// 只有当所有依赖都满足时，才批量添加子任务
	if allDepsProcessed {
		// 批量添加子任务
		m.taskQueue.AddTasks(targetLevel, subTasksList)

		// 存储子任务的层级（用于 TaskCompleted 时减少 runningCounts）
		for _, subTask := range subTasksList {
			levelKey := fmt.Sprintf("%s:original_level", subTask.GetID())
			m.contextData.Store(levelKey, targetLevel)
		}

		// 清空已收集的子任务列表
		m.contextData.Delete(subTasksKey)

		// 如果父任务是模板任务，减少模板任务计数
		if parentTask.IsTemplate() {
			m.decrementTemplateTaskCount(parentTaskID, targetLevel, len(subTasksList))
		}
	}
}

// AddSubTask 动态添加子任务到WorkflowInstance（公共方法，实现接口）
func (m *WorkflowInstanceManagerV2) AddSubTask(subTask types.Task, parentTaskID string) error {
	if subTask == nil {
		return fmt.Errorf("子Task不能为空")
	}
	if subTask.GetID() == "" {
		return fmt.Errorf("子Task ID不能为空")
	}
	if subTask.GetName() == "" {
		return fmt.Errorf("子Task名称不能为空")
	}

	// 类型转换：types.Task -> workflow.Task
	workflowTask, ok := subTask.(workflow.Task)
	if !ok {
		return fmt.Errorf("子Task类型转换失败")
	}

	// 设置isSubTask标志
	workflowTask.SetSubTask(true)

	// 使用Workflow的AddSubTask方法（线程安全）
	if err := m.workflow.AddSubTask(workflowTask, parentTaskID); err != nil {
		return err
	}

	// 发送子任务添加事件到专用channel（统一事件处理模式，将单个子任务包装成数组）
	select {
	case m.addSubTaskChan <- AtomicAddSubTasksEvent{
		SubTasks:  []workflow.Task{workflowTask}, // 将单个子任务包装成数组
		ParentID:  parentTaskID,
		Timestamp: time.Now(),
	}:
		// 事件已发送
	case <-time.After(5 * time.Second):
		log.Printf("警告: addSubTaskChan 发送超时，子任务添加事件可能丢失: TaskID=%s", workflowTask.GetID())
		return fmt.Errorf("子任务添加事件发送超时")
	case <-m.ctx.Done():
		return fmt.Errorf("Context 已取消")
	}

	return nil
}

// AtomicAddSubTasks 原子性地添加多个子任务到WorkflowInstance（公共方法，实现接口）
// 保证要么全部成功，要么全部失败（回滚）
func (m *WorkflowInstanceManagerV2) AtomicAddSubTasks(subTasks []types.Task, parentTaskID string) error {
	if len(subTasks) == 0 {
		return nil // 空列表，直接返回成功
	}

	// 验证所有子任务
	for i, subTask := range subTasks {
		// 使用反射检查接口是否为nil（处理接口类型的nil）
		if subTask == nil {
			return fmt.Errorf("子任务[%d]不能为空", i)
		}
		// 使用反射检查接口的底层值是否为nil
		rv := reflect.ValueOf(subTask)
		if rv.Kind() == reflect.Interface || rv.Kind() == reflect.Ptr {
			if rv.IsNil() {
				return fmt.Errorf("子任务[%d]不能为空", i)
			}
		}
		// 安全地调用GetID
		taskID := subTask.GetID()
		if taskID == "" {
			return fmt.Errorf("子任务[%d] ID不能为空", i)
		}
		taskName := subTask.GetName()
		if taskName == "" {
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

	// 第二步：发送原子性子任务添加事件到专用channel（统一事件处理模式，一次性提交所有任务）
	// 如果事件发送失败，回滚所有已添加的子任务
	select {
	case m.addSubTaskChan <- AtomicAddSubTasksEvent{
		SubTasks:  workflowTasks, // 一次性提交所有子任务
		ParentID:  parentTaskID,
		Timestamp: time.Now(),
	}:
		// 事件已发送
		log.Printf("WorkflowInstance %s: 原子性地批量添加 %d 个子任务成功，父任务: %s", m.instance.ID, len(workflowTasks), parentTaskID)
		return nil
	case <-time.After(5 * time.Second):
		// 发送超时，回滚所有已添加的子任务
		for _, addedTask := range addedSubTasks {
			m.workflow.Tasks.Delete(addedTask.GetID())
			m.workflow.Dependencies.Delete(addedTask.GetID())
			// 从TaskNameIndex中删除
			if taskName := addedTask.GetName(); taskName != "" {
				m.workflow.TaskNameIndex.Delete(taskName)
			}
		}
		log.Printf("警告: addSubTaskChan 发送超时，原子性子任务添加事件可能丢失: ParentID=%s, Count=%d", parentTaskID, len(workflowTasks))
		return fmt.Errorf("原子性子任务添加事件发送超时")
	case <-m.ctx.Done():
		// Context已取消，回滚所有已添加的子任务
		for _, addedTask := range addedSubTasks {
			m.workflow.Tasks.Delete(addedTask.GetID())
			m.workflow.Dependencies.Delete(addedTask.GetID())
			// 从TaskNameIndex中删除
			if taskName := addedTask.GetName(); taskName != "" {
				m.workflow.TaskNameIndex.Delete(taskName)
			}
		}
		return fmt.Errorf("Context 已取消")
	}
}

// Shutdown 优雅关闭WorkflowInstanceManager（公共方法，实现接口）
func (m *WorkflowInstanceManagerV2) Shutdown() {
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
		log.Printf("WorkflowInstance %s: 所有协程已退出", m.instance.ID)
	case <-time.After(30 * time.Second):
		log.Printf("WorkflowInstance %s: 等待协程退出超时", m.instance.ID)
	}
}

// GetControlSignalChannel 获取控制信号通道（公共方法，实现接口）
func (m *WorkflowInstanceManagerV2) GetControlSignalChannel() interface{} {
	return m.controlSignalChan
}

// GetStatusUpdateChannel 获取状态更新通道（公共方法，实现接口）
func (m *WorkflowInstanceManagerV2) GetStatusUpdateChannel() <-chan string {
	return m.statusUpdateChan
}

// GetInstanceID 获取WorkflowInstance ID（公共方法，实现接口）
func (m *WorkflowInstanceManagerV2) GetInstanceID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.instance.ID
}

// GetStatus 获取WorkflowInstance状态（公共方法，实现接口）
func (m *WorkflowInstanceManagerV2) GetStatus() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.instance.Status
}

// maxPendingTaskIDsInSnapshot 进度快照中 PendingTaskIDs 最大数量，避免单次返回数万 ID
const maxPendingTaskIDsInSnapshot = 500

// GetProgress 获取当前实例的内存中任务进度（公共方法，实现接口）
// Running = len(RunningTaskIDs)，Pending = 各层队列待运行任务总数，PendingTaskIDs 为其 ID 列表（当前层优先，可能截断）
func (m *WorkflowInstanceManagerV2) GetProgress() types.ProgressSnapshot {
	total := int(atomic.LoadInt32(&m.taskStats.TotalTasks))
	completed := int(atomic.LoadInt32(&m.taskStats.SuccessTasks))
	failed := int(atomic.LoadInt32(&m.taskStats.FailedTasks))
	var runningIDs, pendingIDs []string
	var pending int
	if m.taskQueue != nil {
		currentLevel := m.taskQueue.GetCurrentLevel()
		maxLevel := m.taskQueue.GetMaxLevel()
		for level := currentLevel; level < maxLevel; level++ {
			ids := m.taskQueue.GetTaskIDsAtLevel(level)
			pending += len(ids)
			for _, id := range ids {
				if len(pendingIDs) < maxPendingTaskIDsInSnapshot {
					pendingIDs = append(pendingIDs, id)
				}
			}
		}
	}
	m.runningTaskIDs.Range(func(key, _ interface{}) bool {
		runningIDs = append(runningIDs, key.(string))
		return true
	})
	running := len(runningIDs)
	return types.ProgressSnapshot{
		Total:          total,
		Completed:      completed,
		Running:        running,
		Failed:         failed,
		Pending:        pending,
		RunningTaskIDs: runningIDs,
		PendingTaskIDs: pendingIDs,
	}
}

// Context 获取context（公共方法，实现接口）
func (m *WorkflowInstanceManagerV2) Context() context.Context {
	return m.ctx
}

// CreateBreakpoint 创建断点数据（公共方法，实现接口）
func (m *WorkflowInstanceManagerV2) CreateBreakpoint() interface{} {
	completedTaskNames := make([]string, 0)
	m.processedNodes.Range(func(key, value interface{}) bool {
		taskID := key.(string)
		if t, exists := m.workflow.GetTasks()[taskID]; exists {
			completedTaskNames = append(completedTaskNames, t.GetName())
		} else if runtimeTask, ok := m.runtimeTasks.Load(taskID); ok {
			completedTaskNames = append(completedTaskNames, runtimeTask.(workflow.Task).GetName())
		}
		return true
	})

	runningTaskNames := make([]string, 0)

	// DAG快照（简化处理）
	dagSnapshot := make(map[string]interface{})
	// 获取DAG的节点数（简化处理）
	allTasks := m.workflow.GetTasks()
	dagSnapshot["node_count"] = len(allTasks)

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

// RestoreFromBreakpoint 从断点数据恢复WorkflowInstance状态（公共方法，实现接口）
func (m *WorkflowInstanceManagerV2) RestoreFromBreakpoint(breakpoint interface{}) error {
	if breakpoint == nil {
		return nil
	}

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

	// 3. 重新初始化任务队列（基于已完成的Task）
	// 注意：V2版本使用队列，需要重新初始化
	m.initTaskQueue()

	// 4. 恢复任务统计（通过taskStatsChan发送task_completed事件）
	// 注意：已完成的任务在initTaskQueue中已经通过task_added事件添加到统计中
	// 这里需要为已完成的任务发送task_completed事件
	for _, taskName := range bp.CompletedTaskNames {
		if taskID, exists := m.workflow.GetTaskIDByName(taskName); exists {
			// 检查任务状态，确定是成功还是失败
			task := m.workflow.GetTasks()[taskID]
			status := "task_completed"
			if task.GetStatus() == "FAILED" {
				status = "task_failed"
			}
			select {
			case m.taskStatsChan <- TaskStatsUpdate{
				Type:       status,
				TaskID:     taskID,
				IsTemplate: task.IsTemplate(),
				IsSubTask:  task.IsSubTask(),
			}:
			default:
				log.Printf("警告: taskStatsChan 已满，任务统计恢复可能丢失: TaskID=%s", taskID)
			}
		}
	}

	return nil
}

// saveAllTaskStatuses 批量保存所有任务状态到数据库（只保存预定义任务，跳过动态任务）
func (m *WorkflowInstanceManagerV2) saveAllTaskStatuses(ctx context.Context) {
	// 如果没有任何Repository，跳过保存
	if m.aggregateRepo == nil && m.taskRepo == nil {
		log.Printf("⚠️ 警告！WorkflowInstance %s: 没有可用的Repository，跳过保存", m.instance.ID)
		return
	}

	allTasks := m.workflow.GetTasks()
	savedCount := 0
	skippedCount := 0

	// 获取已存在的任务实例（用于比较状态）
	var taskInstanceMap map[string]*storage.TaskInstance
	if m.aggregateRepo != nil {
		_, taskInstances, err := m.aggregateRepo.GetWorkflowInstanceWithTasks(ctx, m.instance.ID)
		if err != nil {
			log.Printf("⚠️ WorkflowInstance %s: 查询任务实例失败: %v", m.instance.ID, err)
			return
		}
		taskInstanceMap = make(map[string]*storage.TaskInstance)
		for _, ti := range taskInstances {
			taskInstanceMap[ti.ID] = ti
		}
	} else if m.taskRepo != nil {
		taskInstances, err := m.taskRepo.GetByWorkflowInstanceID(ctx, m.instance.ID)
		if err != nil {
			log.Printf("⚠️ WorkflowInstance %s: 查询任务实例失败: %v", m.instance.ID, err)
			return
		}
		taskInstanceMap = make(map[string]*storage.TaskInstance)
		for _, ti := range taskInstances {
			taskInstanceMap[ti.ID] = ti
		}
	}

	for taskID, workflowTask := range allTasks {
		// 跳过动态生成的子任务（不保存到数据库）
		if workflowTask.IsSubTask() {
			skippedCount++
			continue
		}

		existingTask, exists := taskInstanceMap[taskID]
		if !exists {
			log.Printf("⚠️ WorkflowInstance %s: Task %s 不在数据库中，跳过保存", m.instance.ID, taskID)
			skippedCount++
			continue
		}

		currentStatus := workflowTask.GetStatus()
		if currentStatus == "" {
			if _, processed := m.processedNodes.Load(taskID); processed {
				errorKey := fmt.Sprintf("%s:error", taskID)
				if _, hasError := m.contextData.Load(errorKey); hasError {
					currentStatus = "Failed"
				} else {
					currentStatus = "Success"
				}
			} else {
				continue
			}
		}

		if strings.EqualFold(existingTask.Status, currentStatus) {
			continue
		}

		var updateErr error
		if task.IsFailedStatus(currentStatus) {
			errorKey := fmt.Sprintf("%s:error", taskID)
			errorMsg := ""
			if errorValue, hasError := m.contextData.Load(errorKey); hasError {
				if errStr, ok := errorValue.(string); ok {
					errorMsg = errStr
				}
			}
			updateErr = m.updateTaskInstanceStatusWithError(ctx, taskID, currentStatus, errorMsg)
		} else {
			updateErr = m.updateTaskInstanceStatus(ctx, taskID, currentStatus)
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

// ==================== Repository抽象方法（支持聚合Repository） ====================

// updateWorkflowInstanceStatus 更新WorkflowInstance状态（优先使用聚合Repository）
func (m *WorkflowInstanceManagerV2) updateWorkflowInstanceStatus(ctx context.Context, instanceID, status, errorMsg string) error {
	// 优先使用聚合Repository
	if m.aggregateRepo != nil {
		// 聚合Repository不支持单独更新WorkflowInstance状态，使用基础Repository
		// 注：聚合Repository主要用于事务操作，状态更新仍使用原有Repository
	}
	// 使用原有Repository
	if m.workflowInstanceRepo != nil {
		return m.workflowInstanceRepo.UpdateStatus(ctx, instanceID, status)
	}
	return nil
}

// updateTaskInstanceStatus 更新TaskInstance状态（优先使用聚合Repository）
func (m *WorkflowInstanceManagerV2) updateTaskInstanceStatus(ctx context.Context, taskID, status string) error {
	// 优先使用聚合Repository
	if m.aggregateRepo != nil {
		return m.aggregateRepo.UpdateTaskInstanceStatus(ctx, taskID, status)
	}
	// 使用原有Repository
	if m.taskRepo != nil {
		return m.taskRepo.UpdateStatus(ctx, taskID, status)
	}
	return nil
}

// updateTaskInstanceStatusWithError 更新TaskInstance状态和错误信息（优先使用聚合Repository）
func (m *WorkflowInstanceManagerV2) updateTaskInstanceStatusWithError(ctx context.Context, taskID, status, errorMsg string) error {
	// 优先使用聚合Repository
	if m.aggregateRepo != nil {
		return m.aggregateRepo.UpdateTaskInstanceStatusWithError(ctx, taskID, status, errorMsg)
	}
	// 使用原有Repository
	if m.taskRepo != nil {
		return m.taskRepo.UpdateStatusWithError(ctx, taskID, status, errorMsg)
	}
	return nil
}

// ==================== 子任务结果聚合相关方法 ====================

// handleSubTaskCompletion 处理子任务完成事件（记录结果，触发聚合）
func (m *WorkflowInstanceManagerV2) handleSubTaskCompletion(event TaskStatusEvent) {
	// 获取子任务信息
	var subTask workflow.Task
	if runtimeTask, ok := m.runtimeTasks.Load(event.TaskID); ok {
		subTask = runtimeTask.(workflow.Task)
	} else {
		log.Printf("警告: WorkflowInstance %s: 子任务 %s 在 runtimeTasks 中不存在", m.instance.ID, event.TaskID)
		return
	}

	// 获取父任务ID
	parentTaskID := event.ParentID
	if parentTaskID == "" {
		// 尝试从 contextData 中获取父任务ID
		parentKey := fmt.Sprintf("%s:parent_task_id", event.TaskID)
		if parentValue, exists := m.contextData.Load(parentKey); exists {
			parentTaskID = parentValue.(string)
		}
	}
	if parentTaskID == "" {
		// 尝试从子任务的依赖中获取父任务ID
		deps := subTask.GetDependencies()
		if len(deps) > 0 {
			// 假设第一个依赖是父任务
			if taskID, exists := m.workflow.GetTaskIDByName(deps[0]); exists {
				parentTaskID = taskID
			}
		}
	}

	if parentTaskID == "" {
		log.Printf("警告: WorkflowInstance %s: 无法确定子任务 %s 的父任务ID", m.instance.ID, event.TaskID)
		return
	}

	// 获取子任务跟踪器
	trackerValue, exists := m.subTaskTracker.Load(parentTaskID)
	if !exists {
		log.Printf("警告: WorkflowInstance %s: 父任务 %s 的子任务跟踪器不存在", m.instance.ID, parentTaskID)
		return
	}
	tracker := trackerValue.(*SubTaskTracker)

	// 记录子任务结果
	subTaskResult := SubTaskResult{
		TaskID:   event.TaskID,
		TaskName: subTask.GetName(),
		Status:   event.Status,
		Result:   event.Result,
	}
	tracker.Results.Store(event.TaskID, subTaskResult)

	// 增加完成计数（atomic）
	completedCount := atomic.AddInt32(&tracker.CompletedCount, 1)
	log.Printf("WorkflowInstance %s: 子任务 %s 完成，父任务 %s 进度: %d/%d",
		m.instance.ID, event.TaskID, parentTaskID, completedCount, tracker.TotalCount)

	// 检查是否所有子任务都已完成
	if completedCount >= tracker.TotalCount {
		// 所有子任务完成，触发结果聚合
		m.aggregateSubTaskResults(parentTaskID, tracker)
	}
}

// handleSubTaskFailure 处理子任务失败事件（记录结果，可能触发聚合）
func (m *WorkflowInstanceManagerV2) handleSubTaskFailure(event TaskStatusEvent) {
	// 获取子任务信息
	var subTask workflow.Task
	if runtimeTask, ok := m.runtimeTasks.Load(event.TaskID); ok {
		subTask = runtimeTask.(workflow.Task)
	} else {
		log.Printf("警告: WorkflowInstance %s: 子任务 %s 在 runtimeTasks 中不存在", m.instance.ID, event.TaskID)
		return
	}

	// 获取父任务ID
	parentTaskID := event.ParentID
	if parentTaskID == "" {
		// 尝试从 contextData 中获取父任务ID
		parentKey := fmt.Sprintf("%s:parent_task_id", event.TaskID)
		if parentValue, exists := m.contextData.Load(parentKey); exists {
			parentTaskID = parentValue.(string)
		}
	}
	if parentTaskID == "" {
		deps := subTask.GetDependencies()
		if len(deps) > 0 {
			if taskID, exists := m.workflow.GetTaskIDByName(deps[0]); exists {
				parentTaskID = taskID
			}
		}
	}

	if parentTaskID == "" {
		log.Printf("警告: WorkflowInstance %s: 无法确定子任务 %s 的父任务ID", m.instance.ID, event.TaskID)
		return
	}

	// 获取子任务跟踪器
	trackerValue, exists := m.subTaskTracker.Load(parentTaskID)
	if !exists {
		log.Printf("警告: WorkflowInstance %s: 父任务 %s 的子任务跟踪器不存在", m.instance.ID, parentTaskID)
		return
	}
	tracker := trackerValue.(*SubTaskTracker)

	// 记录子任务失败结果
	errorMsg := ""
	if event.Error != nil {
		errorMsg = event.Error.Error()
	}
	subTaskResult := SubTaskResult{
		TaskID:   event.TaskID,
		TaskName: subTask.GetName(),
		Status:   "Failed",
		Result:   nil,
		Error:    errorMsg,
	}
	tracker.Results.Store(event.TaskID, subTaskResult)

	// 增加完成计数和失败计数（atomic）
	atomic.AddInt32(&tracker.FailedCount, 1)
	completedCount := atomic.AddInt32(&tracker.CompletedCount, 1)
	log.Printf("WorkflowInstance %s: 子任务 %s 失败，父任务 %s 进度: %d/%d",
		m.instance.ID, event.TaskID, parentTaskID, completedCount, tracker.TotalCount)

	// 检查是否所有子任务都已完成（包括失败的）
	if completedCount >= tracker.TotalCount {
		// 所有子任务完成，触发结果聚合
		m.aggregateSubTaskResults(parentTaskID, tracker)
	}
}

// aggregateSubTaskResults 聚合子任务结果到父任务
func (m *WorkflowInstanceManagerV2) aggregateSubTaskResults(parentTaskID string, tracker *SubTaskTracker) {
	log.Printf("WorkflowInstance %s: 开始聚合父任务 %s 的子任务结果", m.instance.ID, parentTaskID)

	// 收集所有子任务结果
	subtaskResults := make([]map[string]interface{}, 0, len(tracker.SubTaskIDs))
	allSucceeded := true

	for _, subTaskID := range tracker.SubTaskIDs {
		if resultValue, exists := tracker.Results.Load(subTaskID); exists {
			result := resultValue.(SubTaskResult)
			subtaskResults = append(subtaskResults, map[string]interface{}{
				"task_id":   result.TaskID,
				"task_name": result.TaskName,
				"status":    result.Status,
				"result":    result.Result,
				"error":     result.Error,
			})
			if !task.IsSuccessStatus(result.Status) {
				allSucceeded = false
			}
		}
	}

	// 获取父任务的原始结果
	var parentResult map[string]interface{}
	if existingResult, exists := m.contextData.Load(parentTaskID); exists {
		if result, ok := existingResult.(map[string]interface{}); ok {
			parentResult = result
		} else {
			// 如果父任务结果不是 map 类型，包装它
			parentResult = map[string]interface{}{
				"original_result": existingResult,
			}
		}
	} else {
		parentResult = make(map[string]interface{})
	}

	// 注入子任务聚合结果
	parentResult["subtask_results"] = subtaskResults
	parentResult["subtask_count"] = len(tracker.SubTaskIDs)
	parentResult["all_subtasks_succeeded"] = allSucceeded

	// 更新 contextData
	m.contextData.Store(parentTaskID, parentResult)

	// 更新 resultCache
	if m.resultCache != nil {
		ttl := 1 * time.Hour
		_ = m.resultCache.Set(parentTaskID, parentResult, ttl)
	}

	log.Printf("WorkflowInstance %s: 父任务 %s 的子任务结果聚合完成，共 %d 个子任务，全部成功: %v",
		m.instance.ID, parentTaskID, len(subtaskResults), allSucceeded)
}

// allSubTasksCompleted 检查当前层级的所有模板任务的子任务是否都已完成
func (m *WorkflowInstanceManagerV2) allSubTasksCompleted(currentLevel int) bool {
	allCompleted := true

	// 遍历所有子任务跟踪器
	m.subTaskTracker.Range(func(key, value interface{}) bool {
		parentTaskID := key.(string)
		tracker := value.(*SubTaskTracker)

		// 检查父任务是否在当前层级
		levelKey := fmt.Sprintf("%s:original_level", parentTaskID)
		if levelValue, exists := m.contextData.Load(levelKey); exists {
			if level, ok := levelValue.(int); ok && level == currentLevel {
				// 检查子任务是否全部完成
				completedCount := atomic.LoadInt32(&tracker.CompletedCount)
				if completedCount < tracker.TotalCount {
					log.Printf("WorkflowInstance %s: 父任务 %s 的子任务未全部完成: %d/%d",
						m.instance.ID, parentTaskID, completedCount, tracker.TotalCount)
					allCompleted = false
					return false // 停止遍历
				}
			}
		}
		return true
	})

	return allCompleted
}
