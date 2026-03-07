# 实时 Workflow 开发指南：股票行情与新闻数据同步

本文档面向使用 Task Engine 的开发者，介绍如何基于**实时 Workflow（Streaming）** 构建**股票行情**与**新闻数据**的实时同步能力。涉及的核心包：`pkg/core/realtime`、`pkg/core/builder`。

---

## 1. 概念概览

### 1.1 执行模式

- **Batch（批处理）**：Workflow 按 DAG 顺序执行，任务跑完即结束。
- **Streaming（流处理）**：Workflow 以**持续运行**的实例存在，内部由**持续任务（Continuous Task）** 长期运行，适合行情、新闻等实时数据接入。

实时行情/新闻场景应使用 **Streaming** 模式。

### 1.2 任务执行模式（RealtimeTask）

| 模式 | 常量 | 说明 |
|------|------|------|
| 一次性 | `ExecutionModeOneShot` | 传统批处理任务，跑一次结束 |
| 持续运行 | `ExecutionModeContinuous` | 长期运行，需配合 `ContinuousTaskConfig`（端点、重连、缓冲等） |
| 事件驱动 | `ExecutionModeEventDriven` | 由事件总线驱动，通过 `EventSubscriptions` 订阅事件 |

做**行情/新闻同步**时，通常用 **Continuous**：一个任务连行情源，一个连新闻源（或轮询新闻 API）。

### 1.3 持续任务类型（ContinuousTaskType）

在 `ContinuousTaskConfig` 中通过 `Type` 指定：

| 类型 | 常量 | 说明 |
|------|------|------|
| 数据采集器 | `TaskTypeDataCollector` | 从外部源拉取/接收数据（如 WebSocket 行情、新闻 API） |
| 流处理器 | `TaskTypeStreamProcessor` | 从内部缓冲区消费数据并处理（如落库、告警） |
| 事件监听器 | `TaskTypeEventListener` | 监听事件总线上某类事件并响应 |
| 定时轮询器 | `TaskTypeScheduledPoller` | 按固定间隔轮询（如定时拉新闻） |

**典型组合**：

- **行情**：`TaskTypeDataCollector`（WebSocket 行情）→ 数据进入缓冲区 → `TaskTypeStreamProcessor` 消费并落库/推送。
- **新闻**：`TaskTypeScheduledPoller` 或 `TaskTypeDataCollector`（长连接推送）拉新闻，再经缓冲区由流处理器写入存储。

---

## 2. 核心组件与数据流

### 2.1 组件关系

```
Workflow (ExecutionMode=streaming)
  └── RealtimeInstanceManager
        ├── 持续任务（ContinuousTask）列表
        ├── DataBuffer（背压缓冲）
        ├── 事件总线（Watermill Pub/Sub）
        └── 内部/外部事件处理
```

- **RealtimeInstanceManager**：在 `Engine.SubmitWorkflow` 时，若 Workflow 为 `streaming`，则由 Engine 创建并用来管理该实例。
- **持续任务**：由 `RealtimeTask` 的 `ExecutionMode == ExecutionModeContinuous` 且 `ContinuousConfig != nil` 解析而来，启动后按 `ContinuousTaskConfig.Type` 执行对应逻辑（见 `realtime_instance_manager.go` 中 `executeTaskLogic`）。
- **DataBuffer**：接收“数据到达”事件推送的原始数据，供流处理器消费；满时触发背压（可配置阈值与动作）。
- **事件**：连接建立/断开、重连、数据到达、背压、任务启停等都会发出 `RealtimeEvent`，可订阅做监控或联动。

### 2.2 数据流简述

1. **数据采集任务**（如行情 WebSocket）收到数据后，应发布 `EventDataArrived`，payload 为 `DataArrivedPayload`（或兼容的 `map`）。
2. 内部处理器 `handleDataArrived` 将 `Payload.Data` 推入 **DataBuffer**（`Push`）；若因背压丢弃则计入失败指标。
3. **流处理任务**（`TaskTypeStreamProcessor`）在 `runStreamProcessor` 中从 DataBuffer `Pop` 消费，并发布 `EventDataProcessed`。
4. 背压：缓冲区使用率超过配置阈值时发布 `EventBackpressure`，低于一半阈值时发布 `EventBackpressureRelieved`。

当前仓库中 **DataCollector 的具体连接与拉取逻辑是占位实现**（如 `runDataCollector` 仅 `time.Sleep`）。你需要在自己的业务代码中实现真实的数据源连接（如 WebSocket/HTTP 客户端），在收到行情或新闻数据时，通过 **RealtimeInstanceManager.PublishEvent** 发布 `EventDataArrived`，才能把数据注入引擎的缓冲与流处理链路。

---

## 3. 用 Builder 定义实时 Workflow

### 3.1 基本步骤

1. 使用 **RealtimeTaskBuilder** 定义实时任务（端点、任务类型、缓冲、重连、事件订阅等）。
2. 使用 **WorkflowBuilder** 的 **WithStreamingMode** 和 **WithRealtimeTask** 组成 Workflow。
3. 通过 **Engine.SubmitWorkflow** 提交，Engine 会创建 **RealtimeInstanceManager** 并启动其中的持续任务。

### 3.2 注册函数（Registry）

数据处理、错误处理、事件处理等若通过“函数名”配置，需在 Engine 的 **FunctionRegistry** 中注册，例如：

```go
eng, err := engine.NewEngineBuilder("./configs/engine.yaml").
    WithJobFunc("handleQuote", handleQuoteFunc).
    WithJobFunc("handleNews", handleNewsFunc).
    WithJobFunc("onError", errorHandlerFunc).
    Build()
```

Builder 里通过 **WithDataHandler("handleQuote")**、**WithErrorHandler("onError")** 等引用这些名字。

### 3.3 定义行情采集任务（示例）

```go
// 假设 registry 为 eng.GetRegistry()
quoteTask, err := builder.NewRealtimeTaskBuilder("stock_quote_collector", "股票行情采集", registry).
    WithContinuousMode().
    WithEndpoint("wss://quote.example.com/ws", "ws").
    WithTaskType(realtime.TaskTypeDataCollector).
    WithBuffer(10000, 100).                    // 缓冲区 10000，批大小 100
    WithFlushInterval(5 * time.Second).
    WithReconnect(true, 0).                    // 启用重连，0 表示无限次
    WithReconnectBackoff(time.Second, 30*time.Second, 2.0).
    WithBackpressure(0.8, "throttle").
    WithDataHandler("handleQuote").
    WithErrorHandler("onError").
    Build()
if err != nil {
    return err
}
```

- **WithEndpoint**：逻辑端点与协议，实际连接在你自己实现的采集逻辑里使用。
- **WithTaskType(TaskTypeDataCollector)**：该任务在实例内会按“数据采集器”类型调度（当前默认实现是占位，真实拉取需在业务侧完成并向事件总线发布 `EventDataArrived`）。
- **WithBuffer / WithFlushInterval**：与缓冲、批处理相关配置（部分会落到 `ContinuousTaskConfig`，供实例管理器或你自定义逻辑使用）。
- **WithReconnect***：重连开关与退避；断开连接时会发布 `EventDisconnected`，内部可触发 `handleReconnect`。

### 3.4 定义新闻轮询任务（示例）

```go
newsTask, err := builder.NewRealtimeTaskBuilder("news_poller", "新闻定时拉取", registry).
    WithContinuousMode().
    WithEndpoint("https://api.news.example.com/v1/news", "http").
    WithTaskType(realtime.TaskTypeScheduledPoller).
    WithFlushInterval(1 * time.Minute).        // 每分钟拉一次
    WithBuffer(5000, 50).
    WithDataHandler("handleNews").
    Build()
if err != nil {
    return err
}
```

- **TaskTypeScheduledPoller**：按 `FlushInterval` 周期执行（当前实现里 `runScheduledPoller` 仅 sleep 该间隔，真实 HTTP 拉取需在业务代码中实现，并同样通过 **PublishEvent(EventDataArrived, ...)** 写入缓冲）。

### 3.5 定义流处理任务（消费缓冲数据）

若希望由引擎的 DataBuffer 统一接收数据，再由“流处理任务”消费（如落库、告警），可增加一个 **StreamProcessor** 任务：

```go
streamTask, err := builder.NewRealtimeTaskBuilder("quote_stream_processor", "行情流处理", registry).
    WithContinuousMode().
    WithTaskType(realtime.TaskTypeStreamProcessor).
    WithBuffer(10000, 100).
    WithDataHandler("handleQuote").
    Build()
```

- **TaskTypeStreamProcessor**：在 `runStreamProcessor` 中从 `DataBuffer.Pop` 取数据，并调用你配置的 DataHandler（需在 Registry 中注册）；数据来源即其他任务通过 `EventDataArrived` 推进缓冲区的内容。

### 3.6 事件订阅（可选）

若要对“数据到达”“背压”“连接断开”等做监控或联动，可在 Builder 上订阅事件：

```go
quoteTask, err = builder.NewRealtimeTaskBuilder(...).
    // ... 上述配置 ...
    SubscribeEvent(realtime.EventDataArrived, "onDataArrived").
    SubscribeEvent(realtime.EventBackpressure, "onBackpressure").
    Build()
```

对应处理函数需在 Registry 中按事件处理函数签名注册（具体签名需与引擎侧调用约定一致）。

### 3.7 组装 Workflow 并提交

```go
wf, err := builder.NewWorkflowBuilder("realtime_market", "实时行情与新闻同步").
    WithStreamingMode().           // 必须：流式执行
    WithRealtimeTask(quoteTask).   // 行情采集
    WithRealtimeTask(newsTask).    // 新闻轮询
    WithRealtimeTask(streamTask).  // 可选：流处理
    Build()
if err != nil {
    return err
}

ctrl, err := eng.SubmitWorkflow(ctx, wf)
if err != nil {
    return err
}
// ctrl.InstanceID() 即为本实例 ID，可用于后续 Pause/Resume/Terminate、查状态、查指标
```

注意：**WithRealtimeTask** 会向 Workflow 加入该实时任务，并保证 Workflow 以 **streaming** 模式运行（参见 `workflow_builder.go`）。

---

## 4. 配置项说明

### 4.1 ContinuousTaskConfig（持续任务）

| 配置 | 说明 | 建议（行情/新闻） |
|------|------|-------------------|
| Endpoint / Protocol | 逻辑端点与协议 | 行情：wss://...；新闻：https://... |
| ReconnectEnabled | 是否自动重连 | 行情建议 true |
| MaxReconnectAttempts | 最大重连次数，0 为不限 | 0 或 10+ |
| ReconnectBackoff | InitialInterval / MaxInterval / Multiplier | 1s / 30s / 2.0 常用 |
| BufferSize / BatchSize | 缓冲条数、批大小 | 按 QPS 与下游处理能力调整 |
| FlushInterval | 刷新/轮询间隔 | 轮询新闻可用 1m～5m |
| BackpressureThreshold | 背压阈值 (0~1) | 0.8 |
| BackpressureAction | drop / block / throttle | throttle 较稳妥 |
| DataHandler / ErrorHandler | 处理函数名 | 在 Registry 中注册 |

### 4.2 RealtimeInstanceManager 选项（创建实例时）

Engine 在创建 **RealtimeInstanceManager** 时使用的选项在 `pkg/core/realtime/options.go` 中定义，例如：

- **WithBufferSize(size)**：默认 10000。
- **WithBackpressureThreshold(threshold)**：默认 0.8。
- **WithShutdownTimeout(timeout)**：优雅关闭等待时间。
- **WithReconnectTimeout(timeout)**：单次重连尝试超时。

这些由 Engine 在 `createRealtimeInstanceManager` 中写死或从配置传入，你只需知道“实例级”缓冲与背压受这些影响即可；若将来 Engine 暴露配置入口，可在此处扩展。

---

## 5. 事件类型与 Payload（events.go）

常用事件与 payload 结构（便于你在业务里发布或订阅）：

| 事件类型 | 说明 | Payload 类型 |
|----------|------|--------------|
| EventDataArrived | 数据到达（推入缓冲） | DataArrivedPayload |
| EventDataProcessed | 单条数据处理完成 | DataArrivedPayload |
| EventTaskStarted / Paused / Resumed / Stopped | 任务状态变化 | TaskStatusPayload |
| EventConnected / EventDisconnected | 连接建立/断开 | ConnectionPayload |
| EventReconnecting / EventReconnected | 重连中/成功 | ConnectionPayload |
| EventError | 错误 | ErrorPayload |
| EventBackpressure / EventBackpressureRelieved | 背压触发/解除 | BackpressurePayload |

**DataArrivedPayload** 含：`Data`（原始数据）、`Source`、`Size`、`Sequence`、`BatchID`。  
业务侧在实现采集逻辑时，构造 **NewRealtimeEvent(EventDataArrived, taskID, instanceID, &DataArrivedPayload{...})** 并调用 **RealtimeInstanceManager.PublishEvent** 即可把行情/新闻写入引擎缓冲。

---

## 6. 缓冲区与背压（buffer.go）

- **DataBuffer**：带容量与阈值的 channel，**Push** 非阻塞，满则丢弃并计 dropped；**PushBlocking** 阻塞。
- **Usage()**：当前使用率；超过阈值触发背压回调（实例管理器会发布 EventBackpressure）。
- **Pop / PopBlocking**：流处理任务从缓冲取数据；**TryPopWithDone** 支持与 done 通道配合做优雅退出。

建议：下游消费能力有限时，适当增大 BufferSize、设置 BackpressureThreshold，并监听 EventBackpressure 做告警或限流。

---

## 7. 运行时控制与可观测性

### 7.1 生命周期

- **SubmitWorkflow**：创建并启动 streaming 实例；返回 **WorkflowController**。
- **PauseWorkflowInstance(ctx, instanceID)**：暂停实例（所有持续任务 Pause）。
- **ResumeWorkflowInstance(ctx, instanceID)**：恢复。
- **TerminateWorkflowInstance(ctx, instanceID, reason)**：终止并做优雅关闭（Shutdown）。

### 7.2 获取 RealtimeInstanceManager 与指标

通过 **Engine.GetInstanceManager(instanceID)** 可得到该实例的 **RealtimeInstanceManager**（需类型断言）。接口提供：

- **GetMetrics()**：TotalEvents、ProcessedEvents、FailedEvents、ActiveTasks、BufferUsage、AverageLatency、Uptime 等。
- **GetContinuousTask(taskID)**：按任务 ID 取 **ContinuousTask**，可查状态、DataCount、ErrorCount、LastDataTime、ReconnectCount 等。
- **PauseContinuousTask(taskID) / ResumeContinuousTask(taskID)**：单任务暂停/恢复。

### 7.3 进度与断点

- **GetProgress()**：返回总任务数、运行中数等（实时实例无“已完成”概念，completed=0）。
- **CreateBreakpoint() / RestoreFromBreakpoint()**：用于暂停时保存任务与缓冲状态，恢复时恢复状态（如任务 state、buffer_stats、metrics）。

---

## 8. 实现“真实”的行情与新闻采集

当前引擎内 **runDataCollector / runScheduledPoller** 仅为占位（sleep 或空逻辑），**不会**真正建连或发 HTTP 请求。你需要：

1. **在业务侧维护 RealtimeInstanceManager 的引用**  
   例如在 SubmitWorkflow 后，用 `GetInstanceManager(ctrl.InstanceID())` 得到 manager，保存到你的业务结构体，供采集 goroutine 使用。

2. **实现行情采集**  
   - 在单独 goroutine 中建立 WebSocket 到 `ContinuousTaskConfig.Endpoint`。  
   - 收到行情消息后，反序列化为你的结构体，再构造 **realtime.NewRealtimeEvent(realtime.EventDataArrived, taskID, instanceID, &realtime.DataArrivedPayload{Data: yourStruct, Source: endpoint, Sequence: seq})**。  
   - 调用 **manager.PublishEvent(ctx, event)**，数据即进入 DataBuffer，供 **TaskTypeStreamProcessor** 消费。

3. **实现新闻拉取**  
   - 定时（如按 FlushInterval）用 HTTP 请求新闻 API。  
   - 将返回的列表或单条新闻封装为 **DataArrivedPayload**，同样 **PublishEvent(EventDataArrived, ...)**。  
   - 若希望“轮询任务”只负责调度，也可在 **TaskTypeScheduledPoller** 的周期逻辑里只发“拉取请求”，由另一个 HTTP 客户端 goroutine 收到响应后再 PublishEvent。

4. **错误与重连**  
   - 连接断开时发布 **EventDisconnected**，并可选发布 **EventError**（Recoverable=true）。  
   - 实例管理器内部会根据 **ReconnectEnabled** 和 **handleReconnect** 做退避重连；你只需在重连成功后重新建连并继续 PublishEvent 即可。

这样，**引擎负责缓冲、背压、事件总线和任务调度**，**你负责数据源协议与 PublishEvent**，即可完成“实时同步股票行情和新闻数据”的闭环。

---

## 9. 文件与包索引

| 文件 | 作用 |
|------|------|
| `pkg/core/builder/realtime_task_builder.go` | 实时任务构建器（端点、类型、缓冲、重连、事件订阅等） |
| `pkg/core/builder/workflow_builder.go` | Workflow 构建器，WithStreamingMode / WithRealtimeTask |
| `pkg/core/realtime/continuous_task.go` | 持续任务状态与配置（ContinuousTaskConfig、ContinuousTask） |
| `pkg/core/realtime/buffer.go` | DataBuffer、背压 |
| `pkg/core/realtime/realtime_instance_manager.go` | 实例管理、持续任务调度、事件发布/订阅、缓冲消费 |
| `pkg/core/realtime/events.go` | 事件类型、Payload 结构、EventHandler |
| `pkg/core/realtime/options.go` | RealtimeInstanceManager 选项（缓冲、背压、超时等） |
| `pkg/core/realtime/realtime_task.go` | RealtimeTask、执行模式、ExtractRealtimeTask |

---

## 10. 小结

- 使用 **Streaming** Workflow + **RealtimeTaskBuilder** 定义行情/新闻的**采集任务**与**流处理任务**。
- 通过 **ContinuousTaskConfig** 配置端点、类型、缓冲、重连、背压；通过 **WithDataHandler/WithErrorHandler** 和 Registry 绑定处理函数。
- 数据由业务侧**真实连接**（WebSocket/HTTP）产生，通过 **PublishEvent(EventDataArrived, DataArrivedPayload)** 注入引擎，经 **DataBuffer** 与 **TaskTypeStreamProcessor** 消费。
- 利用 **事件订阅**、**GetMetrics**、**GetContinuousTask** 做监控与运维，用 **Pause/Resume/Terminate** 做生命周期控制。

按上述方式即可在 Task Engine 上搭建“实时同步股票行情和新闻数据”的完整流水线；若某一步需要更细的代码示例（例如某交易所 WebSocket 协议或新闻 API 的封装），可以在本指南基础上按模块补充。
