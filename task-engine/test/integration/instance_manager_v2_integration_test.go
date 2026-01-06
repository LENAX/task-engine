package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stevelan1995/task-engine/internal/storage/sqlite"
	"github.com/stevelan1995/task-engine/pkg/core/builder"
	"github.com/stevelan1995/task-engine/pkg/core/engine"
	"github.com/stevelan1995/task-engine/pkg/core/executor"
	"github.com/stevelan1995/task-engine/pkg/core/task"
	"github.com/stevelan1995/task-engine/pkg/core/types"
	"github.com/stevelan1995/task-engine/pkg/core/workflow"
)

// TestWorkflowInstanceManagerV2_BasicWorkflow 测试基础workflow执行
func TestWorkflowInstanceManagerV2_BasicWorkflow(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2(t)
	defer cleanup()

	ctx := context.Background()

	// 注册函数
	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		time.Sleep(10 * time.Millisecond)
		return map[string]interface{}{
			"result": "success",
		}, nil
	}
	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 创建任务
	task1, _ := builder.NewTaskBuilder("task1", "任务1", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	task2, _ := builder.NewTaskBuilder("task2", "任务2", registry).
		WithJobFunction("mockFunc", nil).
		WithDependency("task1").
		Build()

	wf.AddTask(task1)
	wf.AddTask(task2)

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

	// 启动
	manager.Start()
	defer func() {
		manager.Shutdown()
		exec.Shutdown() // 确保 executor 也被关闭
	}()

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

// TestWorkflowInstanceManagerV2_DynamicSubTasks 测试动态子任务生成
func TestWorkflowInstanceManagerV2_DynamicSubTasks(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2(t)
	defer cleanup()

	ctx := context.Background()

	// 注册函数
	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return map[string]interface{}{
			"result": "success",
		}, nil
	}
	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 创建模板任务，在handler中添加子任务
	templateTask, _ := builder.NewTaskBuilder("template", "模板任务", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	templateTask.SetTemplate(true)

	// 注册模板任务的Success handler，在handler中批量添加子任务
	templateHandler := func(ctx *task.TaskContext) {
		// 使用 GetDependencyTyped 获取类型安全的 InstanceManager
		type InstanceManagerWithAtomicAddSubTasks interface {
			AtomicAddSubTasks(subTasks []types.Task, parentTaskID string) error
		}

		instanceManager, ok := task.GetDependencyTyped[InstanceManagerWithAtomicAddSubTasks](ctx.Context(), "InstanceManager")
		if !ok {
			fmt.Printf("获取InstanceManager失败，尝试使用 GetDependency\n")
			// 降级方案：使用 GetDependency 然后类型断言
			managerInterface, ok2 := ctx.GetDependency("InstanceManager")
			if !ok2 {
				fmt.Printf("通过 GetDependency 也获取失败\n")
				return
			}
			instanceManager, ok = managerInterface.(InstanceManagerWithAtomicAddSubTasks)
			if !ok {
				fmt.Printf("类型转换失败: %T\n", managerInterface)
				return
			}
		}

		// 先创建所有子任务
		subTasks := make([]types.Task, 0, 10)
		for i := 0; i < 10; i++ {
			subTaskName := fmt.Sprintf("subtask%d", i)
			subTask, err := builder.NewTaskBuilder(
				subTaskName,
				"子任务",
				registry,
			).WithJobFunction("mockFunc", nil).Build()
			if err != nil {
				fmt.Printf("创建子任务失败: %v\n", err)
				return
			}
			subTasks = append(subTasks, subTask)
		}

		// 使用 AtomicAddSubTasks 一次性批量添加所有子任务（保证原子性）
		err := instanceManager.AtomicAddSubTasks(subTasks, templateTask.GetID())
		if err != nil {
			fmt.Printf("批量添加子任务失败: %v\n", err)
			return
		}
		fmt.Printf("成功批量添加 %d 个子任务\n", len(subTasks))
	}
	templateTask.SetStatusHandlers(map[string][]string{
		"SUCCESS": {"templateHandler"},
	})

	// 注册handler函数
	_, err = registry.RegisterTaskHandler(ctx, "templateHandler", templateHandler, "模板任务handler")
	if err != nil {
		t.Fatalf("注册handler失败: %v", err)
	}

	// 先添加任务到workflow
	wf.AddTask(templateTask)

	// 创建executor
	exec, err := executor.NewExecutor(10)
	if err != nil {
		t.Fatalf("创建Executor失败: %v", err)
	}
	exec.Start()
	exec.SetRegistry(registry)

	// 创建Manager（需要在添加任务之后创建，以便初始化时包含任务）
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

// setupTestForV2 设置V2集成测试环境
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

// TestWorkflowInstanceManagerV2_AtomicAddSubTasks_Concurrent 测试并发场景下的AtomicAddSubTasks
func TestWorkflowInstanceManagerV2_AtomicAddSubTasks_Concurrent(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2(t)
	defer cleanup()

	ctx := context.Background()

	// 注册函数
	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return map[string]interface{}{
			"result": "success",
		}, nil
	}
	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 创建多个模板任务，每个都会批量添加子任务
	templateTask1, _ := builder.NewTaskBuilder("template1", "模板任务1", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	templateTask1.SetTemplate(true)

	templateTask2, _ := builder.NewTaskBuilder("template2", "模板任务2", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	templateTask2.SetTemplate(true)

	// 注册handler，批量添加子任务
	templateHandler := func(ctx *task.TaskContext) {
		type InstanceManagerWithAtomicAddSubTasks interface {
			AtomicAddSubTasks(subTasks []types.Task, parentTaskID string) error
		}

		instanceManager, ok := task.GetDependencyTyped[InstanceManagerWithAtomicAddSubTasks](ctx.Context(), "InstanceManager")
		if !ok {
			managerInterface, ok2 := ctx.GetDependency("InstanceManager")
			if !ok2 {
				return
			}
			instanceManager, ok = managerInterface.(InstanceManagerWithAtomicAddSubTasks)
			if !ok {
				return
			}
		}

		// 批量创建并添加子任务
		subTasks := make([]types.Task, 0, 5)
		for i := 0; i < 5; i++ {
			subTaskName := fmt.Sprintf("subtask-%s-%d", ctx.TaskID, i)
			subTask, err := builder.NewTaskBuilder(
				subTaskName,
				"子任务",
				registry,
			).WithJobFunction("mockFunc", nil).Build()
			if err != nil {
				return
			}
			subTasks = append(subTasks, subTask)
		}

		_ = instanceManager.AtomicAddSubTasks(subTasks, ctx.TaskID)
	}

	templateTask1.SetStatusHandlers(map[string][]string{
		"SUCCESS": {"templateHandler"},
	})
	templateTask2.SetStatusHandlers(map[string][]string{
		"SUCCESS": {"templateHandler"},
	})

	_, err = registry.RegisterTaskHandler(ctx, "templateHandler", templateHandler, "模板任务handler")
	if err != nil {
		t.Fatalf("注册handler失败: %v", err)
	}

	wf.AddTask(templateTask1)
	wf.AddTask(templateTask2)

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

	// 等待完成
	timeout := time.After(10 * time.Second)
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
