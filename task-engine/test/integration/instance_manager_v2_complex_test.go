package integration

import (
	"context"
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/stevelan1995/task-engine/pkg/core/builder"
	"github.com/stevelan1995/task-engine/pkg/core/engine"
	"github.com/stevelan1995/task-engine/pkg/core/executor"
	"github.com/stevelan1995/task-engine/pkg/core/task"
	"github.com/stevelan1995/task-engine/pkg/core/types"
	"github.com/stevelan1995/task-engine/pkg/core/workflow"
)

// TestInstanceManagerV2_LargeBatchTasks 测试大批量任务场景
func TestInstanceManagerV2_LargeBatchTasks(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过大批量任务测试（使用 -short 标志）")
	}

	_, registry, wf, cleanup := setupTestForV2Integration(t)
	defer cleanup()

	ctx := context.Background()

	// 创建Job函数
	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		time.Sleep(5 * time.Millisecond)
		return map[string]interface{}{
			"result":  "success",
			"task_id": ctx.TaskID,
		}, nil
	}

	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 创建1000个任务
	taskCount := 1000
	t.Logf("开始创建 %d 个任务...", taskCount)

	for i := 0; i < taskCount; i++ {
		taskName := fmt.Sprintf("task-%d", i)
		taskObj, err := builder.NewTaskBuilder(taskName, fmt.Sprintf("任务%d", i), registry).
			WithJobFunction("mockFunc", nil).
			Build()
		if err != nil {
			t.Fatalf("构建任务 %d 失败: %v", i, err)
		}

		if err := wf.AddTask(taskObj); err != nil {
			t.Fatalf("添加任务 %d 失败: %v", i, err)
		}

		if (i+1)%100 == 0 {
			t.Logf("已创建 %d 个任务", i+1)
		}
	}

	// 创建executor
	exec, err := executor.NewExecutor(1000)
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
		nil, // pluginManager
	)
	if err != nil {
		t.Fatalf("创建Manager失败: %v", err)
	}

	manager.Start()
	defer func() {
		manager.Shutdown()
		exec.Shutdown() // 确保 executor 也被关闭
	}()

	// 等待执行完成
	timeout := time.After(2 * time.Minute)
	startTime := time.Now()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatalf("Workflow执行超时，当前状态: %s", manager.GetStatus())
		case <-ticker.C:
			status := manager.GetStatus()
			if status == "Success" || status == "Failed" {
				elapsed := time.Since(startTime)
				t.Logf("Workflow完成，状态: %s, 耗时: %v", status, elapsed)
				if status != "Success" {
					t.Errorf("Workflow应该成功完成，实际状态: %s", status)
				}
				return
			}
			if time.Since(startTime) > 30*time.Second {
				t.Logf("Workflow状态: %s, 已运行: %v", status, time.Since(startTime))
			}
		}
	}
}

// TestInstanceManagerV2_ComplexDependencies 测试复杂依赖场景
func TestInstanceManagerV2_ComplexDependencies(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2Integration(t)
	defer cleanup()

	ctx := context.Background()

	// 创建Job函数
	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		time.Sleep(10 * time.Millisecond)
		return map[string]interface{}{
			"result":  "success",
			"task_id": ctx.TaskID,
		}, nil
	}

	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 创建复杂的依赖关系：
	// Level 0: root1, root2, root3 (3个根任务)
	// Level 1: mid1 (依赖root1, root2), mid2 (依赖root2, root3), mid3 (依赖root1, root3)
	// Level 2: leaf1 (依赖mid1, mid2), leaf2 (依赖mid2, mid3), leaf3 (依赖mid1, mid3)
	// Level 3: final (依赖leaf1, leaf2, leaf3)

	// Level 0: 根任务
	rootTasks := make([]*task.Task, 3)
	for i := 0; i < 3; i++ {
		taskName := fmt.Sprintf("root%d", i+1)
		taskObj, err := builder.NewTaskBuilder(taskName, fmt.Sprintf("根任务%d", i+1), registry).
			WithJobFunction("mockFunc", nil).
			Build()
		if err != nil {
			t.Fatalf("构建根任务 %d 失败: %v", i, err)
		}
		rootTasks[i] = taskObj
		wf.AddTask(taskObj)
	}

	// Level 1: 中间任务
	midTasks := make([]*task.Task, 3)
	midDeps := [][]string{
		{"root1", "root2"},
		{"root2", "root3"},
		{"root1", "root3"},
	}
	for i := 0; i < 3; i++ {
		taskName := fmt.Sprintf("mid%d", i+1)
		builder := builder.NewTaskBuilder(taskName, fmt.Sprintf("中间任务%d", i+1), registry).
			WithJobFunction("mockFunc", nil)
		for _, dep := range midDeps[i] {
			builder = builder.WithDependency(dep)
		}
		taskObj, err := builder.Build()
		if err != nil {
			t.Fatalf("构建中间任务 %d 失败: %v", i, err)
		}
		midTasks[i] = taskObj
		wf.AddTask(taskObj)
	}

	// Level 2: 叶子任务
	leafTasks := make([]*task.Task, 3)
	leafDeps := [][]string{
		{"mid1", "mid2"},
		{"mid2", "mid3"},
		{"mid1", "mid3"},
	}
	for i := 0; i < 3; i++ {
		taskName := fmt.Sprintf("leaf%d", i+1)
		builder := builder.NewTaskBuilder(taskName, fmt.Sprintf("叶子任务%d", i+1), registry).
			WithJobFunction("mockFunc", nil)
		for _, dep := range leafDeps[i] {
			builder = builder.WithDependency(dep)
		}
		taskObj, err := builder.Build()
		if err != nil {
			t.Fatalf("构建叶子任务 %d 失败: %v", i, err)
		}
		leafTasks[i] = taskObj
		wf.AddTask(taskObj)
	}

	// Level 3: 最终任务
	finalTask, err := builder.NewTaskBuilder("final", "最终任务", registry).
		WithJobFunction("mockFunc", nil).
		WithDependency("leaf1").
		WithDependency("leaf2").
		WithDependency("leaf3").
		Build()
	if err != nil {
		t.Fatalf("构建最终任务失败: %v", err)
	}
	wf.AddTask(finalTask)

	t.Logf("创建了复杂依赖关系：3个根任务 -> 3个中间任务 -> 3个叶子任务 -> 1个最终任务")

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
		nil, // pluginManager
	)
	if err != nil {
		t.Fatalf("创建Manager失败: %v", err)
	}

	manager.Start()
	defer func() {
		manager.Shutdown()
		exec.Shutdown() // 确保 executor 也被关闭
	}()

	// 等待执行完成
	timeout := time.After(30 * time.Second)
	startTime := time.Now()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatalf("Workflow执行超时，当前状态: %s", manager.GetStatus())
		case <-ticker.C:
			status := manager.GetStatus()
			if status == "Success" || status == "Failed" {
				elapsed := time.Since(startTime)
				t.Logf("Workflow完成，状态: %s, 耗时: %v", status, elapsed)
				if status != "Success" {
					t.Errorf("Workflow应该成功完成，实际状态: %s", status)
				}
				return
			}
		}
	}
}

// TestInstanceManagerV2_MultipleTemplateTasks 测试多个模板任务场景
func TestInstanceManagerV2_MultipleTemplateTasks(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2Integration(t)
	defer cleanup()

	ctx := context.Background()

	// 创建Job函数
	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		time.Sleep(10 * time.Millisecond)
		return map[string]interface{}{
			"result":  "success",
			"task_id": ctx.TaskID,
		}, nil
	}

	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 定义模板任务handler，批量添加子任务
	type InstanceManagerWithAtomicAddSubTasks interface {
		AtomicAddSubTasks(subTasks []types.Task, parentTaskID string) error
	}

	templateHandler := func(ctx *task.TaskContext) {
		log.Printf("templateHandler 开始执行，TaskID=%s", ctx.TaskID)
		instanceManager, ok := task.GetDependencyTyped[InstanceManagerWithAtomicAddSubTasks](ctx.Context(), "InstanceManager")
		if !ok {
			managerInterface, ok2 := ctx.GetDependency("InstanceManager")
			if !ok2 {
				log.Printf("错误: templateHandler 无法获取 InstanceManager，TaskID=%s", ctx.TaskID)
				return
			}
			instanceManager, ok = managerInterface.(InstanceManagerWithAtomicAddSubTasks)
			if !ok {
				log.Printf("错误: templateHandler InstanceManager 类型转换失败，TaskID=%s", ctx.TaskID)
				return
			}
		}

		// 为每个模板任务生成5个子任务
		subTasks := make([]types.Task, 0, 5)
		for i := 0; i < 5; i++ {
			subTaskName := fmt.Sprintf("subtask-%s-%d", ctx.TaskID, i)
			subTask, err := builder.NewTaskBuilder(
				subTaskName,
				"子任务",
				registry,
			).WithJobFunction("mockFunc", nil).Build()
			if err != nil {
				log.Printf("错误: templateHandler 创建子任务失败，TaskID=%s, Index=%d, Error=%v", ctx.TaskID, i, err)
				return
			}
			subTasks = append(subTasks, subTask)
		}

		// 添加子任务，检查错误
		if err := instanceManager.AtomicAddSubTasks(subTasks, ctx.TaskID); err != nil {
			log.Printf("错误: templateHandler 添加子任务失败，TaskID=%s, Error=%v", ctx.TaskID, err)
			return
		}
		log.Printf("templateHandler 成功添加 %d 个子任务，TaskID=%s", len(subTasks), ctx.TaskID)
	}

	_, err = registry.RegisterTaskHandler(ctx, "templateHandler", templateHandler, "模板任务handler")
	if err != nil {
		t.Fatalf("注册handler失败: %v", err)
	}

	// 创建3个模板任务
	templateTaskCount := 3
	templateTasks := make([]*task.Task, templateTaskCount)
	for i := 0; i < templateTaskCount; i++ {
		taskName := fmt.Sprintf("template%d", i+1)
		taskObj, err := builder.NewTaskBuilder(taskName, fmt.Sprintf("模板任务%d", i+1), registry).
			WithJobFunction("mockFunc", nil).
			Build()
		if err != nil {
			t.Fatalf("构建模板任务 %d 失败: %v", i, err)
		}
		taskObj.SetTemplate(true)
		taskObj.SetStatusHandlers(map[string][]string{
			"SUCCESS": {"templateHandler"},
		})
		templateTasks[i] = taskObj
		wf.AddTask(taskObj)
	}

	t.Logf("创建了 %d 个模板任务，每个将生成 5 个子任务，总共 %d 个任务",
		templateTaskCount, templateTaskCount+templateTaskCount*5)

	// 创建executor
	exec, err := executor.NewExecutor(20)
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
		nil, // pluginManager
	)
	if err != nil {
		t.Fatalf("创建Manager失败: %v", err)
	}

	manager.Start()
	defer func() {
		manager.Shutdown()
		exec.Shutdown() // 确保 executor 也被关闭
	}()

	// 等待执行完成
	timeout := time.After(30 * time.Second) // 减少超时时间，更快发现问题
	startTime := time.Now()
	ticker := time.NewTicker(500 * time.Millisecond) // 更频繁的检查
	defer ticker.Stop()

	lastStatus := ""
	lastTaskCount := 0
	stuckCount := 0

	for {
		select {
		case <-timeout:
			// 超时时输出详细信息
			allTasks := wf.GetTasks()
			currentStatus := manager.GetStatus()
			t.Logf("=== 超时调试信息 ===")
			t.Logf("当前状态: %s", currentStatus)
			t.Logf("总任务数: %d (期望: %d)", len(allTasks), templateTaskCount+templateTaskCount*5)
			t.Logf("已运行时间: %v", time.Since(startTime))

			// 统计任务状态
			statusCount := make(map[string]int)
			for _, task := range allTasks {
				statusCount[task.GetStatus()]++
			}
			t.Logf("任务状态统计: %+v", statusCount)

			// 检查子任务
			subTaskCount := 0
			for _, task := range allTasks {
				if task.IsSubTask() {
					subTaskCount++
				}
			}
			t.Logf("子任务数: %d (期望: %d)", subTaskCount, templateTaskCount*5)

			t.Fatalf("Workflow执行超时，当前状态: %s, 总任务数: %d (期望: %d)",
				currentStatus, len(allTasks), templateTaskCount+templateTaskCount*5)

		case <-ticker.C:
			status := manager.GetStatus()
			allTasks := wf.GetTasks()
			currentTaskCount := len(allTasks)

			// 检查是否卡住（状态和任务数都没有变化）
			if status == lastStatus && currentTaskCount == lastTaskCount {
				stuckCount++
				if stuckCount > 10 { // 5秒没有变化
					t.Logf("警告: Workflow可能卡住，状态: %s, 任务数: %d, 已卡住: %d次检查",
						status, currentTaskCount, stuckCount)
				}
			} else {
				stuckCount = 0
			}
			lastStatus = status
			lastTaskCount = currentTaskCount

			if status == "Success" || status == "Failed" {
				elapsed := time.Since(startTime)
				t.Logf("Workflow完成，状态: %s, 耗时: %v", status, elapsed)

				// 验证所有任务都已添加
				expectedTaskCount := templateTaskCount + templateTaskCount*5
				if len(allTasks) != expectedTaskCount {
					t.Errorf("期望任务数: %d, 实际任务数: %d", expectedTaskCount, len(allTasks))

					// 输出详细的任务信息
					t.Logf("=== 任务详情 ===")
					for taskID, task := range allTasks {
						t.Logf("TaskID: %s, Name: %s, Status: %s, IsSubTask: %v, IsTemplate: %v",
							taskID, task.GetName(), task.GetStatus(), task.IsSubTask(), task.IsTemplate())
					}
				}

				if status != "Success" {
					t.Errorf("Workflow应该成功完成，实际状态: %s", status)
				}
				return
			}

			// 定期输出进度
			if time.Since(startTime) > 5*time.Second && int(time.Since(startTime).Seconds())%5 == 0 {
				t.Logf("Workflow状态: %s, 已运行: %v, 任务数: %d (期望: %d)",
					status, time.Since(startTime), currentTaskCount, templateTaskCount+templateTaskCount*5)
			}
		}
	}
}

// TestInstanceManagerV2_TemplateTasksWithDependencies 测试带依赖关系的多个模板任务
func TestInstanceManagerV2_TemplateTasksWithDependencies(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2Integration(t)
	defer cleanup()

	ctx := context.Background()

	// 创建Job函数
	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		time.Sleep(10 * time.Millisecond)
		return map[string]interface{}{
			"result":  "success",
			"task_id": ctx.TaskID,
		}, nil
	}

	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 定义模板任务handler
	type InstanceManagerWithAtomicAddSubTasks interface {
		AtomicAddSubTasks(subTasks []types.Task, parentTaskID string) error
	}

	templateHandler := func(ctx *task.TaskContext) {
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

		// 为每个模板任务生成3个子任务
		subTasks := make([]types.Task, 0, 3)
		for i := 0; i < 3; i++ {
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

	_, err = registry.RegisterTaskHandler(ctx, "templateHandler", templateHandler, "模板任务handler")
	if err != nil {
		t.Fatalf("注册handler失败: %v", err)
	}

	// 创建根任务
	rootTask, err := builder.NewTaskBuilder("root", "根任务", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	if err != nil {
		t.Fatalf("构建根任务失败: %v", err)
	}
	wf.AddTask(rootTask)

	// 创建3个模板任务，都依赖根任务
	templateTaskCount := 3
	templateTasks := make([]*task.Task, templateTaskCount)
	for i := 0; i < templateTaskCount; i++ {
		taskName := fmt.Sprintf("template%d", i+1)
		taskObj, err := builder.NewTaskBuilder(taskName, fmt.Sprintf("模板任务%d", i+1), registry).
			WithJobFunction("mockFunc", nil).
			WithDependency("root").
			Build()
		if err != nil {
			t.Fatalf("构建模板任务 %d 失败: %v", i, err)
		}
		taskObj.SetTemplate(true)
		taskObj.SetStatusHandlers(map[string][]string{
			"SUCCESS": {"templateHandler"},
		})
		templateTasks[i] = taskObj
		wf.AddTask(taskObj)
	}

	t.Logf("创建了带依赖关系的模板任务：1个根任务 -> %d 个模板任务（每个生成3个子任务）",
		templateTaskCount)

	// 创建executor
	exec, err := executor.NewExecutor(20)
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
		nil, // pluginManager
	)
	if err != nil {
		t.Fatalf("创建Manager失败: %v", err)
	}

	manager.Start()
	defer func() {
		manager.Shutdown()
		exec.Shutdown() // 确保 executor 也被关闭
	}()

	// 等待执行完成
	timeout := time.After(1 * time.Minute)
	startTime := time.Now()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatalf("Workflow执行超时，当前状态: %s", manager.GetStatus())
		case <-ticker.C:
			status := manager.GetStatus()
			if status == "Success" || status == "Failed" {
				elapsed := time.Since(startTime)
				t.Logf("Workflow完成，状态: %s, 耗时: %v", status, elapsed)
				if status != "Success" {
					t.Errorf("Workflow应该成功完成，实际状态: %s", status)
				}
				// 验证所有任务都已添加
				allTasks := wf.GetTasks()
				expectedTaskCount := 1 + templateTaskCount + templateTaskCount*3 // 1个根任务 + 3个模板任务 + 9个子任务
				if len(allTasks) != expectedTaskCount {
					t.Errorf("期望任务数: %d, 实际任务数: %d", expectedTaskCount, len(allTasks))
				}
				return
			}
			if time.Since(startTime) > 10*time.Second {
				t.Logf("Workflow状态: %s, 已运行: %v", status, time.Since(startTime))
			}
		}
	}
}

// TestInstanceManagerV2_DeepLevelDependencies 测试深层级依赖场景
// 创建一个具有很多层级的依赖链，验证层级推进逻辑在处理深层依赖时的正确性
func TestInstanceManagerV2_DeepLevelDependencies(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2Integration(t)
	defer cleanup()

	ctx := context.Background()

	// 创建Job函数
	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		time.Sleep(5 * time.Millisecond)
		return map[string]interface{}{
			"result":  "success",
			"task_id": ctx.TaskID,
		}, nil
	}

	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 创建深层级依赖链
	// 配置：层级数、每层的任务数
	// 注意：go-dag 库在 AddEdge 时会递归检查循环依赖，对于深层依赖链会非常耗时
	// 因此，对于大批量任务，我们减少层级数和任务数，或者使用更轻量级的DAG实现
	numLevels := 50     // 50 层依赖（减少层级数以避免 go-dag 库的循环检测耗时）
	tasksPerLevel := 30 // 每层 10 个任务（减少任务数）
	totalTasks := numLevels * tasksPerLevel

	t.Logf("开始创建深层级依赖链：%d 层，每层 %d 个任务，总共 %d 个任务", numLevels, tasksPerLevel, totalTasks)

	// 存储每一层的任务名称，用于构建依赖关系
	levelTaskNames := make([][]string, numLevels)

	// Level 0: 根任务（无依赖）
	levelTaskNames[0] = make([]string, tasksPerLevel)
	for i := 0; i < tasksPerLevel; i++ {
		taskName := fmt.Sprintf("level0-task%d", i)
		taskObj, err := builder.NewTaskBuilder(taskName, fmt.Sprintf("层级0任务%d", i), registry).
			WithJobFunction("mockFunc", nil).
			Build()
		if err != nil {
			t.Fatalf("构建层级0任务 %d 失败: %v", i, err)
		}
		levelTaskNames[0][i] = taskName
		wf.AddTask(taskObj)
	}

	// Level 1 到 Level N-1: 每层任务依赖上一层
	for level := 1; level < numLevels; level++ {
		levelTaskNames[level] = make([]string, tasksPerLevel)
		prevLevel := level - 1

		for i := 0; i < tasksPerLevel; i++ {
			taskName := fmt.Sprintf("level%d-task%d", level, i)
			builder := builder.NewTaskBuilder(taskName, fmt.Sprintf("层级%d任务%d", level, i), registry).
				WithJobFunction("mockFunc", nil)

			// 每个任务依赖上一层的前两个任务（创建交叉依赖）
			dep1 := levelTaskNames[prevLevel][i%len(levelTaskNames[prevLevel])]
			dep2 := levelTaskNames[prevLevel][(i+1)%len(levelTaskNames[prevLevel])]
			builder = builder.WithDependency(dep1).WithDependency(dep2)

			taskObj, err := builder.Build()
			if err != nil {
				t.Fatalf("构建层级%d任务%d失败: %v", level, i, err)
			}
			levelTaskNames[level][i] = taskName
			wf.AddTask(taskObj)
		}
	}

	// 验证任务数量
	allTasks := wf.GetTasks()
	if len(allTasks) != totalTasks {
		t.Errorf("期望任务数: %d, 实际任务数: %d", totalTasks, len(allTasks))
	}

	t.Logf("创建了 %d 层依赖链，每层 %d 个任务，总共 %d 个任务", numLevels, tasksPerLevel, totalTasks)

	// 创建executor
	exec, err := executor.NewExecutor(50)
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
		nil, // pluginManager
	)
	if err != nil {
		t.Fatalf("创建Manager失败: %v", err)
	}

	manager.Start()
	defer func() {
		manager.Shutdown()
		exec.Shutdown() // 确保 executor 也被关闭
	}()

	// 等待执行完成
	timeout := time.After(2 * time.Minute)
	startTime := time.Now()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatalf("Workflow执行超时，当前状态: %s", manager.GetStatus())
		case <-ticker.C:
			status := manager.GetStatus()
			if status == "Success" || status == "Failed" {
				elapsed := time.Since(startTime)
				t.Logf("Workflow完成，状态: %s, 耗时: %v", status, elapsed)
				if status != "Success" {
					t.Errorf("Workflow应该成功完成，实际状态: %s", status)
				}

				// 验证所有任务都已完成
				allTasks = wf.GetTasks()
				completedCount := 0
				for _, task := range allTasks {
					if task.GetStatus() == "SUCCESS" || task.GetStatus() == "Success" {
						completedCount++
					}
				}
				if completedCount != totalTasks {
					t.Errorf("期望完成的任务数: %d, 实际完成的任务数: %d", totalTasks, completedCount)
				} else {
					t.Logf("✅ 所有 %d 个任务都已完成", completedCount)
				}

				return
			}
			if time.Since(startTime) > 10*time.Second {
				t.Logf("Workflow状态: %s, 已运行: %v", status, time.Since(startTime))
			}
		}
	}
}
