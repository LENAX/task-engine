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
3. **流处理任务**（`TaskTypeStreamProcessor`）在 `runStreamProcessor` 中从 DataBuffer `Pop` 消费；若配置了 **DataHandler** 且 Engine 传入了 FunctionRegistry，会以 `params["data"]` 调用该 Job 函数，再发布 `EventDataProcessed`。
4. 背压：缓冲区使用率超过配置阈值时发布 `EventBackpressure`，低于一半阈值时发布 `EventBackpressureRelieved`。

数据采集的两种方式（二选一或组合）：

- **推荐：Workflow 内注册采集器**：实现 `realtime.DataCollector` 接口，在 **WorkflowBuilder** 上使用 **WithDataCollector(name, collector)** 注册（与 WithRealtimeTask 同处构建），在 RealtimeTaskBuilder 上使用 **WithCollector(name)**。引擎在运行时会调用 `collector.Run(ctx, config, publish)`，你在收到数据时调用 `publish(NewRealtimeEvent(EventDataArrived, ...))` 即可把数据写入缓冲。若需在采集端打印或打点，可在 `publish` 前加日志。消费端由 **StreamProcessor** 任务从 buffer Pop 并调用 DataHandler（见 3.5、3.8）。**兼容**：也可在 **EngineBuilder.WithDataCollector** 处注册，Engine 在 Workflow 未带注册表时会回退使用。
- **可选：用户侧 PublishEvent**：在业务侧持有 RealtimeInstanceManager 引用，自行建连（WebSocket/HTTP），在收到数据时调用 **RealtimeInstanceManager.PublishEvent** 发布 `EventDataArrived`。无注册采集器时，任务会走占位逻辑（如 sleep），你可在外部注入数据。

---

## 3. 用 Builder 定义实时 Workflow

### 3.1 基本步骤

1. 使用 **RealtimeTaskBuilder** 定义实时任务（端点、任务类型、缓冲、重连、事件订阅等）。
2. 使用 **WorkflowBuilder** 的 **WithStreamingMode** 和 **WithRealtimeTask** 组成 Workflow。
3. 通过 **Engine.SubmitWorkflow** 提交，Engine 会创建 **RealtimeInstanceManager** 并启动其中的持续任务。

### 3.2 注册函数（Registry）

流处理任务使用的 DataHandler、以及采集/事件处理等若通过“函数名”配置，需在 Engine 的 **FunctionRegistry** 中注册。Engine 创建 RealtimeInstanceManager 时会传入该 Registry（`WithFunctionRegistry`），供 `runStreamProcessor` 按名调用 DataHandler。

```go
registry := eng.GetRegistry()
_, _ = registry.Register(ctx, "print_data_job", printDataJob, "打印收到的数据")
// 流处理任务通过 WithJobFunction("print_data_job", nil) 引用；WithJobFunction 会同步设置 DataHandler
```

DataHandler 函数签名为 `func(ctx *task.TaskContext) error`，从 `ctx.Params["data"]` 取缓冲中 Pop 出的单条数据。

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

- **TaskTypeScheduledPoller**：与 DataCollector 统一走 **runDataCollector**，由 **Mode=pull** 与 `FlushInterval` 等配置区分；真实拉取需实现 **DataCollector** 并在 `Run` 内按间隔请求后 **publish(EventDataArrived, ...)**（或使用用户侧 PublishEvent）。

### 3.5 定义流处理任务（消费缓冲数据）

若希望由引擎的 DataBuffer 统一接收数据，再由“流处理任务”消费（如落库、告警、打印），可增加一个 **StreamProcessor** 任务。使用 **WithJobFunction(name, nil)** 即可，会同步设置 DataHandler，供 `runStreamProcessor` 从 buffer Pop 后按名调用：

```go
streamTask, err := builder.NewRealtimeTaskBuilder("quote_stream_processor", "行情流处理", registry).
    WithContinuousMode().
    WithTaskType(realtime.TaskTypeStreamProcessor).
    WithJobFunction("handleQuote", nil).   // 同步设置 DataHandler，runStreamProcessor 会以 params["data"] 调用
    Build()
```

- **TaskTypeStreamProcessor**：在 `runStreamProcessor` 中从 `DataBuffer.Pop` 取数据；若配置了 DataHandler 且 Engine 传入了 FunctionRegistry，会构造 `params["data"] = 单条数据` 并调用该 Job 函数，再发布 `EventDataProcessed`。

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

流式 Workflow 的 **DataCollector 注册在 WorkflowBuilder 处完成**：**WithDataCollector(name, collector)**，name 与 RealtimeTaskBuilder.WithCollector(name) 一致。

```go
wf, err := builder.NewWorkflowBuilder("realtime_market", "实时行情与新闻同步").
    WithStreamingMode().             // 必须：流式执行
    WithDataCollector("quote_ws", quoteCollector).  // 推荐：在此处注册采集器
    WithRealtimeTask(quoteTask).     // 行情采集
    WithRealtimeTask(newsTask).     // 新闻轮询
    WithRealtimeTask(streamTask).   // 可选：流处理
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

### 3.8 完整示例（基于 E2E 用例）

以下三种模式对应 `test/e2e/realtime_collector_e2e_test.go` 中的用例，可直接参考或裁剪使用。

**示例一：有限次 publish 采集器 + 流处理（最小闭环）**

采集器在 `Run` 内发布 N 条后 return，流处理任务从 buffer 消费并调用 DataHandler（如打印）：

```go
// 1) 实现 DataCollector：有限次 publish
type finitePublishCollector struct {
    maxPublish int32
    published  int32
}
func (c *finitePublishCollector) Run(ctx context.Context, config *realtime.ContinuousTaskConfig, publish realtime.PublishFunc) error {
    taskID := ""
    if config != nil {
        taskID = config.ID
    }
    for atomic.LoadInt32(&c.published) < c.maxPublish {
        select {
        case <-ctx.Done():
            return nil
        default:
        }
        n := atomic.AddInt32(&c.published, 1)
        e := realtime.NewRealtimeEvent(realtime.EventDataArrived, taskID, "", &realtime.DataArrivedPayload{
            Data: n, Source: "e2e_finite",
        })
        _ = publish(e)
        time.Sleep(10 * time.Millisecond)
    }
    return nil
}

// 2) DataHandler：从 ctx.Params["data"] 取单条数据（流处理时由 runStreamProcessor 注入）
func printDataJob(ctx *task.TaskContext) error {
    if len(ctx.Params) > 0 {
        b, _ := json.Marshal(ctx.Params)
        log.Printf("[printDataJob] TaskID=%s 收到数据: %s", ctx.TaskID, string(b))
    }
    return nil
}

// 3) 构建引擎、注册 DataHandler，再在 WorkflowBuilder 处注册采集器并组装 Workflow
eng, _ := engine.NewEngineBuilder(configPath).Build()
registry := eng.GetRegistry()
registry.Register(ctx, "print_data_job", printDataJob, "打印收到的数据")

collector := &finitePublishCollector{maxPublish: 5}
collectorTask, _ := builder.NewRealtimeTaskBuilder("e2e_collector", "e2e", registry).
    WithContinuousMode().
    WithTaskType(realtime.TaskTypeDataCollector).
    WithCollector("e2e_finite").
    WithJobFunction("print_data_job", nil).
    Build()

streamTask, _ := builder.NewRealtimeTaskBuilder("e2e_stream_processor", "流处理", registry).
    WithContinuousMode().
    WithTaskType(realtime.TaskTypeStreamProcessor).
    WithJobFunction("print_data_job", nil).
    Build()

wf, _ := builder.NewWorkflowBuilder("e2e_realtime_wf", "e2e").
    WithStreamingMode().
    WithDataCollector("e2e_finite", collector).
    WithRealtimeTask(collectorTask).
    WithRealtimeTask(streamTask).
    Build()
ctrl, _ := eng.SubmitWorkflow(ctx, wf)
```

**示例二：Pull 采集器（HTTP 分页）+ 流处理**

采集器按 `FlushInterval` 轮询 HTTP 接口（如 `GET /api/stk_mins?offset=0&limit=100`），逐条 publish；流处理任务同上。

```go
// 采集器：HTTP 分页拉取并 publish
type stkMinsPullCollector struct {
    client     *http.Client
    published  int32
    maxPublish int32
}
func (c *stkMinsPullCollector) Run(ctx context.Context, config *realtime.ContinuousTaskConfig, publish realtime.PublishFunc) error {
    taskID := config.ID
    baseURL := config.Endpoint
    interval := config.FlushInterval
    if interval <= 0 {
        interval = 200 * time.Millisecond
    }
    cli := c.client
    if cli == nil {
        cli = &http.Client{Timeout: 10 * time.Second}
    }
    offset := 0
    limit := 100
    for {
        select {
        case <-ctx.Done():
            return nil
        default:
        }
        req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
            baseURL+"/api/stk_mins?offset="+strconv.Itoa(offset)+"&limit="+strconv.Itoa(limit), nil)
        resp, err := cli.Do(req)
        if err != nil {
            return err
        }
        var body struct {
            Data  []YourRow `json:"data"`
            Total int       `json:"total"`
        }
        _ = json.NewDecoder(resp.Body).Decode(&body)
        resp.Body.Close()
        for i := range body.Data {
            if c.maxPublish > 0 && atomic.LoadInt32(&c.published) >= c.maxPublish {
                return nil
            }
            e := realtime.NewRealtimeEvent(realtime.EventDataArrived, taskID, "", &realtime.DataArrivedPayload{
                Data: body.Data[i], Source: "stk_mins_pull",
            })
            _ = publish(e)
            atomic.AddInt32(&c.published, 1)
        }
        if len(body.Data) < limit {
            offset = 0
        } else {
            offset += len(body.Data)
        }
        select {
        case <-ctx.Done():
            return nil
        case <-time.After(interval):
        }
    }
}

// 任务与 Workflow（采集器在 WorkflowBuilder 处注册）
eng, _ := engine.NewEngineBuilder(configPath).Build()
registry := eng.GetRegistry()
registry.Register(ctx, "print_data_job", printDataJob, "打印收到的数据")

pullCollector := &stkMinsPullCollector{maxPublish: 50}
collectorTask, _ := builder.NewRealtimeTaskBuilder("stk_pull", "stk_mins_pull", registry).
    WithContinuousMode().
    WithTaskType(realtime.TaskTypeDataCollector).
    WithCollector("stk_mins_pull").
    WithMode(realtime.CollectorModePull).
    WithEndpoint(serverURL, "http").
    WithFlushInterval(300 * time.Millisecond).
    WithJobFunction("print_data_job", nil).
    Build()

streamTask, _ := builder.NewRealtimeTaskBuilder("stk_pull_processor", "流处理", registry).
    WithContinuousMode().
    WithTaskType(realtime.TaskTypeStreamProcessor).
    WithJobFunction("print_data_job", nil).
    Build()

wf, _ := builder.NewWorkflowBuilder("e2e_stk_pull_wf", "e2e").
    WithStreamingMode().
    WithDataCollector("stk_mins_pull", pullCollector).
    WithRealtimeTask(collectorTask).
    WithRealtimeTask(streamTask).
    Build()
```

**示例三：Push 采集器（WebSocket）+ 流处理**

采集器连接 WebSocket，循环 `conn.ReadJSON(&row)`，每收到一条就 `publish(EventDataArrived, row)`；流处理任务同上。

```go
// 采集器：WebSocket 长连接收包并 publish
type stkMinsPushCollector struct {
    maxPublish int32
    published  int32
}
func (c *stkMinsPushCollector) Run(ctx context.Context, config *realtime.ContinuousTaskConfig, publish realtime.PublishFunc) error {
    taskID := config.ID
    wsURL := config.Endpoint
    dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
    conn, _, err := dialer.DialContext(ctx, wsURL, nil)
    if err != nil {
        return err
    }
    defer conn.Close()
    for {
        select {
        case <-ctx.Done():
            return nil
        default:
        }
        if c.maxPublish > 0 && atomic.LoadInt32(&c.published) >= c.maxPublish {
            return nil
        }
        var row YourRow
        if err := conn.ReadJSON(&row); err != nil {
            if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
                return nil
            }
            return err
        }
        e := realtime.NewRealtimeEvent(realtime.EventDataArrived, taskID, "", &realtime.DataArrivedPayload{
            Data: row, Source: "stk_mins_push",
        })
        _ = publish(e)
        atomic.AddInt32(&c.published, 1)
    }
}

// 任务与 Workflow（采集器在 WorkflowBuilder 处注册）
eng, _ := engine.NewEngineBuilder(configPath).Build()
registry := eng.GetRegistry()
registry.Register(ctx, "print_data_job", printDataJob, "打印收到的数据")

pushCollector := &stkMinsPushCollector{maxPublish: 100}
collectorTask, _ := builder.NewRealtimeTaskBuilder("stk_push", "stk_mins_push", registry).
    WithContinuousMode().
    WithTaskType(realtime.TaskTypeDataCollector).
    WithCollector("stk_mins_push").
    WithMode(realtime.CollectorModePush).
    WithEndpoint(wsURL, "ws").
    WithJobFunction("print_data_job", nil).
    Build()

streamTask, _ := builder.NewRealtimeTaskBuilder("stk_push_processor", "流处理", registry).
    WithContinuousMode().
    WithTaskType(realtime.TaskTypeStreamProcessor).
    WithJobFunction("print_data_job", nil).
    Build()

wf, _ := builder.NewWorkflowBuilder("e2e_stk_push_wf", "e2e").
    WithStreamingMode().
    WithDataCollector("stk_mins_push", pushCollector).
    WithRealtimeTask(collectorTask).
    WithRealtimeTask(streamTask).
    Build()
```

三种模式共性：**采集器在 WorkflowBuilder 上通过 WithDataCollector(name, collector) 注册**，与 WithRealtimeTask 同处构建；**采集任务**（DataCollector）负责拉取/接收并 `publish(EventDataArrived)`，**流处理任务**（StreamProcessor）从 buffer 消费并执行 DataHandler。Engine 创建 RealtimeInstanceManager 时优先使用 Workflow 自带的 DataCollectorRegistry，并传入 `WithFunctionRegistry(e.registry)`，runStreamProcessor 才能按名调用 DataHandler。

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
- **WithCollectorRegistry(registry)**：采集器注册表，供 runDataCollector 按名查找。
- **WithFunctionRegistry(registry)**：函数注册表（需实现 `DataHandlerRegistry`，如 Engine 的 `e.registry`），供 runStreamProcessor 按 DataHandler 名调用 Job 函数。
- **WithShutdownTimeout(timeout)**：优雅关闭等待时间。
- **WithReconnectTimeout(timeout)**：单次重连尝试超时。

Engine 在 `createRealtimeInstanceManager` 中会传入 `WithCollectorRegistry(e.collectorRegistry)` 与 `WithFunctionRegistry(e.registry)`，因此流处理任务的 DataHandler 只需在 Engine 的 Registry 中注册即可被调用。

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

### 8.1 推荐方式：引擎内 DataCollector 注册

引擎统一用一种“数据采集”抽象 **DataCollector**，通过配置中的 **Mode（push/pull）+ Endpoint + Config** 区分行为，不再在执行层区分 DataCollector 与 ScheduledPoller 两条分支。

- **接口**：`realtime.DataCollector`，唯一方法 `Run(ctx context.Context, config *ContinuousTaskConfig, publish PublishFunc) error`。  
  `publish` 由引擎注入，签名为 `func(event *RealtimeEvent) error`；收到数据后调用 `publish(NewRealtimeEvent(EventDataArrived, taskID, instanceID, &DataArrivedPayload{...}))` 即可写入缓冲。
- **注册（推荐）**：在 **WorkflowBuilder** 上 **WithDataCollector("quote_ws", myCollector)**，与 WithRealtimeTask 同处构建；任务侧用 **RealtimeTaskBuilder.WithCollector("quote_ws")** 引用。Engine 创建 RealtimeInstanceManager 时优先使用 Workflow 自带的注册表。**兼容**：也可在 **EngineBuilder.WithDataCollector** 注册，Workflow 未带注册表时会回退使用。
- **Mode**：`ContinuousTaskConfig.Mode` 为 **push**（长连接收包）或 **pull**（按间隔拉取）。常量 `realtime.CollectorModePush` / `realtime.CollectorModePull`，或 Builder 上 **WithMode("push")** / **WithMode("pull")**。实现者在 `Run()` 内根据 `config.Mode`、`config.Endpoint`、`config.FlushInterval`/`Params` 决定逻辑；空串视为 push。

**最小示例（收到一条就 publish 一次）**：

```go
type myCollector struct{}

func (c *myCollector) Run(ctx context.Context, config *realtime.ContinuousTaskConfig, publish realtime.PublishFunc) error {
    // 根据 config.Mode 选择 push（阻塞 read 循环）或 pull（按 FlushInterval 请求）
    taskID := config.ID
    if taskID == "" {
        taskID = config.CollectorName
    }
    event := realtime.NewRealtimeEvent(
        realtime.EventDataArrived,
        taskID,
        "", // instanceID 可由 manager 注入，此处可留空
        &realtime.DataArrivedPayload{Data: yourData, Source: config.Endpoint},
    )
    return publish(event)
}
```

**Push 骨架（WebSocket 长连接）**：在 `Run` 内建连 `config.Endpoint`，循环 `conn.ReadMessage()`，收到则 `publish(NewRealtimeEvent(EventDataArrived, ...))`，`ctx.Done()` 时退出并 return。

**Pull 骨架（定时 HTTP）**：在 `Run` 内 for + select，`case <-time.After(config.FlushInterval)` 时发 HTTP 请求，将结果封装为 `DataArrivedPayload` 并 `publish(...)`，`ctx.Done()` 时 return。

构建引擎与 Workflow 示例（采集器在 WorkflowBuilder 处注册）：

```go
eng, _ := engine.NewEngineBuilder(configPath).Build()
quoteCollector := &myCollector{}

quoteTask, _ := builder.NewRealtimeTaskBuilder("quote", "行情", registry).
    WithContinuousMode().
    WithTaskType(realtime.TaskTypeDataCollector).
    WithCollector("quote_ws").
    WithMode(realtime.CollectorModePush).  // 或 WithMode("pull")
    WithEndpoint("wss://...", "ws").
    Build()

wf, _ := builder.NewWorkflowBuilder("realtime_market", "行情").
    WithStreamingMode().
    WithDataCollector("quote_ws", quoteCollector).
    WithRealtimeTask(quoteTask).
    Build()
```

### 8.2 可选方式：用户侧 PublishEvent

若不使用注册采集器，可在业务侧维护 **RealtimeInstanceManager** 引用（例如 `GetInstanceManager(ctrl.InstanceID())`），自行建连（WebSocket/HTTP），在收到数据时调用 **manager.PublishEvent(ctx, NewRealtimeEvent(EventDataArrived, ...))** 注入数据。此时任务若配置了 `WithCollector("")` 或未设置 CollectorName，会走占位逻辑（如 sleep），由你在外部驱动数据。

### 8.3 错误与重连

- 连接断开时发布 **EventDisconnected**，并可选 **EventError**（Recoverable=true）。
- 实例管理器根据 **ReconnectEnabled** 与 **handleReconnect** 做退避重连；采集器在重连成功后继续在 `Run` 内建连并 `publish` 即可。

---

## 9. 文件与包索引

| 文件 | 作用 |
|------|------|
| `pkg/core/realtime/collector.go` | DataCollector 接口、PublishFunc、DataCollectorRegistry 与默认实现 |
| `pkg/core/builder/realtime_task_builder.go` | 实时任务构建器（WithCollector、WithMode、端点、类型、缓冲、重连等） |
| `pkg/core/builder/workflow_builder.go` | Workflow 构建器，WithStreamingMode / WithRealtimeTask |
| `pkg/core/realtime/continuous_task.go` | 持续任务状态与配置（CollectorName、Mode、ContinuousTaskConfig、ContinuousTask） |
| `pkg/core/realtime/buffer.go` | DataBuffer、背压 |
| `pkg/core/realtime/realtime_instance_manager.go` | 实例管理、runDataCollector 唯一生产者路径、事件发布/订阅、缓冲消费 |
| `pkg/core/realtime/events.go` | 事件类型、Payload 结构、EventHandler |
| `pkg/core/realtime/options.go` | RealtimeInstanceManager 选项（WithCollectorRegistry、WithFunctionRegistry、缓冲、背压、超时等） |
| `pkg/core/realtime/realtime_task.go` | RealtimeTask、执行模式、ExtractRealtimeTask |

---

## 10. 小结

- 使用 **Streaming** Workflow + **RealtimeTaskBuilder** 定义行情/新闻的**采集任务**与**流处理任务**。
- **推荐**：实现 **realtime.DataCollector**，在 **WorkflowBuilder.WithDataCollector(name, collector)** 处注册（与 WithRealtimeTask 同处构建），任务上 **WithCollector(name)**；在 `Run(ctx, config, publish)` 内根据 **Mode（push/pull）+ Endpoint + Config** 建连并调用 **publish(NewRealtimeEvent(EventDataArrived, ...))** 注入数据。**兼容**：EngineBuilder.WithDataCollector 仍可用，Workflow 未带注册表时回退使用。
- **流处理**：增加 **TaskTypeStreamProcessor** 任务，用 **WithJobFunction(handlerName, nil)** 指定 DataHandler（会同步写入 ContinuousTaskConfig.DataHandler）；Engine 创建实例时传入 **WithFunctionRegistry(e.registry)**，runStreamProcessor 从 buffer Pop 后以 **params["data"]** 调用该函数。
- **可选**：业务侧持有 RealtimeInstanceManager，自行建连并通过 **PublishEvent(EventDataArrived, ...)** 注入。
- 通过 **ContinuousTaskConfig** 配置端点、Mode、缓冲、重连、背压；DataHandler 需在 Engine 的 Registry 中注册，签名为 `func(ctx *task.TaskContext) error`，从 `ctx.Params["data"]` 取单条数据。
- 完整可运行示例见 **3.8 完整示例（基于 E2E 用例）**，对应 `test/e2e/realtime_collector_e2e_test.go` 中三种场景：有限次 publish、Pull（HTTP 分页）、Push（WebSocket）。
- 利用 **事件订阅**、**GetMetrics**、**GetContinuousTask** 做监控与运维，用 **Pause/Resume/Terminate** 做生命周期控制。

按上述方式即可在 Task Engine 上搭建“实时同步股票行情和新闻数据”的完整流水线；若某一步需要更细的代码示例（例如某交易所 WebSocket 协议或新闻 API 的封装），可以在本指南基础上按模块补充。
