package workflow

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Task 定义Task接口，避免循环依赖（对外导出）
type Task interface {
	GetID() string
	GetName() string
	GetJobFuncName() string
	GetParams() map[string]interface{}
	GetStatus() string
	GetDependencies() []string
}

// Workflow Workflow核心结构体（对外导出）
type Workflow struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Params      map[string]string `json:"params"`
	CreateTime  time.Time         `json:"create_time"`
	Status      string            `json:"status"` // ENABLED/DISABLED
	Tasks       map[string]Task   `json:"tasks"` // Task ID -> Task映射
	Dependencies map[string][]string  `json:"dependencies"` // 后置Task ID -> 前置Task ID列表
}

// WorkflowInstance Workflow实例（对外导出）
type WorkflowInstance struct {
    ID         string    `json:"instance_id"`
    WorkflowID string    `json:"workflow_id"`
    Status     string    `json:"status"` // RUNNING/SUCCESS/FAILED
    StartTime  time.Time `json:"start_time"`
    EndTime    time.Time `json:"end_time"`
}

// NewWorkflow 创建Workflow实例（对外导出）
func NewWorkflow(name, desc string) *Workflow {
	return &Workflow{
		ID:          uuid.NewString(),
		Name:        name,
		Description: desc,
		Status:      "ENABLED",
		CreateTime:  time.Now(),
		Tasks:       make(map[string]Task),
		Dependencies: make(map[string][]string),
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

// GetID 获取Workflow的唯一标识（对外导出）
func (w *Workflow) GetID() string {
	return w.ID
}

// GetName 获取Workflow的业务名称（对外导出）
func (w *Workflow) GetName() string {
	return w.Name
}

// GetTasks 获取当前Workflow下的所有Task（对外导出）
// 返回Task ID到Task实例的映射
func (w *Workflow) GetTasks() map[string]Task {
	if w.Tasks == nil {
		return make(map[string]Task)
	}
	return w.Tasks
}

// GetDependencies 获取Task间的依赖关系（对外导出）
// 返回后置Task ID到前置Task ID列表的映射
func (w *Workflow) GetDependencies() map[string][]string {
	if w.Dependencies == nil {
		return make(map[string][]string)
	}
	return w.Dependencies
}

// AddSubTask 运行时为Workflow添加动态子Task（对外导出）
// subTask: 动态生成的子Task（ID为系统自动生成的UUID，名称需保证唯一）
// parentTaskID: 父Task ID（唯一）
func (w *Workflow) AddSubTask(subTask Task, parentTaskID string) error {
	if subTask == nil {
		return fmt.Errorf("子Task不能为空")
	}
	if subTask.GetID() == "" {
		return fmt.Errorf("子Task ID不能为空")
	}
	if subTask.GetName() == "" {
		return fmt.Errorf("子Task名称不能为空")
	}

	// 检查父Task是否存在
	parentTask, exists := w.Tasks[parentTaskID]
	if !exists {
		return fmt.Errorf("父Task %s 不存在", parentTaskID)
	}

	// 检查子Task名称是否唯一
	for taskID, t := range w.Tasks {
		if t.GetName() == subTask.GetName() && taskID != subTask.GetID() {
			return fmt.Errorf("Task名称 %s 已存在", subTask.GetName())
		}
	}

	// 检查子Task ID是否重复
	if _, exists := w.Tasks[subTask.GetID()]; exists {
		return fmt.Errorf("子Task ID %s 已存在", subTask.GetID())
	}

	// 添加子Task
	if w.Tasks == nil {
		w.Tasks = make(map[string]Task)
	}
	w.Tasks[subTask.GetID()] = subTask

	// 获取子Task的依赖列表（注意：这里需要Task接口支持更新依赖，但接口不能修改，所以依赖关系通过Dependencies字段管理）
	subTaskDeps := subTask.GetDependencies()
	
	// 检查是否已存在该依赖（通过Task名称）
	found := false
	for _, dep := range subTaskDeps {
		if dep == parentTask.GetName() {
			found = true
			break
		}
	}
	// 注意：由于Task是接口，无法直接修改Dependencies字段
	// 依赖关系通过Workflow的Dependencies字段管理

	// 更新依赖关系映射（后置Task ID -> 前置Task ID列表）
	if w.Dependencies == nil {
		w.Dependencies = make(map[string][]string)
	}
	if w.Dependencies[subTask.GetID()] == nil {
		w.Dependencies[subTask.GetID()] = make([]string, 0)
	}
	// 检查是否已存在该依赖
	found = false
	for _, depID := range w.Dependencies[subTask.GetID()] {
		if depID == parentTaskID {
			found = true
			break
		}
	}
	if !found {
		w.Dependencies[subTask.GetID()] = append(w.Dependencies[subTask.GetID()], parentTaskID)
	}

	return nil
}

// Validate 校验Workflow的合法性（对外导出）
// 校验Task名称唯一性、依赖关系合法性
func (w *Workflow) Validate() error {
	if w.Tasks == nil || len(w.Tasks) == 0 {
		return nil // 空Workflow是合法的
	}

	// 检查Task名称唯一性
	nameMap := make(map[string]string) // Task名称 -> Task ID
	for taskID, t := range w.Tasks {
		if t.GetName() == "" {
			return fmt.Errorf("Task %s 名称为空", taskID)
		}
		if existingID, exists := nameMap[t.GetName()]; exists {
			return fmt.Errorf("Task名称 %s 重复（Task ID: %s 和 %s）", t.GetName(), existingID, taskID)
		}
		nameMap[t.GetName()] = taskID
	}

	// 检查依赖关系合法性
	for taskID, t := range w.Tasks {
		deps := t.GetDependencies()
		for _, depName := range deps {
			// 检查依赖的Task名称是否存在
			depTaskID, exists := nameMap[depName]
			if !exists {
				return fmt.Errorf("Task %s 依赖的Task名称 %s 不存在", taskID, depName)
			}
			// 检查是否形成自依赖
			if depTaskID == taskID {
				return fmt.Errorf("Task %s 不能依赖自己", taskID)
			}
		}
	}

	return nil
}
