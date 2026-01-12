# RealtimeInstanceManager 设计文档 - 事件驱动实时采集

## 文档信息

| 项目 | 内容 |
|------|------|
| 文档版本 | V1.1 |
| 设计目标 | 支持长时间运行的实时数据采集任务 |
| 核心特性 | 事件驱动/持续运行/数据流处理/背压控制/优雅重连 |
| 设计原则 | 接口继承/类型统一/自动选择/无混合模式 |
| 创建时间 | 2026-01-07 |
| 更新时间 | 2026-01-07 |
| 状态 | 设计阶段 |

---

## 一、背景与问题分析

### 1.1 当前 InstanceManager 的局限性

现有的 `WorkflowInstanceManagerV2` 是为**批处理任务**设计的：

| 限制项 | 说明 | 影响 |
|--------|------|------|
| **超时机制** | Task 默认 30 秒超时，最大可配置但有限 | 长时间运行任务会被终止 |
| **执行模型** | 任务执行完成后触发后续任务 | 不适合持续运行的任务 |
| **生命周期假设** | Instance 在所有任务完成后标记为 Success | 实时任务无明确"完成"状态 |
| **事件模型** | 基于任务状态变化的事件 | 不支持数据流事件 |

### 1.2 实时数据采集场景需求

| 需求 | 说明 |
|------|------|
| **持续运行** | 任务需要 24/7 运行，无超时限制 |
| **数据流处理** | 处理连续的数据流，而非离散的任务结果 |
| **事件驱动** | 根据外部事件（数据到达、连接断开等）触发处理逻辑 |
| **背压控制** | 当下游处理不及时，需要控制上游数据速率 |
| **优雅重连** | 连接断开后自动重连，数据不丢失 |
| **资源管理** | 长时间运行需要考虑内存泄漏、连接池等 |

### 1.3 典型使用场景

1. **实时行情采集**：从交易所 WebSocket 持续接收行情数据
2. **日志流处理**：持续消费 Kafka 日志消息
3. **传感器数据**：IoT 设备持续上报数据
4. **实时监控**：持续监控系统指标

---

## 二、设计目标

1. **持续运行**：支持无超时的长时间运行任务
2. **事件驱动**：基于事件总线的松耦合架构
3. **流式处理**：支持数据流的实时处理
4. **背压控制**：防止数据积压导致内存溢出
5. **容错恢复**：连接断开自动重连，支持断点续传
6. **资源安全**：防止内存泄漏，支持优雅关闭
7. **接口兼容**：实现 `WorkflowInstanceManager` 接口，与现有 Engine 集成

---

## 三、技术选型

### 3.1 事件驱动库选择

经过调研，选择 **Watermill** 作为事件驱动核心库：

| 库 | 优点 | 缺点 | 适用场景 |
|----|------|------|----------|
| **Watermill** ✅ | 功能强大、多后端支持、活跃维护 | 学习曲线稍陡 | 生产级应用 |
| asaskevich/EventBus | 轻量简单 | 功能有限、纯内存 | 简单场景 |
| go-micro/events | 微服务集成好 | 依赖 go-micro 生态 | 微服务架构 |

**选择 Watermill 的理由**：

1. **多后端支持**：
   - 开发阶段：使用内存 Pub/Sub（`GoChannel`）
   - 生产阶段：无缝切换到 Kafka/RabbitMQ/Redis

2. **特性丰富**：
   - 消息路由（Router）
   - 中间件支持（Middleware）
   - 消息重试（Retry）
   - 消息追踪（Tracing）

3. **生产就绪**：
   - 活跃维护（ThreeDotsLabs 出品）
   - 丰富的文档和示例
   - 广泛的生产使用

### 3.2 依赖说明

```go
// go.mod 新增依赖
require (
    github.com/ThreeDotsLabs/watermill v1.3.5
    github.com/ThreeDotsLabs/watermill-kafka/v2 v2.5.0  // 可选，Kafka 支持
    github.com/ThreeDotsLabs/watermill-redisstream v1.2.0  // 可选，Redis 支持
)
```

---

## 四、整体架构设计

### 4.1 架构图

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          RealtimeInstanceManager                             │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                               │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                         Event Bus (Watermill)                          │  │
│  │  ┌─────────────────────────────────────────────────────────────────┐  │  │
│  │  │                    Message Router                                │  │  │
│  │  │  - Topic: task.data.arrived                                     │  │  │
│  │  │  - Topic: task.status.changed                                   │  │  │
│  │  │  - Topic: task.error.occurred                                   │  │  │
│  │  │  - Topic: connection.state.changed                              │  │  │
│  │  └─────────────────────────────────────────────────────────────────┘  │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                     │                                         │
│         ┌───────────────────────────┼───────────────────────────┐            │
│         ▼                           ▼                           ▼            │
│  ┌──────────────┐          ┌──────────────┐          ┌──────────────┐       │
│  │ DataHandler  │          │ StatusHandler│          │ ErrorHandler │       │
│  │ (数据处理器) │          │ (状态处理器) │          │ (错误处理器) │       │
│  └──────┬───────┘          └──────────────┘          └──────────────┘       │
│         │                                                                     │
│         ▼                                                                     │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                   ContinuousTaskManager (持续任务管理器)               │  │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  │  │
│  │  │ Task A      │  │ Task B      │  │ Task C      │  │ Task D      │  │  │
│  │  │ (Running)   │  │ (Running)   │  │ (Paused)    │  │ (Reconnecting)│ │  │
│  │  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────┘  │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                     │                                         │
│         ┌───────────────────────────┴───────────────────────────┐            │
│         ▼                                                       ▼            │
│  ┌──────────────┐                                      ┌──────────────┐      │
│  │ DataBuffer   │                                      │ StateStore   │      │
│  │ (背压缓冲)   │                                      │ (状态存储)   │      │
│  └──────────────┘                                      └──────────────┘      │
│                                                                               │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 4.2 核心组件职责

| 组件 | 职责 | 说明 |
|------|------|------|
| **Event Bus** | 事件分发 | 基于 Watermill，支持发布/订阅模式 |
| **Message Router** | 消息路由 | 根据 Topic 将消息路由到处理器 |
| **ContinuousTaskManager** | 任务管理 | 管理持续运行任务的生命周期 |
| **DataHandler** | 数据处理 | 处理数据到达事件，执行业务逻辑 |
| **StatusHandler** | 状态处理 | 处理任务状态变化事件 |
| **ErrorHandler** | 错误处理 | 处理错误事件，触发重连/告警 |
| **DataBuffer** | 背压控制 | 缓冲数据，防止下游处理不及 |
| **StateStore** | 状态存储 | 持久化任务状态，支持断点恢复 |

---

## 五、核心数据结构设计

### 5.1 事件类型定义

```go
package realtime

import (
    "time"
    "github.com/ThreeDotsLabs/watermill/message"
)

// EventType 事件类型
type EventType string

const (
    // 数据事件
    EventDataArrived    EventType = "data.arrived"     // 数据到达
    EventDataProcessed  EventType = "data.processed"   // 数据处理完成
    EventDataBatched    EventType = "data.batched"     // 数据批量到达
    
    // 任务状态事件
    EventTaskStarted    EventType = "task.started"     // 任务启动
    EventTaskPaused     EventType = "task.paused"      // 任务暂停
    EventTaskResumed    EventType = "task.resumed"     // 任务恢复
    EventTaskStopped    EventType = "task.stopped"     // 任务停止
    
    // 连接事件
    EventConnected      EventType = "connection.established"  // 连接建立
    EventDisconnected   EventType = "connection.lost"         // 连接断开
    EventReconnecting   EventType = "connection.reconnecting" // 正在重连
    EventReconnected    EventType = "connection.restored"     // 重连成功
    
    // 错误事件
    EventError          EventType = "error.occurred"   // 错误发生
    EventErrorRecovered EventType = "error.recovered"  // 错误恢复
    
    // 背压事件
    EventBackpressure   EventType = "backpressure.triggered" // 背压触发
    EventBackpressureRelieved EventType = "backpressure.relieved" // 背压解除
)

// RealtimeEvent 实时事件基础结构
type RealtimeEvent struct {
    ID           string                 `json:"id"`            // 事件ID（UUID）
    Type         EventType              `json:"type"`          // 事件类型
    TaskID       string                 `json:"task_id"`       // 关联任务ID
    InstanceID   string                 `json:"instance_id"`   // 关联实例ID
    Timestamp    time.Time              `json:"timestamp"`     // 事件时间
    Payload      interface{}            `json:"payload"`       // 事件负载
    Metadata     map[string]string      `json:"metadata"`      // 元数据
    CorrelationID string                `json:"correlation_id"` // 关联ID（用于追踪）
}

// DataArrivedPayload 数据到达事件负载
type DataArrivedPayload struct {
    Data        interface{} `json:"data"`         // 原始数据
    Source      string      `json:"source"`       // 数据来源
    Size        int64       `json:"size"`         // 数据大小（字节）
    Sequence    int64       `json:"sequence"`     // 序列号（用于顺序保证）
    BatchID     string      `json:"batch_id"`     // 批次ID（可选）
}

// ConnectionPayload 连接事件负载
type ConnectionPayload struct {
    Endpoint    string      `json:"endpoint"`     // 连接端点
    Protocol    string      `json:"protocol"`     // 协议类型
    RetryCount  int         `json:"retry_count"`  // 重试次数
    Error       string      `json:"error"`        // 错误信息（断开时）
}

// ErrorPayload 错误事件负载
type ErrorPayload struct {
    ErrorCode   string      `json:"error_code"`   // 错误码
    Message     string      `json:"message"`      // 错误消息
    Stack       string      `json:"stack"`        // 堆栈信息
    Recoverable bool        `json:"recoverable"`  // 是否可恢复
}

// BackpressurePayload 背压事件负载
type BackpressurePayload struct {
    BufferUsage   float64   `json:"buffer_usage"`   // 缓冲区使用率
    QueueLength   int       `json:"queue_length"`   // 队列长度
    Threshold     float64   `json:"threshold"`      // 触发阈值
    Action        string    `json:"action"`         // 采取的动作
}
```

### 5.2 持续任务类型定义

```go
// ContinuousTaskType 持续任务类型
type ContinuousTaskType string

const (
    TaskTypeDataCollector  ContinuousTaskType = "data_collector"   // 数据采集器
    TaskTypeStreamProcessor ContinuousTaskType = "stream_processor" // 流处理器
    TaskTypeEventListener   ContinuousTaskType = "event_listener"   // 事件监听器
    TaskTypeScheduledPoller ContinuousTaskType = "scheduled_poller" // 定时轮询器
)

// ContinuousTaskState 持续任务状态
type ContinuousTaskState string

const (
    StateInitializing   ContinuousTaskState = "initializing"   // 初始化中
    StateRunning        ContinuousTaskState = "running"        // 运行中
    StatePaused         ContinuousTaskState = "paused"         // 已暂停
    StateReconnecting   ContinuousTaskState = "reconnecting"   // 重连中
    StateStopping       ContinuousTaskState = "stopping"       // 停止中
    StateStopped        ContinuousTaskState = "stopped"        // 已停止
    StateError          ContinuousTaskState = "error"          // 错误状态
)

// ContinuousTaskConfig 持续任务配置
type ContinuousTaskConfig struct {
    // 基础配置
    ID               string                 `json:"id"`
    Name             string                 `json:"name"`
    Type             ContinuousTaskType     `json:"type"`
    
    // 连接配置
    Endpoint         string                 `json:"endpoint"`          // 连接端点
    Protocol         string                 `json:"protocol"`          // 协议（ws/wss/tcp/http）
    
    // 重连配置
    ReconnectEnabled bool                   `json:"reconnect_enabled"` // 是否自动重连
    ReconnectBackoff ReconnectBackoffConfig `json:"reconnect_backoff"` // 重连退避配置
    MaxReconnectAttempts int               `json:"max_reconnect_attempts"` // 最大重连次数（0=无限）
    
    // 缓冲配置
    BufferSize       int                    `json:"buffer_size"`       // 缓冲区大小
    BatchSize        int                    `json:"batch_size"`        // 批处理大小
    FlushInterval    time.Duration          `json:"flush_interval"`    // 刷新间隔
    
    // 背压配置
    BackpressureThreshold float64           `json:"backpressure_threshold"` // 背压阈值（0.0-1.0）
    BackpressureAction    string            `json:"backpressure_action"`    // 背压动作（drop/block/throttle）
    
    // 事件订阅
    SubscribedEvents []EventType            `json:"subscribed_events"` // 订阅的事件类型
    
    // 处理函数
    DataHandler      string                 `json:"data_handler"`      // 数据处理函数名
    ErrorHandler     string                 `json:"error_handler"`     // 错误处理函数名
    
    // 自定义参数
    Params           map[string]interface{} `json:"params"`
}

// ReconnectBackoffConfig 重连退避配置
type ReconnectBackoffConfig struct {
    InitialInterval time.Duration `json:"initial_interval"` // 初始间隔（默认1s）
    MaxInterval     time.Duration `json:"max_interval"`     // 最大间隔（默认30s）
    Multiplier      float64       `json:"multiplier"`       // 倍增因子（默认2.0）
    Jitter          float64       `json:"jitter"`           // 抖动因子（默认0.1）
}

// ContinuousTask 持续运行任务
type ContinuousTask struct {
    Config       ContinuousTaskConfig  `json:"config"`
    State        ContinuousTaskState   `json:"state"`
    
    // 运行时状态
    StartTime    time.Time             `json:"start_time"`
    LastDataTime time.Time             `json:"last_data_time"`    // 最后数据时间
    DataCount    int64                 `json:"data_count"`        // 处理数据计数
    ErrorCount   int64                 `json:"error_count"`       // 错误计数
    
    // 连接状态
    Connected    bool                  `json:"connected"`
    ReconnectCount int                 `json:"reconnect_count"`
    
    // 内部组件（不序列化）
    ctx          context.Context       `json:"-"`
    cancel       context.CancelFunc    `json:"-"`
    mu           sync.RWMutex          `json:"-"`
}
```

### 5.3 扩展现有 Task（Workflow 使用现有类型）

**重要设计决策**：
- **不创建 RealtimeWorkflow 类型**：直接使用 `workflow.Workflow`，通过 `ExecutionMode` 字段区分
- **Workflow 执行模式**：在 `workflow.Workflow` 中添加 `ExecutionMode` 字段（见 Engine 兼容设计文档）
- **Task 执行模式**：为实时任务添加扩展字段

```go
// TaskExecutionMode Task 执行模式（用于实时任务）
type TaskExecutionMode string

const (
    ExecutionModeOneShot    TaskExecutionMode = "oneshot"    // 一次性执行（默认，批处理任务）
    ExecutionModeContinuous TaskExecutionMode = "continuous" // 持续运行（实时任务）
    ExecutionModeEventDriven TaskExecutionMode = "event"     // 事件驱动（实时任务）
)

// RealtimeTask 扩展的实时任务（组合现有 Task）
// 注意：仅在 Workflow.ExecutionMode == "streaming" 时使用
type RealtimeTask struct {
    // 嵌入现有 Task
    *task.Task
    
    // 实时扩展字段
    ExecutionMode    TaskExecutionMode       `json:"execution_mode"`      // 任务执行模式
    ContinuousConfig *ContinuousTaskConfig   `json:"continuous_config,omitempty"` // 持续任务配置
    
    // 事件订阅配置
    EventSubscriptions []EventSubscription   `json:"event_subscriptions,omitempty"` // 事件订阅列表
}

// EventSubscription 事件订阅配置
type EventSubscription struct {
    EventType   EventType `json:"event_type"`    // 事件类型
    HandlerName string    `json:"handler_name"`  // 处理器名称
    Filter      string    `json:"filter"`        // 过滤条件（可选，CEL 表达式）
    Priority    int       `json:"priority"`      // 优先级
}
```

**设计说明**：
1. **Workflow 类型统一**：所有 Workflow 都使用 `workflow.Workflow` 类型
2. **执行模式区分**：通过 `Workflow.ExecutionMode` 字段区分批处理和流处理
3. **Task 扩展**：实时任务使用 `RealtimeTask` 包装，包含持续运行配置
4. **无混合模式**：不支持混合模式，Workflow 要么是批处理，要么是流处理

---

## 六、RealtimeInstanceManager 实现

### 6.1 接口定义

**核心设计**：`RealtimeInstanceManager` 接口**继承**自 `types.WorkflowInstanceManager` 接口，并扩展实时特定功能。

```go
package realtime

import (
    "context"
    "time"
    
    "github.com/LENAX/task-engine/pkg/core/types"
)

// RealtimeInstanceManager 实时实例管理器接口
// 
// 设计原则：
// 1. 继承 types.WorkflowInstanceManager 接口，实现所有标准方法
// 2. 扩展实时特定功能（事件发布/订阅、持续任务管理、指标查询等）
// 3. Engine 通过 Workflow.ExecutionMode 字段选择使用此接口或 WorkflowInstanceManagerV2
//
// 使用场景：
// - 当 Workflow.ExecutionMode == "streaming" 时，Engine 创建 RealtimeInstanceManager
// - 当 Workflow.ExecutionMode == "batch" 或为空时，Engine 创建 WorkflowInstanceManagerV2
type RealtimeInstanceManager interface {
    // ============================================
    // 继承 types.WorkflowInstanceManager 接口
    // ============================================
    // 所有标准接口方法必须实现（见接口兼容性分析文档）
    types.WorkflowInstanceManager
    
    // ============================================
    // 实时扩展接口
    // ============================================
    
    // PublishEvent 发布事件到事件总线
    // 用于任务间通信、状态通知等
    PublishEvent(ctx context.Context, event *RealtimeEvent) error
    
    // SubscribeEvent 订阅事件
    // 返回订阅ID，可用于取消订阅
    SubscribeEvent(eventType EventType, handler EventHandler) (SubscriptionID, error)
    
    // UnsubscribeEvent 取消订阅
    UnsubscribeEvent(subscriptionID SubscriptionID) error
    
    // GetContinuousTask 获取持续任务
    // 返回指定任务的运行时状态
    GetContinuousTask(taskID string) (*ContinuousTask, error)
    
    // PauseContinuousTask 暂停持续任务
    // 暂停指定任务的数据采集，但保持连接
    PauseContinuousTask(taskID string) error
    
    // ResumeContinuousTask 恢复持续任务
    // 恢复暂停的任务
    ResumeContinuousTask(taskID string) error
    
    // GetMetrics 获取运行指标
    // 返回实时实例的运行统计信息
    GetMetrics() *RealtimeMetrics
}

// EventHandler 事件处理器
type EventHandler func(ctx context.Context, event *RealtimeEvent) error

// SubscriptionID 订阅ID
type SubscriptionID string

// RealtimeMetrics 运行指标
type RealtimeMetrics struct {
    TotalEvents        int64         `json:"total_events"`
    ProcessedEvents    int64         `json:"processed_events"`
    FailedEvents       int64         `json:"failed_events"`
    ActiveTasks        int           `json:"active_tasks"`
    BufferUsage        float64       `json:"buffer_usage"`
    AverageLatency     time.Duration `json:"average_latency"`
    Uptime             time.Duration `json:"uptime"`
}
```

### 6.2 核心实现

```go
package realtime

import (
    "context"
    "fmt"
    "log"
    "sync"
    "sync/atomic"
    "time"
    
    "github.com/ThreeDotsLabs/watermill"
    "github.com/ThreeDotsLabs/watermill/message"
    "github.com/ThreeDotsLabs/watermill/message/router"
    "github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
    
    "github.com/LENAX/task-engine/pkg/core/task"
    "github.com/LENAX/task-engine/pkg/core/workflow"
    "github.com/LENAX/task-engine/pkg/storage"
)

// realtimeInstanceManagerImpl RealtimeInstanceManager 实现
// 注意：使用 workflow.Workflow 而不是 RealtimeWorkflow
type realtimeInstanceManagerImpl struct {
    // 基础信息
    instance         *workflow.WorkflowInstance
    workflow         *workflow.Workflow  // 使用标准 Workflow 类型，通过 ExecutionMode 字段区分
    
    // 实时任务映射（从 Workflow.Tasks 中筛选出实时任务）
    realtimeTasks    sync.Map            // taskID -> *RealtimeTask
    
    // Watermill 组件
    pubsub           *gochannel.GoChannel  // Pub/Sub（可替换为 Kafka/Redis）
    router           *message.Router       // 消息路由器
    logger           watermill.LoggerAdapter
    
    // 任务管理
    continuousTasks  sync.Map              // taskID -> *ContinuousTask
    taskWg           sync.WaitGroup        // 任务等待组
    
    // 事件订阅管理
    subscriptions    sync.Map              // subscriptionID -> *subscription
    subscriptionID   int64                 // atomic，订阅ID生成器
    
    // 数据缓冲（背压控制）
    dataBuffer       *DataBuffer
    
    // 状态管理
    state            atomic.Value          // RealtimeInstanceState
    
    // 存储层
    stateStore       storage.WorkflowInstanceRepository
    
    // 控制
    ctx              context.Context
    cancel           context.CancelFunc
    wg               sync.WaitGroup
    
    // 通道
    controlSignalChan chan workflow.ControlSignal
    statusUpdateChan  chan string
    
    // 指标
    metrics          *RealtimeMetrics
    startTime        time.Time
    mu               sync.RWMutex
}

// subscription 内部订阅结构
type subscription struct {
    id        SubscriptionID
    eventType EventType
    handler   EventHandler
}

// NewRealtimeInstanceManager 创建 RealtimeInstanceManager
// 
// 前置条件：
// - wf.ExecutionMode 必须为 "streaming"
// - wf 中应包含实时任务（ExecutionMode 为 "continuous" 或 "event" 的 Task）
//
// 注意：Engine 会在 SubmitWorkflow 时根据 Workflow.ExecutionMode 字段
// 自动选择创建 RealtimeInstanceManager 或 WorkflowInstanceManagerV2
func NewRealtimeInstanceManager(
    instance *workflow.WorkflowInstance,
    wf *workflow.Workflow,  // 使用标准 Workflow 类型
    stateStore storage.WorkflowInstanceRepository,
    options ...Option,
) (*realtimeInstanceManagerImpl, error) {
    
    // 校验执行模式
    if wf.GetExecutionMode() != "streaming" {
        return nil, fmt.Errorf("Workflow 执行模式必须为 'streaming'，当前: %s", wf.GetExecutionMode())
    }
    
    // 应用选项
    opts := defaultOptions()
    for _, opt := range options {
        opt(opts)
    }
    
    ctx, cancel := context.WithCancel(context.Background())
    
    // 创建 Watermill logger
    logger := watermill.NewStdLogger(opts.debug, opts.trace)
    
    // 创建 Pub/Sub（默认使用内存，可通过选项替换）
    pubsub := gochannel.NewGoChannel(
        gochannel.Config{
            Persistent:            false,
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
        workflow:          wf,
        pubsub:            pubsub,
        router:            msgRouter,
        logger:            logger,
        dataBuffer:        NewDataBuffer(opts.bufferSize, opts.backpressureThreshold),
        stateStore:        stateStore,
        ctx:               ctx,
        cancel:            cancel,
        controlSignalChan: make(chan workflow.ControlSignal, 10),
        statusUpdateChan:  make(chan string, 10),
        metrics:           &RealtimeMetrics{},
        startTime:         time.Now(),
    }
    
    // 初始化状态
    manager.state.Store(StateInitializing)
    
    // 从 Workflow.Tasks 中识别并存储实时任务
    allTasks := wf.GetTasks()
    for taskID, task := range allTasks {
        // 检查 Task 是否为实时任务
        if rtTask := extractRealtimeTask(task); rtTask != nil {
            manager.realtimeTasks.Store(taskID, rtTask)
            log.Printf("识别实时任务: TaskID=%s, ExecutionMode=%s", taskID, rtTask.ExecutionMode)
        }
    }
    
    // 注册内部事件处理器
    if err := manager.registerInternalHandlers(); err != nil {
        cancel()
        return nil, fmt.Errorf("注册内部处理器失败: %w", err)
    }
    
    return manager, nil
}

// extractRealtimeTask 从 Task 中提取实时任务信息
// 如果 Task 是 RealtimeTask 类型，直接返回
// 否则尝试从 Task.Params 中提取实时任务配置
func extractRealtimeTask(task types.Task) *RealtimeTask {
    // 方式1：检查 Task 类型
    if rtTask, ok := task.(*RealtimeTask); ok {
        return rtTask
    }
    
    // 方式2：从 Task.Params 中提取配置
    params := task.GetParams()
    if params == nil {
        return nil
    }
    
    // 检查是否有 execution_mode 参数
    if mode, ok := params["execution_mode"]; ok {
        modeStr, ok := mode.(string)
        if !ok {
            return nil
        }
        
        // 如果是持续运行或事件驱动模式，创建 RealtimeTask
        if modeStr == "continuous" || modeStr == "event" {
            rtTask := &RealtimeTask{
                Task:          task.(*task.Task), // 类型断言
                ExecutionMode: TaskExecutionMode(modeStr),
            }
            
            // 尝试提取持续任务配置
            if configData, ok := params["continuous_config"]; ok {
                // 从配置数据中构建 ContinuousTaskConfig
                // 这里需要根据实际的数据格式进行解析
                // rtTask.ContinuousConfig = parseContinuousConfig(configData)
            }
            
            return rtTask
        }
    }
    
    return nil
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
    if err := m.stateStore.UpdateStatus(ctx, m.instance.ID, "Running"); err != nil {
        log.Printf("更新状态失败: %v", err)
    }
    
    // 启动消息路由器
    m.wg.Add(1)
    go func() {
        defer m.wg.Done()
        if err := m.router.Run(m.ctx); err != nil {
            log.Printf("消息路由器退出: %v", err)
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
    // 从 Workflow.Tasks 中筛选并启动实时任务
    allTasks := m.workflow.GetTasks()
    for taskID, task := range allTasks {
        // 尝试转换为 RealtimeTask
        if rtTask, ok := m.getRealtimeTask(taskID); ok {
            if rtTask.ExecutionMode == ExecutionModeContinuous && rtTask.ContinuousConfig != nil {
                m.taskWg.Add(1)
                go func(t *RealtimeTask) {
                    defer m.taskWg.Done()
                    m.runContinuousTask(t)
                }(rtTask)
            }
        }
    }
}

// getRealtimeTask 获取实时任务（从 realtimeTasks 映射中获取）
func (m *realtimeInstanceManagerImpl) getRealtimeTask(taskID string) (*RealtimeTask, bool) {
    if value, exists := m.realtimeTasks.Load(taskID); exists {
        return value.(*RealtimeTask), true
    }
    return nil, false
}

// runContinuousTask 运行持续任务
func (m *realtimeInstanceManagerImpl) runContinuousTask(rtTask *RealtimeTask) {
    config := rtTask.ContinuousConfig
    taskCtx, taskCancel := context.WithCancel(m.ctx)
    
    // 创建持续任务状态
    ct := &ContinuousTask{
        Config:    *config,
        State:     StateRunning,
        StartTime: time.Now(),
        ctx:       taskCtx,
        cancel:    taskCancel,
    }
    
    // 存储任务状态
    m.continuousTasks.Store(config.ID, ct)
    
    // 发布启动事件
    m.PublishEvent(taskCtx, &RealtimeEvent{
        Type:       EventTaskStarted,
        TaskID:     config.ID,
        InstanceID: m.instance.ID,
        Timestamp:  time.Now(),
    })
    
    // 任务主循环
    for {
        select {
        case <-taskCtx.Done():
            log.Printf("Task %s: 上下文取消，退出", config.ID)
            ct.State = StateStopped
            return
            
        default:
            // 执行任务特定逻辑
            if err := m.executeTaskLogic(taskCtx, ct); err != nil {
                log.Printf("Task %s: 执行错误: %v", config.ID, err)
                atomic.AddInt64(&ct.ErrorCount, 1)
                
                // 发布错误事件
                m.PublishEvent(taskCtx, &RealtimeEvent{
                    Type:       EventError,
                    TaskID:     config.ID,
                    InstanceID: m.instance.ID,
                    Timestamp:  time.Now(),
                    Payload: &ErrorPayload{
                        Message:     err.Error(),
                        Recoverable: true,
                    },
                })
                
                // 检查是否需要重连
                if config.ReconnectEnabled && ct.State != StateStopping {
                    m.handleReconnect(ct)
                }
            }
        }
    }
}

// executeTaskLogic 执行任务逻辑（根据任务类型分发）
func (m *realtimeInstanceManagerImpl) executeTaskLogic(ctx context.Context, ct *ContinuousTask) error {
    switch ct.Config.Type {
    case TaskTypeDataCollector:
        return m.runDataCollector(ctx, ct)
    case TaskTypeStreamProcessor:
        return m.runStreamProcessor(ctx, ct)
    case TaskTypeEventListener:
        return m.runEventListener(ctx, ct)
    case TaskTypeScheduledPoller:
        return m.runScheduledPoller(ctx, ct)
    default:
        return fmt.Errorf("未知任务类型: %s", ct.Config.Type)
    }
}

// handleReconnect 处理重连逻辑
func (m *realtimeInstanceManagerImpl) handleReconnect(ct *ContinuousTask) {
    ct.mu.Lock()
    ct.State = StateReconnecting
    ct.Connected = false
    ct.mu.Unlock()
    
    backoff := ct.Config.ReconnectBackoff
    if backoff.InitialInterval == 0 {
        backoff.InitialInterval = time.Second
    }
    if backoff.MaxInterval == 0 {
        backoff.MaxInterval = 30 * time.Second
    }
    if backoff.Multiplier == 0 {
        backoff.Multiplier = 2.0
    }
    
    interval := backoff.InitialInterval
    
    for attempt := 1; ; attempt++ {
        // 检查是否超过最大重连次数
        if ct.Config.MaxReconnectAttempts > 0 && attempt > ct.Config.MaxReconnectAttempts {
            log.Printf("Task %s: 超过最大重连次数 %d，停止重连",
                ct.Config.ID, ct.Config.MaxReconnectAttempts)
            ct.State = StateError
            return
        }
        
        // 发布重连事件
        m.PublishEvent(ct.ctx, &RealtimeEvent{
            Type:       EventReconnecting,
            TaskID:     ct.Config.ID,
            InstanceID: m.instance.ID,
            Timestamp:  time.Now(),
            Payload: &ConnectionPayload{
                Endpoint:   ct.Config.Endpoint,
                RetryCount: attempt,
            },
        })
        
        log.Printf("Task %s: 尝试重连 (第 %d 次)，等待 %v",
            ct.Config.ID, attempt, interval)
        
        // 等待退避时间
        select {
        case <-ct.ctx.Done():
            return
        case <-time.After(interval):
        }
        
        // 尝试重连
        if err := m.reconnect(ct); err == nil {
            ct.mu.Lock()
            ct.State = StateRunning
            ct.Connected = true
            ct.ReconnectCount++
            ct.mu.Unlock()
            
            // 发布重连成功事件
            m.PublishEvent(ct.ctx, &RealtimeEvent{
                Type:       EventReconnected,
                TaskID:     ct.Config.ID,
                InstanceID: m.instance.ID,
                Timestamp:  time.Now(),
            })
            
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

// reconnect 执行重连（具体实现依赖任务类型）
func (m *realtimeInstanceManagerImpl) reconnect(ct *ContinuousTask) error {
    // 这里是重连逻辑的占位符
    // 实际实现需要根据任务类型（WebSocket/TCP/HTTP等）进行具体实现
    return nil
}

// PublishEvent 发布事件
func (m *realtimeInstanceManagerImpl) PublishEvent(ctx context.Context, event *RealtimeEvent) error {
    // 生成事件ID（如果没有）
    if event.ID == "" {
        event.ID = generateEventID()
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
    }
    
    m.subscriptions.Store(id, sub)
    
    // 动态添加处理器到路由器
    handlerName := fmt.Sprintf("dynamic_handler_%s", id)
    m.router.AddNoPublisherHandler(
        handlerName,
        string(eventType),
        m.pubsub,
        func(msg *message.Message) error {
            var event RealtimeEvent
            if err := json.Unmarshal(msg.Payload, &event); err != nil {
                return err
            }
            return handler(m.ctx, &event)
        },
    )
    
    return id, nil
}

// UnsubscribeEvent 取消订阅
func (m *realtimeInstanceManagerImpl) UnsubscribeEvent(subscriptionID SubscriptionID) error {
    m.subscriptions.Delete(subscriptionID)
    // 注意：Watermill 不直接支持动态移除 handler
    // 可以通过标记的方式跳过处理
    return nil
}

// Shutdown 优雅关闭
func (m *realtimeInstanceManagerImpl) Shutdown() {
    log.Printf("RealtimeInstance %s: 开始关闭...", m.instance.ID)
    
    m.state.Store(StateStopping)
    
    // 停止所有持续任务
    m.continuousTasks.Range(func(key, value interface{}) bool {
        ct := value.(*ContinuousTask)
        ct.cancel()
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
    case <-time.After(30 * time.Second):
        log.Printf("RealtimeInstance %s: 任务停止超时", m.instance.ID)
    }
    
    // 关闭路由器
    if err := m.router.Close(); err != nil {
        log.Printf("关闭路由器失败: %v", err)
    }
    
    // 关闭 Pub/Sub
    if err := m.pubsub.Close(); err != nil {
        log.Printf("关闭 Pub/Sub 失败: %v", err)
    }
    
    // 取消上下文
    m.cancel()
    
    // 等待其他 goroutine
    m.wg.Wait()
    
    // 更新状态
    m.state.Store(StateStopped)
    
    // 持久化最终状态
    ctx := context.Background()
    m.stateStore.UpdateStatus(ctx, m.instance.ID, "Stopped")
    
    log.Printf("RealtimeInstance %s: 已关闭", m.instance.ID)
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

// Context 获取上下文
func (m *realtimeInstanceManagerImpl) Context() context.Context {
    return m.ctx
}

// AddSubTask 动态添加子任务（兼容接口，实时任务通常不需要）
// 注意：实时任务通常在启动时就确定了所有任务，动态添加子任务不是主要使用场景
// 但为了兼容接口，提供最小实现：将子任务转换为事件驱动的任务
func (m *realtimeInstanceManagerImpl) AddSubTask(subTask types.Task, parentTaskID string) error {
    // 检查实例状态
    if m.GetStatus() != "Running" {
        return fmt.Errorf("实例未运行，无法添加子任务")
    }
    
    // 检查父任务是否存在
    if _, exists := m.continuousTasks.Load(parentTaskID); !exists {
        return fmt.Errorf("父任务 %s 不存在", parentTaskID)
    }
    
    // 将子任务转换为实时任务
    rtTask, err := m.convertToRealtimeTask(subTask)
    if err != nil {
        return fmt.Errorf("转换任务失败: %w", err)
    }
    
    // 如果子任务是持续运行模式，启动它
    if rtTask.ExecutionMode == ExecutionModeContinuous && rtTask.ContinuousConfig != nil {
        m.taskWg.Add(1)
        go func(t *RealtimeTask) {
            defer m.taskWg.Done()
            m.runContinuousTask(t)
        }(rtTask)
    }
    
    // 发布子任务添加事件
    m.PublishEvent(m.ctx, &RealtimeEvent{
        Type:       EventTaskStarted,
        TaskID:     subTask.GetID(),
        InstanceID: m.instance.ID,
        Timestamp:  time.Now(),
        Payload: map[string]interface{}{
            "parent_task_id": parentTaskID,
            "is_subtask":     true,
        },
    })
    
    log.Printf("RealtimeInstance %s: 已添加子任务 %s (父任务: %s)",
        m.instance.ID, subTask.GetID(), parentTaskID)
    
    return nil
}

// AtomicAddSubTasks 原子性地添加多个子任务（兼容接口）
func (m *realtimeInstanceManagerImpl) AtomicAddSubTasks(subTasks []types.Task, parentTaskID string) error {
    // 检查实例状态
    if m.GetStatus() != "Running" {
        return fmt.Errorf("实例未运行，无法添加子任务")
    }
    
    // 检查父任务是否存在
    if _, exists := m.continuousTasks.Load(parentTaskID); !exists {
        return fmt.Errorf("父任务 %s 不存在", parentTaskID)
    }
    
    // 批量转换和启动任务
    var errors []error
    for _, subTask := range subTasks {
        if err := m.AddSubTask(subTask, parentTaskID); err != nil {
            errors = append(errors, fmt.Errorf("添加子任务 %s 失败: %w", subTask.GetID(), err))
        }
    }
    
    // 如果有任何错误，返回第一个错误（原子性：要么全部成功，要么全部失败）
    if len(errors) > 0 {
        // 回滚已添加的任务（这里简化处理，实际应该记录已添加的任务并回滚）
        return fmt.Errorf("批量添加子任务失败: %v", errors[0])
    }
    
    log.Printf("RealtimeInstance %s: 已原子性添加 %d 个子任务 (父任务: %s)",
        m.instance.ID, len(subTasks), parentTaskID)
    
    return nil
}

// convertToRealtimeTask 将普通 Task 转换为 RealtimeTask
func (m *realtimeInstanceManagerImpl) convertToRealtimeTask(task types.Task) (*RealtimeTask, error) {
    // 检查任务是否已经是 RealtimeTask
    if rtTask, ok := task.(*RealtimeTask); ok {
        return rtTask, nil
    }
    
    // 创建默认的实时任务配置
    rtTask := &RealtimeTask{
        Task:          task.(*task.Task), // 类型断言，需要确保 task 是 *task.Task
        ExecutionMode: ExecutionModeEventDriven, // 默认事件驱动模式
    }
    
    return rtTask, nil
}

// CreateBreakpoint 创建断点数据（兼容接口）
// 实时任务的断点数据包括：当前运行的任务状态、缓冲区状态、连接状态等
func (m *realtimeInstanceManagerImpl) CreateBreakpoint() interface{} {
    m.mu.RLock()
    defer m.mu.RUnlock()
    
    // 收集所有持续任务的状态
    taskStates := make(map[string]interface{})
    m.continuousTasks.Range(func(key, value interface{}) bool {
        taskID := key.(string)
        ct := value.(*ContinuousTask)
        
        ct.mu.RLock()
        taskStates[taskID] = map[string]interface{}{
            "state":          ct.State,
            "connected":      ct.Connected,
            "data_count":     ct.DataCount,
            "error_count":    ct.ErrorCount,
            "reconnect_count": ct.ReconnectCount,
            "last_data_time": ct.LastDataTime,
        }
        ct.mu.RUnlock()
        
        return true
    })
    
    // 获取缓冲区状态
    totalIn, totalOut, dropped, usage := m.dataBuffer.Stats()
    
    // 创建断点数据
    breakpoint := &workflow.BreakpointData{
        CompletedTaskNames: []string{}, // 实时任务没有"完成"的概念
        RunningTaskNames:   []string{}, // 将在下面填充
        DAGSnapshot: map[string]interface{}{
            "task_states": taskStates,
            "buffer_stats": map[string]interface{}{
                "total_in":    totalIn,
                "total_out":   totalOut,
                "dropped":     dropped,
                "usage":       usage,
            },
            "metrics": m.GetMetrics(),
        },
        ContextData: map[string]interface{}{
            "instance_status": m.instance.Status,
            "start_time":      m.startTime,
        },
        LastUpdateTime: time.Now(),
    }
    
    // 填充运行中的任务名称
    m.continuousTasks.Range(func(key, value interface{}) bool {
        ct := value.(*ContinuousTask)
        ct.mu.RLock()
        if ct.State == StateRunning || ct.State == StateReconnecting {
            breakpoint.RunningTaskNames = append(breakpoint.RunningTaskNames, ct.Config.Name)
        }
        ct.mu.RUnlock()
        return true
    })
    
    return breakpoint
}

// RestoreFromBreakpoint 从断点数据恢复状态（兼容接口）
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
                // 查找对应的持续任务
                if ctValue, exists := m.continuousTasks.Load(taskID); exists {
                    ct := ctValue.(*ContinuousTask)
                    
                    // 恢复任务状态
                    if state, ok := stateMap["state"].(string); ok {
                        ct.mu.Lock()
                        ct.State = ContinuousTaskState(state)
                        ct.mu.Unlock()
                    }
                    
                    // 恢复统计数据
                    if dataCount, ok := stateMap["data_count"].(int64); ok {
                        atomic.StoreInt64(&ct.DataCount, dataCount)
                    }
                    if errorCount, ok := stateMap["error_count"].(int64); ok {
                        atomic.StoreInt64(&ct.ErrorCount, errorCount)
                    }
                    
                    log.Printf("恢复任务 %s 状态: %v", taskID, stateMap)
                }
            }
        }
    }
    
    // 恢复缓冲区状态（注意：缓冲区数据无法完全恢复，只能恢复统计信息）
    if bufferStats, ok := bp.DAGSnapshot["buffer_stats"].(map[string]interface{}); ok {
        log.Printf("缓冲区统计: %v", bufferStats)
        // 注意：实际缓冲区数据无法恢复，因为数据已经在内存中丢失
        // 只能记录统计信息用于监控
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
        ct.mu.Lock()
        if ct.State == StateRunning {
            ct.State = StatePaused
        }
        ct.mu.Unlock()
        return true
    })
    
    // 创建断点
    breakpoint := m.CreateBreakpoint()
    m.instance.Breakpoint = breakpoint.(*workflow.BreakpointData)
    
    // 持久化状态
    ctx := context.Background()
    m.stateStore.UpdateStatus(ctx, m.instance.ID, "Paused")
    
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
        ct.mu.Lock()
        if ct.State == StatePaused {
            ct.State = StateRunning
        }
        ct.mu.Unlock()
        return true
    })
    
    // 持久化状态
    ctx := context.Background()
    m.stateStore.UpdateStatus(ctx, m.instance.ID, "Running")
    
    // 发送状态更新
    select {
    case m.statusUpdateChan <- "Running":
    default:
    }
}

// handleTerminate 处理终止信号
func (m *realtimeInstanceManagerImpl) handleTerminate() {
    log.Printf("RealtimeInstance %s: 收到终止信号", m.instance.ID)
    
    // 调用 Shutdown 进行优雅关闭
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
            m.metrics.Uptime = time.Since(m.startTime)
            m.metrics.BufferUsage = m.dataBuffer.Usage()
            
            // 统计活跃任务数
            activeCount := 0
            m.continuousTasks.Range(func(key, value interface{}) bool {
                ct := value.(*ContinuousTask)
                ct.mu.RLock()
                if ct.State == StateRunning {
                    activeCount++
                }
                ct.mu.RUnlock()
                return true
            })
            m.metrics.ActiveTasks = activeCount
        }
    }
}

// handleDataArrived 处理数据到达事件（内部处理器）
func (m *realtimeInstanceManagerImpl) handleDataArrived(msg *message.Message) error {
    var event RealtimeEvent
    if err := json.Unmarshal(msg.Payload, &event); err != nil {
        return err
    }
    
    // 更新指标
    atomic.AddInt64(&m.metrics.ProcessedEvents, 1)
    
    // 将数据推入缓冲区（带背压控制）
    if payload, ok := event.Payload.(*DataArrivedPayload); ok {
        if !m.dataBuffer.Push(payload.Data) {
            // 缓冲区满，触发背压
            atomic.AddInt64(&m.metrics.FailedEvents, 1)
            m.PublishEvent(m.ctx, &RealtimeEvent{
                Type:       EventBackpressure,
                TaskID:     event.TaskID,
                InstanceID: event.InstanceID,
                Timestamp:  time.Now(),
                Payload: &BackpressurePayload{
                    BufferUsage: m.dataBuffer.Usage(),
                    QueueLength: len(m.dataBuffer.data),
                    Threshold:   m.dataBuffer.threshold,
                    Action:      "drop",
                },
            })
        }
    }
    
    return nil
}

// handleConnectionLost 处理连接断开事件
func (m *realtimeInstanceManagerImpl) handleConnectionLost(msg *message.Message) error {
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
    }
    
    return nil
}

// handleBackpressure 处理背压事件
func (m *realtimeInstanceManagerImpl) handleBackpressure(msg *message.Message) error {
    var event RealtimeEvent
    if err := json.Unmarshal(msg.Payload, &event); err != nil {
        return err
    }
    
    // 根据背压策略采取行动
    if payload, ok := event.Payload.(*BackpressurePayload); ok {
        log.Printf("背压触发: 缓冲区使用率 %.2f%%, 队列长度 %d, 动作: %s",
            payload.BufferUsage*100, payload.QueueLength, payload.Action)
        
        // 可以在这里实现更复杂的背压处理逻辑
        // 例如：通知上游降低发送速率、增加处理能力等
    }
    
    return nil
}
```

### 6.3 数据缓冲区（背压控制）

```go
package realtime

import (
    "sync"
    "sync/atomic"
)

// DataBuffer 数据缓冲区（用于背压控制）
type DataBuffer struct {
    data          chan interface{}
    capacity      int
    threshold     float64
    backpressure  int32  // atomic，0=正常，1=背压
    
    // 统计
    totalIn       int64  // atomic，总入队数
    totalOut      int64  // atomic，总出队数
    dropped       int64  // atomic，丢弃数
    
    mu sync.RWMutex
}

// NewDataBuffer 创建数据缓冲区
func NewDataBuffer(capacity int, threshold float64) *DataBuffer {
    if capacity <= 0 {
        capacity = 10000
    }
    if threshold <= 0 || threshold > 1 {
        threshold = 0.8
    }
    
    return &DataBuffer{
        data:      make(chan interface{}, capacity),
        capacity:  capacity,
        threshold: threshold,
    }
}

// Push 推入数据（非阻塞）
func (b *DataBuffer) Push(item interface{}) bool {
    select {
    case b.data <- item:
        atomic.AddInt64(&b.totalIn, 1)
        b.checkBackpressure()
        return true
    default:
        // 缓冲区满，丢弃数据
        atomic.AddInt64(&b.dropped, 1)
        return false
    }
}

// PushBlocking 推入数据（阻塞）
func (b *DataBuffer) PushBlocking(item interface{}) {
    b.data <- item
    atomic.AddInt64(&b.totalIn, 1)
    b.checkBackpressure()
}

// Pop 弹出数据（非阻塞）
func (b *DataBuffer) Pop() (interface{}, bool) {
    select {
    case item := <-b.data:
        atomic.AddInt64(&b.totalOut, 1)
        b.checkBackpressure()
        return item, true
    default:
        return nil, false
    }
}

// PopBlocking 弹出数据（阻塞）
func (b *DataBuffer) PopBlocking() interface{} {
    item := <-b.data
    atomic.AddInt64(&b.totalOut, 1)
    b.checkBackpressure()
    return item
}

// Usage 获取使用率
func (b *DataBuffer) Usage() float64 {
    return float64(len(b.data)) / float64(b.capacity)
}

// IsBackpressure 是否处于背压状态
func (b *DataBuffer) IsBackpressure() bool {
    return atomic.LoadInt32(&b.backpressure) == 1
}

// checkBackpressure 检查背压状态
func (b *DataBuffer) checkBackpressure() {
    usage := b.Usage()
    
    if usage >= b.threshold {
        if atomic.CompareAndSwapInt32(&b.backpressure, 0, 1) {
            // 触发背压事件
            // 这里可以发送事件到事件总线
        }
    } else if usage < b.threshold*0.5 {
        if atomic.CompareAndSwapInt32(&b.backpressure, 1, 0) {
            // 解除背压事件
        }
    }
}

// Stats 获取统计信息
func (b *DataBuffer) Stats() (totalIn, totalOut, dropped int64, usage float64) {
    return atomic.LoadInt64(&b.totalIn),
           atomic.LoadInt64(&b.totalOut),
           atomic.LoadInt64(&b.dropped),
           b.Usage()
}
```

---

## 七、扩展 Builder 支持

### 7.1 设计原则

**重要**：不创建 `RealtimeWorkflowBuilder`，直接使用 `WorkflowBuilder` + `WithStreamingMode()` 方法。

```go
// 正确方式：使用 WorkflowBuilder
wf := builder.NewWorkflowBuilder("realtime_workflow", "实时工作流").
    WithStreamingMode().  // 设置执行模式为 "streaming"
    WithTask(realtimeTask).
    Build()

// 错误方式：不要创建 RealtimeWorkflowBuilder
// rtWf := builder.NewRealtimeWorkflowBuilder(...)  // ❌ 不要这样做
```

### 7.2 RealtimeTaskBuilder

```go
package builder

import (
    "github.com/LENAX/task-engine/pkg/core/realtime"
    "github.com/LENAX/task-engine/pkg/core/task"
)

// RealtimeTaskBuilder 实时任务构建器
type RealtimeTaskBuilder struct {
    *TaskBuilder
    executionMode    realtime.TaskExecutionMode
    continuousConfig *realtime.ContinuousTaskConfig
    subscriptions    []realtime.EventSubscription
}

// NewRealtimeTaskBuilder 创建实时任务构建器
func NewRealtimeTaskBuilder(name, desc string, registry task.FunctionRegistry) *RealtimeTaskBuilder {
    return &RealtimeTaskBuilder{
        TaskBuilder:   NewTaskBuilder(name, desc, registry),
        executionMode: realtime.ExecutionModeOneShot,
        subscriptions: make([]realtime.EventSubscription, 0),
    }
}

// WithContinuousMode 设置为持续运行模式
func (b *RealtimeTaskBuilder) WithContinuousMode() *RealtimeTaskBuilder {
    b.executionMode = realtime.ExecutionModeContinuous
    return b
}

// WithEventDrivenMode 设置为事件驱动模式
func (b *RealtimeTaskBuilder) WithEventDrivenMode() *RealtimeTaskBuilder {
    b.executionMode = realtime.ExecutionModeEventDriven
    return b
}

// WithEndpoint 设置连接端点
func (b *RealtimeTaskBuilder) WithEndpoint(endpoint, protocol string) *RealtimeTaskBuilder {
    if b.continuousConfig == nil {
        b.continuousConfig = &realtime.ContinuousTaskConfig{}
    }
    b.continuousConfig.Endpoint = endpoint
    b.continuousConfig.Protocol = protocol
    return b
}

// WithReconnect 配置重连策略
func (b *RealtimeTaskBuilder) WithReconnect(enabled bool, maxAttempts int) *RealtimeTaskBuilder {
    if b.continuousConfig == nil {
        b.continuousConfig = &realtime.ContinuousTaskConfig{}
    }
    b.continuousConfig.ReconnectEnabled = enabled
    b.continuousConfig.MaxReconnectAttempts = maxAttempts
    return b
}

// WithBackpressure 配置背压策略
func (b *RealtimeTaskBuilder) WithBackpressure(threshold float64, action string) *RealtimeTaskBuilder {
    if b.continuousConfig == nil {
        b.continuousConfig = &realtime.ContinuousTaskConfig{}
    }
    b.continuousConfig.BackpressureThreshold = threshold
    b.continuousConfig.BackpressureAction = action
    return b
}

// WithBuffer 配置缓冲区
func (b *RealtimeTaskBuilder) WithBuffer(size int, batchSize int) *RealtimeTaskBuilder {
    if b.continuousConfig == nil {
        b.continuousConfig = &realtime.ContinuousTaskConfig{}
    }
    b.continuousConfig.BufferSize = size
    b.continuousConfig.BatchSize = batchSize
    return b
}

// SubscribeEvent 订阅事件
func (b *RealtimeTaskBuilder) SubscribeEvent(eventType realtime.EventType, handlerName string) *RealtimeTaskBuilder {
    b.subscriptions = append(b.subscriptions, realtime.EventSubscription{
        EventType:   eventType,
        HandlerName: handlerName,
    })
    return b
}

// Build 构建实时任务
func (b *RealtimeTaskBuilder) Build() (*realtime.RealtimeTask, error) {
    baseTask, err := b.TaskBuilder.Build()
    if err != nil {
        return nil, err
    }
    
    return &realtime.RealtimeTask{
        Task:               baseTask,
        ExecutionMode:      b.executionMode,
        ContinuousConfig:   b.continuousConfig,
        EventSubscriptions: b.subscriptions,
    }, nil
}
```

### 7.3 WorkflowBuilder 扩展（不使用 RealtimeWorkflowBuilder）

**设计决策**：不创建 `RealtimeWorkflowBuilder`，直接在 `WorkflowBuilder` 中添加 `WithStreamingMode()` 方法。

```go
// pkg/core/builder/workflow_builder.go

// WithStreamingMode 设置为流处理模式
// 当调用此方法时，Workflow.ExecutionMode 会被设置为 "streaming"
// Engine 会根据此字段自动选择 RealtimeInstanceManager
func (b *WorkflowBuilder) WithStreamingMode() *WorkflowBuilder {
    b.workflow.SetExecutionMode(workflow.ExecutionModeStreaming)
    return b
}

// WithBatchMode 设置为批处理模式（显式设置，可选）
// 默认就是批处理模式，但可以显式调用以明确意图
func (b *WorkflowBuilder) WithBatchMode() *WorkflowBuilder {
    b.workflow.SetExecutionMode(workflow.ExecutionModeBatch)
    return b
}

// WithRealtimeTask 添加实时任务（便捷方法）
// 注意：实时任务需要先通过 RealtimeTaskBuilder 构建
func (b *WorkflowBuilder) WithRealtimeTask(rtTask *realtime.RealtimeTask) *WorkflowBuilder {
    // 添加基础 Task 到 Workflow
    b.WithTask(rtTask.Task)
    
    // 注意：实时任务的扩展信息（ContinuousConfig、EventSubscriptions）
    // 需要在 RealtimeInstanceManager 初始化时从 Task 的元数据中提取
    // 或者通过其他方式传递（如 Task.Params）
    
    return b
}
```

**使用示例**：

```go
// 创建流处理 Workflow
wf, err := builder.NewWorkflowBuilder("quote_workflow", "行情采集工作流").
    WithStreamingMode().  // 设置为流处理模式
    WithRealtimeTask(quoteTask).  // 添加实时任务
    Build()

// Engine 会自动根据 ExecutionMode 选择 RealtimeInstanceManager
controller, err := engine.SubmitWorkflow(ctx, wf)
```

---

## 八、Engine 集成

### 8.1 集成方式

**核心设计**：Engine 通过 `Workflow.ExecutionMode` 字段自动选择 InstanceManager，无需单独的 `SubmitRealtimeWorkflow` 方法。

#### 8.1.1 Engine 修改（见 Engine 兼容设计文档）

```go
// Engine.SubmitWorkflow 方法（更新版）
func (e *Engine) SubmitWorkflow(ctx context.Context, wf *workflow.Workflow) (workflow.WorkflowController, error) {
    // ... 创建 instance 和保存 Workflow ...
    
    // 根据 Workflow.ExecutionMode 字段选择 InstanceManager
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
        // 批处理模式：创建 WorkflowInstanceManagerV2
        manager, managerErr = e.createBatchInstanceManager(instance, wf)
    }
    
    // ... 创建 Controller 和启动 ...
}

// createRealtimeInstanceManager 创建实时 InstanceManager
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
        wf,  // 使用标准 Workflow 类型
        e.workflowInstanceRepo,
        // 选项配置（可以从 Engine 配置中读取）
        realtime.WithBufferSize(10000),
        realtime.WithBackpressureThreshold(0.8),
    )
}
```

#### 8.1.2 实时任务识别

RealtimeInstanceManager 需要从 Workflow.Tasks 中识别实时任务：

```go
// 在 NewRealtimeInstanceManager 中初始化实时任务映射
func NewRealtimeInstanceManager(...) (*realtimeInstanceManagerImpl, error) {
    // ... 初始化代码 ...
    
    // 从 Workflow.Tasks 中识别并存储实时任务
    allTasks := wf.GetTasks()
    for taskID, task := range allTasks {
        // 检查 Task 是否为实时任务
        // 方式1：通过 Task.Params 中的元数据判断
        // 方式2：通过 Task 类型判断（如果是 RealtimeTask）
        // 方式3：通过 Task 的 JobFuncName 判断（如果函数名包含特定前缀）
        
        if isRealtimeTask(task) {
            rtTask := convertToRealtimeTask(task)
            manager.realtimeTasks.Store(taskID, rtTask)
        }
    }
    
    return manager, nil
}

// isRealtimeTask 判断 Task 是否为实时任务
func isRealtimeTask(task types.Task) bool {
    // 方式1：检查 Task.Params 中是否有实时任务标记
    if params := task.GetParams(); params != nil {
        if mode, ok := params["execution_mode"]; ok {
            if modeStr, ok := mode.(string); ok {
                return modeStr == "continuous" || modeStr == "event"
            }
        }
    }
    
    // 方式2：检查 Task 类型
    if _, ok := task.(*realtime.RealtimeTask); ok {
        return true
    }
    
    return false
}
```

### 8.2 设计要点

| 要点 | 说明 |
|------|------|
| **统一接口** | Engine 使用 `SubmitWorkflow`，不区分批处理和流处理 |
| **自动选择** | 根据 `Workflow.ExecutionMode` 字段自动选择 Manager |
| **类型统一** | 所有 Workflow 都使用 `workflow.Workflow` 类型 |
| **无混合模式** | 不支持混合模式，Workflow 要么是批处理，要么是流处理 |

---

## 九、使用示例

### 9.1 实时行情采集示例

```go
package main

import (
    "context"
    "log"
    
    "github.com/LENAX/task-engine/pkg/core/builder"
    "github.com/LENAX/task-engine/pkg/core/engine"
    "github.com/LENAX/task-engine/pkg/core/realtime"
)

func main() {
    // 1. 创建 Engine
    eng, err := engine.NewEngineBuilder("config.yaml").
        WithJobFunc("process_quote", processQuoteFunc).
        WithJobFunc("handle_connection", handleConnectionFunc).
        Build()
    if err != nil {
        log.Fatal(err)
    }
    
    // 2. 启动 Engine
    if err := eng.Start(); err != nil {
        log.Fatal(err)
    }
    defer eng.Stop()
    
    // 3. 创建实时任务
    quoteTask, err := builder.NewRealtimeTaskBuilder("quote_collector", "实时行情采集", eng.GetRegistry()).
        WithContinuousMode().
        WithEndpoint("wss://api.exchange.com/ws", "websocket").
        WithReconnect(true, 0). // 无限重连
        WithBackpressure(0.8, "throttle").
        WithBuffer(50000, 100).
        WithJobFunction("process_quote").
        SubscribeEvent(realtime.EventDataArrived, "process_quote").
        SubscribeEvent(realtime.EventDisconnected, "handle_connection").
        Build()
    if err != nil {
        log.Fatal(err)
    }
    
    // 4. 创建流处理工作流（使用 WorkflowBuilder + WithStreamingMode）
    wf, err := builder.NewWorkflowBuilder("quote_workflow", "行情采集工作流").
        WithStreamingMode().  // 设置为流处理模式，Engine 会自动选择 RealtimeInstanceManager
        WithRealtimeTask(quoteTask).  // 添加实时任务
        Build()
    if err != nil {
        log.Fatal(err)
    }
    
    // 5. 提交工作流（使用统一的 SubmitWorkflow 方法）
    // Engine 会根据 wf.ExecutionMode == "streaming" 自动创建 RealtimeInstanceManager
    controller, err := eng.SubmitWorkflow(context.Background(), wf)
    if err != nil {
        log.Fatal(err)
    }
    
    log.Printf("实时工作流已启动: %s", controller.InstanceID())
    
    // 6. 等待（实际应用中可以使用信号处理）
    select {}
}

// processQuoteFunc 处理行情数据
func processQuoteFunc(ctx *task.TaskContext) <-chan task.TaskState {
    ch := make(chan task.TaskState, 1)
    
    go func() {
        defer close(ch)
        
        // 从参数获取数据
        data, _ := ctx.Params["data"]
        
        // 处理行情数据...
        log.Printf("处理行情: %v", data)
        
        ch <- task.TaskState{
            Status: "Success",
            Data:   data,
        }
    }()
    
    return ch
}

// handleConnectionFunc 处理连接事件
func handleConnectionFunc(ctx *task.TaskContext) <-chan task.TaskState {
    ch := make(chan task.TaskState, 1)
    
    go func() {
        defer close(ch)
        
        event, _ := ctx.Params["event"]
        log.Printf("连接事件: %v", event)
        
        ch <- task.TaskState{
            Status: "Success",
        }
    }()
    
    return ch
}
```

---

## 十、实施计划

### 10.1 阶段一：基础架构（1-2周）

1. **引入 Watermill 依赖**
2. **实现事件类型定义**
3. **实现 DataBuffer**
4. **实现基础的 RealtimeInstanceManager 框架**

### 10.2 阶段二：核心功能（2-3周）

1. **实现持续任务管理**
2. **实现事件订阅/发布**
3. **实现重连机制**
4. **实现背压控制**

### 10.3 阶段三：Builder 和集成（1周）

1. **实现 RealtimeTaskBuilder**
2. **扩展 WorkflowBuilder**：添加 `WithStreamingMode()` 方法
3. **Engine 集成**：修改 `SubmitWorkflow` 方法，根据 `ExecutionMode` 选择 Manager

### 10.4 阶段四：测试和文档（1周）

1. **单元测试**
2. **集成测试**
3. **性能测试**
4. **使用文档**

---

## 十一、风险与应对

| 风险 | 影响 | 应对措施 |
|------|------|----------|
| Watermill 学习曲线 | 开发效率 | 先使用内存 Pub/Sub，逐步深入 |
| 内存泄漏 | 系统稳定性 | 严格的资源管理，定期检查 |
| 背压处理不当 | 数据丢失 | 多策略支持（drop/block/throttle） |
| 重连风暴 | 服务端压力 | 指数退避 + 抖动 |

---

## 十二、设计原则总结

### 12.1 核心设计原则

1. **接口继承**：`RealtimeInstanceManager` 接口继承自 `types.WorkflowInstanceManager`，实现所有标准方法
2. **类型统一**：所有 Workflow 都使用 `workflow.Workflow` 类型，通过 `ExecutionMode` 字段区分
3. **自动选择**：Engine 根据 `Workflow.ExecutionMode` 字段自动选择对应的 InstanceManager
4. **无混合模式**：不支持混合模式，Workflow 要么是批处理（`"batch"`），要么是流处理（`"streaming"`）

### 12.2 关键特性

| 特性 | 说明 |
|------|------|
| **事件驱动** | 基于 Watermill 的成熟事件总线 |
| **持续运行** | 支持 24/7 无超时任务 |
| **背压控制** | 防止数据积压导致内存溢出 |
| **容错恢复** | 自动重连和断点续传 |
| **接口兼容** | 实现 `WorkflowInstanceManager` 接口，与现有 Engine 无缝集成 |
| **类型安全** | 通过接口继承保证类型安全 |

### 12.3 使用流程

```
1. 创建实时任务
   └─→ RealtimeTaskBuilder.Build() → *RealtimeTask

2. 创建流处理 Workflow
   └─→ WorkflowBuilder.WithStreamingMode().WithRealtimeTask() → *Workflow
   └─→ Workflow.ExecutionMode = "streaming"

3. 提交到 Engine
   └─→ Engine.SubmitWorkflow(wf)
   └─→ Engine 检测 ExecutionMode == "streaming"
   └─→ Engine 创建 RealtimeInstanceManager

4. 运行
   └─→ RealtimeInstanceManager 管理持续任务
   └─→ 通过事件总线处理数据流
```

### 12.4 与批处理模式的区别

| 特性 | 批处理模式 | 流处理模式 |
|------|-----------|-----------|
| **Workflow 类型** | `workflow.Workflow` | `workflow.Workflow` |
| **ExecutionMode** | `"batch"` | `"streaming"` |
| **InstanceManager** | `WorkflowInstanceManagerV2` | `RealtimeInstanceManager` |
| **任务类型** | 一次性任务 | 持续运行任务 |
| **超时限制** | 有（默认30秒） | 无 |
| **完成条件** | 所有任务完成 | 持续运行直到停止 |
| **事件模型** | 任务状态事件 | 数据流事件 + 状态事件 |

---

*文档版本: V1.1*  
*创建时间: 2026-01-07*  
*更新时间: 2026-01-07*  
*更新内容: 移除混合模式，明确接口继承关系，统一使用 Workflow 类型*

