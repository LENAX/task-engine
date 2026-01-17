# Task Engine - 异步任务调度引擎

[![Go Version](https://img.shields.io/badge/Go-1.24.2-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

一个功能完整、高可用、易扩展的**通用异步任务调度引擎**，专为量化数据管理系统设计，支持声明式任务定义、DAG自动编排、并发调度、SAGA事务、定时调度等核心特性。

## 📋 目录

- [核心特性](#核心特性)
- [快速开始](#快速开始)
- [使用说明](#使用说明)
- [开发指南](#开发指南)
- [架构设计](#架构设计)
- [示例代码](#示例代码)
- [文档链接](#文档链接)
- [项目状态](#项目状态)

## ✨ 核心特性

### 核心功能（100% 完成）

- ✅ **声明式任务定义** - 基于Builder模式的链式API，简洁易用
- ✅ **DAG自动编排** - 自动解析任务依赖关系，支持动态子任务
- ✅ **并发调度** - 可配置的并发执行池，支持资源隔离
- ✅ **生命周期管控** - 支持启动/暂停/恢复/终止操作
- ✅ **断点恢复** - 系统重启后自动恢复未完成任务
- ✅ **优雅关闭** - 支持优雅关闭机制

### 扩展功能（100% 完成）

- ✅ **SAGA事务支持** - 分布式事务最终一致性保障
- ✅ **定时调度（Cron）** - 支持Crontab表达式定时触发
- ✅ **多数据库支持** - 兼容SQLite/PostgreSQL/MySQL
- ✅ **插件扩展机制** - 支持自定义插件（邮件通知等）
- ✅ **函数自动注册与恢复** - 系统重启后自动恢复函数实例
- ✅ **HTTP API** - 支持 RESTful API 和 CLI 两种模式

## 🚀 快速开始

### 环境要求

- Go 1.24.2 或更高版本
- 数据库（SQLite/PostgreSQL/MySQL，任选其一）

### 安装

```bash
# 克隆项目
git clone https://github.com/LENAX/task-engine.git
cd task-engine/task-engine

# 安装依赖
go mod download

# 构建 CLI 工具
go build -o bin/task-engine ./cmd/task-engine

# 构建 HTTP 服务器
go build -o bin/task-engine-server ./cmd/task-engine-server

# 或使用 Makefile（构建服务器）
make build-server
```

### 最小示例

```go
package main

import (
    "context"
    "log"
    
    "github.com/LENAX/task-engine/pkg/core/engine"
    "github.com/LENAX/task-engine/pkg/core/builder"
)

func main() {
    // 1. 创建引擎
    eng, err := engine.NewEngineBuilder("./configs/engine.yaml").
        WithJobFunc("myJob", func(ctx context.Context) error {
            log.Println("执行任务...")
            return nil
        }).
        Build()
    if err != nil {
        log.Fatal(err)
    }

    // 2. 启动引擎
    if err := eng.Start(context.Background()); err != nil {
        log.Fatal(err)
    }
    defer eng.Stop()

    // 3. 创建并提交Workflow
    wf, _ := builder.NewWorkflowBuilder("example", "示例工作流").
        WithTask(builder.NewTaskBuilder("task1", "任务1", eng.GetRegistry()).
            WithJobFunction("myJob", nil).
            Build()).
        Build()

    ctrl, _ := eng.SubmitWorkflow(context.Background(), wf)
    log.Printf("工作流已提交，实例ID: %s", ctrl.InstanceID())
}
```

### 快速使用

**方式一：使用 CLI 工具**

```bash
# 启动 HTTP API 服务（通过 CLI）
./bin/task-engine server start --port 8080 --config ./configs/engine.yaml

# 在另一个终端使用 CLI 管理 Workflow
./bin/task-engine workflow list
./bin/task-engine workflow execute <workflow-id>
```

**方式二：使用 HTTP API 服务器**

```bash
# 启动独立的 HTTP API 服务器
./bin/task-engine-server --config ./configs/engine.yaml --port 8080

# 使用 curl 或其他 HTTP 客户端调用 API
curl http://localhost:8080/api/v1/workflows
curl -X POST http://localhost:8080/api/v1/workflows/{id}/execute
```

**方式三：在代码中直接使用 SDK**

参考上面的"最小示例"，直接在 Go 代码中使用 Engine SDK。

## 📖 使用说明

### 1. 配置文件

创建配置文件 `configs/engine.yaml`：

```yaml
engine:
  max_concurrency: 100      # 最大并发数
  timeout_seconds: 30       # 默认超时时间（秒）

storage:
  type: "sqlite"            # 数据库类型: sqlite/postgres/mysql
  dsn: "./data.db"         # 数据库连接字符串
```

### 2. 创建引擎

```go
eng, err := engine.NewEngineBuilder("./configs/engine.yaml").
    // 注册Job函数
    WithJobFunc("job1", jobFunction1).
    WithJobFunc("job2", jobFunction2).
    // 注册回调函数
    WithCallbackFunc("onSuccess", successCallback).
    WithCallbackFunc("onFailed", failedCallback).
    // 注册服务依赖
    WithService("MyService", myServiceInstance).
    // 启动时自动恢复函数
    RestoreFunctionsOnStart().
    Build()
```

### 3. 定义Workflow

```go
// 创建Workflow Builder
wfBuilder := builder.NewWorkflowBuilder("workflow_id", "工作流名称")

// 创建Task
task1, _ := builder.NewTaskBuilder("task1", "任务1", eng.GetRegistry()).
    WithJobFunction("job1", map[string]interface{}{
        "param1": "value1",
    }).
    WithTimeout(60).           // 设置超时时间
    WithRetryCount(3).         // 设置重试次数
    Build()

task2, _ := builder.NewTaskBuilder("task2", "任务2", eng.GetRegistry()).
    WithJobFunction("job2", nil).
    WithDependency("task1").   // 依赖task1
    Build()

// 构建Workflow
wf, _ := wfBuilder.
    WithTask(task1).
    WithTask(task2).
    WithCronExpression("0 0 2 * * *").  // 定时调度（每天凌晨2点）
    Build()
```

### 4. 提交并控制Workflow

```go
// 提交Workflow，返回Controller
ctrl, err := eng.SubmitWorkflow(ctx, wf)
if err != nil {
    log.Fatal(err)
}

// 查询状态
status := ctrl.Status()
log.Printf("工作流状态: %s", status)

// 暂停工作流
if err := ctrl.Pause(); err != nil {
    log.Printf("暂停失败: %v", err)
}

// 恢复工作流
if err := ctrl.Resume(); err != nil {
    log.Printf("恢复失败: %v", err)
}

// 终止工作流
if err := ctrl.Terminate(); err != nil {
    log.Printf("终止失败: %v", err)
}
```

### 5. 使用插件

```go
// 创建邮件插件
emailPlugin := plugin.NewEmailPlugin()
emailPlugin.Init(map[string]string{
    "smtp_host": "smtp.example.com",
    "smtp_port": "587",
    "username":  "your_username",
    "password":  "your_password",
    "from":      "sender@example.com",
    "to":        "recipient@example.com",
})

// 在Engine中注册插件
eng, err := engine.NewEngineBuilder(configPath).
    WithPlugin(emailPlugin).
    WithPluginBinding(plugin.PluginBinding{
        PluginName: "email",
        Event:      plugin.EventWorkflowFailed,  // 工作流失败时触发
    }).
    Build()
```

### 6. 使用 CLI 工具

项目提供了功能完整的 CLI 工具，支持通过命令行管理 Workflow 和 Instance：

```bash
# 构建 CLI 工具
go build -o bin/task-engine ./cmd/task-engine

# 查看帮助
./bin/task-engine --help

# 列出所有 Workflow
./bin/task-engine workflow list

# 查看 Workflow 详情
./bin/task-engine workflow show <workflow-id>

# 上传 Workflow 定义文件
./bin/task-engine workflow upload <workflow.yaml>

# 执行 Workflow
./bin/task-engine workflow execute <workflow-id>

# 删除 Workflow
./bin/task-engine workflow delete <workflow-id>

# 列出所有 Instance
./bin/task-engine instance list

# 查看 Instance 状态
./bin/task-engine instance status <instance-id>

# 查看 Workflow 执行历史
./bin/task-engine instance history <workflow-id>

# 暂停 Instance
./bin/task-engine instance pause <instance-id>

# 恢复 Instance
./bin/task-engine instance resume <instance-id>

# 取消 Instance
./bin/task-engine instance cancel <instance-id>

# 启动 HTTP API 服务（通过 CLI）
./bin/task-engine server start --port 8080 --config ./configs/engine.yaml
```

### 7. 使用 HTTP API

项目提供了独立的 HTTP API 服务器，支持 RESTful API 接口：

```bash
# 构建 HTTP 服务器
go build -o bin/task-engine-server ./cmd/task-engine-server

# 启动服务器（默认端口 8080）
./bin/task-engine-server --config ./configs/engine.yaml --port 8080

# 或指定监听地址
./bin/task-engine-server --host 0.0.0.0 --port 8080 --config ./configs/engine.yaml
```

HTTP API 支持的主要接口：

**健康检查**
- `GET /health` - 健康检查
- `GET /ready` - 就绪检查

**Workflow 管理**
- `GET /api/v1/workflows` - 列出所有 Workflow
- `POST /api/v1/workflows` - 上传/创建 Workflow（通过 YAML）
- `GET /api/v1/workflows/{id}` - 获取 Workflow 详情
- `DELETE /api/v1/workflows/{id}` - 删除 Workflow
- `POST /api/v1/workflows/{id}/execute` - 执行 Workflow
- `GET /api/v1/workflows/{id}/history` - 获取 Workflow 执行历史

**Instance 管理**
- `GET /api/v1/instances` - 列出所有 Instance
- `GET /api/v1/instances/{id}` - 获取 Instance 详情
- `GET /api/v1/instances/{id}/tasks` - 获取 Instance 的任务列表
- `POST /api/v1/instances/{id}/pause` - 暂停 Instance
- `POST /api/v1/instances/{id}/resume` - 恢复 Instance
- `POST /api/v1/instances/{id}/cancel` - 取消 Instance

**API 响应格式**

所有 API 响应都遵循统一的格式：

```json
{
  "code": 200,
  "message": "success",
  "data": { ... }
}
```

错误响应：

```json
{
  "code": 400,
  "message": "错误描述",
  "data": null
}
```

### 8. SAGA事务

```go
// 创建带补偿函数的Task
task1, _ := builder.NewTaskBuilder("create_order", "创建订单", registry).
    WithJobFunction("create_order_func", nil).
    WithCompensationFunction("cancel_order_func").  // 补偿函数
    Build()

// 如果任务执行失败，系统会自动按反向顺序执行补偿函数
```

## 🛠️ 开发指南

### 项目结构

```
task-engine/
├── cmd/                    # 命令行入口
│   ├── task-engine/        # CLI工具
│   └── task-engine-server/ # HTTP服务器
├── pkg/                    # 对外暴露的核心包
│   ├── core/               # 核心引擎
│   │   ├── engine/         # 引擎核心
│   │   ├── workflow/       # 工作流定义
│   │   ├── task/           # 任务定义
│   │   ├── builder/        # Builder模式
│   │   ├── executor/       # 执行器
│   │   ├── saga/           # SAGA事务
│   │   └── dag/            # DAG编排
│   ├── storage/            # 存储接口
│   ├── plugin/             # 插件系统
│   ├── config/             # 配置管理
│   ├── api/                # HTTP API
│   └── cli/                # CLI 工具
├── internal/               # 内部实现
│   └── storage/            # 存储实现
├── examples/               # 示例代码
├── test/                   # 测试代码
├── configs/                # 配置文件
└── doc/                    # 文档
```

### 开发环境设置

```bash
# 1. 克隆项目
git clone https://github.com/LENAX/task-engine.git
cd task-engine/task-engine

# 2. 安装依赖
go mod download

# 3. 运行测试
make test

# 4. 查看测试覆盖率
make test-cover
```

### 编写Job函数

Job函数支持两种签名：

```go
// 方式1：使用 TaskContext（推荐）
func myJob(ctx *task.TaskContext) (interface{}, error) {
    // 从context获取依赖
    service, ok := ctx.GetDependency("MyService")
    if !ok {
        return nil, fmt.Errorf("服务未找到")
    }
    
    // 执行业务逻辑
    result := service.(*MyService).DoSomething()
    return result, nil
}

// 方式2：使用标准 context.Context（兼容旧代码）
func myJob(ctx context.Context) error {
    // 从context获取依赖
    service, ok := task.GetDependencyByKey(ctx, "MyService")
    if !ok {
        return fmt.Errorf("服务未找到")
    }
    
    // 执行业务逻辑
    service.(*MyService).DoSomething()
    return nil
}
```

### 编写插件

```go
// 实现 Plugin 接口
type MyPlugin struct {
    // 插件配置
}

func (p *MyPlugin) Name() string {
    return "my_plugin"
}

func (p *MyPlugin) Init(params map[string]string) error {
    // 初始化插件
    return nil
}

func (p *MyPlugin) Execute(data interface{}) error {
    // 执行插件逻辑
    return nil
}

// 注册插件
eng, err := engine.NewEngineBuilder(configPath).
    WithPlugin(&MyPlugin{}).
    Build()
```

### 运行测试

```bash
# 运行所有测试
make test

# 仅运行单元测试
make test-unit

# 仅运行集成测试
make test-integration

# 生成覆盖率报告
make test-cover
# 报告位置: bin/coverage.html
```

### 代码规范

- 遵循 Go 官方代码规范
- 使用接口实现依赖倒置（DIP）
- 核心组件采用接口化设计
- 完整的错误处理和日志记录
- 编写单元测试和集成测试

## 🏗️ 架构设计

### 整体架构

```
┌─────────────────────────────────────────────────────────┐
│                    用户侧 SDK                            │
│  WorkflowBuilder / TaskBuilder / EngineBuilder          │
└────────────────────┬────────────────────────────────────┘
                     │
┌────────────────────▼────────────────────────────────────┐
│                   核心引擎层                             │
│  Engine ──┬── DAG解析器                                 │
│           ├── SAGA事务协调器                            │
│           ├── 定时调度器（Cron）                        │
│           ├── 断点恢复管理器                            │
│           ├── PluginManager                             │
│           ├── JobFunctionRegistry                       │
│           └── WorkflowInstanceManager                   │
└────────────────────┬────────────────────────────────────┘
                     │
┌────────────────────▼────────────────────────────────────┐
│                    执行层                                │
│  Executor（并发执行池）                                  │
│    ├── SAGA补偿执行器                                   │
│    ├── 状态回调处理器                                    │
│    └── 动态子任务生成器                                  │
└────────────────────┬────────────────────────────────────┘
                     │
┌────────────────────▼────────────────────────────────────┐
│                    存储层                                │
│  Repository（多数据库适配）                              │
│    ├── SQLite / PostgreSQL / MySQL                      │
│    ├── Workflow定义存储                                  │
│    └── WorkflowInstance历史存储                         │
└─────────────────────────────────────────────────────────┘
```

### 核心概念

- **Workflow（工作流模板）**：任务流的静态定义，可重复实例化
- **WorkflowInstance（工作流实例）**：基于模板创建的实际运行实例
- **Task（任务）**：最小执行单元，包含业务逻辑和配置
- **DAG（有向无环图）**：任务依赖关系的图形表示
- **SAGA事务**：分布式事务的最终一致性保障机制

### 设计原则

- **依赖倒置原则（DIP）**：核心组件通过接口实现松耦合
- **单一职责原则（SRP）**：每个模块职责明确
- **开闭原则（OCP）**：对扩展开放，对修改关闭
- **接口隔离原则（ISP）**：接口设计精简，避免臃肿

## 💡 示例代码

### 基础示例

完整示例请参考：[examples/example_workflow/main.go](task-engine/examples/example_workflow/main.go)

### 插件示例

完整示例请参考：[examples/plugin_email_example/main.go](task-engine/examples/plugin_email_example/main.go)

### 函数恢复示例

完整示例请参考：[examples/function_restore_example/main.go](task-engine/examples/function_restore_example/main.go)

## 📚 文档链接

### 设计文档

- [量化数据管理系统异步任务调度引擎设计文档](doc/量化数据管理系统异步任务调度引擎设计文档.md) - 完整的设计文档
- [异步任务调度引擎详细设计](doc/dev/异步任务调度引擎详细设计.md) - 核心特性详细设计
- [项目进展总结](doc/项目进展总结.md) - 项目当前状态和完成度

### 开发文档

- [动态添加子任务的设计](doc/dev/动态添加子任务的设计.md)
- [任务函数保存与加载特性的设计](doc/dev/任务函数保存与加载特性的设计.md)
- [配置驱动特性设计](doc/dev/配置驱动特性设计.md)

### 分析文档

- [可用任务获取优化实现总结](doc/analysis/可用任务获取优化实现总结.md)
- [测试修复总结](task-engine/doc/analysis/test_fix_summary.md)

## 📊 项目状态

### 完成度

| 功能模块 | 状态 | 完成度 |
|---------|------|--------|
| 核心功能 | ✅ 完成 | 100% |
| 扩展功能 | ✅ 完成 | 100% (6/6) |
| HTTP API | ✅ 完成 | 100% |

### 核心功能（100%）

- ✅ 声明式任务定义
- ✅ DAG自动编排
- ✅ 并发调度
- ✅ Workflow生命周期管控
- ✅ 断点恢复
- ✅ 优雅关闭

### 扩展功能（100%）

- ✅ SAGA事务支持
- ✅ 定时调度（Cron）
- ✅ 多数据库支持
- ✅ 插件扩展机制
- ✅ 函数自动注册与恢复
- ✅ HTTP API 和 CLI 工具

### 代码质量

- ✅ 接口化设计，符合SOLID原则
- ✅ 完整的错误处理
- ✅ 详细的日志记录
- ✅ 完善的测试覆盖
- ✅ 详细的代码注释

## 🤝 贡献指南

欢迎贡献代码！请遵循以下步骤：

1. Fork 本项目
2. 创建特性分支 (`git checkout -b feature/AmazingFeature`)
3. 提交更改 (`git commit -m 'Add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 开启 Pull Request

### 开发规范

- 遵循 Go 官方代码规范
- 编写单元测试和集成测试
- 更新相关文档
- 确保所有测试通过

## 📝 许可证

本项目采用 MIT 许可证 - 查看 [LICENSE](LICENSE) 文件了解详情

## 🙏 致谢

感谢所有为本项目做出贡献的开发者！

---

**最后更新**: 2026-01-06  
**项目状态**: 生产就绪（核心功能完整，扩展功能丰富）
