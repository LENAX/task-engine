# 缺失功能清单

## 概述

本文档列出 task-engine 项目当前尚未实现的功能。核心引擎功能已完整实现，剩余功能主要是 API 层和运维支持。

---

## 已完成功能汇总

| 模块 | 功能 | 状态 |
|------|------|------|
| 核心引擎 | 声明式任务定义、DAG编排、并发调度 | ✅ 完成 |
| 生命周期 | Workflow 暂停/恢复/终止、断点恢复 | ✅ 完成 |
| 持久化 | JobFunction/TaskHandler 恢复 | ✅ 完成 |
| 事务 | SAGA 协调器、补偿逻辑执行 | ✅ 完成 |
| 定时调度 | CronScheduler、Cron表达式支持 | ✅ 完成 |
| 多数据库 | SQLite/MySQL/PostgreSQL | ✅ 完成 |
| 插件机制 | PluginManager、事件绑定 | ✅ 完成 |
| Builder模式 | WorkflowBuilder、TaskBuilder代码定义 | ✅ 完成 |
| Go SDK | 支持上层项目import导入使用 | ✅ 完成 |

---

## 待实现功能

### 1. HTTP API 服务

**状态**: 未实现

**目标**: 提供 RESTful API，支持 Workflow 的上传、查看、执行、进度查询等操作。

#### 项目结构

```
task-engine/
├── pkg/
│   └── api/                       # HTTP API层（新增）
│       ├── handler/               # 请求处理器
│       │   ├── workflow.go        # Workflow API
│       │   ├── instance.go        # Instance API
│       │   └── health.go          # 健康检查
│       ├── middleware/            # 中间件
│       │   ├── logging.go
│       │   └── recovery.go
│       ├── dto/                   # 数据传输对象
│       │   ├── request.go
│       │   └── response.go
│       ├── router.go              # 路由注册
│       └── server.go              # HTTP服务器
└── cmd/
    └── task-engine-server/        # Standalone程序
        └── main.go
```

#### API 设计

| 方法 | 路径 | 描述 |
|------|------|------|
| POST | `/api/v1/workflows` | 上传/保存Workflow定义(YAML) |
| GET | `/api/v1/workflows` | 列出所有Workflow |
| GET | `/api/v1/workflows/{id}` | 查看Workflow详情 |
| DELETE | `/api/v1/workflows/{id}` | 删除Workflow |
| POST | `/api/v1/workflows/{id}/execute` | 执行Workflow |
| GET | `/api/v1/workflows/{id}/history` | 查询Workflow执行历史 |
| GET | `/api/v1/instances` | 列出所有Instance |
| GET | `/api/v1/instances/{id}` | 查询执行进度/状态 |
| GET | `/api/v1/instances/{id}/tasks` | 查询任务详情 |
| POST | `/api/v1/instances/{id}/pause` | 暂停执行 |
| POST | `/api/v1/instances/{id}/resume` | 恢复执行 |
| POST | `/api/v1/instances/{id}/cancel` | 取消执行 |
| GET | `/health` | 健康检查 |

#### 执行历史查询参数

| 参数 | 类型 | 描述 |
|------|------|------|
| `status` | string | 按状态过滤（Success/Failed/Running/Paused） |
| `limit` | int | 返回记录数量限制，默认20 |
| `offset` | int | 分页偏移量，默认0 |
| `order` | string | 排序方式：`desc`（默认，最新优先）或`asc` |

#### 核心代码设计

```go
// pkg/api/server.go
type APIServer struct {
    engine *engine.Engine
    router *gin.Engine
    wsHub  *ws.Hub
    addr   string
}

func NewAPIServer(eng *engine.Engine, addr string) *APIServer
func (s *APIServer) Start() error
func (s *APIServer) Shutdown(ctx context.Context) error

// pkg/api/handler/workflow.go
type WorkflowHandler struct {
    engine *engine.Engine
}

func (h *WorkflowHandler) Upload(c *gin.Context)   // POST /workflows
func (h *WorkflowHandler) List(c *gin.Context)     // GET /workflows
func (h *WorkflowHandler) Get(c *gin.Context)      // GET /workflows/:id
func (h *WorkflowHandler) Delete(c *gin.Context)   // DELETE /workflows/:id
func (h *WorkflowHandler) Execute(c *gin.Context)  // POST /workflows/:id/execute
func (h *WorkflowHandler) History(c *gin.Context)  // GET /workflows/:id/history

// pkg/api/dto/response.go
type APIResponse[T any] struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
    Data    T      `json:"data,omitempty"`
}

type WorkflowSummary struct {
    ID          string    `json:"id"`
    Name        string    `json:"name"`
    Description string    `json:"description"`
    TaskCount   int       `json:"task_count"`
    CreatedAt   time.Time `json:"created_at"`
}

type InstanceDetail struct {
    ID         string       `json:"id"`
    WorkflowID string       `json:"workflow_id"`
    Status     string       `json:"status"`
    Progress   ProgressInfo `json:"progress"`
    StartedAt  time.Time    `json:"started_at"`
    FinishedAt *time.Time   `json:"finished_at,omitempty"`
}

type ProgressInfo struct {
    Total     int `json:"total"`
    Completed int `json:"completed"`
    Running   int `json:"running"`
    Failed    int `json:"failed"`
    Pending   int `json:"pending"`
}

// pkg/api/dto/request.go
type ExecuteWorkflowRequest struct {
    Params map[string]interface{} `json:"params" binding:"omitempty"`
}

type HistoryQueryRequest struct {
    Status string `form:"status" binding:"omitempty,oneof=Success Failed Running Paused"`
    Limit  int    `form:"limit" binding:"omitempty,min=1,max=100"`
    Offset int    `form:"offset" binding:"omitempty,min=0"`
    Order  string `form:"order" binding:"omitempty,oneof=asc desc"`
}

// pkg/api/dto/response.go - 执行历史响应
type HistoryResponse struct {
    Total   int               `json:"total"`    // 总记录数
    Items   []InstanceSummary `json:"items"`    // 执行历史列表
    HasMore bool              `json:"has_more"` // 是否有更多记录
}

type InstanceSummary struct {
    ID           string     `json:"id"`
    WorkflowID   string     `json:"workflow_id"`
    WorkflowName string     `json:"workflow_name"`
    Status       string     `json:"status"`
    StartedAt    time.Time  `json:"started_at"`
    FinishedAt   *time.Time `json:"finished_at,omitempty"`
    Duration     string     `json:"duration,omitempty"`      // 格式化的执行时长
    ErrorMessage string     `json:"error_message,omitempty"` // 失败时的错误信息
}
```

#### 技术选型

| 组件 | 选择 | 理由 |
|------|------|------|
| Web框架 | `gin-gonic/gin` | 高性能、内置参数校验、支持WebSocket |

**预计工时**: 2-3 天

---

### 2. CLI 命令行工具

**状态**: 未实现

**目标**: 提供命令行工具，支持本地开发和运维操作。

#### 命令结构

```
task-engine
├── workflow                       # Workflow管理
│   ├── upload <file>              # 上传Workflow定义
│   ├── list                       # 列出所有Workflow
│   ├── show <id>                  # 查看详情
│   ├── delete <id>                # 删除Workflow
│   └── execute <id>               # 执行Workflow
├── instance                       # Instance管理
│   ├── list [--status=...]        # 列出Instance
│   ├── status <id>                # 查询执行状态
│   ├── history <workflow-id>      # 查询Workflow执行历史
│   ├── logs <id>                  # 查看日志
│   ├── pause <id>                 # 暂停
│   ├── resume <id>                # 恢复
│   └── cancel <id>                # 取消
├── server                         # 服务管理
│   └── start [--port] [--config]  # 启动HTTP服务
└── version                        # 版本信息
```

#### 项目结构

```
task-engine/
├── pkg/
│   └── cli/                       # CLI层（新增）
│       ├── cmd/
│       │   ├── root.go            # 根命令
│       │   ├── workflow.go        # workflow子命令
│       │   ├── instance.go        # instance子命令
│       │   ├── server.go          # server子命令
│       │   └── version.go         # version命令
│       ├── taskengine/
│       │   └── taskengine.go      # TaskEngine客户端（封装HTTP API调用）
│       └── output/
│           ├── table.go           # 表格输出
│           └── json.go            # JSON输出
└── cmd/
    └── task-engine/               # CLI入口
        └── main.go
```

#### 使用示例

```bash
# 上传Workflow定义
$ task-engine workflow upload ./my-workflow.yaml
✅ Workflow上传成功: wf-abc123

# 列出所有Workflow
$ task-engine workflow list
ID          NAME              TASKS  CREATED
wf-abc123   数据同步工作流      5      2026-01-08 10:00:00

# 执行Workflow
$ task-engine workflow execute wf-abc123
✅ Instance ID: inst-xyz789

# 查询执行状态
$ task-engine instance status inst-xyz789
Instance: inst-xyz789
Status:   Running
Progress: 3/5 (60%)
Tasks:
  ✅ task-1  Success  0.5s
  ✅ task-2  Success  1.2s
  🔄 task-3  Running  2.3s
  ⏳ task-4  Pending
  ⏳ task-5  Pending

# 查询Workflow执行历史
$ task-engine instance history wf-abc123 --limit=5
INSTANCE_ID   STATUS    STARTED_AT           DURATION
inst-xyz789   Running   2026-01-08 10:30:00  2m30s
inst-xyz788   Success   2026-01-08 09:00:00  1m15s
inst-xyz787   Failed    2026-01-07 10:00:00  0m45s
inst-xyz786   Success   2026-01-06 10:00:00  1m20s
inst-xyz785   Success   2026-01-05 10:00:00  1m18s

# 按状态过滤执行历史
$ task-engine instance history wf-abc123 --status=Failed --limit=10
INSTANCE_ID   STATUS    STARTED_AT           ERROR
inst-xyz787   Failed    2026-01-07 10:00:00  数据库连接超时

# 启动HTTP服务
$ task-engine server start --port=8080 --config=./config.yaml
✅ Task Engine Server started on :8080
```

#### 技术选型

| 组件 | 选择 | 理由 |
|------|------|------|
| CLI框架 | `spf13/cobra` | Go生态最流行 |
| 表格输出 | `olekukonko/tablewriter` | 美观的表格输出 |

**预计工时**: 1-2 天

---

### 3. Builder模式代码定义

**状态**: 已完成（核心实现已存在于 `pkg/core/builder/`）

**目标**: 支持使用代码方式定义 Workflow 和 Task，与 YAML 配置方式等价。

#### TaskBuilder 使用示例

```go
import "github.com/stevelan1995/task-engine/pkg/core/builder"

// 方式1：使用JobFunction定义Task
task1, err := builder.NewTaskBuilder("数据提取", "从数据源提取原始数据").
    WithJobFunction("extract_data", map[string]interface{}{
        "source": "database",
        "table":  "users",
        "limit":  1000,
    }).
    WithTimeout(60).           // 超时时间60秒
    WithRetryCount(3).         // 失败重试3次
    WithRetryInterval(5).      // 重试间隔5秒
    Build()

// 方式2：带依赖关系的Task
task2, err := builder.NewTaskBuilder("数据转换", "转换数据格式").
    WithJobFunction("transform_data", nil).
    WithDependencies("数据提取").  // 依赖task1
    WithTimeout(120).
    Build()

// 方式3：带补偿逻辑的Task（SAGA事务）
task3, err := builder.NewTaskBuilder("数据写入", "写入目标数据库").
    WithJobFunction("write_data", nil).
    WithCompensation("rollback_write", nil).  // 补偿函数
    WithDependencies("数据转换").
    Build()
```

#### WorkflowBuilder 使用示例

```go
import "github.com/stevelan1995/task-engine/pkg/core/builder"

// 创建Workflow
wf, err := builder.NewWorkflowBuilder("数据同步工作流", "每日数据同步任务").
    WithCronExpr("0 0 2 * * *").     // 每天凌晨2点执行
    WithTask(task1).                  // 添加Task
    WithTask(task2).
    WithTask(task3).
    WithParams(map[string]string{     // 设置Workflow参数
        "env":    "production",
        "source": "mysql",
    }).
    Build()

// Build()会自动：
// 1. 校验Task名称唯一性
// 2. 根据Task声明的依赖名称解析依赖关系
// 3. 构建DAG并检测循环依赖
// 4. 校验Workflow合法性
```

#### 实时任务（Streaming模式）

```go
// 创建实时任务
rtTask, err := builder.NewRealtimeTaskBuilder("实时数据采集", "采集Kafka消息").
    WithContinuousMode().              // 持续运行模式
    WithBufferSize(1000).              // 缓冲区大小
    WithFlushInterval(time.Second).    // 刷新间隔
    WithJobFunction("kafka_consumer", map[string]interface{}{
        "topic": "events",
    }).
    Build()

// 创建流处理Workflow
streamWf, err := builder.NewWorkflowBuilder("实时处理流程", "实时数据处理").
    WithStreamingMode().               // 流处理模式
    WithRealtimeTask(rtTask).
    Build()
```

#### Builder模式 vs YAML配置对比

| 特性 | Builder模式 | YAML配置 |
|------|-------------|----------|
| 类型安全 | ✅ 编译期检查 | ❌ 运行时校验 |
| IDE支持 | ✅ 自动补全 | ❌ 无提示 |
| 动态构建 | ✅ 支持运行时构建 | ❌ 静态配置 |
| 可读性 | 代码形式 | 声明式配置 |
| 适用场景 | 程序化构建、SDK集成 | 静态配置、运维管理 |

**预计工时**: 已完成

---

### 4. WebSocket 实时状态推送

**状态**: 未实现

**目标**: 提供 WebSocket 接口，实时推送 Workflow/Task 执行状态变更。

#### 接口设计

```
WS /api/v1/ws/instances/{id}    # 订阅指定Instance的状态更新
WS /api/v1/ws/workflows/{id}    # 订阅指定Workflow所有Instance的状态
```

#### 消息格式

```go
// 状态更新消息
type StatusUpdateMessage struct {
    Type       string    `json:"type"`        // "instance_status" | "task_status"
    InstanceID string    `json:"instance_id"`
    TaskID     string    `json:"task_id,omitempty"`
    Status     string    `json:"status"`
    Progress   *Progress `json:"progress,omitempty"`
    Timestamp  time.Time `json:"timestamp"`
    Error      string    `json:"error,omitempty"`
}

type Progress struct {
    Total     int `json:"total"`
    Completed int `json:"completed"`
    Running   int `json:"running"`
    Failed    int `json:"failed"`
}
```

#### 核心代码设计

```go
// pkg/api/ws/hub.go
type Hub struct {
    clients    map[string]map[*Client]bool  // instanceID -> clients
    register   chan *ClientSubscription
    unregister chan *Client
    broadcast  chan *StatusUpdateMessage
}

func NewHub() *Hub
func (h *Hub) Run()
func (h *Hub) BroadcastToInstance(instanceID string, msg *StatusUpdateMessage)

// pkg/api/ws/client.go
type Client struct {
    hub        *Hub
    conn       *websocket.Conn
    instanceID string
    send       chan []byte
}

// pkg/api/handler/ws_handler.go
func (h *WSHandler) HandleConnection(w http.ResponseWriter, r *http.Request)
```

#### 集成方式

在 `WorkflowInstanceManager` 状态变更时，通过 `Hub.BroadcastToInstance()` 推送更新：

```go
// 在 instance_manager_v2.go 中
func (m *WorkflowInstanceManagerV2) updateTaskStatus(taskID, status string) {
    // ... 现有逻辑 ...
    
    // 推送WebSocket消息
    if m.wsHub != nil {
        m.wsHub.BroadcastToInstance(m.instance.ID, &StatusUpdateMessage{
            Type:       "task_status",
            InstanceID: m.instance.ID,
            TaskID:     taskID,
            Status:     status,
            Timestamp:  time.Now(),
        })
    }
}
```

#### 技术选型

使用 `gin` 框架内置的 WebSocket 支持（基于 `gorilla/websocket`）。

**预计工时**: 1-2 天

---

### 5. Go SDK导出方式

**状态**: 已完成（核心包已支持作为SDK导出）

**目标**: 支持上层项目通过 `go get` 方式引入 task-engine，直接使用核心功能。

#### 安装方式

```bash
go get github.com/stevelan1995/task-engine
```

#### 导出包结构

| 包路径 | 说明 | 导出内容 |
|--------|------|----------|
| `pkg/core/engine` | 核心引擎 | Engine, EngineBuilder |
| `pkg/core/builder` | 构建器 | WorkflowBuilder, TaskBuilder, RealtimeTaskBuilder |
| `pkg/core/workflow` | 工作流模型 | Workflow, WorkflowInstance, Task |
| `pkg/core/task` | 任务模型 | Task, TaskContext, FunctionRegistry |
| `pkg/config` | 配置加载 | WorkflowConfig, EngineConfig |
| `pkg/storage` | 存储接口 | Repository接口（实现在internal中） |
| `pkg/plugin` | 插件机制 | Plugin接口, PluginManager |

#### SDK使用示例

```go
package main

import (
    "context"
    "log"

    "github.com/stevelan1995/task-engine/pkg/core/engine"
    "github.com/stevelan1995/task-engine/pkg/core/builder"
)

func main() {
    ctx := context.Background()

    // 1. 创建Engine（使用配置文件）
    eng, err := engine.NewEngineBuilder("./configs/engine.yaml").
        WithJobFunc("my_extract", extractData).
        WithJobFunc("my_transform", transformData).
        WithJobFunc("my_load", loadData).
        WithService("db", dbClient).          // 注入依赖
        RestoreFunctionsOnStart().            // 自动恢复函数
        Build()
    if err != nil {
        log.Fatal(err)
    }

    // 2. 启动Engine
    eng.Start(ctx)
    defer eng.Stop()

    // 3. 使用Builder构建Workflow
    task1, _ := builder.NewTaskBuilder("提取数据", "从源系统提取").
        WithJobFunction("my_extract", nil).
        WithTimeout(60).
        Build()

    task2, _ := builder.NewTaskBuilder("转换数据", "数据清洗转换").
        WithJobFunction("my_transform", nil).
        WithDependencies("提取数据").
        Build()

    task3, _ := builder.NewTaskBuilder("加载数据", "写入目标系统").
        WithJobFunction("my_load", nil).
        WithDependencies("转换数据").
        Build()

    wf, _ := builder.NewWorkflowBuilder("ETL流程", "数据ETL").
        WithTask(task1).
        WithTask(task2).
        WithTask(task3).
        Build()

    // 4. 执行Workflow
    instance, err := eng.ExecuteWorkflow(ctx, wf)
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Instance started: %s", instance.ID)

    // 5. 等待执行完成
    eng.WaitForInstance(ctx, instance.ID)

    // 6. 查询执行历史
    history, _ := eng.GetWorkflowHistory(ctx, wf.ID, 10, 0)
    for _, inst := range history {
        log.Printf("Instance %s: %s", inst.ID, inst.Status)
    }
}

// 业务函数定义
func extractData(ctx *task.TaskContext) (interface{}, error) {
    // 实现数据提取逻辑
    return data, nil
}

func transformData(ctx *task.TaskContext) (interface{}, error) {
    // 获取上游任务结果
    input := ctx.GetUpstreamResult("提取数据")
    // 实现数据转换逻辑
    return transformed, nil
}

func loadData(ctx *task.TaskContext) (interface{}, error) {
    // 获取依赖服务
    db := ctx.GetDependency("db")
    // 实现数据加载逻辑
    return nil, nil
}
```

#### 与现有Engine方法对照

| SDK调用 | 对应Engine方法 |
|---------|----------------|
| `eng.ExecuteWorkflow(ctx, wf)` | 执行Workflow |
| `eng.PauseInstance(ctx, id)` | 暂停Instance |
| `eng.ResumeInstance(ctx, id)` | 恢复Instance |
| `eng.CancelInstance(ctx, id)` | 取消Instance |
| `eng.GetInstance(ctx, id)` | 获取Instance状态 |
| `eng.GetWorkflowHistory(ctx, wfID, limit, offset)` | 查询执行历史 |
| `eng.WaitForInstance(ctx, id)` | 等待Instance完成 |

**预计工时**: 已完成

---

### 6. Standalone 服务程序

**状态**: 未实现

**目标**: 提供独立运行的服务程序，集成 HTTP API 和 WebSocket。

#### 项目结构

```
task-engine/
├── cmd/
│   └── task-engine-server/        # Standalone服务
│       └── main.go
├── configs/
│   └── server.yaml                # 配置示例
└── deployments/
    ├── docker/
    │   └── Dockerfile
    └── systemd/
        └── task-engine.service
```

#### 配置文件

```yaml
# configs/server.yaml
server:
  host: "0.0.0.0"
  port: 8080
  read_timeout: 30s
  write_timeout: 30s

task-engine:
  storage:
    database:
      type: "sqlite"
      dsn: "./data/task-engine.db"
  execution:
    worker_concurrency: 20
    default_task_timeout: 60s

functions:
  builtin:
    - http_request
    - shell_command
```

#### 核心代码

```go
// cmd/task-engine-server/main.go
func main() {
    configPath := flag.String("config", "./configs/server.yaml", "配置文件")
    flag.Parse()

    // 1. 加载配置
    cfg := loadConfig(*configPath)
    
    // 2. 构建Engine
    eng, _ := engine.NewEngineBuilder(cfg.EngineConfig).
        WithBuiltinFunctions().
        RestoreFunctionsOnStart().
        Build()

    // 3. 启动Engine
    ctx := context.Background()
    eng.Start(ctx)

    // 4. 创建WebSocket Hub
    wsHub := ws.NewHub()
    go wsHub.Run()

    // 5. 创建并启动API Server
    apiServer := api.NewAPIServer(eng, wsHub, cfg.Server.Addr())
    go apiServer.Start()

    log.Printf("✅ Task Engine Server started on %s", cfg.Server.Addr())

    // 6. 优雅关闭
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    apiServer.Shutdown(ctx)
    eng.Stop()
}
```

**预计工时**: 1 天

---

## 开发计划

### Phase 1: API层实现（4-6天）

| 任务 | 预计工时 | 优先级 |
|------|----------|--------|
| HTTP API 基础实现 | 2-3天 | P0 |
| WebSocket 实时推送 | 1-2天 | P0 |
| CLI 工具实现 | 1-2天 | P1 |
| Standalone 程序 | 1天 | P1 |

### 依赖关系

```
HTTP API ──┬──> Standalone程序
           │
WebSocket ─┘
           │
CLI ───────┴──> (调用HTTP API)
```

---

## 总结

### 当前状态
- ✅ **核心引擎**: 100% 完成
- ✅ **持久化/恢复**: 100% 完成
- ✅ **SAGA事务**: 100% 完成
- ✅ **定时调度**: 100% 完成
- ✅ **多数据库**: 100% 完成
- ✅ **插件机制**: 100% 完成
- ✅ **Builder模式**: 100% 完成（代码方式定义Workflow/Task）
- ✅ **Go SDK**: 100% 完成（可作为库导出给上层项目）
- ❌ **HTTP API**: 未实现
- ❌ **CLI**: 未实现
- ❌ **WebSocket**: 未实现
- ❌ **Standalone**: 未实现

### 项目定位

| 模式 | 入口 | 适用场景 | 说明 |
|------|------|----------|------|
| **SDK模式** | `go get github.com/stevelan1995/task-engine` | 程序化集成 | 上层项目import后直接使用，支持Builder构建Workflow |
| **库模式** | `pkg/core/*` | 嵌入式使用 | 作为Go库嵌入到现有应用中 |
| **服务模式** | `cmd/task-engine-server` | 独立部署 | 作为独立服务运行，提供HTTP API |
| **CLI模式** | `cmd/task-engine` | 运维管理 | 命令行工具，调用HTTP API进行管理操作 |

#### SDK模式使用流程

```
上层项目
    │
    ├── go get github.com/stevelan1995/task-engine
    │
    ├── import "github.com/stevelan1995/task-engine/pkg/core/engine"
    │   import "github.com/stevelan1995/task-engine/pkg/core/builder"
    │
    ├── engine.NewEngineBuilder(...).Build()
    │
    ├── builder.NewWorkflowBuilder(...).Build()
    │
    └── eng.ExecuteWorkflow(ctx, wf)
```

---

*最后更新: 2026-01-08*
