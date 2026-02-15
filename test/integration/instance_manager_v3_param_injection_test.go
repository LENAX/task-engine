package integration

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LENAX/task-engine/internal/storage/sqlite"
	"github.com/LENAX/task-engine/pkg/core/builder"
	"github.com/LENAX/task-engine/pkg/core/engine"
	"github.com/LENAX/task-engine/pkg/core/task"
	"github.com/LENAX/task-engine/pkg/core/types"
	"github.com/LENAX/task-engine/pkg/core/workflow"
	"github.com/stretchr/testify/require"
)

func TestInstanceManagerV3_ParamInjection_ResultMapping(t *testing.T) {
	tmpDir := t.TempDir()
	repos, err := sqlite.NewRepositories(tmpDir + "/v3-param-injection.db")
	require.NoError(t, err)
	defer repos.Close()

	eng, err := engine.NewEngineWithAggregateRepo(32, 30, repos.WorkflowAggregate)
	require.NoError(t, err)
	eng.SetInstanceManagerVersion(engine.InstanceManagerV3)

	ctx := context.Background()
	require.NoError(t, eng.Start(ctx))
	defer eng.Stop()

	registry := eng.GetRegistry()

	parentJob := func(tc *task.TaskContext) (interface{}, error) {
		return map[string]interface{}{
			"result_field": "injected-value",
		}, nil
	}
	_, err = registry.Register(ctx, "v3ParamParentJob", parentJob, "V3 参数注入-父任务")
	require.NoError(t, err)

	childObserved := make(chan string, 1)
	childJob := func(tc *task.TaskContext) (interface{}, error) {
		mapped := tc.GetParam("mapped_field")
		mappedStr, ok := mapped.(string)
		if !ok || mappedStr == "" {
			return nil, fmt.Errorf("missing mapped_field")
		}
		select {
		case childObserved <- mappedStr:
		default:
		}
		return map[string]interface{}{"mapped_field": mappedStr}, nil
	}
	_, err = registry.Register(ctx, "v3ParamChildJob", childJob, "V3 参数注入-子任务")
	require.NoError(t, err)

	parentTask, err := builder.NewTaskBuilder("parent-task", "父任务", registry).
		WithJobFunction("v3ParamParentJob", nil).
		Build()
	require.NoError(t, err)

	childTask, err := builder.NewTaskBuilder("child-task", "子任务", registry).
		WithJobFunction("v3ParamChildJob", nil).
		WithDependency("parent-task").
		WithRequiredParams([]string{"result_field"}).
		WithResultMapping(map[string]string{
			"mapped_field": "result_field",
		}).
		Build()
	require.NoError(t, err)

	wf := workflow.NewWorkflow("v3-param-injection", "V3 参数注入测试")
	require.NoError(t, wf.AddTask(parentTask))
	require.NoError(t, wf.AddTask(childTask))
	require.NoError(t, eng.RegisterWorkflow(ctx, wf))

	controller, err := eng.SubmitWorkflow(ctx, wf)
	require.NoError(t, err)

	deadline := time.Now().Add(15 * time.Second)
	finalStatus := ""
	for time.Now().Before(deadline) {
		status, getErr := controller.GetStatus()
		require.NoError(t, getErr)
		if status == "Success" || status == "Failed" || status == "Terminated" {
			finalStatus = status
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	require.Equal(t, "Success", finalStatus)
	select {
	case observed := <-childObserved:
		require.Equal(t, "injected-value", observed)
	default:
		t.Fatalf("子任务未观察到注入后的 mapped_field")
	}
}

func TestInstanceManagerV3_ParamInjection_CachedFallback(t *testing.T) {
	tmpDir := t.TempDir()
	repos, err := sqlite.NewRepositories(tmpDir + "/v3-param-cached.db")
	require.NoError(t, err)
	defer repos.Close()

	eng, err := engine.NewEngineWithAggregateRepo(32, 30, repos.WorkflowAggregate)
	require.NoError(t, err)
	eng.SetInstanceManagerVersion(engine.InstanceManagerV3)

	ctx := context.Background()
	require.NoError(t, eng.Start(ctx))
	defer eng.Stop()

	registry := eng.GetRegistry()

	parentJob := func(tc *task.TaskContext) (interface{}, error) {
		return "upstream-raw-result", nil
	}
	_, err = registry.Register(ctx, "v3CachedParentJob", parentJob, "V3 cached 注入-父任务")
	require.NoError(t, err)

	childObserved := make(chan string, 1)
	childJob := func(tc *task.TaskContext) (interface{}, error) {
		cachedByName := tc.GetParam("_cached_parent-task")
		cachedByNameStr, ok := cachedByName.(string)
		if !ok || cachedByNameStr == "" {
			return nil, fmt.Errorf("missing _cached_parent-task")
		}
		select {
		case childObserved <- cachedByNameStr:
		default:
		}
		return map[string]interface{}{"cached": cachedByNameStr}, nil
	}
	_, err = registry.Register(ctx, "v3CachedChildJob", childJob, "V3 cached 注入-子任务")
	require.NoError(t, err)

	parentTask, err := builder.NewTaskBuilder("parent-task", "父任务", registry).
		WithJobFunction("v3CachedParentJob", nil).
		Build()
	require.NoError(t, err)

	childTask, err := builder.NewTaskBuilder("child-task", "子任务", registry).
		WithJobFunction("v3CachedChildJob", nil).
		WithDependency("parent-task").
		Build()
	require.NoError(t, err)

	wf := workflow.NewWorkflow("v3-param-cached-fallback", "V3 cached 回退注入测试")
	require.NoError(t, wf.AddTask(parentTask))
	require.NoError(t, wf.AddTask(childTask))
	require.NoError(t, eng.RegisterWorkflow(ctx, wf))

	controller, err := eng.SubmitWorkflow(ctx, wf)
	require.NoError(t, err)

	deadline := time.Now().Add(15 * time.Second)
	finalStatus := ""
	for time.Now().Before(deadline) {
		status, getErr := controller.GetStatus()
		require.NoError(t, getErr)
		if status == "Success" || status == "Failed" || status == "Terminated" {
			finalStatus = status
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	require.Equal(t, "Success", finalStatus)
	select {
	case observed := <-childObserved:
		require.Equal(t, "upstream-raw-result", observed)
	default:
		t.Fatalf("子任务未观察到注入后的 _cached_parent-task")
	}
}

func TestInstanceManagerV3_ParamInjection_MissingRequiredParams(t *testing.T) {
	tmpDir := t.TempDir()
	repos, err := sqlite.NewRepositories(tmpDir + "/v3-param-missing-required.db")
	require.NoError(t, err)
	defer repos.Close()

	eng, err := engine.NewEngineWithAggregateRepo(32, 30, repos.WorkflowAggregate)
	require.NoError(t, err)
	eng.SetInstanceManagerVersion(engine.InstanceManagerV3)

	ctx := context.Background()
	require.NoError(t, eng.Start(ctx))
	defer eng.Stop()

	registry := eng.GetRegistry()

	parentJob := func(tc *task.TaskContext) (interface{}, error) {
		return map[string]interface{}{
			"other_field": "value",
		}, nil
	}
	_, err = registry.Register(ctx, "v3MissingReqParentJob", parentJob, "V3 缺少必需参数-父任务")
	require.NoError(t, err)

	childJob := func(tc *task.TaskContext) (interface{}, error) {
		// 该任务按预期不会被执行；若执行则返回成功以便仅验证调度层参数校验行为。
		return map[string]interface{}{"unexpected": true}, nil
	}
	_, err = registry.Register(ctx, "v3MissingReqChildJob", childJob, "V3 缺少必需参数-子任务")
	require.NoError(t, err)

	parentTask, err := builder.NewTaskBuilder("parent-task", "父任务", registry).
		WithJobFunction("v3MissingReqParentJob", nil).
		Build()
	require.NoError(t, err)

	childTask, err := builder.NewTaskBuilder("child-task", "子任务", registry).
		WithJobFunction("v3MissingReqChildJob", nil).
		WithDependency("parent-task").
		WithRequiredParams([]string{"missing_field"}).
		Build()
	require.NoError(t, err)

	wf := workflow.NewWorkflow("v3-param-missing-required", "V3 缺少必需参数测试")
	require.NoError(t, wf.AddTask(parentTask))
	require.NoError(t, wf.AddTask(childTask))
	require.NoError(t, eng.RegisterWorkflow(ctx, wf))

	controller, err := eng.SubmitWorkflow(ctx, wf)
	require.NoError(t, err)

	deadline := time.Now().Add(15 * time.Second)
	finalStatus := ""
	for time.Now().Before(deadline) {
		status, getErr := controller.GetStatus()
		require.NoError(t, getErr)
		if status == "Success" || status == "Failed" || status == "Terminated" {
			finalStatus = status
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	require.Equal(t, "Failed", finalStatus)
}

func TestInstanceManagerV3_TemplateDownstreamWaitsAllSubTasksDone(t *testing.T) {
	tmpDir := t.TempDir()
	repos, err := sqlite.NewRepositories(tmpDir + "/v3-template-downstream-wait.db")
	require.NoError(t, err)
	defer repos.Close()

	eng, err := engine.NewEngineWithAggregateRepo(32, 30, repos.WorkflowAggregate)
	require.NoError(t, err)
	eng.SetInstanceManagerVersion(engine.InstanceManagerV3)

	ctx := context.Background()
	require.NoError(t, eng.Start(ctx))
	defer eng.Stop()

	registry := eng.GetRegistry()

	var subTaskDone atomic.Bool

	templateJob := func(tc *task.TaskContext) (interface{}, error) {
		return map[string]interface{}{"ok": true}, nil
	}
	_, err = registry.Register(ctx, "v3TemplateMainJob", templateJob, "V3 模板主任务")
	require.NoError(t, err)

	subTaskJob := func(tc *task.TaskContext) (interface{}, error) {
		time.Sleep(300 * time.Millisecond)
		subTaskDone.Store(true)
		return map[string]interface{}{"sub_done": true}, nil
	}
	_, err = registry.Register(ctx, "v3TemplateSubTaskJob", subTaskJob, "V3 模板子任务")
	require.NoError(t, err)

	downstreamJob := func(tc *task.TaskContext) (interface{}, error) {
		if !subTaskDone.Load() {
			return nil, fmt.Errorf("downstream started before all template subtasks done")
		}
		return map[string]interface{}{"downstream_ok": true}, nil
	}
	_, err = registry.Register(ctx, "v3TemplateDownstreamJob", downstreamJob, "V3 模板下游任务")
	require.NoError(t, err)

	type ManagerWithAddSubTask interface {
		AddSubTask(subTask types.Task, parentTaskID string) error
	}
	templateHandler := func(tc *task.TaskContext) {
		manager, ok := task.GetDependencyTyped[ManagerWithAddSubTask](tc.Context(), "InstanceManager")
		if !ok || manager == nil {
			return
		}
		st, buildErr := builder.NewTaskBuilder("template-subtask", "模板子任务", registry).
			WithJobFunction("v3TemplateSubTaskJob", nil).
			Build()
		if buildErr != nil {
			return
		}
		_ = manager.AddSubTask(st, tc.TaskID)
	}
	_, err = registry.RegisterTaskHandler(ctx, "v3TemplateAddSubTaskHandler", templateHandler, "V3 模板添加子任务处理器")
	require.NoError(t, err)

	templateTask, err := builder.NewTaskBuilder("template-A", "模板任务A", registry).
		WithJobFunction("v3TemplateMainJob", nil).
		Build()
	require.NoError(t, err)
	templateTask.SetTemplate(true)
	templateTask.SetStatusHandlers(map[string][]string{"SUCCESS": {"v3TemplateAddSubTaskHandler"}})

	downstreamTask, err := builder.NewTaskBuilder("downstream-B", "下游任务B", registry).
		WithJobFunction("v3TemplateDownstreamJob", nil).
		WithDependency("template-A").
		Build()
	require.NoError(t, err)

	wf := workflow.NewWorkflow("v3-template-downstream-wait", "模板任务下游等待子任务完成")
	require.NoError(t, wf.AddTask(templateTask))
	require.NoError(t, wf.AddTask(downstreamTask))
	require.NoError(t, eng.RegisterWorkflow(ctx, wf))

	controller, err := eng.SubmitWorkflow(ctx, wf)
	require.NoError(t, err)

	deadline := time.Now().Add(20 * time.Second)
	finalStatus := ""
	for time.Now().Before(deadline) {
		status, getErr := controller.GetStatus()
		require.NoError(t, getErr)
		if status == "Success" || status == "Failed" || status == "Terminated" {
			finalStatus = status
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	require.Equal(t, "Success", finalStatus)
	require.True(t, subTaskDone.Load(), "模板子任务应先于下游任务完成")
}

func TestInstanceManagerV3_TemplateSubTaskResultsInjectedToDownstream(t *testing.T) {
	tmpDir := t.TempDir()
	repos, err := sqlite.NewRepositories(tmpDir + "/v3-template-subtask-results.db")
	require.NoError(t, err)
	defer repos.Close()

	eng, err := engine.NewEngineWithAggregateRepo(64, 30, repos.WorkflowAggregate)
	require.NoError(t, err)
	eng.SetInstanceManagerVersion(engine.InstanceManagerV3)

	ctx := context.Background()
	require.NoError(t, eng.Start(ctx))
	defer eng.Stop()

	registry := eng.GetRegistry()

	templateJob := func(tc *task.TaskContext) (interface{}, error) {
		return map[string]interface{}{
			"generated": 2,
			"status":    "success",
			"sub_tasks": []map[string]interface{}{
				{"name": "fetch-1", "api_url": "https://example.com/1"},
				{"name": "fetch-2", "api_url": "https://example.com/2"},
			},
		}, nil
	}
	_, err = registry.Register(ctx, "v3TplResultTemplateJob", templateJob, "V3 模板任务返回定义列表")
	require.NoError(t, err)

	var seq atomic.Int64
	subTaskJob := func(tc *task.TaskContext) (interface{}, error) {
		n := seq.Add(1)
		return map[string]interface{}{
			"api_metadata": map[string]interface{}{"id": n},
			"api_url":      fmt.Sprintf("https://example.com/%d", n),
		}, nil
	}
	_, err = registry.Register(ctx, "v3TplResultSubTaskJob", subTaskJob, "V3 子任务返回 api_metadata")
	require.NoError(t, err)

	downstreamJob := func(tc *task.TaskContext) (interface{}, error) {
		raw := tc.GetParam("_cached_template-A")
		cached, ok := raw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("missing _cached_template-A map")
		}
		arrRaw, ok := cached["subtask_results"]
		if !ok {
			return nil, fmt.Errorf("missing subtask_results")
		}
		arr, ok := arrRaw.([]map[string]interface{})
		if !ok {
			genericArr, ok2 := arrRaw.([]interface{})
			if !ok2 {
				return nil, fmt.Errorf("invalid subtask_results type: %T", arrRaw)
			}
			converted := make([]map[string]interface{}, 0, len(genericArr))
			for _, item := range genericArr {
				asMap, ok3 := item.(map[string]interface{})
				if !ok3 {
					return nil, fmt.Errorf("invalid subtask_results item type: %T", item)
				}
				converted = append(converted, asMap)
			}
			arr = converted
		}
		if len(arr) != 2 {
			return nil, fmt.Errorf("subtask_results size mismatch: %d", len(arr))
		}
		for _, item := range arr {
			resultRaw, ok4 := item["result"]
			if !ok4 {
				return nil, fmt.Errorf("missing result in subtask_results item")
			}
			resultMap, ok5 := resultRaw.(map[string]interface{})
			if !ok5 {
				return nil, fmt.Errorf("invalid result type: %T", resultRaw)
			}
			if _, ok6 := resultMap["api_metadata"]; !ok6 {
				return nil, fmt.Errorf("missing api_metadata in subtask result")
			}
		}
		return map[string]interface{}{"ok": true}, nil
	}
	_, err = registry.Register(ctx, "v3TplResultDownstreamJob", downstreamJob, "V3 下游读取 subtask_results")
	require.NoError(t, err)

	type ManagerWithAddSubTask interface {
		AddSubTask(subTask types.Task, parentTaskID string) error
	}
	templateHandler := func(tc *task.TaskContext) {
		manager, ok := task.GetDependencyTyped[ManagerWithAddSubTask](tc.Context(), "InstanceManager")
		if !ok || manager == nil {
			return
		}
		for i := 0; i < 2; i++ {
			name := fmt.Sprintf("fetch-detail-%d", i+1)
			st, buildErr := builder.NewTaskBuilder(name, "fetch api detail", registry).
				WithJobFunction("v3TplResultSubTaskJob", nil).
				Build()
			if buildErr != nil {
				return
			}
			_ = manager.AddSubTask(st, tc.TaskID)
		}
	}
	_, err = registry.RegisterTaskHandler(ctx, "v3TplResultHandler", templateHandler, "V3 模板任务添加子任务")
	require.NoError(t, err)

	templateTask, err := builder.NewTaskBuilder("template-A", "模板任务A", registry).
		WithJobFunction("v3TplResultTemplateJob", nil).
		Build()
	require.NoError(t, err)
	templateTask.SetTemplate(true)
	templateTask.SetStatusHandlers(map[string][]string{"SUCCESS": {"v3TplResultHandler"}})

	saveTask, err := builder.NewTaskBuilder("save-all-metadata", "保存所有元数据", registry).
		WithJobFunction("v3TplResultDownstreamJob", nil).
		WithDependency("template-A").
		Build()
	require.NoError(t, err)

	wf := workflow.NewWorkflow("v3-template-subtask-results", "模板子任务结果注入下游")
	require.NoError(t, wf.AddTask(templateTask))
	require.NoError(t, wf.AddTask(saveTask))
	require.NoError(t, eng.RegisterWorkflow(ctx, wf))

	controller, err := eng.SubmitWorkflow(ctx, wf)
	require.NoError(t, err)

	deadline := time.Now().Add(20 * time.Second)
	finalStatus := ""
	for time.Now().Before(deadline) {
		status, getErr := controller.GetStatus()
		require.NoError(t, getErr)
		if status == "Success" || status == "Failed" || status == "Terminated" {
			finalStatus = status
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	require.Equal(t, "Success", finalStatus)
}
