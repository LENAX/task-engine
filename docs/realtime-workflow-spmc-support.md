Realtime Workflow 广播与至少一次语义设计（方案 A）
本文档描述在现有 Realtime Workflow 基础上，如何增加广播能力与进程内 at-least-once 语义，用于支持“单源多下游”的行情 / 新闻实时同步场景。

1. 目标与非目标
目标

广播能力：一份数据可以被同一 Workflow 内的多个订阅者（StreamProcessor / Handler）各自消费一次：
典型下游：DB 批量写入 / 前端推送 / 实时新闻展示等。
进程内 at-least-once 语义：
进程不崩溃时，关键订阅者（如 DB）尽量不丢数据；
进程崩溃后，通过 WAL 回放恢复未处理完成的数据；
允许重复消费，但尽量避免数据丢失。
差异化缓冲策略：
关键下游（DB）：偏向可靠性 → 阻塞 + 大 Buffer；
非关键下游（前端展示）：偏向实时性 → 非阻塞 + 容忍丢弃。
易于使用：
对上层使用者，只需要在 Builder 中声明多个消费者 / 订阅者，无需理解内部细节。
非目标

不追求分布式全局严格 at-least-once / exactly-once，尤其是跨进程、跨集群级别的幂等保障；
不引入额外的外部分布式 MQ（Kafka/NATS 等），保持运维成本低；
暂不解决“跨数据中心多实例协同消费”的复杂场景。
2. 整体架构与概念扩展
2.1 现有架构回顾（简化）
一个 Streaming Workflow 实例由 RealtimeInstanceManager 管理。

当前数据流（单队列 SPMC）：

DataCollector publish(EventDataArrived, DataArrivedPayload{Data: x});
handleDataArrived 把 payload.Data 推入单一 DataBuffer；
一个或多个 TaskTypeStreamProcessor 从 DataBuffer.Pop() 抢数据；
每条数据只被一个 StreamProcessor 处理一次（SPMC worker pool）。
2.2 新增概念：多订阅者广播层
在保持 DataCollector / Event 入口不变的前提下，引入订阅者广播层：

新增内部结构（逻辑概念）：

Subscriber：一个“下游订阅者”，可以对应：
DB 写入消费者；
前端推送消费者；
日志 / 告警消费者等。
每个 Subscriber 绑定：
一个独立的 DataBuffer；
一个或多个 StreamProcessor 任务（通常 1:1，必要时可以 1:N 扩容并行度）。
广播链路：

DataCollector 发布 EventDataArrived；
RealtimeInstanceManager 的内部 handler 不再只写一个 Buffer；
而是：For each Subscriber → 写入对应的 DataBuffer（按其策略选择 Push 或 PushBlocking）；
每个 Subscriber 的 StreamProcessor 从自己的 Buffer 中 Pop：
因为 Buffer 是独立的，所以每个 Subscriber 都会看到一份完整的数据流；
多个 Subscriber 之间互不影响各自的丢弃 / 背压策略。
2.3 本地 WAL 层（可选但推荐）
在广播到各个 Buffer 之前，引入本地 WAL（Write-Ahead Log）：

位置：RealtimeInstanceManager 内部，位于：

[ \text{DataCollector publish} \rightarrow \text{EventDataArrived handler} \rightarrow \underline{\text{WAL 写入}} \rightarrow \text{多 Buffer 广播} ]

存储介质：

使用 BadgerDB 或其他轻量 KV Store，按 (instance_id, sequence) 或 (global_unique_id) 作为 key。
功能：

进程崩溃前已经写入 WAL 但尚未被订阅者“确认处理”的数据，重启时可以回放到各自 Buffer 中；
通过一个简化的“处理确认标记”机制，实现“未确认记录留在 WAL，已确认记录可清理”。
3. 差异化 Buffer 策略设计
3.1 Buffer 策略类型
为每个 Subscriber / Consumer 配置一个 Buffer 策略（BufferPolicy），包含：

模式（Mode）

Blocking：PushBlocking，当缓冲区满时阻塞生产者；
NonBlockingDrop：Push 非阻塞，满了就丢弃数据；
（可扩展）NonBlockingFallback：满了先尝试降级策略，如写入专门的“丢弃日志”。
容量（Capacity）

不同 Subscriber 配不同的 Buffer 大小。
示例：

DB 写入 Subscriber：
Mode = Blocking；
Capacity = 50,000（可配置）；
前端推送 Subscriber：
Mode = NonBlockingDrop；
Capacity = 5,000。
3.2 推入逻辑
伪代码示意：

func (m *realtimeInstanceManagerImpl) broadcastToSubscribers(data interface{}) {
    for _, sub := range m.subscribers {
        switch sub.BufferPolicy.Mode {
        case Blocking:
            sub.Buffer.PushBlocking(data)
        case NonBlockingDrop:
            _ = sub.Buffer.Push(data) // false 时表示丢弃
        }
    }
}
注意：

对关键 Subscriber（DB）：当其 Buffer 满时，会阻塞 DataCollector → 等价为“以 DB 消费能力反压采集器”；
对非关键 Subscriber（前端）：即使 Buffer 满，DataCollector 仍继续工作，只是前端会丢一些点，但整体延迟低。
4. 本地 WAL 设计（进程内 at-least-once）
4.1 WAL 记录模型
WAL 主要需要承载：

数据内容：DataArrivedPayload.Data（可以是任意结构，建议序列化为 JSON 或 msgpack）；
唯一标识：
InstanceID：Workflow 实例 ID；
SequenceID 或 UniqueID：
可以由 RealtimeInstanceManager 维护一个单调递增的 int64 sequence；
或者使用 UUID/ULID。
订阅者处理状态（可选）：
每个 Subscriber 是否“已确认处理”；
简化起见，可以采用“记录级别的全局确认”：
当“所有需要强保证的 Subscriber 都确认处理成功”后，才标记该记录为“可删除”；
或只要 DB Subscriber 确认即可。
示例结构（逻辑）：

type WalRecord struct {
    InstanceID string      `json:"instance_id"`
    SequenceID int64       `json:"sequence_id"`
    Data       interface{} `json:"data"`
    // 可选：订阅者处理状态
}
4.2 写入路径
顺序要求（保证“先 WAL，再入 Buffer”）：

DataCollector 发布 EventDataArrived；
Handler 收到事件：
生成 SequenceID；
序列化 data + instanceID + sequenceID 为 WalRecord；
同步写入 WAL（BadgerDB Set，或 append-only log）；
写入成功后，对所有 Subscriber 执行 broadcastToSubscribers(data)。
这样可以保证：

进程崩溃时，只要 WAL 记录写成功，随后广播到 Buffer 的动作即使部分失败，也可以在重启后回放。
4.3 处理确认与清理
**关键订阅者（例如 DB）**在成功处理某条数据后，需要向 Realtime 框架回报一个“处理成功”的信号，以便清理 WAL：

Handler 端约定：通过 TaskContext 中的某个方法或 Hook 上报“确认”：
比如在 DataHandler 中调用 ctx.MarkProcessed(sequenceID) 或通过内部 API。
实现上：
runStreamProcessor 在调用 DataHandler 前，把 SequenceID 放入 ctx.Params["sequence_id"]；
Handler 成功返回时，在 runStreamProcessor 里代表“默认确认成功”；
若 Handler 返回失败，则：
可以选择不确认，让 WAL 保留，等待后续重试；
或按配置决定是否标记为“失败记录”，由专门的补偿任务处理。
WAL 清理策略：

定期后台任务扫描 WAL 记录：
对于“已确认”的记录进行删除；
对于“长期未确认”的记录，可以：
重试投递到对应 Subscriber 的 Buffer；
或打标为“死信”（dead-letter），单独持久化与报警。
4.4 重启恢复流程
进程重启时：

启动 RealtimeInstanceManager；
从 WAL 中加载该 Workflow 实例对应的所有“未确认”记录；
将这些记录按配置重新推回各个 Subscriber 的 Buffer：
推入时仍然遵循 Buffer 策略；
对于 NonBlockingDrop 类型的订阅者，可以选择只恢复给关键订阅者（如 DB），前端类订阅者可以忽略历史数据，保证启动快速和实时性。
5. 下游幂等性要求
由于 WAL 重试 / 失败重投会导致重复数据，所有关键下游 Handler 必须是幂等的。

5.1 唯一标识与去重
每条数据都应该带有一个可用于去重的 ID，可以是：

从远端数据源自带的 ID（如 trade_id / news_id 等）；
或由 Realtime 框架生成的 (InstanceID, SequenceID) 二元组；
或在业务侧生成一个高等价的 Unique Key（如 symbol@timestamp@seq）。
下游 Handler 的工作模式示例（DB 写入）：

从 ctx.Params["data"] 取出数据；
从 ctx.Params["sequence_id"] 或数据内部字段提取 UniqueID；
在 Redis / DB 中做去重：
Redis 里 SETNX recent_ids:<UniqueID> 1 EX 10；
或在 DB 里用 UPSERT / INSERT ... ON CONFLICT DO NOTHING；
只有在“未见过的 ID”时才实际写入。
5.2 时间窗口与存储成本
若使用 Redis 记最近 10 秒 / 1 分钟的 ID 集合，空间开销与 QPS 线性相关：
行情数据通常只需要保证“短时间内不重复展示 / 不重复写入”即可；
对于历史补数类场景，可以单独用 DB 层唯一索引做长周期幂等。
6. 对上层使用者的配置体验
6.1 单 Workflow 内多消费者广播
用户可以这样构建一个 Workflow：

一个 DataCollector：从远端行情源 / WebSocket / MQ 拉数据；
两个 Subscriber：
db_sink：写数据库；
frontend_sink：推前端。
示意（API 形态仅为说明，可以后续具体设计）：

// 1. 定义 DataCollector（略，沿用现有 Realtime DataCollector 接口）
// 2. 定义 DB 消费者
dbTask, _ := builder.NewRealtimeTaskBuilder("db_sink", "DB 写入", registry).
    WithContinuousMode().
    WithTaskType(realtime.TaskTypeStreamProcessor).
    WithJobFunction("writeToDB", nil).
    WithSubscriberName("db_sink").                // 绑定到 db_sink 订阅者
    WithBufferPolicyBlocking(50000).              // 大 Buffer + 阻塞
    Build()
// 3. 定义前端消费者
frontendTask, _ := builder.NewRealtimeTaskBuilder("frontend_sink", "前端推送", registry).
    WithContinuousMode().
    WithTaskType(realtime.TaskTypeStreamProcessor).
    WithJobFunction("pushToFrontend", nil).
    WithSubscriberName("frontend_sink").          // 绑定到 frontend_sink 订阅者
    WithBufferPolicyNonBlockingDrop(5000).        // 小 Buffer + 非阻塞丢弃
    Build()
// 4. 组装 Workflow
wf, _ := builder.NewWorkflowBuilder("realtime_market", "行情与新闻").
    WithStreamingMode().
    WithDataCollector("quote_ws", quoteCollector).
    WithRealtimeTask(collectorTask).
    WithRealtimeTask(dbTask).
    WithRealtimeTask(frontendTask).
    WithBroadcastEnabled(true).                   // 开启广播模式
    WithWalEnabled(true).                         // 开启 WAL
    Build()
注意：

对使用者来说，只需要关心：
给每个下游起一个 Subscriber 名；
配置 Buffer 策略（Blocking / NonBlockingDrop + 容量）；
打开/关闭 WAL；
广播、WAL、回放、幂等 ID 注入等细节由框架处理。
7. 风险与权衡
性能
WAL 为每条消息增加一次本地写操作（BadgerDB 写入）：
需要在实现中尽量批处理 / 异步（但要平衡“写前日志”的语义与吞吐量）；
多 Subscriber Buffer 的广播意味着数据会被复制 N 份，内存占用增加；
对于行情高 QPS 场景，需要合理设置：
订阅者数量；
Buffer 容量；
WAL 刷盘策略（同步 / 异步 / 批量）。
复杂度
实现 WAL + 恢复 + 确认机制，会显著增加 RealtimeInstanceManager 的复杂度；
需要良好的监控和可观测性（WAL 大小、积压条数、恢复进度等）。
一致性
不同 Subscriber 的 Buffer 策略不同，会导致：
DB 订阅者“尽量不丢、可能慢”；
前端订阅者“可能丢、基本实时”；
这在业务上通常是可接受甚至是期望的（强一致性要求留给 DB）。
8. 小结
本设计以 方案 A（框架内部广播 + 本地 WAL） 为基础：
在不引入外部 MQ 的前提下，为 Realtime Workflow 提供：
单实例多订阅者广播能力；
进程内 at-least-once 近似语义；
可配置的差异化缓冲策略。
对上层调用者而言：
可在 一个 Workflow 中自然地定义多个下游消费者，分别负责 DB 写入、前端推送、新闻展示等；
通过简单配置（Buffer 策略 + 是否启用 WAL + Subscriber 名）即可使用广播能力；
只需在 Handler 中实现幂等逻辑，即可在重复投递与重放情况下保持业务正确性。