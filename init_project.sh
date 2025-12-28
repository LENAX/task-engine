#!/bin/bash
set -e

# 替换为你的模块名
PROJECT_NAME="task-engine"
MODULE_PATH="github.com/stevelan1995/task-engine"

# 清理旧目录
rm -rf $PROJECT_NAME
mkdir -p $PROJECT_NAME && cd $PROJECT_NAME

# 初始化go.mod
go mod init $MODULE_PATH

# ===================== 创建极简目录结构 =====================
# 1. cmd入口
mkdir -p cmd/server cmd/cli

# 2. pkg对外核心（仅core/storage/plugin）
mkdir -p pkg/core/{engine,workflow,task,job,builder,saga,executor}
mkdir -p pkg/storage pkg/plugin

# 3. internal私有（砍掉http，仅保留storage/plugin/common）
mkdir -p internal/storage/{sqlite,mysql}
mkdir -p internal/plugin internal/common

# 4. 辅助目录（简化）
mkdir -p configs scripts test

# ===================== 生成pkg对外核心组件（仅保留要求的） =====================
# pkg/core/engine/engine.go（核心Engine，对外导出）
cat > pkg/core/engine/engine.go << EOF
package engine

import (
    "$MODULE_PATH/pkg/core/workflow"
    "$MODULE_PATH/pkg/core/executor"
    "$MODULE_PATH/pkg/storage"
    "context"
    "log"
)

// Engine 调度引擎核心结构体（对外导出）
type Engine struct {
    executor      *executor.Executor
    workflowRepo  storage.WorkflowRepository
    running       bool
    MaxConcurrency int
    Timeout       int
}

// NewEngine 创建Engine实例（对外导出的工厂方法）
func NewEngine(maxConcurrency, timeout int, repo storage.WorkflowRepository) (*Engine, error) {
    exec, err := executor.NewExecutor(maxConcurrency)
    if err != nil {
        return nil, err
    }
    return &Engine{
        executor:      exec,
        workflowRepo:  repo,
        MaxConcurrency: maxConcurrency,
        Timeout:       timeout,
        running:       false,
    }, nil
}

// Start 启动引擎（对外导出）
func (e *Engine) Start(ctx context.Context) error {
    if e.running {
        return nil
    }
    e.running = true
    log.Println("✅ 量化任务引擎已启动")
    return nil
}

// Stop 停止引擎（对外导出）
func (e *Engine) Stop() {
    if !e.running {
        return
    }
    e.running = false
    e.executor.Shutdown()
    log.Println("✅ 量化任务引擎已停止")
}

// RegisterWorkflow 注册Workflow到引擎（对外导出）
func (e *Engine) RegisterWorkflow(ctx context.Context, wf *workflow.Workflow) error {
    if !e.running {
        return logError("engine_not_running", "引擎未启动")
    }
    if err := e.workflowRepo.Save(ctx, wf); err != nil {
        return err
    }
    log.Printf("✅ 注册Workflow成功：%s", wf.ID)
    return nil
}

// 内部辅助函数（小写，不导出）
func logError(code, msg string) error {
    return fmt.Errorf("%s: %s", code, msg)
}
EOF

# pkg/core/workflow/workflow.go（Workflow核心，对外导出）
cat > pkg/core/workflow/workflow.go << EOF
package workflow

import (
    "time"
    "github.com/google/uuid"
)

// Workflow Workflow核心结构体（对外导出）
type Workflow struct {
    ID          string            \`json:"id"\`
    Name        string            \`json:"name"\`
    Description string            \`json:"description"\`
    Params      map[string]string \`json:"params"\`
    CreateTime  time.Time         \`json:"create_time"\`
    Status      string            \`json:"status"\` // ENABLED/DISABLED
}

// WorkflowInstance Workflow实例（对外导出）
type WorkflowInstance struct {
    ID         string    \`json:"instance_id"\`
    WorkflowID string    \`json:"workflow_id"\`
    Status     string    \`json:"status"\` // RUNNING/SUCCESS/FAILED
    StartTime  time.Time \`json:"start_time"\`
    EndTime    time.Time \`json:"end_time"\`
}

// NewWorkflow 创建Workflow实例（对外导出）
func NewWorkflow(name, desc string) *Workflow {
    return &Workflow{
        ID:          uuid.NewString(),
        Name:        name,
        Description: desc,
        Status:      "ENABLED",
        CreateTime:  time.Now(),
    }
}

// Run 运行Workflow（对外导出）
func (w *Workflow) Run() (*WorkflowInstance, error) {
    instance := &WorkflowInstance{
        ID:         uuid.NewString(),
        WorkflowID: w.ID,
        Status:     "RUNNING",
        StartTime:  time.Now(),
    }
    return instance, nil
}
EOF

# pkg/core/builder/workflow_builder.go（WorkflowBuilder，对外导出）
cat > pkg/core/builder/workflow_builder.go << EOF
package builder

import (
    "$MODULE_PATH/pkg/core/workflow"
)

// WorkflowBuilder Workflow构建器（对外导出）
type WorkflowBuilder struct {
    wf *workflow.Workflow
}

// NewWorkflowBuilder 创建构建器（对外导出）
func NewWorkflowBuilder(name, desc string) *WorkflowBuilder {
    return &WorkflowBuilder{
        wf: workflow.NewWorkflow(name, desc),
    }
}

// WithParams 设置自定义参数（链式构建，对外导出）
func (b *WorkflowBuilder) WithParams(params map[string]string) *WorkflowBuilder {
    b.wf.Params = params
    return b
}

// Build 构建Workflow实例（对外导出）
func (b *WorkflowBuilder) Build() *workflow.Workflow {
    return b.wf
}
EOF

# pkg/storage/workflow_repo.go（存储接口，仅对外暴露）
cat > pkg/storage/workflow_repo.go << EOF
package storage

import (
    "$MODULE_PATH/pkg/core/workflow"
    "context"
)

// WorkflowRepository Workflow存储接口（对外导出）
type WorkflowRepository interface {
    // Save 保存Workflow（对外接口）
    Save(ctx context.Context, wf *workflow.Workflow) error
    // GetByID 根据ID查询Workflow（对外接口）
    GetByID(ctx context.Context, id string) (*workflow.Workflow, error)
    // Delete 删除Workflow（对外接口）
    Delete(ctx context.Context, id string) error
}
EOF

# pkg/plugin/plugin.go（插件基础接口，对外导出）
cat > pkg/plugin/plugin.go << EOF
package plugin

// Plugin 插件基础接口（对外导出）
type Plugin interface {
    // Name 插件名称（对外导出）
    Name() string
    // Init 初始化插件（对外导出）
    Init(params map[string]string) error
    // Execute 执行插件逻辑（对外导出）
    Execute(data interface{}) error
}

// NewEmailAlertPlugin 创建邮件告警插件（对外导出）
func NewEmailAlertPlugin() Plugin {
    return &EmailAlertPlugin{
        name: "email_alert",
    }
}

// NewSmsAlertPlugin 创建短信告警插件（对外导出）
func NewSmsAlertPlugin() Plugin {
    return &SmsAlertPlugin{
        name: "sms_alert",
    }
}
EOF

# pkg/plugin/email_alert.go（内建插件，对外导出）
cat > pkg/plugin/email_alert.go << EOF
package plugin

import "log"

// EmailAlertPlugin 邮件告警插件（对外导出）
type EmailAlertPlugin struct {
    name string
    smtpHost string
    smtpPort int
}

// Name 插件名称（实现Plugin接口，对外导出）
func (e *EmailAlertPlugin) Name() string {
    return e.name
}

// Init 初始化插件（实现Plugin接口，对外导出）
func (e *EmailAlertPlugin) Init(params map[string]string) error {
    e.smtpHost = params["smtp_host"]
    e.smtpPort = 25
    log.Println("✅ 邮件告警插件初始化完成")
    return nil
}

// Execute 执行邮件告警（实现Plugin接口，对外导出）
func (e *EmailAlertPlugin) Execute(data interface{}) error {
    log.Printf("📧 发送邮件告警：%v", data)
    return nil
}
EOF

# ===================== 生成internal私有实现（砍掉http） =====================
# internal/storage/sqlite/workflow_sqlite.go（存储具体实现，私有）
cat > internal/storage/sqlite/workflow_sqlite.go << EOF
package sqlite

import (
    "$MODULE_PATH/pkg/core/workflow"
    "$MODULE_PATH/pkg/storage"
    "context"
    "sync"
)

// workflowRepo SQLite实现（小写，不导出）
type workflowRepo struct {
    data map[string]*workflow.Workflow
    mu   sync.RWMutex
}

// NewWorkflowRepo 创建SQLite存储实例（内部工厂方法，不导出）
func NewWorkflowRepo() storage.WorkflowRepository {
    return &workflowRepo{
        data: make(map[string]*workflow.Workflow),
    }
}

// Save 实现存储接口（内部实现）
func (r *workflowRepo) Save(ctx context.Context, wf *workflow.Workflow) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.data[wf.ID] = wf
    return nil
}

// GetByID 实现存储接口（内部实现）
func (r *workflowRepo) GetByID(ctx context.Context, id string) (*workflow.Workflow, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    return r.data[id], nil
}

// Delete 实现存储接口（内部实现）
func (r *workflowRepo) Delete(ctx context.Context, id string) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    delete(r.data, id)
    return nil
}
EOF

# ===================== 生成cmd入口（极简） =====================
# cmd/server/main.go（仅调用pkg对外组件）
cat > cmd/server/main.go << EOF
package main

import (
    "$MODULE_PATH/pkg/core/engine"
    "$MODULE_PATH/pkg/core/workflow"
    "$MODULE_PATH/pkg/core/builder"
    "$MODULE_PATH/internal/storage/sqlite"
    "context"
    "log"
)

func main() {
    // 1. 创建存储接口实例（内部实现，对外仅依赖接口）
    repo := sqlite.NewWorkflowRepo()

    // 2. 创建引擎（调用对外核心组件）
    eng, err := engine.NewEngine(100, 30, repo)
    if err != nil {
        log.Fatal("创建引擎失败:", err)
    }

    // 3. 启动引擎
    if err := eng.Start(context.Background()); err != nil {
        log.Fatal("启动引擎失败:", err)
    }
    defer eng.Stop()

    // 4. 构建Workflow（调用对外Builder）
    wf := builder.NewWorkflowBuilder("测试任务", "极简结构测试").
        WithParams(map[string]string{"key": "value"}).
        Build()

    // 5. 注册Workflow
    if err := eng.RegisterWorkflow(context.Background(), wf); err != nil {
        log.Fatal("注册Workflow失败:", err)
    }

    log.Println("🎉 服务端启动完成（极简结构）")
    select {} // 阻塞运行
}
EOF

# ===================== 生成极简辅助文件 =====================
# configs/engine.yaml（简化）
cat > configs/engine.yaml << EOF
engine:
  max_concurrency: 100
  timeout_seconds: 30
storage:
  type: "sqlite"
  dsn: "./data.db"
EOF

# Makefile（极简）
cat > Makefile << EOF
MODULE := $MODULE_PATH
BINARY_SERVER := bin/server

build-server:
	@mkdir -p bin
	go build -o \$(BINARY_SERVER) ./cmd/server

run-server:
	go run ./cmd/server

clean:
	rm -rf bin/

.PHONY: build-server run-server clean
EOF

# 完成提示
echo "🎉 极简目录结构初始化完成！"
echo "📁 对外暴露目录：pkg/core/、pkg/storage/（仅接口）、pkg/plugin/（内建插件）"
echo "🚀 运行测试：cd $PROJECT_NAME && make run-server"