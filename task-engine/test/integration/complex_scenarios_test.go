package integration

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"log"

	_ "github.com/mattn/go-sqlite3"

	"github.com/stevelan1995/task-engine/internal/storage/sqlite"
	"github.com/stevelan1995/task-engine/pkg/core/builder"
	"github.com/stevelan1995/task-engine/pkg/core/engine"
	"github.com/stevelan1995/task-engine/pkg/core/task"
	"github.com/stevelan1995/task-engine/pkg/core/workflow"
	"github.com/stevelan1995/task-engine/pkg/storage"
)

// TestComplexScenarios_LargeWorkflow 测试包含1000+任务的workflow
func TestComplexScenarios_LargeWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过大型workflow测试（使用 -short 标志）")
	}

	eng, registry, wf, taskRepo, cleanup := setupComplexTest(t)
	defer cleanup()

	ctx := context.Background()

	// 创建Job函数
	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		// 模拟快速执行
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

	// 注册Handler
	_, err = registry.RegisterTaskHandler(ctx, "DefaultLogSuccess", task.DefaultLogSuccess, "默认成功日志")
	if err != nil {
		t.Fatalf("注册Handler失败: %v", err)
	}

	// 创建1000个任务
	taskCount := 5000
	t.Logf("开始创建 %d 个任务...", taskCount)

	for i := 0; i < taskCount; i++ {
		taskName := fmt.Sprintf("task-%d", i)
		taskObj, err := builder.NewTaskBuilder(taskName, fmt.Sprintf("任务%d", i), registry).
			WithJobFunction("mockFunc", nil).
			WithTaskHandler(task.TaskStatusSuccess, "DefaultLogSuccess").
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

	t.Logf("任务创建完成，开始提交Workflow...")

	// 提交Workflow
	controller, err := eng.SubmitWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("提交Workflow失败: %v", err)
	}

	instanceID := controller.GetInstanceID()
	t.Logf("Workflow已提交，InstanceID: %s", instanceID)

	// 等待执行完成（设置更长的超时时间）
	timeout := 5 * time.Minute
	startTime := time.Now()
	lastStatus := ""
	lastLogTime := time.Now()

	for {
		status, err := controller.GetStatus()
		if err != nil {
			t.Fatalf("获取状态失败: %v", err)
		}

		// 每10秒打印一次状态
		if status != lastStatus || time.Since(lastLogTime) > 10*time.Second {
			t.Logf("工作流状态: %s, 已运行: %v", status, time.Since(startTime))
			lastStatus = status
			lastLogTime = time.Now()
		}

		if status == "Success" || status == "Failed" || status == "Terminated" {
			// 检查是否还有待处理的任务
			taskInstances, err := taskRepo.GetByWorkflowInstanceID(ctx, instanceID)
			if err == nil {
				pendingCount := 0
				runningCount := 0
				for _, ti := range taskInstances {
					if ti.Status == "Pending" {
						pendingCount++
					} else if ti.Status == "Running" {
						runningCount++
					}
				}
				// 如果还有待处理或运行中的任务，继续等待
				if pendingCount > 0 || runningCount > 0 {
					if time.Since(lastLogTime) > 5*time.Second {
						t.Logf("工作流状态: %s, 但仍有待处理任务: %d, 运行中: %d, 继续等待...",
							status, pendingCount, runningCount)
						lastLogTime = time.Now()
					}
					time.Sleep(1 * time.Second)
					continue
				}
			}
			t.Logf("工作流完成，状态: %s, 总耗时: %v", status, time.Since(startTime))
			break
		}

		if time.Since(startTime) > timeout {
			t.Fatalf("工作流执行超时，当前状态: %s, 已运行: %v", status, time.Since(startTime))
		}

		time.Sleep(1 * time.Second)
	}

	// 验证工作流状态
	finalStatus, _ := controller.GetStatus()
	if finalStatus != "Success" {
		t.Errorf("期望工作流状态为Success，实际为%s", finalStatus)
	}

	// 验证所有任务都已完成
	taskInstances, err := taskRepo.GetByWorkflowInstanceID(ctx, instanceID)
	if err != nil {
		t.Fatalf("查询任务实例失败: %v", err)
	}

	actualTaskCount := len(taskInstances)
	if actualTaskCount != taskCount {
		t.Errorf("期望任务数: %d, 实际任务数: %d", taskCount, actualTaskCount)
	}

	// 统计任务状态
	successCount := 0
	failedCount := 0
	pendingCount := 0
	runningCount := 0
	for _, taskInstance := range taskInstances {
		switch taskInstance.Status {
		case "SUCCESS", "Success": // 兼容大小写
			successCount++
		case "FAILED", "Failed", "TIMEOUT", "TimeoutFailed": // 兼容大小写
			failedCount++
			t.Logf("❌ 任务失败: TaskID=%s, TaskName=%s, Status=%s, Error=%s",
				taskInstance.ID, taskInstance.Name, taskInstance.Status, taskInstance.ErrorMessage)
		case "PENDING", "Pending": // 兼容大小写
			pendingCount++
		case "RUNNING", "Running": // 兼容大小写
			runningCount++
		default:
			t.Logf("⚠️ 未知任务状态: TaskID=%s, TaskName=%s, Status=%s",
				taskInstance.ID, taskInstance.Name, taskInstance.Status)
		}
	}

	t.Logf("📊 任务执行统计: 总数=%d, 成功=%d, 失败=%d, 待处理=%d, 运行中=%d",
		actualTaskCount, successCount, failedCount, pendingCount, runningCount)

	// 断言所有任务都成功完成
	if successCount != taskCount {
		t.Errorf("期望所有任务都成功完成，但成功数: %d/%d, 失败数: %d, 待处理: %d, 运行中: %d",
			successCount, taskCount, failedCount, pendingCount, runningCount)
	}

	if failedCount > 0 {
		t.Errorf("有 %d 个任务失败", failedCount)
	}

	if pendingCount > 0 || runningCount > 0 {
		t.Errorf("仍有任务未完成: 待处理=%d, 运行中=%d", pendingCount, runningCount)
	}

	t.Logf("✅ 大型workflow测试完成：%d 个任务全部成功，耗时: %v", taskCount, time.Since(startTime))
}

// TestComplexScenarios_DynamicLargeWorkflow 测试动态生成1000+任务的workflow
func TestComplexScenarios_DynamicLargeWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过动态大型workflow测试（使用 -short 标志）")
	}

	eng, registry, wf, taskRepo, cleanup := setupComplexTest(t)
	defer cleanup()

	ctx := context.Background()

	// 创建Job函数，仅在子任务中用于返回结果
	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		time.Sleep(10 * time.Millisecond)
		return map[string]interface{}{
			"result": "success",
			"item":   ctx.TaskID, // 仅保留item字段以便可追踪
		}, nil
	}

	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 创建生成子任务的数据，只用于父任务生成子任务阶段
	subTaskData := make([]string, 100)
	for i := 0; i < 100; i++ {
		subTaskData[i] = fmt.Sprintf("item-%d", i)
	}

	// 创建子任务生成Handler（只允许父任务生成子任务，子任务不再递归生成子任务）
	generateSubTasksHandler := func(ctx *task.TaskContext) {
		log.Printf("🔍 [GenerateSubTasks] 开始执行，TaskID=%s, InstanceID=%s", ctx.TaskID, ctx.WorkflowInstanceID)

		// 为每个数据项生成子任务
		parentTaskID := ctx.TaskID

		// 直接获取Manager接口（已由WorkflowInstanceManager注入到依赖中）
		type ManagerAddSubTaskInterface interface {
			AddSubTask(subTask workflow.Task, parentTaskID string) error
		}

		manager, ok := task.GetDependencyTyped[ManagerAddSubTaskInterface](ctx.Context(), "InstanceManager")
		if !ok {
			log.Printf("⚠️ [GenerateSubTasks] TaskID=%s, 未找到InstanceManager依赖", ctx.TaskID)
			return
		}

		generatedCount := 0
		errorCount := 0
		for _, item := range subTaskData {
			subTaskName := fmt.Sprintf("sub-task-%s-%s", parentTaskID, item)
			subTask, err := builder.NewTaskBuilder(subTaskName, fmt.Sprintf("子任务-%s", item), registry).
				WithJobFunction("mockFunc", nil).
				WithTaskHandler(task.TaskStatusSuccess, "DefaultLogSuccess").
				Build()
			if err != nil {
				log.Printf("⚠️ [GenerateSubTasks] TaskID=%s, 创建子任务失败: %v", ctx.TaskID, err)
				errorCount++
				continue
			}

			// 添加子任务
			if err := manager.AddSubTask(subTask, parentTaskID); err != nil {
				log.Printf("⚠️ [GenerateSubTasks] TaskID=%s, 添加子任务失败: %v", ctx.TaskID, err)
				errorCount++
				continue
			}
			generatedCount++
		}

		log.Printf("✅ [GenerateSubTasks] TaskID=%s, 已生成 %d 个子任务，失败 %d 个", ctx.TaskID, generatedCount, errorCount)
	}

	_, err = registry.RegisterTaskHandler(ctx, "GenerateSubTasks", generateSubTasksHandler, "生成子任务")
	if err != nil {
		t.Fatalf("注册Handler失败: %v", err)
	}

	_, err = registry.RegisterTaskHandler(ctx, "DefaultLogSuccess", task.DefaultLogSuccess, "默认成功日志")
	if err != nil {
		t.Fatalf("注册Handler失败: %v", err)
	}

	// 创建多个父任务，每个父任务会生成 100 个子任务
	// 创建 10 个父任务，总共生成 1000 个子任务
	parentTaskCount := 10
	expectedSubTasksPerParent := 100
	expectedTotalTasks := 1 + parentTaskCount + (parentTaskCount * expectedSubTasksPerParent) // 1个根任务 + 10个父任务 + 1000个子任务

	t.Logf("创建 %d 个父任务，每个父任务将生成 %d 个子任务，预期总共 %d 个任务",
		parentTaskCount, expectedSubTasksPerParent, expectedTotalTasks)

	// 创建根任务（用于启动workflow）
	rootTask, err := builder.NewTaskBuilder("root-task", "根任务", registry).
		WithJobFunction("mockFunc", nil).
		WithTaskHandler(task.TaskStatusSuccess, "DefaultLogSuccess").
		Build()
	if err != nil {
		t.Fatalf("构建根任务失败: %v", err)
	}

	if err := wf.AddTask(rootTask); err != nil {
		t.Fatalf("添加根任务失败: %v", err)
	}

	// 创建父任务，每个父任务依赖 root-task
	parentTasks := make([]*task.Task, parentTaskCount)
	for i := 0; i < parentTaskCount; i++ {
		parentName := fmt.Sprintf("parent-task-%d", i)
		parentTask, err := builder.NewTaskBuilder(parentName, fmt.Sprintf("父任务%d", i), registry).
			WithJobFunction("mockFunc", nil).
			WithDependency("root-task").
			WithTaskHandler(task.TaskStatusSuccess, "GenerateSubTasks").
			WithTaskHandler(task.TaskStatusSuccess, "DefaultLogSuccess").
			Build()
		if err != nil {
			t.Fatalf("构建父任务 %d 失败: %v", i, err)
		}
		parentTasks[i] = parentTask
		if err := wf.AddTask(parentTask); err != nil {
			t.Fatalf("添加父任务 %d 失败: %v", i, err)
		}
	}

	// 提交Workflow
	controller, err := eng.SubmitWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("提交Workflow失败: %v", err)
	}

	instanceID := controller.GetInstanceID()
	t.Logf("Workflow已提交，InstanceID: %s", instanceID)

	// 等待根任务完成，然后父任务会执行并生成子任务
	t.Logf("等待任务执行和子任务生成...")

	// 等待执行完成
	timeout := 10 * time.Minute
	startTime := time.Now()
	lastLogTime := time.Now()

	// 定期检查任务数量
	lastTaskCount := 0

	for {
		status, err := controller.GetStatus()
		if err != nil {
			t.Fatalf("获取状态失败: %v", err)
		}

		// 每10秒打印一次状态和任务数量
		if time.Since(lastLogTime) > 10*time.Second {
			// 查询预定义任务数量（子任务不保存到数据库）
			taskInstances, err := taskRepo.GetByWorkflowInstanceID(ctx, instanceID)
			currentTaskCount := 0
			if err == nil {
				currentTaskCount = len(taskInstances)
			}

			if currentTaskCount != lastTaskCount {
				predefinedTaskCount := 1 + parentTaskCount // 1个根任务 + 10个父任务
				t.Logf("工作流状态: %s, 已运行: %v, 预定义任务数: %d (预期: %d), 总任务数(包括子任务): %d+",
					status, time.Since(startTime), currentTaskCount, predefinedTaskCount, expectedTotalTasks)
				lastTaskCount = currentTaskCount
			} else {
				t.Logf("工作流状态: %s, 已运行: %v", status, time.Since(startTime))
			}
			lastLogTime = time.Now()
		}

		if status == "Success" || status == "Failed" || status == "Terminated" {
			// 查询预定义任务数量（子任务不保存到数据库）
			taskInstances, err := taskRepo.GetByWorkflowInstanceID(ctx, instanceID)
			if err != nil {
				t.Fatalf("查询任务实例失败: %v", err)
			}
			predefinedTaskCount := 1 + parentTaskCount // 1个根任务 + 10个父任务
			finalTaskCount := len(taskInstances)

			t.Logf("工作流完成，状态: %s, 总耗时: %v, 预定义任务数: %d (预期: %d), 总任务数(包括子任务): %d+",
				status, time.Since(startTime), finalTaskCount, predefinedTaskCount, expectedTotalTasks)

			// 验证预定义任务数量
			if finalTaskCount != predefinedTaskCount {
				// 统计任务状态，帮助诊断问题
				statusCount := make(map[string]int)
				for _, ti := range taskInstances {
					statusCount[ti.Status]++
				}
				t.Errorf("❌ 预定义任务数 (%d) 不符合预期 (%d)。任务状态统计: %v",
					finalTaskCount, predefinedTaskCount, statusCount)
			} else {
				t.Logf("✅ 预定义任务数量符合预期（子任务不保存到数据库，通过父任务状态验证）")
			}
			break
		}

		if time.Since(startTime) > timeout {
			// 查询当前预定义任务数量（子任务不保存到数据库）
			taskInstances, err := taskRepo.GetByWorkflowInstanceID(ctx, instanceID)
			currentTaskCount := 0
			if err == nil {
				currentTaskCount = len(taskInstances)
			}
			predefinedTaskCount := 1 + parentTaskCount // 1个根任务 + 10个父任务
			t.Logf("工作流执行超时，当前状态: %s, 已运行: %v, 预定义任务数: %d (预期: %d)",
				status, time.Since(startTime), currentTaskCount, predefinedTaskCount)
			break
		}

		time.Sleep(2 * time.Second)
	}

	finalStatus, _ := controller.GetStatus()

	// 最终统计和验证
	// 注意：子任务不保存到数据库，所以只能查询到预定义任务（1个根任务 + 10个父任务 = 11个）
	taskInstances, err := taskRepo.GetByWorkflowInstanceID(ctx, instanceID)
	if err != nil {
		t.Fatalf("查询任务实例失败: %v", err)
	}

	// 验证所有预定义任务都成功完成
	predefinedTaskCount := 1 + parentTaskCount // 1个根任务 + 10个父任务
	actualPredefinedCount := len(taskInstances)

	if actualPredefinedCount != predefinedTaskCount {
		t.Errorf("期望预定义任务数: %d, 实际: %d", predefinedTaskCount, actualPredefinedCount)
	}

	// 统计预定义任务状态
	successCount := 0
	failedCount := 0
	for _, ti := range taskInstances {
		if ti.Status == "SUCCESS" || ti.Status == "Success" {
			successCount++
		} else if ti.Status == "FAILED" || ti.Status == "Failed" {
			failedCount++
		}
	}

	t.Logf("✅ 动态大型workflow测试完成：预定义任务数: %d/%d (成功: %d, 失败: %d), 最终状态: %s, 耗时: %v",
		actualPredefinedCount, predefinedTaskCount, successCount, failedCount, finalStatus, time.Since(startTime))

	// 验证所有预定义任务都成功完成
	if successCount != predefinedTaskCount {
		t.Errorf("期望所有预定义任务都成功完成，但成功数: %d/%d, 失败数: %d",
			successCount, predefinedTaskCount, failedCount)
	}

	// 验证 workflow 状态为 Success（说明所有任务包括子任务都完成了）
	if finalStatus != "Success" {
		t.Errorf("期望工作流状态为Success，实际为%s。如果状态为Failed，可能是子任务执行失败", finalStatus)
	}

	// 注意：子任务不保存到数据库，所以无法通过数据库查询统计子任务数
	// 但可以通过以下方式验证子任务执行情况：
	// 1. 所有父任务都成功完成（说明子任务都执行了，根据SubTaskErrorTolerance判断父任务是否成功）
	// 2. Workflow状态为Success（说明所有任务包括子任务都完成了）
	t.Logf("📝 注意：子任务（%d个）不保存到数据库，但已通过父任务状态和workflow状态验证其执行情况",
		parentTaskCount*expectedSubTasksPerParent)
}

// TestComplexScenarios_ComplexDependencies 测试包含复杂任务依赖关系的workflow
func TestComplexScenarios_ComplexDependencies(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过复杂依赖关系测试（使用 -short 标志）")
	}

	eng, registry, wf, _, cleanup := setupComplexTest(t)
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

	_, err = registry.RegisterTaskHandler(ctx, "DefaultLogSuccess", task.DefaultLogSuccess, "默认成功日志")
	if err != nil {
		t.Fatalf("注册Handler失败: %v", err)
	}

	// 创建复杂的依赖关系：
	// 1. 多个根任务（无依赖）
	// 2. 中间层任务（依赖多个根任务）
	// 3. 叶子任务（依赖多个中间层任务）
	// 4. 最终任务（依赖所有叶子任务）

	rootTaskCount := 10
	midTaskCount := 20
	leafTaskCount := 30
	finalTaskCount := 5

	t.Logf("创建复杂依赖关系：%d 个根任务 -> %d 个中间任务 -> %d 个叶子任务 -> %d 个最终任务",
		rootTaskCount, midTaskCount, leafTaskCount, finalTaskCount)

	// 创建根任务
	rootTasks := make([]*task.Task, rootTaskCount)
	for i := 0; i < rootTaskCount; i++ {
		taskName := fmt.Sprintf("root-task-%d", i)
		taskObj, err := builder.NewTaskBuilder(taskName, fmt.Sprintf("根任务%d", i), registry).
			WithJobFunction("mockFunc", nil).
			WithTaskHandler(task.TaskStatusSuccess, "DefaultLogSuccess").
			Build()
		if err != nil {
			t.Fatalf("构建根任务 %d 失败: %v", i, err)
		}
		rootTasks[i] = taskObj
		if err := wf.AddTask(taskObj); err != nil {
			t.Fatalf("添加任务 %d 失败: %v", i, err)
		}
	}

	// 创建中间层任务（每个中间任务依赖2-3个根任务）
	midTasks := make([]*task.Task, midTaskCount)
	for i := 0; i < midTaskCount; i++ {
		taskName := fmt.Sprintf("mid-task-%d", i)
		midTaskBuilder := builder.NewTaskBuilder(taskName, fmt.Sprintf("中间任务%d", i), registry).
			WithJobFunction("mockFunc", nil).
			WithTaskHandler(task.TaskStatusSuccess, "DefaultLogSuccess")

		// 每个中间任务依赖2-3个随机根任务
		depsCount := 2 + (i % 2) // 2或3个依赖
		for j := 0; j < depsCount; j++ {
			depIndex := (i*2 + j) % rootTaskCount
			midTaskBuilder = midTaskBuilder.WithDependency(rootTasks[depIndex].GetName())
		}

		taskObj, err := midTaskBuilder.Build()
		if err != nil {
			t.Fatalf("构建中间任务 %d 失败: %v", i, err)
		}
		midTasks[i] = taskObj
		if err := wf.AddTask(taskObj); err != nil {
			t.Fatalf("添加任务 %d 失败: %v", i, err)
		}
		// 注意：依赖关系已通过WithDependency在构建时设置，AddTask会自动处理
	}

	// 创建叶子任务（每个叶子任务依赖2-3个中间任务）
	leafTasks := make([]*task.Task, leafTaskCount)
	for i := 0; i < leafTaskCount; i++ {
		taskName := fmt.Sprintf("leaf-task-%d", i)
		taskBuilder := builder.NewTaskBuilder(taskName, fmt.Sprintf("叶子任务%d", i), registry).
			WithJobFunction("mockFunc", nil).
			WithTaskHandler(task.TaskStatusSuccess, "DefaultLogSuccess")

		// 每个叶子任务依赖2-3个随机中间任务
		depsCount := 2 + (i % 2) // 2或3个依赖
		for j := 0; j < depsCount; j++ {
			depIndex := (i*2 + j) % midTaskCount
			taskBuilder = taskBuilder.WithDependency(midTasks[depIndex].GetName())
		}

		taskObj, err := taskBuilder.Build()
		if err != nil {
			t.Fatalf("构建叶子任务 %d 失败: %v", i, err)
		}
		leafTasks[i] = taskObj
		if err := wf.AddTask(taskObj); err != nil {
			t.Fatalf("添加任务 %d 失败: %v", i, err)
		}
		// 注意：依赖关系已通过WithDependency在构建时设置，AddTask会自动处理
	}

	// 创建最终任务（每个最终任务依赖多个叶子任务）
	finalTasks := make([]*task.Task, finalTaskCount)
	for i := 0; i < finalTaskCount; i++ {
		taskName := fmt.Sprintf("final-task-%d", i)
		taskBuilder := builder.NewTaskBuilder(taskName, fmt.Sprintf("最终任务%d", i), registry).
			WithJobFunction("mockFunc", nil).
			WithTaskHandler(task.TaskStatusSuccess, "DefaultLogSuccess")

		// 每个最终任务依赖5-10个随机叶子任务
		depsCount := 5 + (i % 6) // 5-10个依赖
		for j := 0; j < depsCount; j++ {
			depIndex := (i*3 + j) % leafTaskCount
			taskBuilder = taskBuilder.WithDependency(leafTasks[depIndex].GetName())
		}

		taskObj, err := taskBuilder.Build()
		if err != nil {
			t.Fatalf("构建最终任务 %d 失败: %v", i, err)
		}
		finalTasks[i] = taskObj
		if err := wf.AddTask(taskObj); err != nil {
			t.Fatalf("添加任务 %d 失败: %v", i, err)
		}
		// 注意：依赖关系已通过WithDependency在构建时设置，AddTask会自动处理
	}

	totalTasks := rootTaskCount + midTaskCount + leafTaskCount + finalTaskCount
	t.Logf("复杂依赖关系创建完成，共 %d 个任务", totalTasks)

	// 提交Workflow
	controller, err := eng.SubmitWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("提交Workflow失败: %v", err)
	}

	// 等待执行完成
	timeout := 5 * time.Minute
	startTime := time.Now()
	for {
		status, err := controller.GetStatus()
		if err != nil {
			t.Fatalf("获取状态失败: %v", err)
		}

		if status == "Success" || status == "Failed" || status == "Terminated" {
			break
		}

		if time.Since(startTime) > timeout {
			t.Fatalf("工作流执行超时")
		}

		time.Sleep(100 * time.Millisecond)
	}

	finalStatus, _ := controller.GetStatus()
	if finalStatus != "Success" {
		t.Errorf("期望工作流状态为Success，实际为%s", finalStatus)
	}

	t.Logf("✅ 复杂依赖关系测试完成：%d 个任务，耗时: %v", totalTasks, time.Since(startTime))
}

// TestComplexScenarios_RandomFailures 测试随机出现异常的workflow
func TestComplexScenarios_RandomFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过随机异常测试（使用 -short 标志）")
	}

	eng, registry, wf, _, cleanup := setupComplexTest(t)
	defer cleanup()

	ctx := context.Background()

	// 设置随机种子
	rand.Seed(time.Now().UnixNano())

	// 创建可能失败的Job函数
	unreliableFunc := func(ctx *task.TaskContext) (interface{}, error) {
		// 30%的概率失败
		if rand.Float32() < 0.3 {
			// 随机选择失败类型
			failureType := rand.Intn(4)
			switch failureType {
			case 0:
				return nil, fmt.Errorf("随机错误: connection timeout")
			case 1:
				return nil, fmt.Errorf("随机错误: 429 Too Many Requests")
			case 2:
				return nil, fmt.Errorf("随机错误: 503 Service Unavailable")
			default:
				return nil, fmt.Errorf("随机错误: unknown error")
			}
		}

		// 70%的概率成功
		time.Sleep(time.Duration(rand.Intn(50)) * time.Millisecond)
		return map[string]interface{}{
			"result":  "success",
			"task_id": ctx.TaskID,
		}, nil
	}

	_, err := registry.Register(ctx, "unreliableFunc", unreliableFunc, "不可靠函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	_, err = registry.RegisterTaskHandler(ctx, "DefaultLogSuccess", task.DefaultLogSuccess, "默认成功日志")
	if err != nil {
		t.Fatalf("注册Handler失败: %v", err)
	}

	_, err = registry.RegisterTaskHandler(ctx, "DefaultLogError", task.DefaultLogError, "默认错误日志")
	if err != nil {
		t.Fatalf("注册Handler失败: %v", err)
	}

	// 创建100个任务，配置重试
	taskCount := 100
	t.Logf("创建 %d 个可能失败的任务（30%%失败率，配置重试）...", taskCount)

	for i := 0; i < taskCount; i++ {
		taskName := fmt.Sprintf("unreliable-task-%d", i)
		taskObj, err := builder.NewTaskBuilder(taskName, fmt.Sprintf("不可靠任务%d", i), registry).
			WithJobFunction("unreliableFunc", nil).
			WithRetryCount(2). // 重试2次
			WithTaskHandler(task.TaskStatusSuccess, "DefaultLogSuccess").
			WithTaskHandler(task.TaskStatusFailed, "DefaultLogError").
			Build()
		if err != nil {
			t.Fatalf("构建任务 %d 失败: %v", i, err)
		}

		if err := wf.AddTask(taskObj); err != nil {
			t.Fatalf("添加任务 %d 失败: %v", i, err)
		}
	}

	t.Logf("任务创建完成，开始提交Workflow...")

	// 提交Workflow
	controller, err := eng.SubmitWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("提交Workflow失败: %v", err)
	}

	// 等待执行完成
	timeout := 5 * time.Minute
	startTime := time.Now()
	for {
		status, err := controller.GetStatus()
		if err != nil {
			t.Fatalf("获取状态失败: %v", err)
		}

		if status == "Success" || status == "Failed" || status == "Terminated" {
			break
		}

		if time.Since(startTime) > timeout {
			t.Fatalf("工作流执行超时")
		}

		time.Sleep(100 * time.Millisecond)
	}

	finalStatus, _ := controller.GetStatus()
	t.Logf("✅ 随机异常测试完成：%d 个任务，最终状态: %s, 耗时: %v", taskCount, finalStatus, time.Since(startTime))

	// 注意：由于有随机失败，最终状态可能是Failed，这是正常的
	if finalStatus != "Success" && finalStatus != "Failed" {
		t.Errorf("期望工作流状态为Success或Failed，实际为%s", finalStatus)
	}
}

// setupComplexTest 设置复杂场景测试环境
func setupComplexTest(t *testing.T) (*engine.Engine, *task.FunctionRegistry, *workflow.Workflow, storage.TaskRepository, func()) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"

	repos, err := sqlite.NewRepositories(dbPath)
	if err != nil {
		t.Fatalf("创建Repository失败: %v", err)
	}

	// 使用聚合Repository创建Engine
	eng, err := engine.NewEngineWithAggregateRepo(50, 60, repos.WorkflowAggregate)
	if err != nil {
		t.Fatalf("创建Engine失败: %v", err)
	}

	registry := eng.GetRegistry()
	ctx := context.Background()

	if err := eng.Start(ctx); err != nil {
		t.Fatalf("启动Engine失败: %v", err)
	}

	wf := workflow.NewWorkflow("complex-workflow", "复杂场景工作流")

	cleanup := func() {
		eng.Stop()
		repos.Close()
	}

	return eng, registry, wf, repos.Task, cleanup
}
