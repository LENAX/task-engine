#!/bin/bash
set -e

# 项目名称（固定为task-engine）
PROJECT_NAME="task-engine"
# 默认模块路径（可根据需要修改）
DEFAULT_MODULE="github.com/stevelan1995/task-engine"

# 1. 获取用户输入的模块路径
read -p "请输入Go模块路径（默认: $DEFAULT_MODULE）: " MODULE_PATH
MODULE_PATH=${MODULE_PATH:-$DEFAULT_MODULE}

# 2. 创建项目根目录并进入
echo "🔧 正在创建项目目录: $PROJECT_NAME"
rm -rf "$PROJECT_NAME" # 清理已存在的同名目录（可选）
mkdir -p "$PROJECT_NAME"
cd "$PROJECT_NAME" || exit 1

# 3. 初始化go.mod
echo "🔧 初始化Go模块: $MODULE_PATH"
go mod init "$MODULE_PATH"

# 4. 定义所有需要创建的目录（按你提供的结构）
DIRECTORIES=(
    "cmd/server"
    "cmd/cli"
    "pkg/model"
    "pkg/config"
    "pkg/common/constant"
    "pkg/common/util"
    "pkg/common/errors"
    "pkg/common/context"
    "pkg/repository/interface"
    "pkg/repository/sqlite"
    "pkg/repository/mysql"
    "pkg/repository/postgres"
    "pkg/builder"
    "pkg/controller"
    "pkg/job"
    "pkg/plugin/interface"
    "pkg/plugin/builtin"
    "pkg/saga"
    "pkg/cron"
    "pkg/dag"
    "pkg/engine"
    "pkg/executor"
    "api/handler"
    "api/router"
    "api/dto"
    "api/response"
    "test/unit"
    "test/integration"
    "test/e2e"
    "test/mock"
    "scripts/sql"
    "configs"
    "docs/design"
    "docs/api"
)

# 5. 创建所有目录
echo "🔧 创建项目目录结构..."
for dir in "${DIRECTORIES[@]}"; do
    mkdir -p "$dir"
done

# 6. 生成核心可运行的main.go文件（cmd/server/main.go）
echo "🔧 生成核心入口文件: cmd/server/main.go"
cat > cmd/server/main.go << EOF
package main

import (
    "context"
    "fmt"
    "log"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "$MODULE_PATH/api/router"
    "$MODULE_PATH/pkg/config"
    "$MODULE_PATH/pkg/engine"
)

// 程序入口：初始化引擎 + 启动HTTP服务
func main() {
    // 1. 初始化配置
    cfg, err := config.Load("configs/engine.yaml")
    if err != nil {
        log.Fatalf("❌ 加载配置失败: %v", err)
    }
    log.Printf("✅ 配置加载成功 | 引擎模式: %s", cfg.Mode)

    // 2. 初始化调度引擎
    eng, err := engine.NewEngine(cfg)
    if err != nil {
        log.Fatalf("❌ 引擎初始化失败: %v", err)
    }
    log.Println("✅ 量化任务引擎初始化完成")

    // 3. 初始化HTTP路由
    r := router.InitRouter(eng)

    // 4. 启动HTTP服务
    server := &http.Server{
        Addr:    fmt.Sprintf(":%d", cfg.HTTPPort),
        Handler: r,
    }

    // 异步启动服务
    go func() {
        log.Printf("✅ HTTP服务启动成功 | 地址: http://localhost:%d", cfg.HTTPPort)
        if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            log.Fatalf("❌ HTTP服务启动失败: %v", err)
        }
    }()

    // 5. 优雅关闭
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    log.Println("\n🔴 开始优雅关闭服务...")

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    if err := server.Shutdown(ctx); err != nil {
        log.Fatalf("❌ 服务关闭失败: %v", err)
    }

    // 关闭引擎
    eng.Stop()
    log.Println("✅ 量化任务引擎已优雅关闭")
}
EOF

# 7. 生成cmd/server/bootstrap.go（空文件+基础注释）
cat > cmd/server/bootstrap.go << EOF
package main

// Bootstrap 引擎启动引导
// 负责初始化存储、插件、Job注册中心等核心组件
func Bootstrap() error {
    // TODO: 实现初始化逻辑
    return nil
}
EOF

# 8. 生成cmd/cli/main.go（基础CLI入口）
cat > cmd/cli/main.go << EOF
package main

import (
    "fmt"
    "os"

    "$MODULE_PATH/pkg/engine"
    "$MODULE_PATH/pkg/config"
)

// CLI工具入口：支持手动触发/查询Workflow
func main() {
    if len(os.Args) < 2 {
        fmt.Println("使用说明:")
        fmt.Println("  cli run <workflow-id>   - 运行指定Workflow实例")
        fmt.Println("  cli query <instance-id> - 查询实例状态")
        fmt.Println("  cli stop <instance-id>  - 停止实例")
        os.Exit(1)
    }

    // 初始化配置和引擎
    cfg, _ := config.Load("configs/engine.yaml")
    eng, _ := engine.NewEngine(cfg)

    // 解析命令
    cmd := os.Args[1]
    switch cmd {
    case "run":
        if len(os.Args) < 3 {
            fmt.Println("请指定Workflow ID")
            os.Exit(1)
        }
        fmt.Printf("开始运行Workflow: %s\n", os.Args[2])
        // TODO: 实现运行逻辑
    case "query":
        if len(os.Args) < 3 {
            fmt.Println("请指定实例ID")
            os.Exit(1)
        }
        fmt.Printf("查询实例状态: %s\n", os.Args[2])
        // TODO: 实现查询逻辑
    case "stop":
        if len(os.Args) < 3 {
            fmt.Println("请指定实例ID")
            os.Exit(1)
        }
        fmt.Printf("停止实例: %s\n", os.Args[2])
        // TODO: 实现停止逻辑
    default:
        fmt.Printf("未知命令: %s\n", cmd)
        os.Exit(1)
    }
}
EOF

# 9. 生成所有空白文件（按你提供的结构）
echo "🔧 创建空白业务文件..."
BLANK_FILES=(
    # pkg/model
    "pkg/model/workflow.go"
    "pkg/model/workflow_instance.go"
    "pkg/model/task.go"
    "pkg/model/task_instance.go"
    "pkg/model/job_function.go"
    "pkg/model/saga.go"
    "pkg/model/plugin.go"
    # pkg/config
    "pkg/config/config.go"
    "pkg/config/loader.go"
    "pkg/config/validator.go"
    # pkg/common/constant
    "pkg/common/constant/status.go"
    "pkg/common/constant/error_code.go"
    "pkg/common/constant/default.go"
    # pkg/common/util
    "pkg/common/util/uuid.go"
    "pkg/common/util/time.go"
    "pkg/common/util/json.go"
    "pkg/common/util/logger.go"
    # pkg/common/errors
    "pkg/common/errors/business_error.go"
    # pkg/common/context
    "pkg/common/context/engine_context.go"
    # pkg/repository/interface
    "pkg/repository/interface/workflow_repo.go"
    "pkg/repository/interface/instance_repo.go"
    "pkg/repository/interface/task_repo.go"
    "pkg/repository/interface/job_func_repo.go"
    # pkg/repository/sqlite
    "pkg/repository/sqlite/workflow_sqlite.go"
    "pkg/repository/sqlite/instance_sqlite.go"
    "pkg/repository/sqlite/task_sqlite.go"
    "pkg/repository/sqlite/job_func_sqlite.go"
    # pkg/repository/mysql
    "pkg/repository/mysql/workflow_mysql.go"
    "pkg/repository/mysql/instance_mysql.go"
    "pkg/repository/mysql/task_sqlite.go"
    "pkg/repository/mysql/job_func_mysql.go"
    # pkg/repository/postgres
    "pkg/repository/postgres/workflow_postgres.go"
    "pkg/repository/postgres/instance_postgres.go"
    "pkg/repository/postgres/task_postgres.go"
    "pkg/repository/postgres/job_func_postgres.go"
    # pkg/repository
    "pkg/repository/factory.go"
    # pkg/builder
    "pkg/builder/workflow_builder.go"
    "pkg/builder/task_builder.go"
    "pkg/builder/instance_builder.go"
    # pkg/controller
    "pkg/controller/workflow_controller.go"
    "pkg/controller/instance_manager.go"
    # pkg/job
    "pkg/job/registry.go"
    "pkg/job/registry_memory.go"
    "pkg/job/serializer.go"
    # pkg/plugin/interface
    "pkg/plugin/interface/plugin.go"
    # pkg/plugin
    "pkg/plugin/manager.go"
    "pkg/plugin/loader.go"
    # pkg/plugin/builtin
    "pkg/plugin/builtin/email_alert.go"
    "pkg/plugin/builtin/sms_alert.go"
    "pkg/plugin/builtin/log_plugin.go"
    # pkg/saga
    "pkg/saga/coordinator.go"
    "pkg/saga/compensation.go"
    "pkg/saga/manager.go"
    # pkg/cron
    "pkg/cron/scheduler.go"
    "pkg/cron/parser.go"
    "pkg/cron/manager.go"
    # pkg/dag
    "pkg/dag/parser.go"
    "pkg/dag/validator.go"
    "pkg/dag/rearrange.go"
    # pkg/engine
    "pkg/engine/engine.go"
    "pkg/engine/dispatcher.go"
    "pkg/engine/state_sync.go"
    "pkg/engine/breakpoint.go"
    "pkg/engine/instance_manager.go"
    # pkg/executor
    "pkg/executor/pool.go"
    "pkg/executor/task_executor.go"
    "pkg/executor/callback.go"
    "pkg/executor/subtask.go"
    # api/handler
    "api/handler/workflow_handler.go"
    "api/handler/instance_handler.go"
    "api/handler/task_handler.go"
    "api/handler/plugin_handler.go"
    # api/router
    "api/router/router.go"
    "api/router/middleware.go"
    # api/dto
    "api/dto/workflow_dto.go"
    "api/dto/instance_dto.go"
    "api/dto/task_dto.go"
    # api/response
    "api/response/response.go"
    # test/unit
    "test/unit/builder_test.go"
    "test/unit/dag_test.go"
    "test/unit/job_registry_test.go"
    "test/unit/plugin_test.go"
    # test/integration
    "test/integration/engine_executor_test.go"
    "test/integration/repository_test.go"
    "test/integration/api_test.go"
    # test/e2e
    "test/e2e/full_flow_test.go"
    # test/mock
    "test/mock/mock_repository.go"
    "test/mock/mock_plugin.go"
    # scripts/sql
    "scripts/sql/sqlite_schema.sql"
    "scripts/sql/mysql_schema.sql"
    "scripts/sql/postgres_schema.sql"
    # scripts
    "scripts/build.sh"
    "scripts/deploy.sh"
    "scripts/test.sh"
    # configs
    "configs/engine.yaml"
    "configs/engine.toml"
    "configs/engine.json"
    # docs/design
    "docs/design/architecture.md"
    "docs/design/module_design.md"
    "docs/design/db_design.md"
    # docs/api
    "docs/api/swagger.json"
    # docs
    "docs/user_guide.md"
    "docs/dev_guide.md"
)

for file in "${BLANK_FILES[@]}"; do
    touch "$file"
    # 给空白Go文件添加基础包声明（提升易用性）
    if [[ $file == *.go ]]; then
        # 提取包名（最后一级目录）
        pkg_name=$(basename "$(dirname "$file")")
        echo "package $pkg_name" > "$file"
    fi
done

# 10. 生成基础配置文件
echo "🔧 生成基础配置文件..."
# configs/engine.yaml（示例配置）
cat > configs/engine.yaml << EOF
# 量化任务引擎配置
mode: "dev"        # 运行模式：dev/test/prod
http_port: 8080    # HTTP服务端口
database:
  type: "sqlite"   # 数据库类型：sqlite/mysql/postgres
  path: "./data.db" # SQLite路径
  host: "localhost"
  port: 3306
  user: "root"
  password: "123456"
  dbname: "quant_task"
engine:
  max_concurrency: 100 # 最大并发任务数
  timeout: 30s         # 任务超时时间
  breakpoint: true     # 是否开启断点恢复
plugin:
  builtin:
    email_alert: true
    sms_alert: false
EOF

# 11. 生成.gitignore（Go项目通用）
cat > .gitignore << EOF
# Binaries for programs and plugins
*.exe
*.exe~
*.dll
*.so
*.dylib
quant-task-engine
cmd/server/server
cmd/cli/cli

# Test binary, built with 'go test -c'
*.test

# Output of the go coverage tool, specifically when used with LiteIDE
*.out

# Dependency directories (remove the comment below to include it)
# vendor/

# Go module files
# go.mod
# go.sum

# IDE-specific files
.idea/
.vscode/
*.swp
*.swo
.DS_Store

# Data files
*.db
data/
logs/

# Config files (本地配置，不上传)
configs/local.yaml
EOF

# 12. 生成Makefile（简化构建/运行/测试命令）
cat > Makefile << EOF
# 量化任务引擎Makefile
MODULE := $MODULE_PATH
BINARY_SERVER := bin/server
BINARY_CLI := bin/cli

# 构建目录
mkdir -p bin

# 构建服务端
build-server:
	go build -o \$(BINARY_SERVER) ./cmd/server

# 构建CLI
build-cli:
	go build -o \$(BINARY_CLI) ./cmd/cli

# 构建所有
build: build-server build-cli

# 运行服务端
run-server:
	go run ./cmd/server

# 运行CLI（示例：make run-cli args="run wf-001"）
run-cli:
	go run ./cmd/cli \$(args)

# 单元测试
test-unit:
	go test -v ./test/unit/...

# 集成测试
test-integration:
	go test -v ./test/integration/...

# 清理构建产物
clean:
	rm -rf bin/

.PHONY: build-server build-cli build run-server run-cli test-unit test-integration clean
EOF

# 13. 生成README.md（项目说明）
cat > README.md << EOF
# quant-task-engine
量化任务调度引擎 - 支持DAG编排、SAGA事务、定时调度、插件扩展的分布式任务引擎

## 快速上手

### 1. 安装依赖
\`\`\`bash
go mod tidy
\`\`\`

### 2. 运行服务端
\`\`\`bash
# 方式1：直接运行
go run ./cmd/server

# 方式2：通过Makefile
make run-server
\`\`\`

### 3. 运行CLI工具
\`\`\`bash
# 查看帮助
go run ./cmd/cli

# 运行指定Workflow
make run-cli args="run wf-001"

# 查询实例状态
make run-cli args="query ins-001"
\`\`\`

### 4. 构建二进制
\`\`\`bash
# 构建服务端+CLI
make build

# 运行构建后的服务端
./bin/server
\`\`\`

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
EOF

# 14. 生成基础的配置加载和引擎基础文件（保证main.go能编译）
cat > pkg/config/config.go << EOF
package config

// Config 引擎核心配置
type Config struct {
    Mode     string \`yaml:"mode"\`
    HTTPPort int    \`yaml:"http_port"\`
    Database struct {
        Type     string \`yaml:"type"\`
        Path     string \`yaml:"path"\`
        Host     string \`yaml:"host"\`
        Port     int    \`yaml:"port"\`
        User     string \`yaml:"user"\`
        Password string \`yaml:"password"\`
        DBName   string \`yaml:"dbname"\`
    } \`yaml:"database"\`
    Engine struct {
        MaxConcurrency int        \`yaml:"max_concurrency"\`
        Timeout        string     \`yaml:"timeout"\`
        Breakpoint     bool       \`yaml:"breakpoint"\`
    } \`yaml:"engine"\`
    Plugin struct {
        Builtin struct {
            EmailAlert bool \`yaml:"email_alert"\`
            SMSAlert   bool \`yaml:"sms_alert"\`
        } \`yaml:"builtin"\`
    } \`yaml:"plugin"\`
}
EOF

cat > pkg/config/loader.go << EOF
package config

import (
    "os"

    "gopkg.in/yaml.v3"
)

// Load 加载配置文件
func Load(path string) (*Config, error) {
    // 读取配置文件
    data, err := os.ReadFile(path)
    if err != nil {
        // 若文件不存在，返回默认配置
        return &Config{
            Mode:     "dev",
            HTTPPort: 8080,
        }, nil
    }

    // 解析YAML
    var cfg Config
    if err := yaml.Unmarshal(data, &cfg); err != nil {
        return nil, err
    }

    return &cfg, nil
}
EOF

cat > pkg/engine/engine.go << EOF
package engine

import (
    "$MODULE_PATH/pkg/config"
)

// Engine 调度引擎核心结构体
type Engine struct {
    cfg *config.Config
}

// NewEngine 创建引擎实例
func NewEngine(cfg *config.Config) (*Engine, error) {
    return &Engine{
        cfg: cfg,
    }, nil
}

// Stop 停止引擎
func (e *Engine) Stop() {
    // TODO: 实现引擎停止逻辑（关闭存储连接、停止定时任务等）
}
EOF

cat > api/router/router.go << EOF
package router

import (
    "net/http"

    "$MODULE_PATH/pkg/engine"
)

// InitRouter 初始化HTTP路由
func InitRouter(eng *engine.Engine) *http.ServeMux {
    mux := http.NewServeMux()

    // 健康检查接口
    mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte("{\"code\":0,\"message\":\"success\",\"data\":\"engine is running\"}"))
    })

    // TODO: 注册其他业务接口
    return mux
}
EOF

# 15. 安装必要依赖（保证编译通过）
echo "🔧 安装基础依赖..."
go get gopkg.in/yaml.v3

# 16. 完成提示
echo -e "\n🎉 quant-task-engine 项目初始化完成！"
echo "📁 项目路径: $(pwd)"
echo -e "\n🚀 快速运行命令："
echo "  cd $PROJECT_NAME"
echo "  go mod tidy"
echo "  make run-server"
echo -e "\n✅ 访问健康检查接口：http://localhost:8080/health"