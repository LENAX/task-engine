package engine

import (
	"context"
	"fmt"
	"log"

	"github.com/stevelan1995/task-engine/pkg/core/executor"
	"github.com/stevelan1995/task-engine/pkg/core/workflow"
	"github.com/stevelan1995/task-engine/pkg/storage"
)

// Engine 调度引擎核心结构体（对外导出）
type Engine struct {
	executor       *executor.Executor
	workflowRepo   storage.WorkflowRepository
	running        bool
	MaxConcurrency int
	Timeout        int
}

// NewEngine 创建Engine实例（对外导出的工厂方法）
func NewEngine(maxConcurrency, timeout int, repo storage.WorkflowRepository) (*Engine, error) {
	exec, err := executor.NewExecutor(maxConcurrency)
	if err != nil {
		return nil, err
	}
	return &Engine{
		executor:       exec,
		workflowRepo:   repo,
		MaxConcurrency: maxConcurrency,
		Timeout:        timeout,
		running:        false,
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
