// Package realtime 提供实时数据采集任务的实例管理器
package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/google/uuid"

	"github.com/LENAX/task-engine/pkg/core/task"
	"github.com/LENAX/task-engine/pkg/core/types"
	"github.com/LENAX/task-engine/pkg/core/workflow"
	"github.com/LENAX/task-engine/pkg/storage"
)

// ShutdownOnTerminateOnly 表示该 Manager 在收到 Terminate 后由内部 controlSignalHandler 自行调用 Shutdown，
// 外部（如 Engine.Stop）不应再对其调用 Shutdown()，否则可能造成 wg 死锁。Engine 应只发 Terminate 并轮询 GetStatus() 直到 Stopped。
type ShutdownOnTerminateOnly interface {
	ShutdownOnTerminateOnly() bool
}

// RealtimeInstanceManager 实时实例管理器接口
// 继承 types.WorkflowInstanceManager 接口，并扩展实时特定功能
type RealtimeInstanceManager interface {
	types.WorkflowInstanceManager

	// PublishEvent 发布事件到事件总线
	PublishEvent(ctx context.Context, event *RealtimeEvent) error

	// SubscribeEvent 订阅事件
	SubscribeEvent(eventType EventType, handler EventHandler) (SubscriptionID, error)

	// UnsubscribeEvent 取消订阅
	UnsubscribeEvent(subscriptionID SubscriptionID) error

	// GetContinuousTask 获取持续任务
	GetContinuousTask(taskID string) (*ContinuousTask, error)

	// PauseContinuousTask 暂停持续任务
	PauseContinuousTask(taskID string) error

	// ResumeContinuousTask 恢复持续任务
	ResumeContinuousTask(taskID string) error

	// GetMetrics 获取运行指标
	GetMetrics() *RealtimeMetrics

	// GetDataBuffer 获取数据缓冲区（供测试或监控使用）
	GetDataBuffer() *DataBuffer
}

// RealtimeMetrics 运行指标
type RealtimeMetrics struct {
	TotalEvents     int64         `json:"total_events"`
	ProcessedEvents int64         `json:"processed_events"`
	FailedEvents    int64         `json:"failed_events"`
	ActiveTasks     int           `json:"active_tasks"`
	BufferUsage     float64       `json:"buffer_usage"`
	AverageLatency  time.Duration `json:"average_latency"`
	Uptime          time.Duration `json:"uptime"`
}

// realtimeInstanceManagerImpl RealtimeInstanceManager 实现
type realtimeInstanceManagerImpl struct {
	// 基础信息
	instance *workflow.WorkflowInstance
	wf       *workflow.Workflow

	// 实时任务映射
	realtimeTasks sync.Map // taskID -> *RealtimeTask

	// Watermill 组件
	pubsub *gochannel.GoChannel
	router *message.Router
	logger watermill.LoggerAdapter

	// 任务管理
	continuousTasks sync.Map       // taskID -> *ContinuousTask
	taskWg          sync.WaitGroup // 任务等待组

	// 事件订阅管理
	subscriptions  sync.Map // subscriptionID -> *subscription
	subscriptionID int64    // atomic，订阅ID生成器

	// 数据缓冲（背压控制）
	dataBuffer *DataBuffer

	// 采集器注册表（可选）
	collectorRegistry DataCollectorRegistry

	// 函数注册表（可选，用于 runStreamProcessor 调用 DataHandler）
	functionRegistry DataHandlerRegistry

	// 状态管理
	state atomic.Value // ContinuousTaskState

	// 存储层
	stateStore storage.WorkflowInstanceRepository

	// 控制
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// shutdownMu 保证同一时间只有一个 Shutdown 执行，避免 Engine.Stop 与 handleTerminate 并发调用导致 wg.Wait 死锁
	shutdownMu sync.Mutex

	// 通道
	controlSignalChan chan workflow.ControlSignal
	statusUpdateChan  chan string

	// 指标
	metrics   *RealtimeMetrics
	startTime time.Time

	// 配置选项
	opts *options

	mu sync.RWMutex
}

// subscription 内部订阅结构
type subscription struct {
	id        SubscriptionID
	eventType EventType
	handler   EventHandler
	active    bool
}

// NewRealtimeInstanceManager 创建 RealtimeInstanceManager
func NewRealtimeInstanceManager(
	instance *workflow.WorkflowInstance,
	wf *workflow.Workflow,
	stateStore storage.WorkflowInstanceRepository,
	opts ...Option,
) (RealtimeInstanceManager, error) {
	// 校验执行模式
	if wf.GetExecutionMode() != workflow.ExecutionModeStreaming {
		return nil, fmt.Errorf("Workflow 执行模式必须为 'streaming'，当前: %s", wf.GetExecutionMode())
	}

	// 应用选项
	options := defaultOptions()
	for _, opt := range opts {
		opt(options)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// 创建 Watermill logger
	logger := watermill.NewStdLogger(options.debug, options.trace)

	// 创建 Pub/Sub
	pubsub := gochannel.NewGoChannel(
		gochannel.Config{
			Persistent:                     false,
			BlockPublishUntilSubscriberAck: false,
		},
		logger,
	)

	// 创建消息路由器
	routerConfig := message.RouterConfig{}
	msgRouter, err := message.NewRouter(routerConfig, logger)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("创建消息路由器失败: %w", err)
	}

	manager := &realtimeInstanceManagerImpl{
		instance:          instance,
		wf:                wf,
		pubsub:            pubsub,
		router:            msgRouter,
		logger:            logger,
		dataBuffer:        NewDataBuffer(options.bufferSize, options.backpressureThreshold),
		collectorRegistry: options.collectorRegistry,
		functionRegistry:  options.functionRegistry,
		stateStore:        stateStore,
		ctx:               ctx,
		cancel:            cancel,
		controlSignalChan: make(chan workflow.ControlSignal, 10),
		statusUpdateChan:  make(chan string, 10),
		metrics:           &RealtimeMetrics{},
		startTime:         time.Now(),
		opts:              options,
	}

	// 初始化状态
	manager.state.Store(StateInitializing)

	// 从 Workflow.Tasks 中识别并存储实时任务
	allTasks := wf.GetTasks()
	for taskID, t := range allTasks {
		if rtTask := ExtractRealtimeTask(t); rtTask != nil {
			manager.realtimeTasks.Store(taskID, rtTask)
			log.Printf("识别实时任务: TaskID=%s, ExecutionMode=%s", taskID, rtTask.ExecutionMode)
		}
	}

	// 注册内部事件处理器
	if err := manager.registerInternalHandlers(); err != nil {
		cancel()
		return nil, fmt.Errorf("注册内部处理器失败: %w", err)
	}

	// 设置背压回调
	manager.dataBuffer.SetBackpressureCallback(func(usage float64) {
		manager.PublishEvent(ctx, NewRealtimeEvent(
			EventBackpressure,
			"",
			instance.ID,
			&BackpressurePayload{
				BufferUsage: usage,
				QueueLength: manager.dataBuffer.Len(),
				Threshold:   manager.dataBuffer.GetThreshold(),
				Action:      "throttle",
			},
		))
	})

	manager.dataBuffer.SetBackpressureRelieveCallback(func(usage float64) {
		manager.PublishEvent(ctx, NewRealtimeEvent(
			EventBackpressureRelieved,
			"",
			instance.ID,
			&BackpressurePayload{
				BufferUsage: usage,
				QueueLength: manager.dataBuffer.Len(),
				Threshold:   manager.dataBuffer.GetThreshold(),
				Action:      "resume",
			},
		))
	})

	return manager, nil
}

// registerInternalHandlers 注册内部事件处理器
func (m *realtimeInstanceManagerImpl) registerInternalHandlers() error {
	// 数据到达处理器
	m.router.AddNoPublisherHandler(
		"data_arrived_handler",
		string(EventDataArrived),
		m.pubsub,
		m.handleDataArrived,
	)

	// 连接状态处理器
	m.router.AddNoPublisherHandler(
		"connection_handler",
		string(EventDisconnected),
		m.pubsub,
		m.handleConnectionLost,
	)

	// 错误处理器
	m.router.AddNoPublisherHandler(
		"error_handler",
		string(EventError),
		m.pubsub,
		m.handleError,
	)

	// 背压处理器
	m.router.AddNoPublisherHandler(
		"backpressure_handler",
		string(EventBackpressure),
		m.pubsub,
		m.handleBackpressure,
	)

	return nil
}

// Start 启动实例
func (m *realtimeInstanceManagerImpl) Start() {
	m.mu.Lock()
	m.instance.Status = "Running"
	m.instance.StartTime = time.Now()
	m.mu.Unlock()

	// 更新状态
	m.state.Store(StateRunning)

	// 持久化状态
	ctx := context.Background()
	if m.stateStore != nil {
		if err := m.stateStore.UpdateStatus(ctx, m.instance.ID, "Running"); err != nil {
			log.Printf("更新状态失败: %v", err)
		}
	}

	// 启动消息路由器：watermill Router.Run(ctx) 在部分版本中不会随 ctx 取消而立即返回，
	// 故由本 goroutine 在 ctx.Done() 时主动 Close() 并等待 Run 返回；若 Run 未在超时内返回则本 goroutine 仍退出，避免 wg.Wait 死锁
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		runDone := make(chan struct{})
		go func() {
			if err := m.router.Run(m.ctx); err != nil {
				log.Printf("消息路由器退出: %v", err)
			}
			close(runDone)
		}()
		select {
		case <-m.ctx.Done():
			// 在后台触发 Close，避免本 goroutine 卡在 router.Close() 内导致 wg.Wait 死锁
			go func() { _ = m.router.Close() }()
			routerWait := 2 * time.Second
			if m.opts.shutdownTimeout > 0 && m.opts.shutdownTimeout < routerWait {
				routerWait = m.opts.shutdownTimeout
			}
			select {
			case <-runDone:
			case <-time.After(routerWait):
				log.Printf("RealtimeInstance %s: 等待 router.Run 退出超时", m.instance.ID)
			}
		case <-runDone:
		}
	}()

	// 启动持续任务
	m.startContinuousTasks()

	// 启动控制信号处理
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.controlSignalHandler()
	}()

	// 启动指标收集
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.metricsCollector()
	}()

	// 发送状态更新
	select {
	case m.statusUpdateChan <- "Running":
	default:
	}

	log.Printf("RealtimeInstance %s: 已启动", m.instance.ID)
}

// startContinuousTasks 启动所有持续任务
func (m *realtimeInstanceManagerImpl) startContinuousTasks() {
	m.realtimeTasks.Range(func(key, value interface{}) bool {
		taskID := key.(string)
		rtTask := value.(*RealtimeTask)

		if rtTask.ExecutionMode == ExecutionModeContinuous && rtTask.ContinuousConfig != nil {
			// 创建 ContinuousTask
			ct := NewContinuousTask(*rtTask.ContinuousConfig)
			ct.Start(m.ctx)
			m.continuousTasks.Store(taskID, ct)

			// 启动任务运行 goroutine
			m.taskWg.Add(1)
			go func(taskID string, ct *ContinuousTask) {
				defer m.taskWg.Done()
				m.runContinuousTask(taskID, ct)
			}(taskID, ct)

			// 发布启动事件
			m.PublishEvent(m.ctx, NewRealtimeEvent(
				EventTaskStarted,
				taskID,
				m.instance.ID,
				&TaskStatusPayload{
					TaskID:    taskID,
					TaskName:  rtTask.GetName(),
					OldStatus: string(StateInitializing),
					NewStatus: string(StateRunning),
				},
			))
		}
		return true
	})
}

// runContinuousTask 运行持续任务
func (m *realtimeInstanceManagerImpl) runContinuousTask(taskID string, ct *ContinuousTask) {
	log.Printf("Task %s: 开始运行", taskID)

	for {
		select {
		case <-ct.Context().Done():
			log.Printf("Task %s: 上下文取消，退出", taskID)
			ct.SetState(StateStopped)
			return

		default:
			// 检查任务状态
			state := ct.GetState()
			if state == StateStopping || state == StateStopped {
				return
			}

			if state == StatePaused {
				// 暂停时等待
				time.Sleep(100 * time.Millisecond)
				continue
			}

			// 执行任务逻辑
			if err := m.executeTaskLogic(ct); err != nil {
				log.Printf("Task %s: 执行错误: %v", taskID, err)
				ct.IncrementErrorCount()

				// 发布错误事件
				m.PublishEvent(m.ctx, NewRealtimeEvent(
					EventError,
					taskID,
					m.instance.ID,
					&ErrorPayload{
						Message:     err.Error(),
						Recoverable: true,
					},
				))

				// 检查是否需要重连
				if ct.Config.ReconnectEnabled && ct.GetState() != StateStopping {
					m.handleReconnect(ct)
				}
			}
		}
	}
}

// executeTaskLogic 执行任务逻辑
// TaskTypeDataCollector 与 TaskTypeScheduledPoller 统一走 runDataCollector，由 Config.Mode 与用户实现区分 push/pull
func (m *realtimeInstanceManagerImpl) executeTaskLogic(ct *ContinuousTask) error {
	switch ct.Config.Type {
	case TaskTypeDataCollector, TaskTypeScheduledPoller:
		return m.runDataCollector(ct)
	case TaskTypeStreamProcessor:
		return m.runStreamProcessor(ct)
	case TaskTypeEventListener:
		return m.runEventListener(ct)
	default:
		return fmt.Errorf("未知任务类型: %s", ct.Config.Type)
	}
}

// runDataCollector 运行数据采集器（唯一生产者路径；DataCollector 与 ScheduledPoller 均走此逻辑）
func (m *realtimeInstanceManagerImpl) runDataCollector(ct *ContinuousTask) error {
	if m.collectorRegistry != nil && ct.Config.CollectorName != "" {
		collector, ok := m.collectorRegistry.Get(ct.Config.CollectorName)
		if !ok {
			return fmt.Errorf("未注册的采集器: %s", ct.Config.CollectorName)
		}
		publish := func(event *RealtimeEvent) error {
			return m.PublishEvent(m.ctx, event)
		}
		return collector.Run(ct.Context(), &ct.Config, publish)
	}
	// 占位：未配置采集器时短 sleep
	time.Sleep(100 * time.Millisecond)
	return nil
}

// runStreamProcessor 运行流处理器
func (m *realtimeInstanceManagerImpl) runStreamProcessor(ct *ContinuousTask) error {
	// 从缓冲区获取数据并处理
	data, ok := m.dataBuffer.Pop()
	if !ok {
		time.Sleep(10 * time.Millisecond)
		return nil
	}

	ct.IncrementDataCount()
	ct.UpdateLastDataTime()

	// 若配置了 DataHandler 且注册表可用，则调用 Job 函数处理数据
	if ct.Config.DataHandler != "" && m.functionRegistry != nil {
		if fn := m.functionRegistry.GetByName(ct.Config.DataHandler); fn != nil {
			params := map[string]interface{}{"data": data}
			taskCtx := task.NewTaskContext(m.ctx, ct.Config.ID, ct.Config.Name, m.wf.GetID(), m.instance.ID, params)
			stateCh := fn(taskCtx)
			if state := <-stateCh; state.Status == "Failed" && state.Error != nil {
				log.Printf("Task %s DataHandler %s 执行失败: %v", ct.Config.ID, ct.Config.DataHandler, state.Error)
				ct.IncrementErrorCount()
			}
		}
	}

	// 发布数据处理完成事件
	m.PublishEvent(m.ctx, NewRealtimeEvent(
		EventDataProcessed,
		ct.Config.ID,
		m.instance.ID,
		&DataArrivedPayload{
			Data:     data,
			Source:   ct.Config.Endpoint,
			Sequence: ct.GetDataCount(),
		},
	))

	return nil
}

// runEventListener 运行事件监听器
func (m *realtimeInstanceManagerImpl) runEventListener(ct *ContinuousTask) error {
	// 等待事件
	time.Sleep(100 * time.Millisecond)
	return nil
}

// handleReconnect 处理重连逻辑
func (m *realtimeInstanceManagerImpl) handleReconnect(ct *ContinuousTask) {
	ct.SetState(StateReconnecting)
	ct.SetConnected(false)

	backoff := ct.Config.ReconnectBackoff
	if backoff.InitialInterval == 0 {
		backoff = DefaultReconnectBackoffConfig()
	}

	interval := backoff.InitialInterval

	for attempt := 1; ; attempt++ {
		// 检查是否超过最大重连次数
		if ct.Config.MaxReconnectAttempts > 0 && attempt > ct.Config.MaxReconnectAttempts {
			log.Printf("Task %s: 超过最大重连次数 %d，停止重连",
				ct.Config.ID, ct.Config.MaxReconnectAttempts)
			ct.SetState(StateError)
			return
		}

		// 发布重连事件
		m.PublishEvent(m.ctx, NewRealtimeEvent(
			EventReconnecting,
			ct.Config.ID,
			m.instance.ID,
			&ConnectionPayload{
				Endpoint:   ct.Config.Endpoint,
				Protocol:   ct.Config.Protocol,
				RetryCount: attempt,
			},
		))

		log.Printf("Task %s: 尝试重连 (第 %d 次)，等待 %v",
			ct.Config.ID, attempt, interval)

		// 等待退避时间
		select {
		case <-ct.Context().Done():
			return
		case <-time.After(interval):
		}

		// 尝试重连
		if err := m.reconnect(ct); err == nil {
			ct.SetState(StateRunning)
			ct.SetConnected(true)
			ct.IncrementReconnectCount()

			// 发布重连成功事件
			m.PublishEvent(m.ctx, NewRealtimeEvent(
				EventReconnected,
				ct.Config.ID,
				m.instance.ID,
				&ConnectionPayload{
					Endpoint:   ct.Config.Endpoint,
					Protocol:   ct.Config.Protocol,
					RetryCount: attempt,
				},
			))

			log.Printf("Task %s: 重连成功", ct.Config.ID)
			return
		}

		// 计算下次退避时间
		interval = time.Duration(float64(interval) * backoff.Multiplier)
		if interval > backoff.MaxInterval {
			interval = backoff.MaxInterval
		}
	}
}

// reconnect 执行重连
func (m *realtimeInstanceManagerImpl) reconnect(ct *ContinuousTask) error {
	// 占位实现，实际需要根据协议类型实现具体重连逻辑
	return nil
}

// Shutdown 优雅关闭
func (m *realtimeInstanceManagerImpl) Shutdown() {
	m.shutdownMu.Lock()
	defer m.shutdownMu.Unlock()
	if m.state.Load() == StateStopped {
		return
	}
	log.Printf("RealtimeInstance %s: 开始关闭...", m.instance.ID)
	m.state.Store(StateStopping)

	// 先取消上下文，使 router 包装 goroutine、metricsCollector 以及依赖 m.ctx 的 task 能立即收到 Done 并退出，
	// 避免本函数卡在 taskWg.Wait() 或 wg.Wait() 导致永远无法执行到 cancel()
	m.cancel()

	// 停止所有持续任务（ct.Stop 取消任务上下文，配合上面 m.cancel() 使 task 尽快退出）
	m.continuousTasks.Range(func(key, value interface{}) bool {
		ct := value.(*ContinuousTask)
		ct.Stop()
		return true
	})

	// 等待任务完成（带超时）
	done := make(chan struct{})
	go func() {
		m.taskWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Printf("RealtimeInstance %s: 所有任务已停止", m.instance.ID)
	case <-time.After(m.opts.shutdownTimeout):
		log.Printf("RealtimeInstance %s: 任务停止超时", m.instance.ID)
	}
	// 再关闭路由器与 Pub/Sub（若 Run 未随 ctx 退出，Close 会令其返回）
	if err := m.router.Close(); err != nil {
		log.Printf("关闭路由器失败: %v", err)
	}
	if err := m.pubsub.Close(); err != nil {
		log.Printf("关闭 Pub/Sub 失败: %v", err)
	}

	// 等待其他 goroutine（router 包装、metricsCollector）退出，带超时以防 watermill 未及时响应 cancel/Close
	wgDone := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(wgDone)
	}()
	wgWait := 3 * time.Second
	if m.opts.shutdownTimeout > 0 && m.opts.shutdownTimeout < wgWait {
		wgWait = m.opts.shutdownTimeout
	}
	select {
	case <-wgDone:
	case <-time.After(wgWait):
		log.Printf("RealtimeInstance %s: 等待 router/metrics 退出超时", m.instance.ID)
	}

	// 更新状态（含 instance.Status 以便 Engine.Stop 轮询 GetStatus() 能见到 Stopped）
	m.state.Store(StateStopped)
	m.mu.Lock()
	m.instance.Status = "Stopped"
	m.mu.Unlock()

	// 持久化最终状态
	ctx := context.Background()
	if m.stateStore != nil {
		m.stateStore.UpdateStatus(ctx, m.instance.ID, "Stopped")
	}

	// 发送状态更新
	select {
	case m.statusUpdateChan <- "Stopped":
	default:
	}

	log.Printf("RealtimeInstance %s: 已关闭", m.instance.ID)
}

// PublishEvent 发布事件
func (m *realtimeInstanceManagerImpl) PublishEvent(ctx context.Context, event *RealtimeEvent) error {
	if event.ID == "" {
		event.ID = uuid.NewString()
	}

	// 序列化事件
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("序列化事件失败: %w", err)
	}

	// 创建 Watermill 消息
	msg := message.NewMessage(event.ID, payload)
	msg.Metadata.Set("event_type", string(event.Type))
	msg.Metadata.Set("task_id", event.TaskID)
	msg.Metadata.Set("instance_id", event.InstanceID)
	msg.Metadata.Set("timestamp", event.Timestamp.Format(time.RFC3339Nano))

	// 发布消息
	if err := m.pubsub.Publish(string(event.Type), msg); err != nil {
		return fmt.Errorf("发布事件失败: %w", err)
	}

	// 更新指标
	atomic.AddInt64(&m.metrics.TotalEvents, 1)

	return nil
}

// SubscribeEvent 订阅事件
func (m *realtimeInstanceManagerImpl) SubscribeEvent(eventType EventType, handler EventHandler) (SubscriptionID, error) {
	id := SubscriptionID(fmt.Sprintf("sub-%d", atomic.AddInt64(&m.subscriptionID, 1)))

	sub := &subscription{
		id:        id,
		eventType: eventType,
		handler:   handler,
		active:    true,
	}

	m.subscriptions.Store(id, sub)

	// 动态添加处理器到路由器
	handlerName := fmt.Sprintf("dynamic_handler_%s", id)
	m.router.AddNoPublisherHandler(
		handlerName,
		string(eventType),
		m.pubsub,
		func(msg *message.Message) error {
			// 检查订阅是否仍然活跃
			if subValue, ok := m.subscriptions.Load(id); ok {
				sub := subValue.(*subscription)
				if !sub.active {
					return nil
				}

				var event RealtimeEvent
				if err := json.Unmarshal(msg.Payload, &event); err != nil {
					return err
				}
				return handler(&event)
			}
			return nil
		},
	)

	return id, nil
}

// UnsubscribeEvent 取消订阅
func (m *realtimeInstanceManagerImpl) UnsubscribeEvent(subscriptionID SubscriptionID) error {
	if subValue, ok := m.subscriptions.Load(subscriptionID); ok {
		sub := subValue.(*subscription)
		sub.active = false
	}
	m.subscriptions.Delete(subscriptionID)
	return nil
}

// GetContinuousTask 获取持续任务
func (m *realtimeInstanceManagerImpl) GetContinuousTask(taskID string) (*ContinuousTask, error) {
	if ctValue, exists := m.continuousTasks.Load(taskID); exists {
		return ctValue.(*ContinuousTask), nil
	}
	return nil, fmt.Errorf("任务 %s 不存在", taskID)
}

// PauseContinuousTask 暂停持续任务
func (m *realtimeInstanceManagerImpl) PauseContinuousTask(taskID string) error {
	ct, err := m.GetContinuousTask(taskID)
	if err != nil {
		return err
	}

	ct.Pause()

	// 发布暂停事件
	m.PublishEvent(m.ctx, NewRealtimeEvent(
		EventTaskPaused,
		taskID,
		m.instance.ID,
		&TaskStatusPayload{
			TaskID:    taskID,
			TaskName:  ct.Config.Name,
			OldStatus: string(StateRunning),
			NewStatus: string(StatePaused),
		},
	))

	return nil
}

// ResumeContinuousTask 恢复持续任务
func (m *realtimeInstanceManagerImpl) ResumeContinuousTask(taskID string) error {
	ct, err := m.GetContinuousTask(taskID)
	if err != nil {
		return err
	}

	ct.Resume()

	// 发布恢复事件
	m.PublishEvent(m.ctx, NewRealtimeEvent(
		EventTaskResumed,
		taskID,
		m.instance.ID,
		&TaskStatusPayload{
			TaskID:    taskID,
			TaskName:  ct.Config.Name,
			OldStatus: string(StatePaused),
			NewStatus: string(StateRunning),
		},
	))

	return nil
}

// GetMetrics 获取运行指标
func (m *realtimeInstanceManagerImpl) GetMetrics() *RealtimeMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return &RealtimeMetrics{
		TotalEvents:     atomic.LoadInt64(&m.metrics.TotalEvents),
		ProcessedEvents: atomic.LoadInt64(&m.metrics.ProcessedEvents),
		FailedEvents:    atomic.LoadInt64(&m.metrics.FailedEvents),
		ActiveTasks:     m.metrics.ActiveTasks,
		BufferUsage:     m.dataBuffer.Usage(),
		AverageLatency:  m.metrics.AverageLatency,
		Uptime:          time.Since(m.startTime),
	}
}

// ============================================
// 实现 WorkflowInstanceManager 接口的所有方法
// ============================================

// GetControlSignalChannel 获取控制信号通道
func (m *realtimeInstanceManagerImpl) GetControlSignalChannel() interface{} {
	return m.controlSignalChan
}

// GetStatusUpdateChannel 获取状态更新通道
func (m *realtimeInstanceManagerImpl) GetStatusUpdateChannel() <-chan string {
	return m.statusUpdateChan
}

// GetInstanceID 获取实例ID
func (m *realtimeInstanceManagerImpl) GetInstanceID() string {
	return m.instance.ID
}

// GetStatus 获取实例状态
func (m *realtimeInstanceManagerImpl) GetStatus() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.instance.Status
}

// GetProgress 获取当前实例的内存中任务进度（实现 types.WorkflowInstanceManager 接口）
// 实时实例以持续任务数为总任务数，均视为运行中
func (m *realtimeInstanceManagerImpl) GetProgress() types.ProgressSnapshot {
	var total int
	m.continuousTasks.Range(func(_, _ interface{}) bool {
		total++
		return true
	})
	return types.ProgressSnapshot{
		Total:     total,
		Completed: 0,
		Running:   total,
		Failed:    0,
		Pending:   0,
	}
}

// Context 获取上下文
func (m *realtimeInstanceManagerImpl) Context() context.Context {
	return m.ctx
}

// AddSubTask 动态添加子任务
func (m *realtimeInstanceManagerImpl) AddSubTask(subTask types.Task, parentTaskID string) error {
	// 检查实例状态
	if m.GetStatus() != "Running" {
		return fmt.Errorf("实例未运行，无法添加子任务")
	}

	// 检查父任务是否存在
	if _, exists := m.continuousTasks.Load(parentTaskID); !exists {
		// 也检查 realtimeTasks
		if _, exists := m.realtimeTasks.Load(parentTaskID); !exists {
			return fmt.Errorf("父任务 %s 不存在", parentTaskID)
		}
	}

	// 将子任务转换为实时任务
	rtTask, err := m.convertToRealtimeTask(subTask)
	if err != nil {
		return fmt.Errorf("转换任务失败: %w", err)
	}

	// 存储实时任务
	m.realtimeTasks.Store(subTask.GetID(), rtTask)

	// 如果子任务是持续运行模式，启动它
	if rtTask.ExecutionMode == ExecutionModeContinuous && rtTask.ContinuousConfig != nil {
		ct := NewContinuousTask(*rtTask.ContinuousConfig)
		ct.Start(m.ctx)
		m.continuousTasks.Store(subTask.GetID(), ct)

		m.taskWg.Add(1)
		go func(taskID string, ct *ContinuousTask) {
			defer m.taskWg.Done()
			m.runContinuousTask(taskID, ct)
		}(subTask.GetID(), ct)
	}

	// 发布子任务添加事件
	m.PublishEvent(m.ctx, NewRealtimeEvent(
		EventTaskStarted,
		subTask.GetID(),
		m.instance.ID,
		map[string]interface{}{
			"parent_task_id": parentTaskID,
			"is_subtask":     true,
		},
	))

	log.Printf("RealtimeInstance %s: 已添加子任务 %s (父任务: %s)",
		m.instance.ID, subTask.GetID(), parentTaskID)

	return nil
}

// AtomicAddSubTasks 原子性地添加多个子任务
func (m *realtimeInstanceManagerImpl) AtomicAddSubTasks(subTasks []types.Task, parentTaskID string) error {
	// 检查实例状态
	if m.GetStatus() != "Running" {
		return fmt.Errorf("实例未运行，无法添加子任务")
	}

	// 检查父任务是否存在
	if _, exists := m.continuousTasks.Load(parentTaskID); !exists {
		if _, exists := m.realtimeTasks.Load(parentTaskID); !exists {
			return fmt.Errorf("父任务 %s 不存在", parentTaskID)
		}
	}

	// 批量添加
	var addedTasks []string
	for _, subTask := range subTasks {
		if err := m.AddSubTask(subTask, parentTaskID); err != nil {
			// 回滚已添加的任务
			for _, taskID := range addedTasks {
				m.realtimeTasks.Delete(taskID)
				if ct, exists := m.continuousTasks.Load(taskID); exists {
					ct.(*ContinuousTask).Stop()
					m.continuousTasks.Delete(taskID)
				}
			}
			return fmt.Errorf("添加子任务 %s 失败: %w", subTask.GetID(), err)
		}
		addedTasks = append(addedTasks, subTask.GetID())
	}

	log.Printf("RealtimeInstance %s: 已原子性添加 %d 个子任务 (父任务: %s)",
		m.instance.ID, len(subTasks), parentTaskID)

	return nil
}

// convertToRealtimeTask 将普通 Task 转换为 RealtimeTask
func (m *realtimeInstanceManagerImpl) convertToRealtimeTask(t types.Task) (*RealtimeTask, error) {
	// 检查任务是否已经是 RealtimeTask
	if rtTask, ok := t.(*RealtimeTask); ok {
		return rtTask, nil
	}

	// 尝试类型断言为 *task.Task
	baseTask, ok := t.(*task.Task)
	if !ok {
		return nil, fmt.Errorf("无法转换任务类型")
	}

	// 创建默认的实时任务配置
	rtTask := &RealtimeTask{
		Task:          baseTask,
		ExecutionMode: ExecutionModeEventDriven, // 默认事件驱动模式
	}

	return rtTask, nil
}

// CreateBreakpoint 创建断点数据
func (m *realtimeInstanceManagerImpl) CreateBreakpoint() interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 收集所有持续任务的状态
	taskStates := make(map[string]interface{})
	var runningTaskNames []string

	m.continuousTasks.Range(func(key, value interface{}) bool {
		taskID := key.(string)
		ct := value.(*ContinuousTask)

		taskStates[taskID] = ct.GetSnapshot()

		state := ct.GetState()
		if state == StateRunning || state == StateReconnecting {
			runningTaskNames = append(runningTaskNames, ct.Config.Name)
		}

		return true
	})

	// 获取缓冲区状态
	totalIn, totalOut, dropped, usage := m.dataBuffer.Stats()

	// 创建断点数据
	return &workflow.BreakpointData{
		CompletedTaskNames: []string{}, // 实时任务没有"完成"的概念
		RunningTaskNames:   runningTaskNames,
		DAGSnapshot: map[string]interface{}{
			"task_states": taskStates,
			"buffer_stats": map[string]interface{}{
				"total_in":  totalIn,
				"total_out": totalOut,
				"dropped":   dropped,
				"usage":     usage,
			},
			"metrics": map[string]interface{}{
				"total_events":     atomic.LoadInt64(&m.metrics.TotalEvents),
				"processed_events": atomic.LoadInt64(&m.metrics.ProcessedEvents),
				"failed_events":    atomic.LoadInt64(&m.metrics.FailedEvents),
				"active_tasks":     m.metrics.ActiveTasks,
				"uptime":           time.Since(m.startTime).String(),
			},
		},
		ContextData: map[string]interface{}{
			"instance_status": m.instance.Status,
			"start_time":      m.startTime,
		},
		LastUpdateTime: time.Now(),
	}
}

// RestoreFromBreakpoint 从断点数据恢复状态
func (m *realtimeInstanceManagerImpl) RestoreFromBreakpoint(breakpoint interface{}) error {
	bp, ok := breakpoint.(*workflow.BreakpointData)
	if !ok {
		return fmt.Errorf("断点数据类型错误")
	}

	log.Printf("RealtimeInstance %s: 从断点恢复状态", m.instance.ID)

	// 恢复任务状态
	if taskStates, ok := bp.DAGSnapshot["task_states"].(map[string]interface{}); ok {
		for taskID, stateData := range taskStates {
			if stateMap, ok := stateData.(map[string]interface{}); ok {
				if ctValue, exists := m.continuousTasks.Load(taskID); exists {
					ct := ctValue.(*ContinuousTask)

					// 恢复任务状态
					if state, ok := stateMap["state"].(string); ok {
						ct.SetState(ContinuousTaskState(state))
					}

					log.Printf("恢复任务 %s 状态: %v", taskID, stateMap)
				}
			}
		}
	}

	// 恢复实例状态
	if instanceStatus, ok := bp.ContextData["instance_status"].(string); ok {
		m.mu.Lock()
		m.instance.Status = instanceStatus
		m.mu.Unlock()
	}

	log.Printf("RealtimeInstance %s: 断点恢复完成", m.instance.ID)

	return nil
}

// controlSignalHandler 控制信号处理器
func (m *realtimeInstanceManagerImpl) controlSignalHandler() {
	for {
		select {
		case <-m.ctx.Done():
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
func (m *realtimeInstanceManagerImpl) handlePause() {
	log.Printf("RealtimeInstance %s: 收到暂停信号", m.instance.ID)

	m.mu.Lock()
	m.instance.Status = "Paused"
	m.mu.Unlock()

	// 暂停所有持续任务
	m.continuousTasks.Range(func(key, value interface{}) bool {
		ct := value.(*ContinuousTask)
		if ct.GetState() == StateRunning {
			ct.Pause()
		}
		return true
	})

	// 创建断点
	breakpoint := m.CreateBreakpoint()
	m.instance.Breakpoint = breakpoint.(*workflow.BreakpointData)

	// 持久化状态
	ctx := context.Background()
	if m.stateStore != nil {
		m.stateStore.UpdateStatus(ctx, m.instance.ID, "Paused")
	}

	// 发送状态更新
	select {
	case m.statusUpdateChan <- "Paused":
	default:
	}
}

// handleResume 处理恢复信号
func (m *realtimeInstanceManagerImpl) handleResume() {
	log.Printf("RealtimeInstance %s: 收到恢复信号", m.instance.ID)

	m.mu.Lock()
	m.instance.Status = "Running"
	m.mu.Unlock()

	// 恢复所有暂停的任务
	m.continuousTasks.Range(func(key, value interface{}) bool {
		ct := value.(*ContinuousTask)
		if ct.GetState() == StatePaused {
			ct.Resume()
		}
		return true
	})

	// 持久化状态
	ctx := context.Background()
	if m.stateStore != nil {
		m.stateStore.UpdateStatus(ctx, m.instance.ID, "Running")
	}

	// 发送状态更新
	select {
	case m.statusUpdateChan <- "Running":
	default:
	}
}

// handleTerminate 处理终止信号
func (m *realtimeInstanceManagerImpl) handleTerminate() {
	log.Printf("RealtimeInstance %s: 收到终止信号", m.instance.ID)
	m.Shutdown()
}

// metricsCollector 指标收集器
func (m *realtimeInstanceManagerImpl) metricsCollector() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return

		case <-ticker.C:
			// 更新指标
			m.mu.Lock()
			m.metrics.Uptime = time.Since(m.startTime)
			m.metrics.BufferUsage = m.dataBuffer.Usage()

			// 统计活跃任务数
			activeCount := 0
			m.continuousTasks.Range(func(key, value interface{}) bool {
				ct := value.(*ContinuousTask)
				if ct.GetState() == StateRunning {
					activeCount++
				}
				return true
			})
			m.metrics.ActiveTasks = activeCount
			m.mu.Unlock()
		}
	}
}

// handleDataArrived 处理数据到达事件
func (m *realtimeInstanceManagerImpl) handleDataArrived(msg *message.Message) error {
	select {
	case <-m.ctx.Done():
		return nil
	default:
	}
	var event RealtimeEvent
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		return err
	}

	// 更新指标
	atomic.AddInt64(&m.metrics.ProcessedEvents, 1)

	// 将数据推入缓冲区（带背压控制）
	if payload, ok := event.Payload.(*DataArrivedPayload); ok {
		if !m.dataBuffer.Push(payload.Data) {
			atomic.AddInt64(&m.metrics.FailedEvents, 1)
		}
	} else if payloadMap, ok := event.Payload.(map[string]interface{}); ok {
		if !m.dataBuffer.Push(payloadMap) {
			atomic.AddInt64(&m.metrics.FailedEvents, 1)
		}
	}

	return nil
}

// handleConnectionLost 处理连接断开事件
func (m *realtimeInstanceManagerImpl) handleConnectionLost(msg *message.Message) error {
	select {
	case <-m.ctx.Done():
		return nil
	default:
	}
	var event RealtimeEvent
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		return err
	}

	// 查找对应的持续任务并触发重连
	if ctValue, exists := m.continuousTasks.Load(event.TaskID); exists {
		ct := ctValue.(*ContinuousTask)
		if ct.Config.ReconnectEnabled {
			go m.handleReconnect(ct)
		}
	}

	return nil
}

// handleError 处理错误事件
func (m *realtimeInstanceManagerImpl) handleError(msg *message.Message) error {
	select {
	case <-m.ctx.Done():
		return nil
	default:
	}
	var event RealtimeEvent
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		return err
	}

	// 更新错误计数
	atomic.AddInt64(&m.metrics.FailedEvents, 1)

	// 记录错误日志
	if payload, ok := event.Payload.(*ErrorPayload); ok {
		log.Printf("任务 %s 发生错误: %s (可恢复: %v)",
			event.TaskID, payload.Message, payload.Recoverable)
	} else if payloadMap, ok := event.Payload.(map[string]interface{}); ok {
		log.Printf("任务 %s 发生错误: %v", event.TaskID, payloadMap)
	}

	return nil
}

// handleBackpressure 处理背压事件
func (m *realtimeInstanceManagerImpl) handleBackpressure(msg *message.Message) error {
	select {
	case <-m.ctx.Done():
		return nil
	default:
	}
	var event RealtimeEvent
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		return err
	}

	if payload, ok := event.Payload.(*BackpressurePayload); ok {
		log.Printf("背压触发: 缓冲区使用率 %.2f%%, 队列长度 %d, 动作: %s",
			payload.BufferUsage*100, payload.QueueLength, payload.Action)
	} else if payloadMap, ok := event.Payload.(map[string]interface{}); ok {
		log.Printf("背压触发: %v", payloadMap)
	}

	return nil
}

// PushData 推送数据到缓冲区（供外部调用）
func (m *realtimeInstanceManagerImpl) PushData(data interface{}) bool {
	return m.dataBuffer.Push(data)
}

// GetDataBuffer 获取数据缓冲区（供测试使用）
func (m *realtimeInstanceManagerImpl) GetDataBuffer() *DataBuffer {
	return m.dataBuffer
}

// ShutdownOnTerminateOnly 实现 ShutdownOnTerminateOnly 接口，Engine.Stop 将只发 Terminate 并轮询状态，不调用 Shutdown()
func (m *realtimeInstanceManagerImpl) ShutdownOnTerminateOnly() bool {
	return true
}
