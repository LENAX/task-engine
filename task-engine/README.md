# quant-task-engine
量化任务调度引擎 - 支持DAG编排、SAGA事务、定时调度、插件扩展的分布式任务引擎

## 快速上手

### 1. 安装依赖
```bash
go mod tidy
```

### 2. 运行服务端
```bash
# 方式1：直接运行
go run ./cmd/server

# 方式2：通过Makefile
make run-server
```

### 3. 运行CLI工具
```bash
# 查看帮助
go run ./cmd/cli

# 运行指定Workflow
make run-cli args="run wf-001"

# 查询实例状态
make run-cli args="query ins-001"
```

### 4. 构建二进制
```bash
# 构建服务端+CLI
make build

# 运行构建后的服务端
./bin/server
```

## 核心特性
- ✅ DAG任务编排：支持复杂依赖的任务流程
- ✅ SAGA事务：分布式事务补偿机制
- ✅ 定时调度：基于Cron表达式的任务触发
- ✅ 断点恢复：任务失败后从断点继续执行
- ✅ 插件扩展：支持邮件/短信告警等内置插件，可自定义扩展
- ✅ 多存储适配：支持SQLite/MySQL/PostgreSQL
- ✅ 优雅关闭：支持信号量触发的服务优雅退出

## 项目架构
详见 docs/design/architecture.md
