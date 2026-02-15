package engine

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LENAX/task-engine/pkg/core/cache"
	"github.com/LENAX/task-engine/pkg/core/executor"
	"github.com/LENAX/task-engine/pkg/core/task"
	"github.com/LENAX/task-engine/pkg/core/types"
	"github.com/LENAX/task-engine/pkg/core/workflow"
	"github.com/LENAX/task-engine/pkg/plugin"
	"github.com/LENAX/task-engine/pkg/storage"
)

type WorkflowInstanceManagerV3 struct {
	instance             *workflow.WorkflowInstance
	workflow             *workflow.Workflow
	executor             executor.Executor
	aggregateRepo        storage.WorkflowAggregateRepository
	workflowInstanceRepo storage.WorkflowInstanceRepository
	registry             task.FunctionRegistry
	pluginManager        plugin.PluginManager

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	controlSignalChan chan workflow.ControlSignal
	statusUpdateChan  chan string
	inbox             chan schedulerMsg

	mu sync.RWMutex

	tasks         map[string]workflow.Task
	taskNameToID  map[string]string
	children      map[string][]string
	pendingDeps   map[string]int
	readyMain     []string
	mainSubmitted map[string]bool
	mainDone      map[string]bool
	running       map[string]bool

	subTaskPools            map[string]*subTaskPool
	subToParent             map[string]string
	activePoolIDs           []string
	rrIdx                   int
	templateHandlerInFlight map[string]bool

	contextData sync.Map
	resultCache cache.ResultCache

	totalTasks   atomic.Int64
	successTasks atomic.Int64
	failedTasks  atomic.Int64
	runningTasks atomic.Int64
	paused       atomic.Bool
}

type schedulerMsg interface{ isSchedulerMsg() }

type addSubTasksMsg struct {
	parentID string
	tasks    []workflow.Task
	reply    chan error
}

func (addSubTasksMsg) isSchedulerMsg() {}

type taskResultMsg struct {
	taskID string
	result interface{}
	err    error
}

func (taskResultMsg) isSchedulerMsg() {}

type signalMsg struct{ sig workflow.ControlSignal }

func (signalMsg) isSchedulerMsg() {}

type templateHandlerDoneMsg struct {
	taskID string
}

func (templateHandlerDoneMsg) isSchedulerMsg() {}

func NewWorkflowInstanceManagerV3(
	instance *workflow.WorkflowInstance,
	wf *workflow.Workflow,
	exec executor.Executor,
	_ storage.TaskRepository,
	workflowInstanceRepo storage.WorkflowInstanceRepository,
	registry task.FunctionRegistry,
	pluginManager plugin.PluginManager,
) (*WorkflowInstanceManagerV3, error) {
	return newV3(instance, wf, exec, nil, workflowInstanceRepo, registry, pluginManager)
}

func NewWorkflowInstanceManagerV3WithAggregate(
	instance *workflow.WorkflowInstance,
	wf *workflow.Workflow,
	exec executor.Executor,
	aggregateRepo storage.WorkflowAggregateRepository,
	_ storage.TaskRepository,
	workflowInstanceRepo storage.WorkflowInstanceRepository,
	registry task.FunctionRegistry,
	pluginManager plugin.PluginManager,
) (*WorkflowInstanceManagerV3, error) {
	return newV3(instance, wf, exec, aggregateRepo, workflowInstanceRepo, registry, pluginManager)
}

func newV3(
	instance *workflow.WorkflowInstance,
	wf *workflow.Workflow,
	exec executor.Executor,
	aggregateRepo storage.WorkflowAggregateRepository,
	workflowInstanceRepo storage.WorkflowInstanceRepository,
	registry task.FunctionRegistry,
	pluginManager plugin.PluginManager,
) (*WorkflowInstanceManagerV3, error) {
	ctx, cancel := context.WithCancel(context.Background())
	m := &WorkflowInstanceManagerV3{
		instance:                instance,
		workflow:                wf,
		executor:                exec,
		aggregateRepo:           aggregateRepo,
		workflowInstanceRepo:    workflowInstanceRepo,
		registry:                registry,
		pluginManager:           pluginManager,
		ctx:                     ctx,
		cancel:                  cancel,
		controlSignalChan:       make(chan workflow.ControlSignal, 10),
		statusUpdateChan:        make(chan string, 10),
		inbox:                   make(chan schedulerMsg, 4096),
		tasks:                   make(map[string]workflow.Task),
		taskNameToID:            make(map[string]string),
		children:                make(map[string][]string),
		pendingDeps:             make(map[string]int),
		readyMain:               make([]string, 0, 32),
		mainSubmitted:           make(map[string]bool),
		mainDone:                make(map[string]bool),
		running:                 make(map[string]bool),
		subTaskPools:            make(map[string]*subTaskPool),
		subToParent:             make(map[string]string),
		activePoolIDs:           make([]string, 0, 8),
		templateHandlerInFlight: make(map[string]bool),
		resultCache:             cache.NewMemoryResultCache(),
	}
	if err := m.initState(); err != nil {
		return nil, err
	}
	if registry != nil {
		_ = registry.RegisterDependencyWithKey("InstanceManager", &InstanceManagerInterfaceV3{manager: m})
	}
	return m, nil
}

func (m *WorkflowInstanceManagerV3) initState() error {
	tasks := m.workflow.GetTasks()
	deps := m.workflow.GetDependencies()
	for id, t := range tasks {
		m.tasks[id] = t
		m.taskNameToID[t.GetName()] = id
	}
	for id := range tasks {
		m.pendingDeps[id] = len(deps[id])
		for _, p := range deps[id] {
			m.children[p] = append(m.children[p], id)
		}
		if m.pendingDeps[id] == 0 {
			m.readyMain = append(m.readyMain, id)
		}
	}
	m.totalTasks.Store(int64(len(tasks)))
	return nil
}

func (m *WorkflowInstanceManagerV3) Start() {
	m.mu.Lock()
	m.instance.Status = "Running"
	m.instance.StartTime = time.Now()
	m.mu.Unlock()
	m.persistStatus("Running")
	m.publishStatus("Running")
	m.wg.Add(2)
	go m.actorLoop()
	go m.controlLoop()
}

func (m *WorkflowInstanceManagerV3) Shutdown() {
	m.cancel()
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
	}
}

func (m *WorkflowInstanceManagerV3) GetControlSignalChannel() interface{}  { return m.controlSignalChan }
func (m *WorkflowInstanceManagerV3) GetStatusUpdateChannel() <-chan string { return m.statusUpdateChan }

func (m *WorkflowInstanceManagerV3) AddSubTask(subTask types.Task, parentTaskID string) error {
	return m.AtomicAddSubTasks([]types.Task{subTask}, parentTaskID)
}

func (m *WorkflowInstanceManagerV3) AtomicAddSubTasks(subTasks []types.Task, parentTaskID string) error {
	if parentTaskID == "" {
		return fmt.Errorf("parentTaskID不能为空")
	}
	tasks := make([]workflow.Task, 0, len(subTasks))
	for _, t := range subTasks {
		if t == nil {
			return fmt.Errorf("子任务不能为空")
		}
		t.SetSubTask(true)
		tasks = append(tasks, t)
	}
	reply := make(chan error, 1)
	select {
	case m.inbox <- addSubTasksMsg{parentID: parentTaskID, tasks: tasks, reply: reply}:
	case <-m.ctx.Done():
		return fmt.Errorf("instance已关闭")
	}
	select {
	case err := <-reply:
		return err
	case <-m.ctx.Done():
		return fmt.Errorf("instance已关闭")
	}
}

func (m *WorkflowInstanceManagerV3) RestoreFromBreakpoint(breakpoint interface{}) error {
	if breakpoint == nil {
		return nil
	}
	bp, ok := breakpoint.(*workflow.BreakpointData)
	if !ok {
		return fmt.Errorf("断点类型错误")
	}
	for k, v := range bp.ContextData {
		m.contextData.Store(k, v)
	}
	return nil
}

func (m *WorkflowInstanceManagerV3) CreateBreakpoint() interface{} {
	return &workflow.BreakpointData{LastUpdateTime: time.Now()}
}
func (m *WorkflowInstanceManagerV3) GetInstanceID() string { return m.instance.ID }
func (m *WorkflowInstanceManagerV3) GetStatus() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.instance.Status
}
func (m *WorkflowInstanceManagerV3) Context() context.Context { return m.ctx }

func (m *WorkflowInstanceManagerV3) GetProgress() types.ProgressSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	total := int(m.totalTasks.Load())
	completed := int(m.successTasks.Load())
	failed := int(m.failedTasks.Load())
	running := int(m.runningTasks.Load())
	pending := total - completed - failed - running
	if pending < 0 {
		pending = 0
	}
	runningIDs := make([]string, 0, len(m.running))
	for id := range m.running {
		runningIDs = append(runningIDs, id)
	}
	pendingIDs := append([]string{}, m.readyMain...)
	return types.ProgressSnapshot{Total: total, Completed: completed, Failed: failed, Running: running, Pending: pending, RunningTaskIDs: runningIDs, PendingTaskIDs: pendingIDs}
}

func (m *WorkflowInstanceManagerV3) controlLoop() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case sig := <-m.controlSignalChan:
			switch sig {
			case workflow.SignalPause:
				m.paused.Store(true)
				m.mu.Lock()
				m.instance.Status = "Paused"
				m.mu.Unlock()
				m.persistStatus("Paused")
				m.publishStatus("Paused")
			case workflow.SignalResume:
				m.paused.Store(false)
				m.mu.Lock()
				m.instance.Status = "Running"
				m.mu.Unlock()
				m.persistStatus("Running")
				m.publishStatus("Running")
			case workflow.SignalTerminate:
				m.mu.Lock()
				m.instance.Status = "Terminated"
				m.mu.Unlock()
				m.persistStatus("Terminated")
				m.publishStatus("Terminated")
				m.cancel()
				return
			}
		}
	}
}

func (m *WorkflowInstanceManagerV3) actorLoop() {
	defer m.wg.Done()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case msg := <-m.inbox:
			m.handleMsg(msg)
		case <-ticker.C:
			m.submit()
			m.checkDone()
		}
	}
}

func (m *WorkflowInstanceManagerV3) handleMsg(msg schedulerMsg) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch v := msg.(type) {
	case addSubTasksMsg:
		pool := m.subTaskPools[v.parentID]
		if pool == nil {
			pool = newSubTaskPool(v.parentID)
			m.subTaskPools[v.parentID] = pool
			m.activePoolIDs = append(m.activePoolIDs, v.parentID)
		}
		for _, st := range v.tasks {
			m.tasks[st.GetID()] = st
			m.taskNameToID[st.GetName()] = st.GetID()
			m.subToParent[st.GetID()] = v.parentID
		}
		pool.addTasks(v.tasks)
		m.totalTasks.Add(int64(len(v.tasks)))
		v.reply <- nil
	case taskResultMsg:
		m.onResult(v)
	case templateHandlerDoneMsg:
		m.onTemplateHandlerDone(v.taskID)
	}
}

func (m *WorkflowInstanceManagerV3) onResult(v taskResultMsg) {
	if !m.running[v.taskID] {
		return
	}
	delete(m.running, v.taskID)
	m.runningTasks.Add(-1)
	taskObj := m.tasks[v.taskID]
	if taskObj == nil {
		return
	}
	if v.err != nil {
		taskObj.SetStatus("FAILED")
		m.failedTasks.Add(1)
		if parent, ok := m.subToParent[v.taskID]; ok {
			if p := m.subTaskPools[parent]; p != nil {
				p.taskFailed()
			}
			return
		}
		m.mainDone[v.taskID] = true
		for _, c := range m.children[v.taskID] {
			m.mainDone[c] = true
			m.failedTasks.Add(1)
		}
		return
	}
	taskObj.SetStatus("SUCCESS")
	m.successTasks.Add(1)
	m.contextData.Store(v.taskID, v.result)
	_ = m.resultCache.Set(v.taskID, v.result, 24*time.Hour)
	if parent, ok := m.subToParent[v.taskID]; ok {
		if p := m.subTaskPools[parent]; p != nil {
			p.taskCompleted()
			m.tryCompleteTemplateTask(parent, p)
		}
		return
	}
	if taskObj.IsTemplate() {
		m.templateHandlerInFlight[v.taskID] = true
		m.executeStatusHandlerAsync(taskObj, "SUCCESS", v.result, "", func() {
			select {
			case m.inbox <- templateHandlerDoneMsg{taskID: v.taskID}:
			case <-m.ctx.Done():
			}
		})
		return
	}
	m.mainDone[v.taskID] = true
	m.executeStatusHandlerAsync(taskObj, "SUCCESS", v.result, "", nil)
	for _, c := range m.children[v.taskID] {
		m.pendingDeps[c]--
		if m.pendingDeps[c] <= 0 && !m.mainSubmitted[c] && !m.mainDone[c] {
			m.readyMain = append(m.readyMain, c)
		}
	}
}

func (m *WorkflowInstanceManagerV3) onTemplateHandlerDone(taskID string) {
	delete(m.templateHandlerInFlight, taskID)
	p := m.subTaskPools[taskID]
	if p != nil {
		p.markJobDone()
		m.tryCompleteTemplateTask(taskID, p)
	} else {
		m.completeMainTaskAndReleaseChildren(taskID)
	}
}

func (m *WorkflowInstanceManagerV3) tryCompleteTemplateTask(taskID string, p *subTaskPool) {
	if p == nil || !p.allDone {
		return
	}
	m.aggregateTemplateSubTaskResults(taskID, p)
	m.completeMainTaskAndReleaseChildren(taskID)
}

// normalizeSubTaskResult 将子任务返回值规范为 map，便于下游 GetUpstreamResult/GetSubTaskResults 一致读取（如 api_metadata、api_url）
func normalizeSubTaskResult(v interface{}) map[string]interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	return map[string]interface{}{"_raw": v}
}

func (m *WorkflowInstanceManagerV3) aggregateTemplateSubTaskResults(parentTaskID string, p *subTaskPool) {
	if p == nil {
		return
	}

	subtaskResults := make([]map[string]interface{}, 0, p.total)
	subTasksForDownstream := make([]map[string]interface{}, 0, p.total) // 与 subtask_results 同序，每项为 { result: map }，供下游通过 GetUpstreamResult 任意键一致读取
	allSucceeded := true

	for _, st := range p.tasks {
		if st == nil {
			continue
		}
		subTaskID := st.GetID()
		status := st.GetStatus()
		var subResult interface{}
		if resultValue, exists := m.contextData.Load(subTaskID); exists {
			subResult = resultValue
		}
		if status != "SUCCESS" {
			allSucceeded = false
		}
		normalizedResult := normalizeSubTaskResult(subResult)
		subtaskResults = append(subtaskResults, map[string]interface{}{
			"task_id":   subTaskID,
			"task_name": st.GetName(),
			"status":    status,
			"result":    normalizedResult,
		})
		// sub_tasks 每项同时带 result 与子任务返回的所有顶层字段，下游可按 item["result"] 或 item["api_metadata"] 等任意方式读取
		element := make(map[string]interface{}, len(normalizedResult)+1)
		element["result"] = normalizedResult
		for k, v := range normalizedResult {
			element[k] = v
		}
		subTasksForDownstream = append(subTasksForDownstream, element)
	}

	var parentResult map[string]interface{}
	if existingResult, exists := m.contextData.Load(parentTaskID); exists {
		if asMap, ok := existingResult.(map[string]interface{}); ok {
			parentResult = asMap
		} else {
			parentResult = map[string]interface{}{
				"original_result": existingResult,
			}
		}
	} else {
		parentResult = make(map[string]interface{})
	}

	parentResult["subtask_results"] = subtaskResults
	parentResult["subtask_count"] = len(subtaskResults)
	parentResult["all_subtasks_succeeded"] = allSucceeded
	parentResult["sub_tasks"] = subTasksForDownstream // 与 subtask_results 同序；每项含 "result" 及子任务返回的全体顶层字段，下游可按 ["result"] 或 ["api_metadata"] 等任意键读取

	m.contextData.Store(parentTaskID, parentResult)
	if m.resultCache != nil {
		_ = m.resultCache.Set(parentTaskID, parentResult, 24*time.Hour)
	}
}

func (m *WorkflowInstanceManagerV3) completeMainTaskAndReleaseChildren(taskID string) {
	if m.mainDone[taskID] {
		return
	}
	m.mainDone[taskID] = true
	for _, c := range m.children[taskID] {
		m.pendingDeps[c]--
		if m.pendingDeps[c] <= 0 && !m.mainSubmitted[c] && !m.mainDone[c] {
			m.readyMain = append(m.readyMain, c)
		}
	}
}

func (m *WorkflowInstanceManagerV3) prepareTaskParams(t workflow.Task, taskID string) error {
	if t == nil {
		return fmt.Errorf("task不能为空")
	}
	if err := m.validateAndMapParams(t); err != nil {
		return err
	}
	m.injectCachedResults(t)
	return nil
}

func (m *WorkflowInstanceManagerV3) validateAndMapParams(t workflow.Task) error {
	requiredParams := t.GetRequiredParams()
	resultMapping := t.GetResultMapping()

	if len(requiredParams) > 0 {
		deps := t.GetDependencies()
		missingParams := make([]string, 0)

		for _, requiredParam := range requiredParams {
			found := false
			if t.GetParams()[requiredParam] != nil {
				found = true
			} else {
				for _, depName := range deps {
					depTaskID, exists := m.taskNameToID[depName]
					if !exists {
						continue
					}
					if upstreamResultValue, ok := m.contextData.Load(depTaskID); ok {
						if upstreamResult, typeOK := upstreamResultValue.(map[string]interface{}); typeOK {
							if _, hasKey := upstreamResult[requiredParam]; hasKey {
								found = true
								break
							}
						}
					}
				}
			}

			if !found {
				missingParams = append(missingParams, requiredParam)
			}
		}

		if len(missingParams) > 0 {
			return fmt.Errorf("缺少必需参数: %v", missingParams)
		}
	}

	if len(resultMapping) > 0 {
		deps := t.GetDependencies()
		for targetParam, sourceField := range resultMapping {
			if _, exists := t.GetParam(targetParam); exists {
				continue
			}
			for _, depName := range deps {
				depTaskID, exists := m.taskNameToID[depName]
				if !exists {
					continue
				}
				if upstreamResultValue, ok := m.contextData.Load(depTaskID); ok {
					if upstreamResult, typeOK := upstreamResultValue.(map[string]interface{}); typeOK {
						if sourceValue, hasKey := upstreamResult[sourceField]; hasKey {
							t.SetParam(targetParam, sourceValue)
							break
						}
					}
				}
			}
		}
	}

	return nil
}

func (m *WorkflowInstanceManagerV3) injectCachedResults(t workflow.Task) {
	if m.resultCache == nil || t == nil {
		return
	}

	resultMapping := t.GetResultMapping()
	hasResultMapping := len(resultMapping) > 0

	for _, depName := range t.GetDependencies() {
		depTaskID, exists := m.taskNameToID[depName]
		if !exists {
			continue
		}

		cachedResult, found := m.resultCache.Get(depTaskID)
		if !found {
			continue
		}

		upstreamResult, ok := cachedResult.(map[string]interface{})
		if !ok {
			cacheKeyByID := fmt.Sprintf("_cached_%s", depTaskID)
			cacheKeyByName := fmt.Sprintf("_cached_%s", depName)
			if _, exists := t.GetParam(cacheKeyByID); !exists {
				t.SetParam(cacheKeyByID, cachedResult)
			}
			if _, exists := t.GetParam(cacheKeyByName); !exists {
				t.SetParam(cacheKeyByName, cachedResult)
			}
			continue
		}

		if hasResultMapping {
			for targetParam, sourceField := range resultMapping {
				if sourceValue, hasKey := upstreamResult[sourceField]; hasKey {
					if _, exists := t.GetParam(targetParam); !exists {
						t.SetParam(targetParam, sourceValue)
					}
				}
			}
			continue
		}

		// 下游任务用 _cached_<上游任务名> 或 _cached_<上游任务ID> 取缓存；推荐用任务名，见 doc/notes/下游任务如何获取上游结果-V3.md
		cacheKeyByID := fmt.Sprintf("_cached_%s", depTaskID)
		cacheKeyByName := fmt.Sprintf("_cached_%s", depName)
		if _, exists := t.GetParam(cacheKeyByID); !exists {
			t.SetParam(cacheKeyByID, cachedResult)
		}
		if _, exists := t.GetParam(cacheKeyByName); !exists {
			t.SetParam(cacheKeyByName, cachedResult)
		}
	}
}

func (m *WorkflowInstanceManagerV3) submit() {
	if m.paused.Load() {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	maxConc := m.workflow.MaxConcurrentTask
	if maxConc <= 0 {
		maxConc = 10
	}
	for int(m.runningTasks.Load()) < maxConc {
		taskID := m.pick()
		if taskID == "" {
			return
		}
		t := m.tasks[taskID]
		if t == nil {
			continue
		}
		m.mainSubmitted[taskID] = true
		m.running[taskID] = true
		m.runningTasks.Add(1)
		if err := m.prepareTaskParams(t, taskID); err != nil {
			m.onResult(taskResultMsg{taskID: taskID, err: err})
			continue
		}
		id := taskID
		err := m.executor.SubmitTask(&executor.PendingTask{
			Task:       t,
			WorkflowID: m.instance.WorkflowID,
			InstanceID: m.instance.ID,
			OnComplete: func(res *executor.TaskResult) {
				select {
				case m.inbox <- taskResultMsg{taskID: id, result: res.Data}:
				case <-m.ctx.Done():
				}
			},
			OnError: func(e error) {
				select {
				case m.inbox <- taskResultMsg{taskID: id, err: e}:
				case <-m.ctx.Done():
				}
			},
			InstanceManager: &InstanceManagerInterfaceV3{
				manager: m,
			},
		})
		if err != nil {
			delete(m.running, taskID)
			m.runningTasks.Add(-1)
			m.failedTasks.Add(1)
		}
	}
}

func (m *WorkflowInstanceManagerV3) pick() string {
	if len(m.readyMain) > 0 {
		id := m.readyMain[0]
		m.readyMain = m.readyMain[1:]
		return id
	}
	for i := 0; i < len(m.activePoolIDs); i++ {
		idx := (m.rrIdx + i) % len(m.activePoolIDs)
		p := m.subTaskPools[m.activePoolIDs[idx]]
		if p == nil {
			continue
		}
		next := p.next(1)
		if len(next) == 0 {
			continue
		}
		p.addRunning(1)
		m.rrIdx = (idx + 1) % len(m.activePoolIDs)
		return next[0].GetID()
	}
	return ""
}

func (m *WorkflowInstanceManagerV3) checkDone() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.running) > 0 || len(m.readyMain) > 0 {
		return
	}
	if len(m.templateHandlerInFlight) > 0 {
		return
	}
	for _, pid := range m.activePoolIDs {
		p := m.subTaskPools[pid]
		if p != nil && (p.runningCount > 0 || p.hasReady() || !p.allDone) {
			return
		}
	}
	if m.instance.Status == "Paused" {
		return
	}
	if m.failedTasks.Load() > 0 {
		m.instance.Status = "Failed"
		m.persistStatus("Failed")
		m.publishStatus("Failed")
	} else {
		m.instance.Status = "Success"
		m.persistStatus("Success")
		m.publishStatus("Success")
	}
	m.cancel()
}

func (m *WorkflowInstanceManagerV3) executeStatusHandlerAsync(taskObj workflow.Task, status string, result interface{}, errMsg string, done func()) {
	if m.registry == nil || len(taskObj.GetStatusHandlers()) == 0 {
		if done != nil {
			done()
		}
		return
	}
	handlerIDs, exists := taskObj.GetStatusHandlers()[status]
	if !exists || len(handlerIDs) == 0 {
		if done != nil {
			done()
		}
		return
	}
	go func() {
		ctx := context.Background()
		ctx = m.registry.WithDependencies(ctx)
		params := make(map[string]interface{})
		for k, v := range taskObj.GetParams() {
			params[k] = v
		}
		if result != nil {
			params["result"] = result
			params["_result_data"] = result
		}
		if errMsg != "" {
			params["error"] = errMsg
			params["_error_message"] = errMsg
		}
		params["_status"] = status
		params["_previous_status"] = taskObj.GetStatus()
		taskCtx := task.NewTaskContext(
			ctx,
			taskObj.GetID(),
			taskObj.GetName(),
			m.instance.WorkflowID,
			m.instance.ID,
			params,
		)
		for _, handlerID := range handlerIDs {
			handler := m.registry.GetTaskHandler(handlerID)
			if handler == nil {
				handler = m.registry.GetTaskHandlerByName(handlerID)
			}
			if handler != nil {
				handler(taskCtx)
			}
		}
		if done != nil {
			done()
		}
	}()
}

func (m *WorkflowInstanceManagerV3) publishStatus(status string) {
	select {
	case m.statusUpdateChan <- status:
	default:
	}
}

func (m *WorkflowInstanceManagerV3) persistStatus(status string) {
	ctx := context.Background()
	if m.aggregateRepo != nil {
		_ = m.aggregateRepo.UpdateWorkflowInstanceStatus(ctx, m.instance.ID, status)
		return
	}
	if m.workflowInstanceRepo != nil {
		_ = m.workflowInstanceRepo.UpdateStatus(ctx, m.instance.ID, status)
	}
}

type InstanceManagerInterfaceV3 struct {
	manager *WorkflowInstanceManagerV3
}

func (i *InstanceManagerInterfaceV3) AddSubTask(subTask types.Task, parentTaskID string) error {
	return i.manager.AddSubTask(subTask, parentTaskID)
}

func (i *InstanceManagerInterfaceV3) AtomicAddSubTasks(subTasks []types.Task, parentTaskID string) error {
	return i.manager.AtomicAddSubTasks(subTasks, parentTaskID)
}
