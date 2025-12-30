package unit

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stevelan1995/task-engine/pkg/core/builder"
	"github.com/stevelan1995/task-engine/pkg/core/executor"
	"github.com/stevelan1995/task-engine/pkg/core/task"
)

func TestExecutor_Basic(t *testing.T) {
	exec, err := executor.NewExecutor(10)
	if err != nil {
		t.Fatalf("创建Executor失败: %v", err)
	}
	defer exec.Shutdown()

	exec.Start()

	// 创建 registry 并注册函数
	registry := task.NewFunctionRegistry(nil, nil)
	testFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	_, err = registry.Register(context.Background(), "func1", testFunc, "测试函数1")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}
	exec.SetRegistry(registry)

	taskBuilder := builder.NewTaskBuilder("task1", "任务1", registry)
	taskBuilder = taskBuilder.WithJobFunction("func1", nil)
	task1, err := taskBuilder.Build()
	if err != nil {
		t.Fatalf("构建任务失败: %v", err)
	}

	pendingTask := &executor.PendingTask{
		Task:       task1,
		WorkflowID: "wf1",
		InstanceID: "inst1",
		Domain:     "",
		MaxRetries: 0,
	}

	err = exec.SubmitTask(pendingTask)
	if err != nil {
		t.Fatalf("提交任务失败: %v", err)
	}

	// 等待任务完成
	time.Sleep(100 * time.Millisecond)
}

func TestExecutor_SetPoolSize(t *testing.T) {
	exec, _ := executor.NewExecutor(10)
	defer exec.Shutdown()

	err := exec.SetPoolSize(20)
	if err != nil {
		t.Fatalf("设置池大小失败: %v", err)
	}

	err = exec.SetPoolSize(0)
	if err == nil {
		t.Fatal("期望返回错误（大小必须大于0），但未返回")
	}

	err = exec.SetPoolSize(2000)
	if err == nil {
		t.Fatal("期望返回错误（大小不能超过1000），但未返回")
	}
}

func TestExecutor_SetDomainPoolSize(t *testing.T) {
	exec, _ := executor.NewExecutor(100)
	defer exec.Shutdown()

	err := exec.SetDomainPoolSize("domain1", 20)
	if err != nil {
		t.Fatalf("设置业务域池大小失败: %v", err)
	}

	err = exec.SetDomainPoolSize("domain2", 90)
	if err == nil {
		t.Fatal("期望返回错误（子池总和超过全局最大并发数），但未返回")
	}
}

func TestExecutor_GetDomainPoolStatus(t *testing.T) {
	exec, _ := executor.NewExecutor(100)
	defer exec.Shutdown()

	err := exec.SetDomainPoolSize("domain1", 20)
	if err != nil {
		t.Fatalf("设置业务域池大小失败: %v", err)
	}

	current, max, err := exec.GetDomainPoolStatus("domain1")
	if err != nil {
		t.Fatalf("查询业务域状态失败: %v", err)
	}

	if max != 20 {
		t.Errorf("最大并发数错误，期望: 20, 实际: %d", max)
	}

	if current < 0 || current > max {
		t.Errorf("当前可用数错误，期望: 0-%d, 实际: %d", max, current)
	}

	_, _, err = exec.GetDomainPoolStatus("non-existent")
	if err == nil {
		t.Fatal("期望返回错误（业务域不存在），但未返回")
	}
}

func TestExecutor_ConcurrentExecution(t *testing.T) {
	exec, _ := executor.NewExecutor(5)
	defer exec.Shutdown()
	exec.Start()

	// 创建并注册一个简单的Job函数
	registry := task.NewFunctionRegistry(nil, nil)
	testFunc := func(ctx context.Context) error {
		time.Sleep(10 * time.Millisecond) // 模拟工作
		return nil
	}
	_, err := registry.Register(context.Background(), "func", testFunc, "测试函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}
	exec.SetRegistry(registry)

	var wg sync.WaitGroup
	var mu sync.Mutex
	completedTasks := make([]string, 0)

	// 提交10个任务
	for i := 0; i < 10; i++ {
		wg.Add(1)
		taskBuilder := builder.NewTaskBuilder("task"+string(rune(i)), "任务", registry)
		taskBuilder = taskBuilder.WithJobFunction("func", nil)
		task, err := taskBuilder.Build()
		if err != nil {
			t.Fatalf("构建任务失败: %v", err)
		}

		pendingTask := &executor.PendingTask{
			Task:       task,
			WorkflowID: "wf1",
			InstanceID: "inst1",
			Domain:     "",
			MaxRetries: 0,
			OnComplete: func(result *executor.TaskResult) {
				mu.Lock()
				completedTasks = append(completedTasks, result.TaskID)
				mu.Unlock()
				wg.Done()
			},
		}

		err = exec.SubmitTask(pendingTask)
		if err != nil {
			t.Fatalf("提交任务失败: %v", err)
		}
	}

	// 等待所有任务完成（最多等待5秒）
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// 所有任务完成
		if len(completedTasks) != 10 {
			t.Errorf("完成任务数量错误，期望: 10, 实际: %d", len(completedTasks))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("任务执行超时")
	}
}

func TestExecutor_Timeout(t *testing.T) {
	exec, _ := executor.NewExecutor(5)
	defer exec.Shutdown()
	exec.Start()

	// 创建 registry 并注册函数
	registry := task.NewFunctionRegistry(nil, nil)
	testFunc := func(ctx *task.TaskContext) (interface{}, error) {
		time.Sleep(2 * time.Second) // 模拟长时间运行
		return "success", nil
	}
	_, err := registry.Register(context.Background(), "func1", testFunc, "测试函数1")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}
	exec.SetRegistry(registry)

	taskBuilder := builder.NewTaskBuilder("task1", "任务1", registry)
	taskBuilder = taskBuilder.WithJobFunction("func1", nil)
	task1, err := taskBuilder.
		WithTimeout(1). // 1秒超时
		Build()
	if err != nil {
		t.Fatalf("构建任务失败: %v", err)
	}

	var result *executor.TaskResult
	var resultMu sync.Mutex

	pendingTask := &executor.PendingTask{
		Task:       task1,
		WorkflowID: "wf1",
		InstanceID: "inst1",
		Domain:     "",
		MaxRetries: 0,
		OnComplete: func(r *executor.TaskResult) {
			resultMu.Lock()
			result = r
			resultMu.Unlock()
		},
		OnError: func(err error) {
			resultMu.Lock()
			if result == nil {
				result = &executor.TaskResult{Error: err}
			} else {
				result.Error = err
			}
			resultMu.Unlock()
		},
	}

	err = exec.SubmitTask(pendingTask)
	if err != nil {
		t.Fatalf("提交任务失败: %v", err)
	}

	// 等待任务完成
	time.Sleep(2 * time.Second)

	resultMu.Lock()
	defer resultMu.Unlock()
	if result == nil {
		t.Fatal("任务结果为空")
	}
}

func TestExecutor_Shutdown(t *testing.T) {
	exec, _ := executor.NewExecutor(5)
	exec.Start()

	// 创建 registry 并注册函数
	registry := task.NewFunctionRegistry(nil, nil)
	testFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	_, err := registry.Register(context.Background(), "func", testFunc, "测试函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}
	exec.SetRegistry(registry)

	// 提交一些任务
	for i := 0; i < 5; i++ {
		taskBuilder := builder.NewTaskBuilder("task"+string(rune(i)), "任务", registry)
		taskBuilder = taskBuilder.WithJobFunction("func", nil)
		task, err := taskBuilder.Build()
		if err != nil {
			t.Fatalf("构建任务失败: %v", err)
		}

		pendingTask := &executor.PendingTask{
			Task:       task,
			WorkflowID: "wf1",
			InstanceID: "inst1",
		}

		exec.SubmitTask(pendingTask)
	}

	// 关闭执行器
	_ = exec.Shutdown()

	// 尝试提交新任务应该失败
	newTaskBuilder := builder.NewTaskBuilder("new-task", "新任务", registry)
	newTaskBuilder = newTaskBuilder.WithJobFunction("func", nil)
	task, err := newTaskBuilder.Build()
	if err != nil {
		t.Fatalf("构建任务失败: %v", err)
	}

	pendingTask := &executor.PendingTask{
		Task:       task,
		WorkflowID: "wf1",
		InstanceID: "inst1",
	}

	err = exec.SubmitTask(pendingTask)
	if err == nil {
		t.Fatal("期望返回错误（Executor已关闭），但未返回")
	}
}
