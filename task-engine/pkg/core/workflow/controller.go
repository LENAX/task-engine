package workflow

import (
	"fmt"
	"sync"
	"time"
)

// ControlSignal 控制信号类型（内部使用）
type ControlSignal int

const (
	SignalPause ControlSignal = iota
	SignalResume
	SignalTerminate
)

// WorkflowController Workflow生命周期控制器（对外导出）
type WorkflowController interface {
	// Pause 暂停当前关联的WorkflowInstance
	Pause() error
	// Resume 恢复当前关联的WorkflowInstance
	Resume() error
	// Terminate 终止当前关联的WorkflowInstance
	Terminate() error
	// GetStatus 查询当前关联的WorkflowInstance状态
	GetStatus() (string, error)
	// GetInstanceID 获取当前关联的WorkflowInstance唯一标识
	GetInstanceID() string
	// UpdateStatus 更新状态（内部方法，供Engine使用）
	// 注意：这不是接口方法，但Engine可以通过类型断言访问
	UpdateStatus(newStatus string)
	// GetStatusUpdateChannel 获取状态更新通道（内部方法，供Engine使用）
	GetStatusUpdateChannel() <-chan string
}

// workflowController WorkflowController实现（内部结构）
type workflowController struct {
	instanceID        string
	status            string
	controlSignalChan chan ControlSignal
	statusUpdateChan  chan string
	mu                sync.RWMutex
	onPause           func() error
	onResume          func() error
	onTerminate       func() error
	onGetStatus       func() (string, error)
}

// NewWorkflowController 创建WorkflowController实例（对外导出）
func NewWorkflowController(instanceID string) WorkflowController {
	return &workflowController{
		instanceID:        instanceID,
		status:            "Ready",
		controlSignalChan: make(chan ControlSignal, 10), // 带缓冲，容量10
		statusUpdateChan:  make(chan string, 10),
	}
}

// NewWorkflowControllerWithCallbacks 创建带回调的WorkflowController实例（对外导出）
func NewWorkflowControllerWithCallbacks(
	instanceID string,
	onPause func() error,
	onResume func() error,
	onTerminate func() error,
	onGetStatus func() (string, error),
) WorkflowController {
	return &workflowController{
		instanceID:        instanceID,
		status:            "Ready",
		controlSignalChan: make(chan ControlSignal, 10),
		statusUpdateChan:  make(chan string, 10),
		onPause:           onPause,
		onResume:          onResume,
		onTerminate:       onTerminate,
		onGetStatus:       onGetStatus,
	}
}

// GetInstanceID 获取WorkflowInstance唯一标识（对外导出）
func (c *workflowController) GetInstanceID() string {
	return c.instanceID
}

// GetStatus 查询WorkflowInstance状态（对外导出）
func (c *workflowController) GetStatus() (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.onGetStatus != nil {
		return c.onGetStatus()
	}

	return c.status, nil
}

// Pause 暂停WorkflowInstance（对外导出）
// 发送信号后等待执行确认
func (c *workflowController) Pause() error {
	c.mu.Lock()
	currentStatus := c.status
	c.mu.Unlock()

	// 状态校验
	if currentStatus != "Running" {
		return fmt.Errorf("WorkflowInstance %s 当前状态为 %s，无法暂停（仅Running状态可暂停）", c.instanceID, currentStatus)
	}

	// 如果有回调函数，执行回调（发送信号）
	if c.onPause != nil {
		if err := c.onPause(); err != nil {
			return err
		}
	} else {
		// 发送暂停信号
		select {
		case c.controlSignalChan <- SignalPause:
			// 信号已发送
		default:
			return fmt.Errorf("WorkflowInstance %s 控制信号通道已满", c.instanceID)
		}
	}

	// 等待信号执行确认（通过状态更新）
	return c.waitForStatusChange("Paused", 5*time.Second)
}

// Resume 恢复WorkflowInstance（对外导出）
// 发送信号后等待执行确认
func (c *workflowController) Resume() error {
	c.mu.Lock()
	currentStatus := c.status
	c.mu.Unlock()

	// 状态校验
	if currentStatus != "Paused" {
		return fmt.Errorf("WorkflowInstance %s 当前状态为 %s，无法恢复（仅Paused状态可恢复）", c.instanceID, currentStatus)
	}

	// 如果有回调函数，执行回调（发送信号）
	if c.onResume != nil {
		if err := c.onResume(); err != nil {
			return err
		}
	} else {
		// 发送恢复信号
		select {
		case c.controlSignalChan <- SignalResume:
			// 信号已发送
		default:
			return fmt.Errorf("WorkflowInstance %s 控制信号通道已满", c.instanceID)
		}
	}

	// 等待信号执行确认（通过状态更新）
	return c.waitForStatusChange("Running", 5*time.Second)
}

// Terminate 终止WorkflowInstance（对外导出）
// 发送信号后等待执行确认
func (c *workflowController) Terminate() error {
	c.mu.Lock()
	currentStatus := c.status
	c.mu.Unlock()

	// 状态校验（Terminated/Success/Failed状态不能再终止）
	if currentStatus == "Terminated" || currentStatus == "Success" || currentStatus == "Failed" {
		return fmt.Errorf("WorkflowInstance %s 当前状态为 %s，无法终止", c.instanceID, currentStatus)
	}

	// 如果有回调函数，执行回调（发送信号）
	if c.onTerminate != nil {
		if err := c.onTerminate(); err != nil {
			return err
		}
	} else {
		// 发送终止信号
		select {
		case c.controlSignalChan <- SignalTerminate:
			// 信号已发送
		default:
			return fmt.Errorf("WorkflowInstance %s 控制信号通道已满", c.instanceID)
		}
	}

	// 等待信号执行确认（通过状态更新）
	return c.waitForStatusChange("Terminated", 5*time.Second)
}

// GetControlSignalChannel 获取控制信号通道（内部方法，供Engine使用）
func (c *workflowController) GetControlSignalChannel() <-chan ControlSignal {
	return c.controlSignalChan
}

// UpdateStatus 更新状态（内部方法，供Engine使用）
func (c *workflowController) UpdateStatus(newStatus string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = newStatus

	// 发送状态更新信号（非阻塞）
	select {
	case c.statusUpdateChan <- newStatus:
	default:
		// 通道已满，忽略
	}
}

// GetStatusUpdateChannel 获取状态更新通道（内部方法，供Engine使用）
func (c *workflowController) GetStatusUpdateChannel() <-chan string {
	return c.statusUpdateChan
}

// SetStatusUpdateChannel 设置状态更新通道（内部方法，供Engine使用）
// 允许Engine将Manager的状态更新转发到Controller
func (c *workflowController) SetStatusUpdateChannel(ch chan<- string) {
	// 这个方法用于设置一个可写的状态更新通道
	// 但实际上，我们通过UpdateStatus方法来更新状态
	// 这个方法保留用于未来扩展
}

// waitForStatusChange 等待状态变为指定状态（内部方法）
// 通过监听状态更新通道或轮询状态，等待状态变为目标状态
func (c *workflowController) waitForStatusChange(targetStatus string, timeout time.Duration) error {
	// 先检查当前状态是否已经是目标状态
	c.mu.RLock()
	currentStatus := c.status
	c.mu.RUnlock()

	if currentStatus == targetStatus {
		return nil
	}

	// 如果onGetStatus可用，使用轮询方式（更可靠）
	if c.onGetStatus != nil {
		// 使用轮询方式检查状态
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		timer := time.NewTimer(timeout)
		defer timer.Stop()

		for {
			select {
			case newStatus := <-c.statusUpdateChan:
				// 更新内部状态
				c.mu.Lock()
				c.status = newStatus
				c.mu.Unlock()

				// 检查是否达到目标状态
				if newStatus == targetStatus {
					return nil
				}
			case <-ticker.C:
				// 轮询检查状态
				actualStatus, err := c.onGetStatus()
				if err == nil && actualStatus == targetStatus {
					c.mu.Lock()
					c.status = actualStatus
					c.mu.Unlock()
					return nil
				}
			case <-timer.C:
				// 超时，获取最终状态
				actualStatus, _ := c.onGetStatus()
				if actualStatus == targetStatus {
					c.mu.Lock()
					c.status = actualStatus
					c.mu.Unlock()
					return nil
				}
				return fmt.Errorf("等待状态变更超时: 期望 %s, 实际 %s", targetStatus, actualStatus)
			}
		}
	}

	// 如果没有onGetStatus，仅通过状态更新通道等待
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case newStatus := <-c.statusUpdateChan:
			// 更新内部状态
			c.mu.Lock()
			c.status = newStatus
			c.mu.Unlock()

			// 检查是否达到目标状态
			if newStatus == targetStatus {
				return nil
			}
		case <-timer.C:
			// 超时
			c.mu.RLock()
			finalStatus := c.status
			c.mu.RUnlock()
			return fmt.Errorf("等待状态变更超时: 期望 %s, 实际 %s", targetStatus, finalStatus)
		}
	}
}
