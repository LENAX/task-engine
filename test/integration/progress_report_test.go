package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LENAX/task-engine/internal/storage/sqlite"
	"github.com/LENAX/task-engine/pkg/api"
	"github.com/LENAX/task-engine/pkg/api/dto"
	"github.com/LENAX/task-engine/pkg/core/builder"
	"github.com/LENAX/task-engine/pkg/core/engine"
	"github.com/LENAX/task-engine/pkg/core/executor"
	"github.com/LENAX/task-engine/pkg/core/task"
	"github.com/LENAX/task-engine/pkg/core/types"
	"github.com/LENAX/task-engine/pkg/core/workflow"
	"github.com/LENAX/task-engine/pkg/plugin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

// TestProgressReport_SimpleWorkflow_GetProgress 验证简单工作流能正确通过 GetProgress 获取进度
func TestProgressReport_SimpleWorkflow_GetProgress(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2Integration(t)
	defer cleanup()

	ctx := context.Background()

	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		time.Sleep(15 * time.Millisecond)
		return map[string]interface{}{"result": "success"}, nil
	}
	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	require.NoError(t, err)

	// 3 个串行任务
	for i := 1; i <= 3; i++ {
		name := fmt.Sprintf("task%d", i)
		deps := []string{}
		if i > 1 {
			deps = append(deps, fmt.Sprintf("task%d", i-1))
		}
		taskObj, err := builder.NewTaskBuilder(name, name, registry).
			WithJobFunction("mockFunc", nil).
			WithDependencies(deps).
			Build()
		require.NoError(t, err)
		require.NoError(t, wf.AddTask(taskObj))
	}

	exec, err := executor.NewExecutor(10)
	require.NoError(t, err)
	exec.Start()
	exec.SetRegistry(registry)

	instance := workflow.NewWorkflowInstance(wf.GetID())
	manager, err := engine.NewWorkflowInstanceManagerV2(
		instance, wf, exec, nil, nil, registry, plugin.NewPluginManager(),
	)
	require.NoError(t, err)

	manager.Start()
	defer func() {
		manager.Shutdown()
		exec.Shutdown()
	}()

	// 轮询直到进度已初始化（taskStats 通过 channel 异步更新）
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		progress := manager.GetProgress()
		if progress.Total == 3 {
			require.True(t, progress.Completed+progress.Failed+progress.Pending+progress.Running == progress.Total,
				"Completed+Failed+Pending+Running 应等于 Total")
			break
		}
		if manager.GetStatus() == "Success" || manager.GetStatus() == "Failed" {
			break
		}
	}
	progress := manager.GetProgress()
	require.Equal(t, 3, progress.Total, "进度 Total 应为 3")

	// 等待执行完成
	timeout := time.After(15 * time.Second)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-timeout:
			t.Fatalf("执行超时，当前状态: %s", manager.GetStatus())
		case <-ticker.C:
			if manager.GetStatus() == "Success" || manager.GetStatus() == "Failed" {
				break
			}
			continue
		}
		break
	}

	require.Equal(t, "Success", manager.GetStatus(), "工作流应成功完成")

	// 完成后进度：3 个全部完成
	progress = manager.GetProgress()
	require.Equal(t, 3, progress.Total)
	require.Equal(t, 3, progress.Completed)
	require.Equal(t, 0, progress.Failed)
}

// TestProgressReport_DynamicTaskWorkflow_GetProgress 验证带动态子任务的工作流能正确统计进度
func TestProgressReport_DynamicTaskWorkflow_GetProgress(t *testing.T) {
	_, registry, wf, cleanup := setupTestForV2Integration(t)
	defer cleanup()

	ctx := context.Background()

	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		time.Sleep(10 * time.Millisecond)
		return map[string]interface{}{"result": "success"}, nil
	}
	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	require.NoError(t, err)

	type InstanceManagerWithAtomicAddSubTasks interface {
		AtomicAddSubTasks(subTasks []types.Task, parentTaskID string) error
	}

	templateHandler := func(ctx *task.TaskContext) {
		instanceManager, ok := task.GetDependencyTyped[InstanceManagerWithAtomicAddSubTasks](ctx.Context(), "InstanceManager")
		if !ok {
			if mi, ok2 := ctx.GetDependency("InstanceManager"); ok2 {
				instanceManager, _ = mi.(InstanceManagerWithAtomicAddSubTasks)
			}
		}
		if instanceManager == nil {
			return
		}
		subTasks := make([]types.Task, 0, 5)
		for i := 0; i < 5; i++ {
			subTaskName := fmt.Sprintf("subtask-%s-%d", ctx.TaskID, i)
			subTask, err := builder.NewTaskBuilder(subTaskName, "子任务", registry).
				WithJobFunction("mockFunc", nil).Build()
			if err != nil {
				return
			}
			subTasks = append(subTasks, subTask)
		}
		_ = instanceManager.AtomicAddSubTasks(subTasks, ctx.TaskID)
	}

	_, err = registry.RegisterTaskHandler(ctx, "templateHandler", templateHandler, "模板handler")
	require.NoError(t, err)

	templateTask, err := builder.NewTaskBuilder("template", "模板任务", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	require.NoError(t, err)
	templateTask.SetTemplate(true)
	templateTask.SetStatusHandlers(map[string][]string{"SUCCESS": {"templateHandler"}})
	require.NoError(t, wf.AddTask(templateTask))

	exec, err := executor.NewExecutor(20)
	require.NoError(t, err)
	exec.Start()
	exec.SetRegistry(registry)

	instance := workflow.NewWorkflowInstance(wf.GetID())
	manager, err := engine.NewWorkflowInstanceManagerV2(
		instance, wf, exec, nil, nil, registry, plugin.NewPluginManager(),
	)
	require.NoError(t, err)

	manager.Start()
	defer func() {
		manager.Shutdown()
		exec.Shutdown()
	}()

	// 运行过程中轮询进度，验证 Total 会从 1 变为 6，Completed 最终为 6
	var maxTotal, finalCompleted int
	timeout := time.After(20 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatalf("执行超时，状态: %s, maxTotal: %d, finalCompleted: %d",
				manager.GetStatus(), maxTotal, finalCompleted)
		case <-ticker.C:
			p := manager.GetProgress()
			if p.Total > maxTotal {
				maxTotal = p.Total
			}
			if manager.GetStatus() == "Success" || manager.GetStatus() == "Failed" {
				finalCompleted = p.Completed
				goto done
			}
		}
	}
done:

	require.Equal(t, "Success", manager.GetStatus(), "带动态任务的工作流应成功完成")
	// 1 个模板 + 5 个子任务 = 6
	require.Equal(t, 6, maxTotal, "进度中 Total 应包含动态子任务，最大为 6")
	require.Equal(t, 6, finalCompleted, "完成后 Completed 应为 6")

	p := manager.GetProgress()
	require.Equal(t, 6, p.Total)
	require.Equal(t, 6, p.Completed)
	require.Equal(t, 0, p.Failed)
}

// TestProgressReport_API_InstanceDetail_UsesInMemoryProgress 验证 API 获取实例详情时运行中实例使用内存进度
func TestProgressReport_API_InstanceDetail_UsesInMemoryProgress(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/progress_api_test.db"

	repos, err := sqlite.NewRepositories(dbPath)
	require.NoError(t, err)
	defer repos.Close()

	eng, err := engine.NewEngineWithAggregateRepo(10, 30, repos.WorkflowAggregate)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, eng.Start(ctx))
	defer eng.Stop()

	registry := eng.GetRegistry()
	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		time.Sleep(30 * time.Millisecond)
		return map[string]interface{}{"result": "ok"}, nil
	}
	_, err = registry.Register(ctx, "mockFunc", mockFunc, "测试函数")
	require.NoError(t, err)

	wf := workflow.NewWorkflow("wf-progress-api", "进度API测试")
	task1, _ := builder.NewTaskBuilder("task1", "任务1", registry).WithJobFunction("mockFunc", nil).Build()
	task2, _ := builder.NewTaskBuilder("task2", "任务2", registry).WithJobFunction("mockFunc", nil).WithDependency("task1").Build()
	require.NoError(t, wf.AddTask(task1))
	require.NoError(t, wf.AddTask(task2))

	require.NoError(t, eng.RegisterWorkflow(ctx, wf))

	controller, err := eng.SubmitWorkflow(ctx, wf)
	require.NoError(t, err)
	instanceID := controller.GetInstanceID()

	router := api.SetupRouter(eng, "test")

	// 运行中调用 GET /api/v1/instances/:id，应返回内存进度（Total=2）
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/instances/"+instanceID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp dto.APIResponse[dto.InstanceDetail]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, instanceID, resp.Data.ID)
	require.Equal(t, 2, resp.Data.Progress.Total, "运行中实例应返回内存进度 Total=2")
	require.True(t, resp.Data.Progress.Completed+resp.Data.Progress.Pending+resp.Data.Progress.Running+resp.Data.Progress.Failed == resp.Data.Progress.Total)

	// 等待实例完成（轮询 GET 或内存进度）
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		snap, ok := eng.GetInstanceProgress(instanceID)
		if ok && snap.Completed == 2 {
			require.Equal(t, 2, snap.Total)
			require.Equal(t, 2, snap.Completed)
			return
		}
		// 可能已结束并从内存移除，用 API 再查一次
		req2, _ := http.NewRequest(http.MethodGet, "/api/v1/instances/"+instanceID, nil)
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)
		if w2.Code != http.StatusOK {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		var resp2 dto.APIResponse[dto.InstanceDetail]
		if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if resp2.Data.Status == "Success" {
			require.Equal(t, 2, resp2.Data.Progress.Total)
			require.Equal(t, 2, resp2.Data.Progress.Completed)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("等待实例完成超时")
}

// TestProgressReport_LongRunningWorkflow_PrintProgress 长时间运行的 workflow，运行期间持续打印进度
func TestProgressReport_LongRunningWorkflow_PrintProgress(t *testing.T) {
	if testing.Short() {
		t.Skip("长时间运行测试，使用 -short 可跳过")
	}

	_, registry, wf, cleanup := setupTestForV2Integration(t)
	defer cleanup()

	ctx := context.Background()

	// 每个任务执行约 1.5 秒，共 8 个串行任务，总时长约 12 秒
	taskDuration := 1500 * time.Millisecond
	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		time.Sleep(taskDuration)
		return map[string]interface{}{"result": "success", "task_id": ctx.TaskID}, nil
	}
	_, err := registry.Register(ctx, "mockFunc", mockFunc, "长时间任务")
	require.NoError(t, err)

	numTasks := 8
	for i := 1; i <= numTasks; i++ {
		name := fmt.Sprintf("task%d", i)
		deps := []string{}
		if i > 1 {
			deps = append(deps, fmt.Sprintf("task%d", i-1))
		}
		taskObj, err := builder.NewTaskBuilder(name, name, registry).
			WithJobFunction("mockFunc", nil).
			WithDependencies(deps).
			Build()
		require.NoError(t, err)
		require.NoError(t, wf.AddTask(taskObj))
	}

	exec, err := executor.NewExecutor(4)
	require.NoError(t, err)
	exec.Start()
	exec.SetRegistry(registry)

	instance := workflow.NewWorkflowInstance(wf.GetID())
	manager, err := engine.NewWorkflowInstanceManagerV2(
		instance, wf, exec, nil, nil, registry, plugin.NewPluginManager(),
	)
	require.NoError(t, err)

	manager.Start()
	defer func() {
		manager.Shutdown()
		exec.Shutdown()
	}()

	startTime := time.Now()
	printInterval := 500 * time.Millisecond
	nextPrint := startTime.Add(printInterval)

	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatalf("执行超时，当前状态: %s", manager.GetStatus())
		case now := <-ticker.C:
			status := manager.GetStatus()
			if now.After(nextPrint) || status == "Success" || status == "Failed" {
				p := manager.GetProgress()
				elapsed := time.Since(startTime).Round(time.Millisecond)
				pct := 0.0
				if p.Total > 0 {
					pct = 100 * float64(p.Completed+p.Failed) / float64(p.Total)
				}
				t.Logf("[%s] 状态=%s | 总=%d 完成=%d 运行=%d 失败=%d 待执行=%d | 进度=%.1f%%",
					elapsed, status, p.Total, p.Completed, p.Running, p.Failed, p.Pending, pct)
				nextPrint = now.Add(printInterval)
			}
			if status == "Success" || status == "Failed" {
				require.Equal(t, "Success", status, "工作流应成功完成")
				p := manager.GetProgress()
				require.Equal(t, numTasks, p.Total)
				require.Equal(t, numTasks, p.Completed)
				t.Logf("总耗时: %v", time.Since(startTime).Round(time.Millisecond))
				return
			}
		}
	}
}
