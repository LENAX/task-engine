package engine

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/robfig/cron/v3"
	"github.com/LENAX/task-engine/pkg/core/workflow"
)

// CronScheduler 定时调度器（对外导出）
type CronScheduler struct {
	cron      *cron.Cron
	engine    *Engine
	workflows map[string]*workflow.Workflow // workflowID -> Workflow映射
	entries   map[string]cron.EntryID        // workflowID -> cron.EntryID映射
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewCronScheduler 创建定时调度器（对外导出）
func NewCronScheduler(eng *Engine) *CronScheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &CronScheduler{
		cron:      cron.New(cron.WithSeconds()), // 支持秒级精度
		engine:    eng,
		workflows: make(map[string]*workflow.Workflow),
		entries:   make(map[string]cron.EntryID),
		ctx:       ctx,
		cancel:    cancel,
	}
}

// RegisterWorkflow 注册Workflow到定时调度器（对外导出）
func (cs *CronScheduler) RegisterWorkflow(wf *workflow.Workflow) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// 检查是否已注册
	if _, exists := cs.workflows[wf.GetID()]; exists {
		return fmt.Errorf("Workflow %s 已注册到定时调度器", wf.GetID())
	}

	// 检查是否启用定时调度
	if !wf.IsCronEnabled() {
		return fmt.Errorf("Workflow %s 未启用定时调度", wf.GetID())
	}

	// 检查Cron表达式
	cronExpr := wf.GetCronExpr()
	if cronExpr == "" {
		return fmt.Errorf("Workflow %s 未设置Cron表达式", wf.GetID())
	}

	// 验证Cron表达式（使用Parser支持秒级精度）
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	_, err := parser.Parse(cronExpr)
	if err != nil {
		return fmt.Errorf("Workflow %s 的Cron表达式无效: %w", wf.GetID(), err)
	}

	// 添加Cron任务
	entryID, err := cs.cron.AddFunc(cronExpr, func() {
		cs.triggerWorkflow(wf)
	})
	if err != nil {
		return fmt.Errorf("添加Cron任务失败: %w", err)
	}

	// 保存映射
	cs.workflows[wf.GetID()] = wf
	cs.entries[wf.GetID()] = entryID

	log.Printf("✅ [Cron调度器] 已注册Workflow: ID=%s, Name=%s, CronExpr=%s", wf.GetID(), wf.GetName(), cronExpr)
	return nil
}

// UnregisterWorkflow 取消注册Workflow（对外导出）
func (cs *CronScheduler) UnregisterWorkflow(workflowID string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// 检查是否已注册
	entryID, exists := cs.entries[workflowID]
	if !exists {
		return fmt.Errorf("Workflow %s 未注册到定时调度器", workflowID)
	}

	// 移除Cron任务
	cs.cron.Remove(entryID)

	// 删除映射
	delete(cs.workflows, workflowID)
	delete(cs.entries, workflowID)

	log.Printf("✅ [Cron调度器] 已取消注册Workflow: ID=%s", workflowID)
	return nil
}

// triggerWorkflow 触发Workflow执行（内部方法）
func (cs *CronScheduler) triggerWorkflow(wf *workflow.Workflow) {
	log.Printf("🕐 [Cron调度器] 触发Workflow执行: ID=%s, Name=%s", wf.GetID(), wf.GetName())

	// 创建Workflow副本（避免并发修改）
	wfCopy := workflow.NewWorkflow(wf.GetName(), wf.Description)
	wfCopy.SetCronExpr(wf.GetCronExpr())
	wfCopy.SetCronEnabled(wf.IsCronEnabled())

	// 复制所有Task
	allTasks := wf.GetTasks()
	for taskID, task := range allTasks {
		if err := wfCopy.AddTask(task); err != nil {
			log.Printf("⚠️ [Cron调度器] 复制Task失败: WorkflowID=%s, TaskID=%s, Error=%v", wf.GetID(), taskID, err)
		}
	}

	// 复制其他属性
	if err := wfCopy.SetTransactional(wf.Transactional); err != nil {
		log.Printf("⚠️ [Cron调度器] 设置Transactional失败: WorkflowID=%s, Error=%v", wf.GetID(), err)
	}
	wfCopy.SetSubTaskErrorTolerance(wf.GetSubTaskErrorTolerance())
	wfCopy.SetMaxConcurrentTask(wf.GetMaxConcurrentTask())

	// 提交Workflow执行
	ctx := context.Background()
	_, err := cs.engine.SubmitWorkflow(ctx, wfCopy)
	if err != nil {
		log.Printf("❌ [Cron调度器] 提交Workflow失败: WorkflowID=%s, Error=%v", wf.GetID(), err)
	} else {
		log.Printf("✅ [Cron调度器] Workflow已提交执行: WorkflowID=%s", wf.GetID())
	}
}

// Start 启动定时调度器（对外导出）
func (cs *CronScheduler) Start() {
	cs.cron.Start()
	log.Println("✅ [Cron调度器] 已启动")
}

// Stop 停止定时调度器（对外导出）
func (cs *CronScheduler) Stop() {
	cs.cron.Stop()
	cs.cancel()
	log.Println("✅ [Cron调度器] 已停止")
}

// GetRegisteredWorkflows 获取已注册的Workflow列表（对外导出）
func (cs *CronScheduler) GetRegisteredWorkflows() []string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	workflowIDs := make([]string, 0, len(cs.workflows))
	for workflowID := range cs.workflows {
		workflowIDs = append(workflowIDs, workflowID)
	}
	return workflowIDs
}

