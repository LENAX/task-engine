package saga

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/stevelan1995/task-engine/pkg/core/task"
)

// Coordinator SAGA事务协调器接口（对外导出）
type Coordinator interface {
	// AddStep 添加事务步骤
	AddStep(step *TransactionStep)
	// GetState 获取当前事务状态
	GetState() TransactionState
	// GetSteps 获取所有步骤
	GetSteps() []*TransactionStep
	// Commit 提交事务（所有步骤成功）
	Commit() error
	// Compensate 执行补偿（按反向顺序）
	Compensate(ctx context.Context) error
	// MarkStepSuccess 标记步骤成功
	MarkStepSuccess(taskID string)
	// MarkStepFailed 标记步骤失败
	MarkStepFailed(taskID string)
}

// coordinatorImpl SAGA事务协调器实现（内部实现）
type coordinatorImpl struct {
	transactionID string
	state         TransactionState
	steps         []*TransactionStep
	registry      *task.FunctionRegistry
	mu            sync.RWMutex
}

// NewCoordinator 创建SAGA协调器（对外导出）
func NewCoordinator(transactionID string, registry *task.FunctionRegistry) Coordinator {
	return &coordinatorImpl{
		transactionID: transactionID,
		state:         TransactionStatePending,
		steps:         make([]*TransactionStep, 0),
		registry:      registry,
	}
}

// AddStep 添加事务步骤（实现Coordinator接口）
func (c *coordinatorImpl) AddStep(step *TransactionStep) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.steps = append(c.steps, step)
}

// GetState 获取当前事务状态（实现Coordinator接口）
func (c *coordinatorImpl) GetState() TransactionState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// GetSteps 获取所有步骤（实现Coordinator接口）
func (c *coordinatorImpl) GetSteps() []*TransactionStep {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]*TransactionStep, len(c.steps))
	copy(result, c.steps)
	return result
}

// Commit 提交事务（所有步骤成功）（实现Coordinator接口）
func (c *coordinatorImpl) Commit() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.state.CanTransitionTo(TransactionStateCommitted) {
		return fmt.Errorf("当前状态 %s 不能转换到 Committed", c.state)
	}

	c.state = TransactionStateCommitted
	log.Printf("✅ [SAGA] TransactionID=%s, 事务已提交", c.transactionID)
	return nil
}

// Compensate 执行补偿（按反向顺序）（实现Coordinator接口）
func (c *coordinatorImpl) Compensate(ctx context.Context) error {
	c.mu.Lock()
	if !c.state.CanTransitionTo(TransactionStateCompensating) {
		c.mu.Unlock()
		return fmt.Errorf("当前状态 %s 不能转换到 Compensating", c.state)
	}
	c.state = TransactionStateCompensating
	steps := make([]*TransactionStep, len(c.steps))
	copy(steps, c.steps)
	c.mu.Unlock()

	log.Printf("🔄 [SAGA] TransactionID=%s, 开始执行补偿，共 %d 个步骤", c.transactionID, len(steps))

	// 按反向顺序执行补偿
	for i := len(steps) - 1; i >= 0; i-- {
		step := steps[i]
		if step.Status != "Success" {
			// 只补偿成功执行的步骤
			continue
		}

		if step.CompensateFuncName == "" {
			log.Printf("⚠️ [SAGA] TransactionID=%s, Step=%s, 未配置补偿函数，跳过", c.transactionID, step.TaskName)
			continue
		}

		// 从registry获取补偿函数（作为TaskHandler）
		compensateHandler := c.registry.GetTaskHandlerByName(step.CompensateFuncName)
		if compensateHandler == nil {
			// 尝试通过ID获取
			if step.CompensateFuncID != "" {
				compensateHandler = c.registry.GetTaskHandler(step.CompensateFuncID)
			}
		}

		if compensateHandler == nil {
			log.Printf("⚠️ [SAGA] TransactionID=%s, Step=%s, 补偿函数 %s 未找到，跳过", c.transactionID, step.TaskName, step.CompensateFuncName)
			continue
		}

		// 创建TaskContext执行补偿
		taskCtx := task.NewTaskContext(
			ctx,
			step.TaskID,
			step.TaskName,
			"", // WorkflowID
			"", // WorkflowInstanceID
			map[string]interface{}{
				"_saga_transaction_id": c.transactionID,
				"_compensate_step":     i + 1,
				"_total_steps":         len(steps),
			},
		)

		// 执行补偿函数
		log.Printf("🔄 [SAGA] TransactionID=%s, 执行补偿步骤 %d/%d: TaskID=%s, TaskName=%s",
			c.transactionID, len(steps)-i, len(steps), step.TaskID, step.TaskName)

		// 在goroutine中执行，避免阻塞
		go func(handler task.TaskHandlerType, ctx *task.TaskContext, stepName string) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("❌ [SAGA] TransactionID=%s, 补偿步骤 %s 执行panic: %v", c.transactionID, stepName, r)
				}
			}()
			handler(ctx)
		}(compensateHandler, taskCtx, step.TaskName)
	}

	// 更新状态为已补偿
	c.mu.Lock()
	c.state = TransactionStateCompensated
	c.mu.Unlock()

	log.Printf("✅ [SAGA] TransactionID=%s, 补偿执行完成", c.transactionID)
	return nil
}

// MarkStepSuccess 标记步骤成功（实现Coordinator接口）
func (c *coordinatorImpl) MarkStepSuccess(taskID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, step := range c.steps {
		if step.TaskID == taskID {
			step.Status = "Success"
			break
		}
	}
}

// MarkStepFailed 标记步骤失败（实现Coordinator接口）
func (c *coordinatorImpl) MarkStepFailed(taskID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, step := range c.steps {
		if step.TaskID == taskID {
			step.Status = "Failed"
			break
		}
	}
}

