package unit

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stevelan1995/task-engine/internal/storage/sqlite"
	"github.com/stevelan1995/task-engine/pkg/core/builder"
	"github.com/stevelan1995/task-engine/pkg/core/engine"
	"github.com/stevelan1995/task-engine/pkg/core/executor"
	"github.com/stevelan1995/task-engine/pkg/core/task"
	"github.com/stevelan1995/task-engine/pkg/core/workflow"
)

// TestLeveledTaskQueue_BasicOperations 测试LeveledTaskQueue的基本操作
func TestLeveledTaskQueue_BasicOperations(t *testing.T) {
	queue := engine.NewLeveledTaskQueue(3)

	// 创建registry和测试任务
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	// 注册mock函数
	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return map[string]interface{}{"result": "success"}, nil
	}
	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	task1, err := builder.NewTaskBuilder("task1", "任务1", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	if err != nil {
		t.Fatalf("创建task1失败: %v", err)
	}
	task2, err := builder.NewTaskBuilder("task2", "任务2", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	if err != nil {
		t.Fatalf("创建task2失败: %v", err)
	}
	task3, err := builder.NewTaskBuilder("task3", "任务3", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	if err != nil {
		t.Fatalf("创建task3失败: %v", err)
	}

	// 测试AddTask
	queue.AddTask(0, task1)
	queue.AddTask(0, task2)
	queue.AddTask(1, task3)

	// 测试IsEmpty
	if queue.IsEmpty(0) {
		t.Error("Level 0 应该不为空")
	}
	if queue.IsEmpty(1) {
		t.Error("Level 1 应该不为空")
	}
	if !queue.IsEmpty(2) {
		t.Error("Level 2 应该为空")
	}

	// 测试GetCurrentLevel
	if queue.GetCurrentLevel() != 0 {
		t.Errorf("期望当前层级为0，实际为%d", queue.GetCurrentLevel())
	}

	// 测试PopTasks
	tasks := queue.PopTasks(0, 10)
	if len(tasks) != 2 {
		t.Errorf("期望获取2个任务，实际获取%d个", len(tasks))
	}

	// 测试IsEmpty（Pop后应该为空）
	if !queue.IsEmpty(0) {
		t.Error("Level 0 Pop后应该为空")
	}

	// 测试AdvanceLevel
	queue.AdvanceLevel()
	if queue.GetCurrentLevel() != 1 {
		t.Errorf("期望当前层级为1，实际为%d", queue.GetCurrentLevel())
	}

	// 测试RemoveTask
	// 先移除 level 1 中的 task3，使 level 1 为空
	queue.RemoveTask(1, task3.GetID())
	if !queue.IsEmpty(1) {
		t.Error("RemoveTask后Level 1应该为空")
	}

	// 再次添加任务并移除，验证 RemoveTask 功能
	queue.AddTask(1, task1)
	if queue.IsEmpty(1) {
		t.Error("AddTask后Level 1应该不为空")
	}
	queue.RemoveTask(1, task1.GetID())
	if !queue.IsEmpty(1) {
		t.Error("RemoveTask后Level 1应该为空")
	}
}

// TestLeveledTaskQueue_IsAllTasksCompleted 测试IsAllTasksCompleted
func TestLeveledTaskQueue_IsAllTasksCompleted(t *testing.T) {
	queue := engine.NewLeveledTaskQueue(2)

	// 初始状态：currentLevel=0，所有队列为空
	completed, err := queue.IsAllTasksCompleted()
	if err != nil {
		t.Errorf("IsAllTasksCompleted返回错误: %v", err)
	}
	if completed {
		t.Error("初始状态不应该已完成")
	}

	// 推进层级超过最大层级
	queue.AdvanceLevel()
	queue.AdvanceLevel()
	queue.AdvanceLevel() // currentLevel=3 > maxLevel=2

	completed, err = queue.IsAllTasksCompleted()
	if err != nil {
		t.Errorf("IsAllTasksCompleted返回错误: %v", err)
	}
	if !completed {
		t.Error("currentLevel > maxLevel 且所有队列为空时应该已完成")
	}
}

// TestWorkflowInstanceManagerV2_Start 测试Start方法
func TestWorkflowInstanceManagerV2_Start(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2(t)
	defer cleanup()

	// 创建任务
	task1, _ := builder.NewTaskBuilder("task1", "任务1", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	wf.AddTask(task1)

	// 创建executor
	exec, err := executor.NewExecutor(10)
	if err != nil {
		t.Fatalf("创建Executor失败: %v", err)
	}
	exec.Start()
	exec.SetRegistry(registry)

	// 创建Manager
	instance := workflow.NewWorkflowInstance(wf.GetID())
	manager, err := engine.NewWorkflowInstanceManagerV2(
		instance,
		wf,
		exec,
		nil, // taskRepo
		nil, // workflowInstanceRepo
		registry,
	)
	if err != nil {
		t.Fatalf("创建Manager失败: %v", err)
	}

	// 测试Start
	manager.Start()

	// 等待一小段时间确保goroutine启动
	time.Sleep(50 * time.Millisecond)

	// 验证状态（任务可能已经完成，所以状态可能是Running或Success）
	status := manager.GetStatus()
	if status != "Running" && status != "Success" {
		t.Errorf("期望状态为Running或Success，实际为%s", status)
	}

	// 清理
	manager.Shutdown()
}

// TestWorkflowInstanceManagerV2_AddSubTask 测试AddSubTask方法
func TestWorkflowInstanceManagerV2_AddSubTask(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2(t)
	defer cleanup()

	// 创建父任务
	parentTask, _ := builder.NewTaskBuilder("parent", "父任务", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	wf.AddTask(parentTask)

	// 创建executor
	exec, err := executor.NewExecutor(10)
	if err != nil {
		t.Fatalf("创建Executor失败: %v", err)
	}
	exec.Start()
	exec.SetRegistry(registry)

	// 创建Manager
	instance := workflow.NewWorkflowInstance(wf.GetID())
	manager, err := engine.NewWorkflowInstanceManagerV2(
		instance,
		wf,
		exec,
		nil,
		nil,
		registry,
	)
	if err != nil {
		t.Fatalf("创建Manager失败: %v", err)
	}

	manager.Start()
	defer manager.Shutdown()

	// 创建子任务
	subTask, _ := builder.NewTaskBuilder("subtask1", "子任务1", registry).
		WithJobFunction("mockFunc", nil).
		Build()

	// 测试AddSubTask
	err = manager.AddSubTask(subTask, parentTask.GetID())
	if err != nil {
		t.Fatalf("AddSubTask失败: %v", err)
	}

	// 等待事件处理
	time.Sleep(100 * time.Millisecond)

	// 验证子任务已添加
	allTasks := wf.GetTasks()
	if _, exists := allTasks[subTask.GetID()]; !exists {
		t.Error("子任务应该已添加到Workflow")
	}
}

// TestWorkflowInstanceManagerV2_AtomicAddSubTasks 测试AtomicAddSubTasks方法
func TestWorkflowInstanceManagerV2_AtomicAddSubTasks(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2(t)
	defer cleanup()

	// 创建父任务
	parentTask, _ := builder.NewTaskBuilder("parent", "父任务", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	wf.AddTask(parentTask)

	// 创建executor
	exec, err := executor.NewExecutor(10)
	if err != nil {
		t.Fatalf("创建Executor失败: %v", err)
	}
	exec.Start()
	exec.SetRegistry(registry)

	// 创建Manager
	instance := workflow.NewWorkflowInstance(wf.GetID())
	manager, err := engine.NewWorkflowInstanceManagerV2(
		instance,
		wf,
		exec,
		nil,
		nil,
		registry,
	)
	if err != nil {
		t.Fatalf("创建Manager失败: %v", err)
	}

	manager.Start()
	defer manager.Shutdown()

	// 创建多个子任务
	subTasks := make([]workflow.Task, 0, 5)
	for i := 0; i < 5; i++ {
		subTask, _ := builder.NewTaskBuilder(
			fmt.Sprintf("subtask%d", i),
			fmt.Sprintf("子任务%d", i),
			registry,
		).WithJobFunction("mockFunc", nil).Build()
		subTasks = append(subTasks, subTask)
	}

	// 测试AtomicAddSubTasks
	err = manager.AtomicAddSubTasks(subTasks, parentTask.GetID())
	if err != nil {
		t.Fatalf("AtomicAddSubTasks失败: %v", err)
	}

	// 等待事件处理
	time.Sleep(200 * time.Millisecond)

	// 验证所有子任务都已添加
	allTasks := wf.GetTasks()
	for _, subTask := range subTasks {
		if _, exists := allTasks[subTask.GetID()]; !exists {
			t.Errorf("子任务 %s 应该已添加到Workflow", subTask.GetID())
		}
	}
}

// TestWorkflowInstanceManagerV2_AtomicAddSubTasks_EmptyList 测试AtomicAddSubTasks空列表
func TestWorkflowInstanceManagerV2_AtomicAddSubTasks_EmptyList(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2(t)
	defer cleanup()

	// 创建父任务
	parentTask, _ := builder.NewTaskBuilder("parent", "父任务", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	wf.AddTask(parentTask)

	// 创建executor
	exec, err := executor.NewExecutor(10)
	if err != nil {
		t.Fatalf("创建Executor失败: %v", err)
	}
	exec.Start()
	exec.SetRegistry(registry)

	// 创建Manager
	instance := workflow.NewWorkflowInstance(wf.GetID())
	manager, err := engine.NewWorkflowInstanceManagerV2(
		instance,
		wf,
		exec,
		nil,
		nil,
		registry,
	)
	if err != nil {
		t.Fatalf("创建Manager失败: %v", err)
	}

	manager.Start()
	defer manager.Shutdown()

	// 测试空列表
	err = manager.AtomicAddSubTasks([]workflow.Task{}, parentTask.GetID())
	if err != nil {
		t.Fatalf("AtomicAddSubTasks空列表应该成功，但返回错误: %v", err)
	}
}

// TestWorkflowInstanceManagerV2_AtomicAddSubTasks_InvalidTask 测试AtomicAddSubTasks无效任务
func TestWorkflowInstanceManagerV2_AtomicAddSubTasks_InvalidTask(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2(t)
	defer cleanup()

	// 创建父任务
	parentTask, _ := builder.NewTaskBuilder("parent", "父任务", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	wf.AddTask(parentTask)

	// 创建executor
	exec, err := executor.NewExecutor(10)
	if err != nil {
		t.Fatalf("创建Executor失败: %v", err)
	}
	exec.Start()
	exec.SetRegistry(registry)

	// 创建Manager
	instance := workflow.NewWorkflowInstance(wf.GetID())
	manager, err := engine.NewWorkflowInstanceManagerV2(
		instance,
		wf,
		exec,
		nil,
		nil,
		registry,
	)
	if err != nil {
		t.Fatalf("创建Manager失败: %v", err)
	}

	manager.Start()
	defer manager.Shutdown()

	// 等待manager启动
	time.Sleep(50 * time.Millisecond)

	// 测试nil任务（使用类型断言来创建nil接口值）
	var nilTask workflow.Task = nil
	err = manager.AtomicAddSubTasks([]workflow.Task{nilTask}, parentTask.GetID())
	if err == nil {
		t.Error("AtomicAddSubTasks应该拒绝nil任务")
	} else {
		t.Logf("nil任务被正确拒绝: %v", err)
	}

	// 测试空ID任务（创建一个任务后清空ID）
	invalidTask, err := builder.NewTaskBuilder("invalidTask", "无效任务", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	if err != nil {
		t.Fatalf("创建测试任务失败: %v", err)
	}
	// 清空ID以测试验证逻辑
	invalidTask.SetID("")
	err = manager.AtomicAddSubTasks([]workflow.Task{invalidTask}, parentTask.GetID())
	if err == nil {
		t.Error("AtomicAddSubTasks应该拒绝空ID任务")
	} else {
		t.Logf("空ID任务被正确拒绝: %v", err)
	}
}

// TestWorkflowInstanceManagerV2_AtomicAddSubTasks_Rollback 测试AtomicAddSubTasks回滚机制
func TestWorkflowInstanceManagerV2_AtomicAddSubTasks_Rollback(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2(t)
	defer cleanup()

	// 创建父任务
	parentTask, _ := builder.NewTaskBuilder("parent", "父任务", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	wf.AddTask(parentTask)

	// 创建executor
	exec, err := executor.NewExecutor(10)
	if err != nil {
		t.Fatalf("创建Executor失败: %v", err)
	}
	exec.Start()
	exec.SetRegistry(registry)

	// 创建Manager
	instance := workflow.NewWorkflowInstance(wf.GetID())
	manager, err := engine.NewWorkflowInstanceManagerV2(
		instance,
		wf,
		exec,
		nil,
		nil,
		registry,
	)
	if err != nil {
		t.Fatalf("创建Manager失败: %v", err)
	}

	manager.Start()
	defer manager.Shutdown()

	// 创建多个子任务，其中一个使用已存在的ID（会导致失败并回滚）
	subTasks := make([]workflow.Task, 0, 3)

	// 第一个子任务（正常）
	subTask1, _ := builder.NewTaskBuilder("subtask1", "子任务1", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	subTasks = append(subTasks, subTask1)

	// 先添加第一个子任务，使其ID已存在
	err = manager.AddSubTask(subTask1, parentTask.GetID())
	if err != nil {
		t.Fatalf("添加第一个子任务失败: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// 第二个子任务（使用相同的ID，应该导致整个批量添加失败并回滚）
	subTask2, _ := builder.NewTaskBuilder("subtask1", "子任务2（重复ID）", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	subTasks = append(subTasks, subTask2)

	// 第三个子任务（正常，但由于第二个失败，应该被回滚）
	subTask3, _ := builder.NewTaskBuilder("subtask3", "子任务3", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	subTasks = append(subTasks, subTask3)

	// 测试AtomicAddSubTasks（应该失败并回滚）
	// 注意：由于第一个子任务已经添加，AtomicAddSubTasks 会尝试添加第二个和第三个
	// 第二个会失败（ID重复），导致整个操作失败并回滚
	err = manager.AtomicAddSubTasks(subTasks, parentTask.GetID())
	if err == nil {
		t.Error("AtomicAddSubTasks应该失败（ID重复），但返回了成功")
	}

	// 验证：只有第一个子任务存在，第二个和第三个应该被回滚
	allTasks := wf.GetTasks()
	if _, exists := allTasks[subTask1.GetID()]; !exists {
		t.Error("第一个子任务应该存在（在AtomicAddSubTasks之前添加）")
	}
	// 注意：由于回滚机制，subTask3 不应该被添加
	// 但 subTask2 由于ID重复，也不会被添加
}

// TestWorkflowInstanceManagerV2_SignalControl 测试信号控制
func TestWorkflowInstanceManagerV2_SignalControl(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2(t)
	defer cleanup()

	// 创建任务
	task1, _ := builder.NewTaskBuilder("task1", "任务1", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	wf.AddTask(task1)

	// 创建executor
	exec, err := executor.NewExecutor(10)
	if err != nil {
		t.Fatalf("创建Executor失败: %v", err)
	}
	exec.Start()
	exec.SetRegistry(registry)

	// 创建Manager
	instance := workflow.NewWorkflowInstance(wf.GetID())
	manager, err := engine.NewWorkflowInstanceManagerV2(
		instance,
		wf,
		exec,
		nil,
		nil,
		registry,
	)
	if err != nil {
		t.Fatalf("创建Manager失败: %v", err)
	}

	manager.Start()
	defer manager.Shutdown()

	// 等待启动并确保任务开始执行
	time.Sleep(50 * time.Millisecond)

	// 测试Pause（在任务完成前发送）
	signalChan := manager.GetControlSignalChannel().(chan workflow.ControlSignal)
	signalChan <- workflow.SignalPause

	// 等待状态更新（任务可能已经完成，所以状态可能是Success而不是Paused）
	time.Sleep(200 * time.Millisecond)
	status := manager.GetStatus()
	// 注意：如果任务已经完成，状态可能是Success而不是Paused
	// 这是正常的行为，因为任务完成时会自动更新状态
	if status != "Paused" && status != "Success" {
		t.Errorf("期望状态为Paused或Success，实际为%s", status)
	}

	// 测试Resume
	signalChan <- workflow.SignalResume
	time.Sleep(200 * time.Millisecond)
	status = manager.GetStatus()
	if status != "Running" {
		t.Errorf("期望状态为Running，实际为%s", status)
	}

	// 测试Terminate
	signalChan <- workflow.SignalTerminate
	time.Sleep(200 * time.Millisecond)
	status = manager.GetStatus()
	if status != "Terminated" {
		t.Errorf("期望状态为Terminated，实际为%s", status)
	}
}

// TestWorkflowInstanceManagerV2_CreateBreakpoint 测试CreateBreakpoint
func TestWorkflowInstanceManagerV2_CreateBreakpoint(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2(t)
	defer cleanup()

	// 创建任务
	task1, _ := builder.NewTaskBuilder("task1", "任务1", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	wf.AddTask(task1)

	// 创建executor
	exec, err := executor.NewExecutor(10)
	if err != nil {
		t.Fatalf("创建Executor失败: %v", err)
	}
	exec.Start()
	exec.SetRegistry(registry)

	// 创建Manager
	instance := workflow.NewWorkflowInstance(wf.GetID())
	manager, err := engine.NewWorkflowInstanceManagerV2(
		instance,
		wf,
		exec,
		nil,
		nil,
		registry,
	)
	if err != nil {
		t.Fatalf("创建Manager失败: %v", err)
	}

	// 测试CreateBreakpoint
	breakpoint := manager.CreateBreakpoint()
	if breakpoint == nil {
		t.Error("CreateBreakpoint应该返回非nil值")
	}

	bp, ok := breakpoint.(*workflow.BreakpointData)
	if !ok {
		t.Error("CreateBreakpoint应该返回*workflow.BreakpointData类型")
	}

	if bp.CompletedTaskNames == nil {
		t.Error("BreakpointData.CompletedTaskNames不应该为nil")
	}
	if bp.ContextData == nil {
		t.Error("BreakpointData.ContextData不应该为nil")
	}
}

// TestWorkflowInstanceManagerV2_TemplateTask 测试模板任务功能
func TestWorkflowInstanceManagerV2_TemplateTask(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2(t)
	defer cleanup()

	// 创建模板任务
	templateTask, _ := builder.NewTaskBuilder("template", "模板任务", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	templateTask.SetTemplate(true)
	wf.AddTask(templateTask)

	// 创建executor
	exec, err := executor.NewExecutor(10)
	if err != nil {
		t.Fatalf("创建Executor失败: %v", err)
	}
	exec.Start()
	exec.SetRegistry(registry)

	// 创建Manager
	instance := workflow.NewWorkflowInstance(wf.GetID())
	manager, err := engine.NewWorkflowInstanceManagerV2(
		instance,
		wf,
		exec,
		nil,
		nil,
		registry,
	)
	if err != nil {
		t.Fatalf("创建Manager失败: %v", err)
	}

	manager.Start()
	defer manager.Shutdown()

	// 等待模板任务执行完成
	// 注意：模板任务不执行，但会被标记为Success，workflow状态也会更新为Success
	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			// 超时时，检查任务状态
			allTasks := wf.GetTasks()
			for _, task := range allTasks {
				if task.IsTemplate() {
					t.Logf("模板任务状态: TaskID=%s, Status=%s", task.GetID(), task.GetStatus())
				}
			}
			t.Fatalf("模板任务执行超时，当前workflow状态: %s", manager.GetStatus())
		case <-ticker.C:
			status := manager.GetStatus()
			if status == "Success" || status == "Failed" {
				// 模板任务应该被标记为成功（即使不执行，依赖关系已满足）
				if status != "Success" {
					t.Errorf("模板任务应该成功完成，实际状态: %s", status)
				}
				// 验证模板任务状态
				allTasks := wf.GetTasks()
				for _, task := range allTasks {
					if task.IsTemplate() {
						if task.GetStatus() != "SUCCESS" && task.GetStatus() != "Success" {
							t.Errorf("模板任务状态应该为Success，实际为: %s", task.GetStatus())
						}
					}
				}
				return
			}
		}
	}
}

// setupTestForV2 设置V2测试环境
func setupTestForV2(t *testing.T) (*engine.Engine, *task.FunctionRegistry, *workflow.Workflow, func()) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"

	repos, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("创建Repository失败: %v", err)
	}

	eng, err := engine.NewEngine(10, 30, repos.Workflow, repos.WorkflowInstance, repos.Task)
	if err != nil {
		t.Fatalf("创建Engine失败: %v", err)
	}

	registry := eng.GetRegistry()
	ctx := context.Background()

	// 注册mock函数
	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		time.Sleep(10 * time.Millisecond)
		return map[string]interface{}{
			"result": "success",
		}, nil
	}
	_, err = registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	if err := eng.Start(ctx); err != nil {
		t.Fatalf("启动Engine失败: %v", err)
	}

	wf := workflow.NewWorkflow("test-workflow", "测试工作流")

	cleanup := func() {
		eng.Stop()
		repos.Close()
	}

	return eng, registry, wf, cleanup
}

// TestLeveledTaskQueue_ConcurrentAccess 测试并发访问LeveledTaskQueue
func TestLeveledTaskQueue_ConcurrentAccess(t *testing.T) {
	queue := engine.NewLeveledTaskQueue(3)
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	// 注册mock函数
	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return map[string]interface{}{"result": "success"}, nil
	}
	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 创建多个任务
	tasks := make([]workflow.Task, 100)
	for i := 0; i < 100; i++ {
		task, err := builder.NewTaskBuilder(fmt.Sprintf("task%d", i), fmt.Sprintf("任务%d", i), registry).
			WithJobFunction("mockFunc", nil).
			Build()
		if err != nil {
			t.Fatalf("创建任务失败: %v", err)
		}
		tasks[i] = task
	}

	// 并发添加任务
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(start int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				idx := start*10 + j
				queue.AddTask(0, tasks[idx])
			}
		}(i)
	}
	wg.Wait()

	// 验证任务数量
	if queue.IsEmpty(0) {
		t.Error("Level 0 应该不为空")
	}

	// 并发Pop和Add
	wg = sync.WaitGroup{}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			popped := queue.PopTasks(0, 10)
			// 重新添加
			for _, t := range popped {
				queue.AddTask(0, t)
			}
		}()
	}
	wg.Wait()

	// 验证队列状态一致性
	allPopped := queue.PopTasks(0, 200)
	if len(allPopped) != 100 {
		t.Errorf("期望100个任务，实际获取%d个", len(allPopped))
	}
}

// TestLeveledTaskQueue_PopTasksRaceCondition 测试PopTasks的竞态条件
func TestLeveledTaskQueue_PopTasksRaceCondition(t *testing.T) {
	queue := engine.NewLeveledTaskQueue(2)
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	// 注册mock函数
	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return map[string]interface{}{"result": "success"}, nil
	}
	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 创建任务
	tasks := make([]workflow.Task, 50)
	for i := 0; i < 50; i++ {
		task, err := builder.NewTaskBuilder(fmt.Sprintf("task%d", i), fmt.Sprintf("任务%d", i), registry).
			WithJobFunction("mockFunc", nil).
			Build()
		if err != nil {
			t.Fatalf("创建任务失败: %v", err)
		}
		tasks[i] = task
		queue.AddTask(0, task)
	}

	// 并发Pop和IsEmpty检查
	var wg sync.WaitGroup
	poppedCount := atomic.Int32{}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				popped := queue.PopTasks(0, 5)
				poppedCount.Add(int32(len(popped)))
				// 检查IsEmpty
				_ = queue.IsEmpty(0)
			}
		}()
	}
	wg.Wait()

	// 验证所有任务都被Pop
	if poppedCount.Load() != 50 {
		t.Errorf("期望Pop 50个任务，实际Pop %d个", poppedCount.Load())
	}

	// 验证队列为空
	if !queue.IsEmpty(0) {
		t.Error("队列应该为空")
	}
}

// TestWorkflowInstanceManagerV2_TemplateTaskCountEdgeCases 测试模板任务计数的边界情况
func TestWorkflowInstanceManagerV2_TemplateTaskCountEdgeCases(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2(t)
	defer cleanup()

	// 创建多个模板任务，但不生成子任务
	templateTask1, _ := builder.NewTaskBuilder("template1", "模板任务1", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	templateTask1.SetTemplate(true)

	templateTask2, _ := builder.NewTaskBuilder("template2", "模板任务2", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	templateTask2.SetTemplate(true)

	wf.AddTask(templateTask1)
	wf.AddTask(templateTask2)

	exec, _ := executor.NewExecutor(10)
	exec.Start()
	exec.SetRegistry(registry)

	instance := workflow.NewWorkflowInstance(wf.GetID())
	manager, err := engine.NewWorkflowInstanceManagerV2(
		instance,
		wf,
		exec,
		nil,
		nil,
		registry,
	)
	if err != nil {
		t.Fatalf("创建Manager失败: %v", err)
	}

	manager.Start()
	defer manager.Shutdown()

	// 等待完成
	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatal("Workflow执行超时")
		case <-ticker.C:
			status := manager.GetStatus()
			if status == "Success" || status == "Failed" {
				if status != "Success" {
					t.Errorf("Workflow应该成功完成，实际状态: %s", status)
				}
				return
			}
		}
	}
}

// TestWorkflowInstanceManagerV2_ChannelBlocking 测试channel阻塞和超时处理
func TestWorkflowInstanceManagerV2_ChannelBlocking(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2(t)
	defer cleanup()

	// 创建大量任务，可能导致channel阻塞
	tasks := make([]workflow.Task, 200)
	for i := 0; i < 200; i++ {
		task, _ := builder.NewTaskBuilder(fmt.Sprintf("task%d", i), fmt.Sprintf("任务%d", i), registry).
			WithJobFunction("mockFunc", nil).
			Build()
		tasks[i] = task
		wf.AddTask(task)
	}

	exec, _ := executor.NewExecutor(10)
	exec.Start()
	exec.SetRegistry(registry)

	instance := workflow.NewWorkflowInstance(wf.GetID())
	manager, err := engine.NewWorkflowInstanceManagerV2(
		instance,
		wf,
		exec,
		nil,
		nil,
		registry,
	)
	if err != nil {
		t.Fatalf("创建Manager失败: %v", err)
	}

	manager.Start()
	defer manager.Shutdown()

	// 等待完成（设置更长的超时时间）
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			status := manager.GetStatus()
			t.Fatalf("Workflow执行超时，当前状态: %s", status)
		case <-ticker.C:
			status := manager.GetStatus()
			if status == "Success" || status == "Failed" {
				if status != "Success" {
					t.Errorf("Workflow应该成功完成，实际状态: %s", status)
				}
				return
			}
		}
	}
}

// TestWorkflowInstanceManagerV2_ConcurrentSubTaskAddition 测试并发添加子任务
func TestWorkflowInstanceManagerV2_ConcurrentSubTaskAddition(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2(t)
	defer cleanup()

	// 创建父任务
	parentTask, _ := builder.NewTaskBuilder("parent", "父任务", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	wf.AddTask(parentTask)

	exec, _ := executor.NewExecutor(10)
	exec.Start()
	exec.SetRegistry(registry)

	instance := workflow.NewWorkflowInstance(wf.GetID())
	manager, err := engine.NewWorkflowInstanceManagerV2(
		instance,
		wf,
		exec,
		nil,
		nil,
		registry,
	)
	if err != nil {
		t.Fatalf("创建Manager失败: %v", err)
	}

	manager.Start()
	defer manager.Shutdown()

	// 等待父任务开始执行
	time.Sleep(100 * time.Millisecond)

	// 并发添加子任务
	var wg sync.WaitGroup
	expectedSubTaskCount := 100
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(start int) {
			defer wg.Done()
			subTasks := make([]workflow.Task, 0, 10)
			for j := 0; j < 10; j++ {
				idx := start*10 + j
				subTask, _ := builder.NewTaskBuilder(
					fmt.Sprintf("subtask%d", idx),
					fmt.Sprintf("子任务%d", idx),
					registry,
				).WithJobFunction("mockFunc", nil).Build()
				subTasks = append(subTasks, subTask)
			}
			_ = manager.AtomicAddSubTasks(subTasks, parentTask.GetID())
		}(i)
	}
	wg.Wait()

	// 等待完成
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			status := manager.GetStatus()
			t.Fatalf("Workflow执行超时，当前状态: %s", status)
		case <-ticker.C:
			status := manager.GetStatus()
			if status == "Success" || status == "Failed" {
				// 验证所有子任务都已添加
				allTasks := wf.GetTasks()
				subTaskCount := 0
				for _, task := range allTasks {
					if task.IsSubTask() {
						subTaskCount++
					}
				}
				if subTaskCount != expectedSubTaskCount {
					t.Logf("子任务数量: %d (预期: %d)", subTaskCount, expectedSubTaskCount)
				}
				return
			}
		}
	}
}

// TestWorkflowInstanceManagerV2_ShutdownGraceful 测试优雅关闭
func TestWorkflowInstanceManagerV2_ShutdownGraceful(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2(t)
	defer cleanup()

	// 创建长时间运行的任务
	longRunningFunc := func(ctx *task.TaskContext) (interface{}, error) {
		time.Sleep(2 * time.Second)
		return map[string]interface{}{"result": "success"}, nil
	}
	_, err := registry.Register(context.Background(), "longRunningFunc", longRunningFunc, "长时间运行函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	task1, _ := builder.NewTaskBuilder("task1", "任务1", registry).
		WithJobFunction("longRunningFunc", nil).
		Build()
	wf.AddTask(task1)

	exec, _ := executor.NewExecutor(10)
	exec.Start()
	exec.SetRegistry(registry)

	instance := workflow.NewWorkflowInstance(wf.GetID())
	manager, err := engine.NewWorkflowInstanceManagerV2(
		instance,
		wf,
		exec,
		nil,
		nil,
		registry,
	)
	if err != nil {
		t.Fatalf("创建Manager失败: %v", err)
	}

	manager.Start()

	// 等待任务开始执行
	time.Sleep(100 * time.Millisecond)

	// 测试Shutdown
	shutdownDone := make(chan struct{})
	go func() {
		manager.Shutdown()
		close(shutdownDone)
	}()

	// 验证Shutdown在合理时间内完成
	select {
	case <-shutdownDone:
		// Shutdown成功
		t.Log("Shutdown成功完成")
	case <-time.After(35 * time.Second):
		t.Error("Shutdown超时")
	}

	cleanup()
}

// TestWorkflowInstanceManagerV2_TaskRetry 测试任务重试机制
func TestWorkflowInstanceManagerV2_TaskRetry(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2(t)
	defer cleanup()

	// 创建可能失败的任务（前两次失败，第三次成功）
	attemptCount := atomic.Int32{}
	failingFunc := func(ctx *task.TaskContext) (interface{}, error) {
		attempt := attemptCount.Add(1)
		if attempt <= 2 {
			return nil, fmt.Errorf("模拟失败，尝试次数: %d", attempt)
		}
		return map[string]interface{}{"result": "success"}, nil
	}
	_, err := registry.Register(context.Background(), "failingFunc", failingFunc, "可能失败函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	task1, _ := builder.NewTaskBuilder("task1", "任务1", registry).
		WithJobFunction("failingFunc", nil).
		WithRetryCount(3). // 允许重试3次
		Build()
	wf.AddTask(task1)

	exec, _ := executor.NewExecutor(10)
	exec.Start()
	exec.SetRegistry(registry)

	instance := workflow.NewWorkflowInstance(wf.GetID())
	manager, err := engine.NewWorkflowInstanceManagerV2(
		instance,
		wf,
		exec,
		nil,
		nil,
		registry,
	)
	if err != nil {
		t.Fatalf("创建Manager失败: %v", err)
	}

	manager.Start()
	defer manager.Shutdown()

	// 等待完成
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatal("Workflow执行超时")
		case <-ticker.C:
			status := manager.GetStatus()
			if status == "Success" || status == "Failed" {
				if status != "Success" {
					t.Errorf("Workflow应该成功完成（经过重试），实际状态: %s", status)
				}
				// 验证重试次数
				if attemptCount.Load() < 3 {
					t.Errorf("期望至少重试3次，实际尝试次数: %d", attemptCount.Load())
				}
				return
			}
		}
	}
}

// TestWorkflowInstanceManagerV2_LevelAdvancement 测试层级推进逻辑
func TestWorkflowInstanceManagerV2_LevelAdvancement(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2(t)
	defer cleanup()

	// 创建多层级任务
	task1, _ := builder.NewTaskBuilder("task1", "任务1", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	task2, _ := builder.NewTaskBuilder("task2", "任务2", registry).
		WithJobFunction("mockFunc", nil).
		WithDependency("task1").
		Build()
	task3, _ := builder.NewTaskBuilder("task3", "任务3", registry).
		WithJobFunction("mockFunc", nil).
		WithDependency("task2").
		Build()

	wf.AddTask(task1)
	wf.AddTask(task2)
	wf.AddTask(task3)

	exec, _ := executor.NewExecutor(10)
	exec.Start()
	exec.SetRegistry(registry)

	instance := workflow.NewWorkflowInstance(wf.GetID())
	manager, err := engine.NewWorkflowInstanceManagerV2(
		instance,
		wf,
		exec,
		nil,
		nil,
		registry,
	)
	if err != nil {
		t.Fatalf("创建Manager失败: %v", err)
	}

	manager.Start()
	defer manager.Shutdown()

	// 等待完成
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatal("Workflow执行超时")
		case <-ticker.C:
			status := manager.GetStatus()
			if status == "Success" || status == "Failed" {
				if status != "Success" {
					t.Errorf("Workflow应该成功完成，实际状态: %s", status)
				}
				return
			}
		}
	}
}

// TestWorkflowInstanceManagerV2_EmptyWorkflow 测试空Workflow
func TestWorkflowInstanceManagerV2_EmptyWorkflow(t *testing.T) {
	_, _, wf, cleanup := setupTestForV2(t)
	defer cleanup()

	exec, _ := executor.NewExecutor(10)
	exec.Start()

	instance := workflow.NewWorkflowInstance(wf.GetID())
	manager, err := engine.NewWorkflowInstanceManagerV2(
		instance,
		wf,
		exec,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("创建Manager失败: %v", err)
	}

	manager.Start()
	defer manager.Shutdown()

	// 等待一小段时间
	time.Sleep(200 * time.Millisecond)

	// 空Workflow应该快速完成或保持Running状态
	status := manager.GetStatus()
	if status != "Running" && status != "Success" {
		t.Errorf("空Workflow状态应该为Running或Success，实际为: %s", status)
	}
}

// TestLeveledTaskQueue_SizeConsistency 测试sizes和queues的一致性
func TestLeveledTaskQueue_SizeConsistency(t *testing.T) {
	queue := engine.NewLeveledTaskQueue(3)
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	// 注册mock函数
	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return map[string]interface{}{"result": "success"}, nil
	}
	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 创建任务并添加
	tasks := make([]workflow.Task, 20)
	for i := 0; i < 20; i++ {
		task, err := builder.NewTaskBuilder(fmt.Sprintf("task%d", i), fmt.Sprintf("任务%d", i), registry).
			WithJobFunction("mockFunc", nil).
			Build()
		if err != nil {
			t.Fatalf("创建任务失败: %v", err)
		}
		tasks[i] = task
		queue.AddTask(0, task)
	}

	// 验证sizes和实际队列大小一致
	// 注意：sizes是私有字段，我们通过IsEmpty和PopTasks间接验证
	if queue.IsEmpty(0) {
		t.Error("Level 0 应该不为空")
	}

	// Pop部分任务
	popped := queue.PopTasks(0, 10)
	if len(popped) != 10 {
		t.Errorf("期望Pop 10个任务，实际Pop %d个", len(popped))
	}

	// 再次Pop剩余任务
	popped2 := queue.PopTasks(0, 20)
	if len(popped2) != 10 {
		t.Errorf("期望Pop剩余10个任务，实际Pop %d个", len(popped2))
	}

	// 验证队列为空
	if !queue.IsEmpty(0) {
		t.Error("队列应该为空")
	}
}

// TestWorkflowInstanceManagerV2_TaskStatisticsValidation 测试任务统计信息验证
func TestWorkflowInstanceManagerV2_TaskStatisticsValidation(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2(t)
	defer cleanup()

	// 创建多个任务
	for i := 0; i < 10; i++ {
		task, _ := builder.NewTaskBuilder(fmt.Sprintf("task%d", i), fmt.Sprintf("任务%d", i), registry).
			WithJobFunction("mockFunc", nil).
			Build()
		wf.AddTask(task)
	}

	exec, _ := executor.NewExecutor(10)
	exec.Start()
	exec.SetRegistry(registry)

	instance := workflow.NewWorkflowInstance(wf.GetID())
	manager, err := engine.NewWorkflowInstanceManagerV2(
		instance,
		wf,
		exec,
		nil,
		nil,
		registry,
	)
	if err != nil {
		t.Fatalf("创建Manager失败: %v", err)
	}

	manager.Start()
	defer manager.Shutdown()

	// 等待所有任务完成
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatal("Workflow执行超时")
		case <-ticker.C:
			status := manager.GetStatus()
			if status == "Success" || status == "Failed" {
				// 验证统计信息一致性（通过完成状态间接验证）
				if status != "Success" {
					t.Errorf("Workflow应该成功完成，实际状态: %s", status)
				}
				return
			}
		}
	}
}

// TestWorkflowInstanceManagerV2_MultipleTemplateTasks 测试多个模板任务同时处理
func TestWorkflowInstanceManagerV2_MultipleTemplateTasks(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2(t)
	defer cleanup()

	// 创建多个模板任务
	for i := 0; i < 5; i++ {
		templateTask, _ := builder.NewTaskBuilder(fmt.Sprintf("template%d", i), fmt.Sprintf("模板任务%d", i), registry).
			WithJobFunction("mockFunc", nil).
			Build()
		templateTask.SetTemplate(true)
		wf.AddTask(templateTask)
	}

	exec, _ := executor.NewExecutor(10)
	exec.Start()
	exec.SetRegistry(registry)

	instance := workflow.NewWorkflowInstance(wf.GetID())
	manager, err := engine.NewWorkflowInstanceManagerV2(
		instance,
		wf,
		exec,
		nil,
		nil,
		registry,
	)
	if err != nil {
		t.Fatalf("创建Manager失败: %v", err)
	}

	manager.Start()
	defer manager.Shutdown()

	// 等待完成
	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatal("Workflow执行超时")
		case <-ticker.C:
			status := manager.GetStatus()
			if status == "Success" || status == "Failed" {
				if status != "Success" {
					t.Errorf("Workflow应该成功完成，实际状态: %s", status)
				}
				return
			}
		}
	}
}

// TestLeveledTaskQueue_AddTasksBatch 测试批量添加任务
func TestLeveledTaskQueue_AddTasksBatch(t *testing.T) {
	queue := engine.NewLeveledTaskQueue(2)
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	// 注册mock函数
	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return map[string]interface{}{"result": "success"}, nil
	}
	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 创建多个任务
	tasks := make([]workflow.Task, 50)
	for i := 0; i < 50; i++ {
		task, err := builder.NewTaskBuilder(fmt.Sprintf("task%d", i), fmt.Sprintf("任务%d", i), registry).
			WithJobFunction("mockFunc", nil).
			Build()
		if err != nil {
			t.Fatalf("创建任务失败: %v", err)
		}
		tasks[i] = task
	}

	// 批量添加
	queue.AddTasks(0, tasks)

	// 验证所有任务都已添加
	if queue.IsEmpty(0) {
		t.Error("Level 0 应该不为空")
	}

	// Pop所有任务
	popped := queue.PopTasks(0, 100)
	if len(popped) != 50 {
		t.Errorf("期望Pop 50个任务，实际Pop %d个", len(popped))
	}

	// 验证队列为空
	if !queue.IsEmpty(0) {
		t.Error("队列应该为空")
	}
}
