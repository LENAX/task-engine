package workflow

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/stevelan1995/task-engine/pkg/core/types"
)

// Task 使用公共包中的Task接口（类型别名，保持向后兼容）
// 注意：为了保持向后兼容，保留这个类型别名
// 新代码可以直接使用 types.Task
type Task = types.Task

// TaskInfo 使用公共包中的TaskInfo接口（类型别名，保持向后兼容）
type TaskInfo = types.TaskInfo

// Workflow Workflow核心结构体（对外导出）
type Workflow struct {
	ID                    string       `json:"id"`
	Name                  string       `json:"name"`
	Description           string       `json:"description"`
	Params                sync.Map     `json:"-"` // 参数映射（线程安全）
	CreateTime            time.Time    `json:"create_time"`
	status                string       // ENABLED/DISABLED（私有字段，通过setter修改）
	statusMu              sync.Mutex   // 保护status字段
	Tasks                 sync.Map     `json:"-"`                        // Task ID -> Task映射（线程安全）
	TaskNameIndex         sync.Map     `json:"-"`                        // Task Name -> Task ID映射（线程安全）
	Dependencies          sync.Map     `json:"-"`                        // 后置Task ID -> 前置Task ID列表（线程安全）
	SubTaskErrorTolerance float64      `json:"sub_task_error_tolerance"` // 子任务错误容忍度（0-1），默认0
	Transactional         bool         `json:"transactional"`            // 是否启用事务（预留字段）
	TransactionMode       string       `json:"transaction_mode"`         // 事务模式（预留字段）
	MaxConcurrentTask     int          `json:"max_concurrent_task"`      // 最大并发任务数，默认10
	fieldsMu              sync.RWMutex // 保护 SubTaskErrorTolerance, Transactional, TransactionMode, MaxConcurrentTask 字段
}

// BreakpointData 断点数据（对外导出）
type BreakpointData struct {
	CompletedTaskNames []string               `json:"completed_task_names"` // 已完成的Task名称列表
	RunningTaskNames   []string               `json:"running_task_names"`   // 暂停时正在运行的Task名称
	DAGSnapshot        map[string]interface{} `json:"dag_snapshot"`         // DAG拓扑快照（JSON格式）
	ContextData        map[string]interface{} `json:"context_data"`         // 上下文数据（Task间传递的中间结果）
	LastUpdateTime     time.Time              `json:"last_update_time"`     // 最后更新时间
}

// WorkflowInstance Workflow实例（对外导出）
type WorkflowInstance struct {
	ID           string          `json:"instance_id"`   // 实例ID（系统自动生成的UUID）
	WorkflowID   string          `json:"workflow_id"`   // Workflow ID
	Status       string          `json:"status"`        // 状态（Ready/Running/Paused/Terminated/Success/Failed）
	Breakpoint   *BreakpointData `json:"breakpoint"`    // 断点数据（仅Paused/Terminated状态有值）
	StartTime    time.Time       `json:"start_time"`    // 启动时间
	EndTime      *time.Time      `json:"end_time"`      // 结束时间（成功/终止/失败时设置）
	ErrorMessage string          `json:"error_message"` // 错误信息（失败时设置）
	CreateTime   time.Time       `json:"create_time"`   // 创建时间
}

// NewWorkflow 创建Workflow实例（对外导出）
func NewWorkflow(name, desc string) *Workflow {
	wf := &Workflow{
		ID:                    uuid.NewString(),
		Name:                  name,
		Description:           desc,
		status:                "ENABLED",
		CreateTime:            time.Now(),
		Tasks:                 sync.Map{},
		TaskNameIndex:         sync.Map{},
		Dependencies:          sync.Map{},
		SubTaskErrorTolerance: 0.0,   // 默认值0，不允许子任务失败
		Transactional:         false, // 默认不启用事务
		TransactionMode:       "",    // 默认空字符串
		MaxConcurrentTask:     10,    // 默认最大并发任务数为10
	}
	return wf
}

// Run 运行Workflow（对外导出）
func (w *Workflow) Run() (*WorkflowInstance, error) {
	now := time.Now()
	instance := &WorkflowInstance{
		ID:         uuid.NewString(),
		WorkflowID: w.ID,
		Status:     "Ready", // 初始状态为Ready，等待启动
		StartTime:  now,
		CreateTime: now,
	}
	return instance, nil
}

// NewWorkflowInstance 创建WorkflowInstance实例（对外导出）
func NewWorkflowInstance(workflowID string) *WorkflowInstance {
	now := time.Now()
	return &WorkflowInstance{
		ID:         uuid.NewString(),
		WorkflowID: workflowID,
		Status:     "Ready",
		StartTime:  now,
		CreateTime: now,
	}
}

// GetID 获取Workflow的唯一标识（对外导出）
func (w *Workflow) GetID() string {
	return w.ID
}

// GetName 获取Workflow的业务名称（对外导出）
func (w *Workflow) GetName() string {
	return w.Name
}

// GetStatus 获取Workflow的状态（对外导出，线程安全）
func (w *Workflow) GetStatus() string {
	w.statusMu.Lock()
	defer w.statusMu.Unlock()
	return w.status
}

// SetStatus 设置Workflow的状态（对外导出，线程安全）
func (w *Workflow) SetStatus(status string) {
	w.statusMu.Lock()
	defer w.statusMu.Unlock()
	w.status = status
}

// GetParams 获取Workflow的参数（对外导出，线程安全）
func (w *Workflow) GetParams() map[string]string {
	result := make(map[string]string)
	return result
}

// SetParams 设置Workflow的参数（对外导出，线程安全）
func (w *Workflow) SetParams(params map[string]string) {
	if params == nil {
		return
	}
	for k, v := range params {
		w.Params.Store(k, v)
	}
}

// GetParam 获取单个参数（对外导出，线程安全）
func (w *Workflow) GetParam(key string) (string, bool) {
	if value, exists := w.Params.Load(key); exists {
		if strValue, ok := value.(string); ok {
			return strValue, true
		}
	}
	return "", false
}

// SetParam 设置单个参数（对外导出，线程安全）
func (w *Workflow) SetParam(key, value string) {
	w.Params.Store(key, value)
}

// GetTasks 获取当前Workflow下的所有Task（对外导出）
// 返回Task ID到Task实例的映射（线程安全）
func (w *Workflow) GetTasks() map[string]Task {
	result := make(map[string]Task)
	w.Tasks.Range(func(key, value interface{}) bool {
		taskID := key.(string)
		task := value.(Task)
		result[taskID] = task
		return true
	})
	return result
}

// GetDependencies 获取Task间的依赖关系（对外导出）
// 返回后置Task ID到前置Task ID列表的映射（线程安全）
func (w *Workflow) GetDependencies() map[string][]string {
	result := make(map[string][]string)
	w.Dependencies.Range(func(key, value interface{}) bool {
		taskID := key.(string)
		deps := value.([]string)
		result[taskID] = deps
		return true
	})
	return result
}

// AddTask 添加Task到Workflow（对外导出，线程安全）
// task: 要添加的Task（ID和名称需保证唯一）
// 返回错误如果Task ID或名称已存在
func (w *Workflow) AddTask(task Task) error {
	if task == nil {
		return fmt.Errorf("Task不能为空")
	}
	if task.GetID() == "" {
		return fmt.Errorf("Task ID不能为空")
	}
	if task.GetName() == "" {
		return fmt.Errorf("Task名称不能为空")
	}

	taskID := task.GetID()
	taskName := task.GetName()

	// 检查Task ID是否重复（使用LoadOrStore确保原子性）
	if _, loaded := w.Tasks.LoadOrStore(taskID, task); loaded {
		return fmt.Errorf("Task ID %s 已存在", taskID)
	}

	// 检查Task名称是否唯一（使用TaskNameIndex快速查找）
	if existingID, exists := w.TaskNameIndex.LoadOrStore(taskName, taskID); exists {
		// 如果名称已存在，回滚Task
		w.Tasks.Delete(taskID)
		return fmt.Errorf("Task名称 %s 已存在（Task ID: %s）", taskName, existingID)
	}

	// 初始化依赖关系（如果Task有依赖，需要验证依赖的Task是否存在）
	deps := task.GetDependencies()
	if len(deps) > 0 {
		depIDs := make([]string, 0, len(deps))
		for _, depName := range deps {
			// 使用TaskNameIndex快速查找依赖的Task ID
			depIDValue, exists := w.TaskNameIndex.Load(depName)
			if !exists {
				// 依赖的Task不存在，回滚Task和索引
				w.Tasks.Delete(taskID)
				w.TaskNameIndex.Delete(taskName)
				return fmt.Errorf("Task %s 依赖的Task名称 %s 不存在", taskName, depName)
			}
			depID := depIDValue.(string)
			if depID == taskID {
				// 自依赖，回滚Task和索引
				w.Tasks.Delete(taskID)
				w.TaskNameIndex.Delete(taskName)
				return fmt.Errorf("Task %s 不能依赖自己", taskName)
			}
			depIDs = append(depIDs, depID)
		}

		// 保存依赖关系
		w.Dependencies.Store(taskID, depIDs)
	}

	return nil
}

// GetTask 根据Task ID获取Task（对外导出，线程安全）
// taskID: Task ID
// 返回Task实例和是否存在
func (w *Workflow) GetTask(taskID string) (Task, bool) {
	if taskID == "" {
		return nil, false
	}
	value, exists := w.Tasks.Load(taskID)
	if !exists {
		return nil, false
	}
	return value.(Task), true
}

// GetTaskByName 根据Task名称获取Task（对外导出，线程安全）
// taskName: Task名称
// 返回Task实例和是否存在
func (w *Workflow) GetTaskByName(taskName string) (Task, bool) {
	if taskName == "" {
		return nil, false
	}
	// 使用TaskNameIndex快速查找Task ID
	taskIDValue, exists := w.TaskNameIndex.Load(taskName)
	if !exists {
		return nil, false
	}
	taskID := taskIDValue.(string)
	// 根据Task ID获取Task
	return w.GetTask(taskID)
}

// GetTaskIDByName 根据Task名称获取Task ID（对外导出，线程安全）
// taskName: Task名称
// 返回Task ID和是否存在
func (w *Workflow) GetTaskIDByName(taskName string) (string, bool) {
	if taskName == "" {
		return "", false
	}
	// 使用TaskNameIndex快速查找Task ID
	taskIDValue, exists := w.TaskNameIndex.Load(taskName)
	if !exists {
		return "", false
	}
	return taskIDValue.(string), true
}

// UpdateTask 更新Workflow中的Task（对外导出，线程安全）
// taskID: 要更新的Task ID
// task: 新的Task实例（必须保持相同的ID）
// 返回错误如果Task不存在或ID不匹配
func (w *Workflow) UpdateTask(taskID string, task Task) error {
	if taskID == "" {
		return fmt.Errorf("Task ID不能为空")
	}
	if task == nil {
		return fmt.Errorf("Task不能为空")
	}
	if task.GetID() != taskID {
		return fmt.Errorf("Task ID不匹配：期望 %s，实际 %s", taskID, task.GetID())
	}

	// 检查Task是否存在
	oldTaskValue, exists := w.Tasks.Load(taskID)
	if !exists {
		return fmt.Errorf("Task ID %s 不存在", taskID)
	}
	oldTask := oldTaskValue.(Task)
	oldTaskName := oldTask.GetName()
	newTaskName := task.GetName()

	// 如果名称改变了，检查新名称是否与其他Task冲突
	if oldTaskName != newTaskName {
		// 使用TaskNameIndex快速检查名称冲突
		if existingIDValue, exists := w.TaskNameIndex.Load(newTaskName); exists {
			existingID := existingIDValue.(string)
			if existingID != taskID {
				return fmt.Errorf("Task名称 %s 已存在（Task ID: %s）", newTaskName, existingID)
			}
		}
		// 更新TaskNameIndex：删除旧名称，添加新名称
		w.TaskNameIndex.Delete(oldTaskName)
		w.TaskNameIndex.Store(newTaskName, taskID)
	}

	// 更新Task
	w.Tasks.Store(taskID, task)

	// 更新依赖关系（如果Task有依赖，需要验证依赖的Task是否存在）
	deps := task.GetDependencies()
	if len(deps) > 0 {
		depIDs := make([]string, 0, len(deps))
		for _, depName := range deps {
			// 使用TaskNameIndex快速查找依赖的Task ID
			depIDValue, exists := w.TaskNameIndex.Load(depName)
			if !exists {
				return fmt.Errorf("Task %s 依赖的Task名称 %s 不存在", newTaskName, depName)
			}
			depID := depIDValue.(string)
			if depID == taskID {
				return fmt.Errorf("Task %s 不能依赖自己", newTaskName)
			}
			depIDs = append(depIDs, depID)
		}

		// 更新依赖关系
		w.Dependencies.Store(taskID, depIDs)
	} else {
		// 如果没有依赖，删除依赖关系
		w.Dependencies.Delete(taskID)
	}

	return nil
}

// DeleteTask 从Workflow中删除Task（对外导出，线程安全）
// taskID: 要删除的Task ID
// 返回错误如果Task不存在或存在其他Task依赖它
func (w *Workflow) DeleteTask(taskID string) error {
	if taskID == "" {
		return fmt.Errorf("Task ID不能为空")
	}

	// 检查Task是否存在，并获取Task名称用于删除索引
	taskValue, exists := w.Tasks.Load(taskID)
	if !exists {
		return fmt.Errorf("Task ID %s 不存在", taskID)
	}
	task := taskValue.(Task)
	taskName := task.GetName()

	// 检查是否有其他Task依赖此Task
	hasDependent := false
	var dependentTaskID string
	w.Dependencies.Range(func(key, value interface{}) bool {
		tid := key.(string)
		deps := value.([]string)
		for _, depID := range deps {
			if depID == taskID {
				hasDependent = true
				dependentTaskID = tid
				return false // 停止遍历
			}
		}
		return true
	})

	if hasDependent {
		return fmt.Errorf("无法删除Task %s，因为Task %s 依赖它", taskID, dependentTaskID)
	}

	// 删除Task、TaskNameIndex和其依赖关系
	w.Tasks.Delete(taskID)
	w.TaskNameIndex.Delete(taskName)
	w.Dependencies.Delete(taskID)

	// 从其他Task的依赖关系中删除对此Task的引用
	w.Dependencies.Range(func(key, value interface{}) bool {
		tid := key.(string)
		deps := value.([]string)
		newDeps := make([]string, 0, len(deps))
		for _, depID := range deps {
			if depID != taskID {
				newDeps = append(newDeps, depID)
			}
		}
		if len(newDeps) != len(deps) {
			// 有依赖被删除，更新依赖关系
			if len(newDeps) == 0 {
				w.Dependencies.Delete(tid)
			} else {
				w.Dependencies.Store(tid, newDeps)
			}
		}
		return true
	})

	return nil
}

// AddSubTask 运行时为Workflow添加动态子Task（对外导出，线程安全）
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

	subTaskID := subTask.GetID()
	subTaskName := subTask.GetName()

	// 检查子Task ID是否重复（使用LoadOrStore确保原子性）
	if _, loaded := w.Tasks.LoadOrStore(subTaskID, subTask); loaded {
		return fmt.Errorf("子Task ID %s 已存在", subTaskID)
	}

	// 检查父Task是否存在
	parentTaskValue, exists := w.Tasks.Load(parentTaskID)
	if !exists {
		// 如果父任务不存在，回滚子任务
		w.Tasks.Delete(subTaskID)
		return fmt.Errorf("父Task %s 不存在", parentTaskID)
	}
	_ = parentTaskValue.(Task) // 验证父任务存在，但不需要使用

	// 检查子Task名称是否唯一（使用TaskNameIndex快速查找）
	if existingID, exists := w.TaskNameIndex.LoadOrStore(subTaskName, subTaskID); exists {
		// 如果名称已存在，回滚子任务
		w.Tasks.Delete(subTaskID)
		return fmt.Errorf("Task名称 %s 已存在（Task ID: %s）", subTaskName, existingID)
	}

	// 更新依赖关系映射（后置Task ID -> 前置Task ID列表）
	// 使用LoadOrStore确保原子性
	depsValue, _ := w.Dependencies.LoadOrStore(subTaskID, make([]string, 0))
	deps := depsValue.([]string)

	// 检查是否已存在该依赖
	found := false
	for _, depID := range deps {
		if depID == parentTaskID {
			found = true
			break
		}
	}

	if !found {
		// 创建新的依赖列表（避免并发修改）
		newDeps := make([]string, len(deps), len(deps)+1)
		copy(newDeps, deps)
		newDeps = append(newDeps, parentTaskID)
		w.Dependencies.Store(subTaskID, newDeps)
	}

	return nil
}

// Validate 校验Workflow的合法性（对外导出）
// 校验Task名称唯一性、依赖关系合法性
func (w *Workflow) Validate() error {
	// 使用TaskNameIndex构建nameMap，同时验证索引和实际任务的一致性
	nameMap := make(map[string]string) // Task名称 -> Task ID
	taskCount := 0

	// 首先从TaskNameIndex构建nameMap
	w.TaskNameIndex.Range(func(key, value interface{}) bool {
		taskName := key.(string)
		taskID := value.(string)
		nameMap[taskName] = taskID
		return true
	})

	// 验证每个Task的名称和索引的一致性
	var duplicateName, existingID, duplicateID, emptyNameTaskID string
	w.Tasks.Range(func(key, value interface{}) bool {
		taskID := key.(string)
		t := value.(Task)
		taskCount++
		taskName := t.GetName()

		if taskName == "" {
			emptyNameTaskID = taskID
			return false // 停止遍历
		}

		// 检查索引中的Task ID是否匹配
		indexedID, existsInIndex := nameMap[taskName]
		if existsInIndex && indexedID != taskID {
			// 名称冲突：同一个名称对应不同的Task ID
			duplicateName = taskName
			existingID = indexedID
			duplicateID = taskID
			return false // 停止遍历
		}

		// 检查是否有其他Task使用相同名称
		if !existsInIndex {
			// 索引中没有，添加到nameMap
			nameMap[taskName] = taskID
		} else {
			// 验证索引中的ID是否与当前Task ID一致
			if indexedID != taskID {
				duplicateName = taskName
				existingID = indexedID
				duplicateID = taskID
				return false
			}
		}

		return true
	})

	if taskCount == 0 {
		return nil // 空Workflow是合法的
	}

	// 检查发现的错误
	if emptyNameTaskID != "" {
		return fmt.Errorf("Task %s 名称为空", emptyNameTaskID)
	}
	if duplicateName != "" {
		return fmt.Errorf("Task名称 %s 重复（Task ID: %s 和 %s）", duplicateName, existingID, duplicateID)
	}

	// 检查依赖关系合法性
	w.Tasks.Range(func(key, value interface{}) bool {
		taskID := key.(string)
		t := value.(Task)
		deps := t.GetDependencies()
		for _, depName := range deps {
			// 使用TaskNameIndex快速查找依赖的Task ID
			depTaskIDValue, exists := w.TaskNameIndex.Load(depName)
			if !exists {
				// 如果索引中没有，尝试从nameMap查找（可能索引未同步）
				depTaskID, existsInMap := nameMap[depName]
				if !existsInMap {
					return false // 停止遍历，返回错误
				}
				depTaskIDValue = depTaskID
			}
			depTaskID := depTaskIDValue.(string)

			// 检查是否形成自依赖
			if depTaskID == taskID {
				return false // 停止遍历，返回错误
			}
		}
		return true
	})

	// 如果上面遍历时发现错误，重新遍历找出具体的错误
	var missingDepName, selfDepTaskID, selfDepTaskName string
	w.Tasks.Range(func(key, value interface{}) bool {
		taskID := key.(string)
		t := value.(Task)
		deps := t.GetDependencies()
		for _, depName := range deps {
			depTaskIDValue, exists := w.TaskNameIndex.Load(depName)
			if !exists {
				depTaskID, existsInMap := nameMap[depName]
				if !existsInMap {
					missingDepName = depName
					return false
				}
				depTaskIDValue = depTaskID
			}
			depTaskID := depTaskIDValue.(string)
			if depTaskID == taskID {
				selfDepTaskID = taskID
				selfDepTaskName = t.GetName()
				return false
			}
		}
		return true
	})

	if missingDepName != "" {
		return fmt.Errorf("Task依赖的Task名称 %s 不存在", missingDepName)
	}
	if selfDepTaskID != "" {
		return fmt.Errorf("Task %s (ID: %s) 不能依赖自己", selfDepTaskName, selfDepTaskID)
	}

	return nil
}

// GetSubTaskErrorTolerance 获取子任务错误容忍度（对外导出，线程安全）
func (w *Workflow) GetSubTaskErrorTolerance() float64 {
	w.fieldsMu.RLock()
	defer w.fieldsMu.RUnlock()
	return w.SubTaskErrorTolerance
}

// SetSubTaskErrorTolerance 设置子任务错误容忍度（对外导出，线程安全）
// tolerance: 容忍度值，范围0-1，0表示不允许子任务失败，1表示允许所有子任务失败
func (w *Workflow) SetSubTaskErrorTolerance(tolerance float64) error {
	if tolerance < 0 || tolerance > 1 {
		return fmt.Errorf("子任务错误容忍度必须在0-1之间，当前值: %f", tolerance)
	}
	w.fieldsMu.Lock()
	defer w.fieldsMu.Unlock()
	// 如果transactional已开启，SubTaskErrorTolerance必须为0
	if w.Transactional && tolerance > 0 {
		return fmt.Errorf("当transactional开启时，SubTaskErrorTolerance必须为0，当前值: %f", tolerance)
	}
	w.SubTaskErrorTolerance = tolerance
	return nil
}

// GetTransactional 获取是否启用事务（对外导出，线程安全）
func (w *Workflow) GetTransactional() bool {
	w.fieldsMu.RLock()
	defer w.fieldsMu.RUnlock()
	return w.Transactional
}

// SetTransactional 设置是否启用事务（对外导出，线程安全）
func (w *Workflow) SetTransactional(transactional bool) error {
	w.fieldsMu.Lock()
	defer w.fieldsMu.Unlock()
	// 如果开启transactional，SubTaskErrorTolerance必须为0
	if transactional && w.SubTaskErrorTolerance > 0 {
		return fmt.Errorf("开启transactional时，SubTaskErrorTolerance必须为0，当前值: %f", w.SubTaskErrorTolerance)
	}
	w.Transactional = transactional
	return nil
}

// GetTransactionMode 获取事务模式（对外导出，线程安全）
func (w *Workflow) GetTransactionMode() string {
	w.fieldsMu.RLock()
	defer w.fieldsMu.RUnlock()
	return w.TransactionMode
}

// SetTransactionMode 设置事务模式（对外导出，线程安全）
func (w *Workflow) SetTransactionMode(mode string) {
	w.fieldsMu.Lock()
	defer w.fieldsMu.Unlock()
	w.TransactionMode = mode
}

// GetMaxConcurrentTask 获取最大并发任务数（对外导出，线程安全）
func (w *Workflow) GetMaxConcurrentTask() int {
	w.fieldsMu.RLock()
	defer w.fieldsMu.RUnlock()
	return w.MaxConcurrentTask
}

// SetMaxConcurrentTask 设置最大并发任务数（对外导出，线程安全）
// maxConcurrent: 最大并发任务数，必须大于0
func (w *Workflow) SetMaxConcurrentTask(maxConcurrent int) error {
	if maxConcurrent <= 0 {
		return fmt.Errorf("最大并发任务数必须大于0，当前值: %d", maxConcurrent)
	}
	w.fieldsMu.Lock()
	defer w.fieldsMu.Unlock()
	w.MaxConcurrentTask = maxConcurrent
	return nil
}
