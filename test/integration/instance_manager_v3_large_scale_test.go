package integration

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

// TestInstanceManagerV3_LargeScaleSubTasks 验证 V3 路径在大规模动态子任务下可稳定完成。
func TestInstanceManagerV3_LargeScaleSubTasks(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过大规模测试（使用 -short 标志）")
	}

	const (
		templateCount = 30
		subTaskPerTpl = 200
	)

	tmpDir := t.TempDir()
	repos, err := sqlite.NewRepositories(tmpDir + "/v3-large-scale.db")
	require.NoError(t, err)
	defer repos.Close()

	eng, err := engine.NewEngineWithAggregateRepo(256, 60, repos.WorkflowAggregate)
	require.NoError(t, err)
	eng.SetInstanceManagerVersion(engine.InstanceManagerV3)

	ctx := context.Background()
	require.NoError(t, eng.Start(ctx))
	defer eng.Stop()

	// 模拟网络 I/O：本地 HTTP 服务用于每个任务发起一次真实请求。
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer mockServer.Close()
	httpClient := &http.Client{Timeout: 2 * time.Second}

	registry := eng.GetRegistry()
	baseJob := func(tc *task.TaskContext) (interface{}, error) {
		req, reqErr := http.NewRequestWithContext(tc.Context(), http.MethodGet, mockServer.URL, nil)
		if reqErr != nil {
			return nil, reqErr
		}
		resp, doErr := httpClient.Do(req)
		if doErr != nil {
			return nil, doErr
		}
		defer resp.Body.Close()
		_, _ = io.ReadAll(resp.Body)
		return map[string]interface{}{
			"task_id": tc.TaskID,
			"status":  resp.StatusCode,
		}, nil
	}
	_, err = registry.Register(ctx, "v3LargeBaseJob", baseJob, "V3大规模基础任务")
	require.NoError(t, err)

	type ManagerWithAtomicAddSubTasks interface {
		AtomicAddSubTasks(subTasks []types.Task, parentTaskID string) error
	}
	templateHandler := func(tc *task.TaskContext) {
		manager, ok := task.GetDependencyTyped[ManagerWithAtomicAddSubTasks](tc.Context(), "InstanceManager")
		if !ok {
			raw, ok2 := tc.GetDependency("InstanceManager")
			if !ok2 {
				return
			}
			manager, _ = raw.(ManagerWithAtomicAddSubTasks)
		}
		if manager == nil {
			return
		}

		subTasks := make([]types.Task, 0, subTaskPerTpl)
		for i := 0; i < subTaskPerTpl; i++ {
			name := fmt.Sprintf("sub-%s-%d", tc.TaskID, i)
			st, buildErr := builder.NewTaskBuilder(name, "大规模子任务", registry).
				WithJobFunction("v3LargeBaseJob", nil).
				Build()
			if buildErr != nil {
				return
			}
			subTasks = append(subTasks, st)
		}
		_ = manager.AtomicAddSubTasks(subTasks, tc.TaskID)
	}
	_, err = registry.RegisterTaskHandler(ctx, "v3LargeTemplateHandler", templateHandler, "V3大规模模板处理器")
	require.NoError(t, err)

	wf := workflow.NewWorkflow("v3-large-scale", "V3大规模子任务稳定性测试")
	for i := 0; i < templateCount; i++ {
		name := fmt.Sprintf("tpl-%d", i)
		tpl, buildErr := builder.NewTaskBuilder(name, "模板任务", registry).
			WithJobFunction("v3LargeBaseJob", nil).
			Build()
		require.NoError(t, buildErr)
		tpl.SetTemplate(true)
		tpl.SetStatusHandlers(map[string][]string{"SUCCESS": {"v3LargeTemplateHandler"}})
		require.NoError(t, wf.AddTask(tpl))
	}

	require.NoError(t, eng.RegisterWorkflow(ctx, wf))
	ctrl, err := eng.SubmitWorkflow(ctx, wf)
	require.NoError(t, err)

	instanceID := ctrl.GetInstanceID()
	maxTotal := 0
	deadline := time.Now().Add(90 * time.Second)
	finalStatus := ""

	for time.Now().Before(deadline) {
		if p, ok := eng.GetInstanceProgress(instanceID); ok {
			if p.Total > maxTotal {
				maxTotal = p.Total
			}
		}
		s, getErr := ctrl.GetStatus()
		require.NoError(t, getErr)
		if s == "Success" || s == "Failed" {
			finalStatus = s
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	require.Equal(t, "Success", finalStatus, "V3大规模任务应成功完成")
	require.Greater(t, maxTotal, templateCount, "运行中进度应明显超过模板任务数，表示已纳入动态子任务")
}

// TestInstanceManagerV3_Stress_10x5500 验证 V3 在 10x5500 动态子任务规模下可完成。
func TestInstanceManagerV3_Stress_10x5500(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过压测（使用 -short 标志）")
	}

	const (
		templateCount = 10
		subTaskPerTpl = 5500
	)

	tmpDir := t.TempDir()
	repos, err := sqlite.NewRepositories(tmpDir + "/v3-stress-10x5500.db")
	require.NoError(t, err)
	defer repos.Close()

	eng, err := engine.NewEngineWithAggregateRepo(512, 120, repos.WorkflowAggregate)
	require.NoError(t, err)
	eng.SetInstanceManagerVersion(engine.InstanceManagerV3)

	ctx := context.Background()
	require.NoError(t, eng.Start(ctx))
	defer eng.Stop()

	registry := eng.GetRegistry()
	baseJob := func(tc *task.TaskContext) (interface{}, error) {
		return map[string]interface{}{"task_id": tc.TaskID}, nil
	}
	_, err = registry.Register(ctx, "v3StressBaseJob", baseJob, "V3压测基础任务")
	require.NoError(t, err)

	type ManagerWithAtomicAddSubTasks interface {
		AtomicAddSubTasks(subTasks []types.Task, parentTaskID string) error
	}
	templateHandler := func(tc *task.TaskContext) {
		manager, ok := task.GetDependencyTyped[ManagerWithAtomicAddSubTasks](tc.Context(), "InstanceManager")
		if !ok {
			raw, ok2 := tc.GetDependency("InstanceManager")
			if !ok2 {
				return
			}
			manager, _ = raw.(ManagerWithAtomicAddSubTasks)
		}
		if manager == nil {
			return
		}

		subTasks := make([]types.Task, 0, subTaskPerTpl)
		for i := 0; i < subTaskPerTpl; i++ {
			name := fmt.Sprintf("stress-sub-%s-%d", tc.TaskID, i)
			st, buildErr := builder.NewTaskBuilder(name, "压测子任务", registry).
				WithJobFunction("v3StressBaseJob", nil).
				Build()
			if buildErr != nil {
				return
			}
			subTasks = append(subTasks, st)
		}
		_ = manager.AtomicAddSubTasks(subTasks, tc.TaskID)
	}
	_, err = registry.RegisterTaskHandler(ctx, "v3StressTemplateHandler", templateHandler, "V3压测模板处理器")
	require.NoError(t, err)

	wf := workflow.NewWorkflow("v3-stress-10x5500", "V3 10x5500 压测")
	for i := 0; i < templateCount; i++ {
		name := fmt.Sprintf("stress-tpl-%d", i)
		tpl, buildErr := builder.NewTaskBuilder(name, "压测模板任务", registry).
			WithJobFunction("v3StressBaseJob", nil).
			Build()
		require.NoError(t, buildErr)
		tpl.SetTemplate(true)
		tpl.SetStatusHandlers(map[string][]string{"SUCCESS": {"v3StressTemplateHandler"}})
		require.NoError(t, wf.AddTask(tpl))
	}

	require.NoError(t, eng.RegisterWorkflow(ctx, wf))
	ctrl, err := eng.SubmitWorkflow(ctx, wf)
	require.NoError(t, err)

	instanceID := ctrl.GetInstanceID()
	maxTotal := 0
	start := time.Now()
	deadline := time.Now().Add(8 * time.Minute)
	finalStatus := ""
	nextMilestone := 1000
	lastMilestoneDone := 0
	lastMilestoneAt := start

	for time.Now().Before(deadline) {
		if p, ok := eng.GetInstanceProgress(instanceID); ok {
			if p.Total > maxTotal {
				maxTotal = p.Total
			}
			done := p.Completed + p.Failed
			if done >= nextMilestone {
				elapsed := time.Since(start).Seconds()
				avgRate := float64(done) / elapsed
				windowDone := done - lastMilestoneDone
				windowElapsed := time.Since(lastMilestoneAt).Seconds()
				windowRate := 0.0
				if windowElapsed > 0 {
					windowRate = float64(windowDone) / windowElapsed
				}
				t.Logf("[speed] done=%d elapsed=%.2fs avg=%.2f tasks/s window=%d/%.2fs=%.2f tasks/s",
					done, elapsed, avgRate, windowDone, windowElapsed, windowRate)
				lastMilestoneDone = done
				lastMilestoneAt = time.Now()
				for done >= nextMilestone {
					nextMilestone += 1000
				}
			}
		}
		s, getErr := ctrl.GetStatus()
		require.NoError(t, getErr)
		if s == "Success" || s == "Failed" {
			finalStatus = s
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Logf("10x5500 压测结果: status=%s, maxTotal=%d, elapsed=%v", finalStatus, maxTotal, time.Since(start))
	require.Equal(t, "Success", finalStatus, "V3 10x5500 压测应成功完成")
	require.Greater(t, maxTotal, templateCount, "运行中总量应明显高于模板任务数")
}
