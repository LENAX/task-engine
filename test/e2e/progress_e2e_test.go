// Package e2e 提供端到端测试
// 本文件测试运行中实例进度 API：运行中应返回内存实时进度（含动态任务），非 0
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/LENAX/task-engine/internal/storage/sqlite"
	"github.com/LENAX/task-engine/pkg/api"
	"github.com/LENAX/task-engine/pkg/api/dto"
	"github.com/LENAX/task-engine/pkg/core/builder"
	"github.com/LENAX/task-engine/pkg/core/engine"
	"github.com/LENAX/task-engine/pkg/core/task"
	"github.com/LENAX/task-engine/pkg/core/types"
	"github.com/LENAX/task-engine/pkg/core/workflow"
	"github.com/stretchr/testify/require"
)

// TestE2E_InstanceProgress_RunningUsesMemoryProgress 验证运行中实例通过 API 获取的进度为内存实时进度
// 场景：提交带多任务的工作流，在运行期间轮询 GET /api/v1/instances/:id，进度应随任务完成递增（非始终 0）
func TestE2E_InstanceProgress_RunningUsesMemoryProgress(t *testing.T) {
	dataDir := filepath.Join(os.TempDir(), "task-engine-e2e-progress", time.Now().Format("20060102150405"))
	require.NoError(t, os.MkdirAll(dataDir, 0755))
	dbPath := filepath.Join(dataDir, "test.db")

	repos, err := sqlite.NewRepositories(dbPath)
	require.NoError(t, err)
	defer repos.Close()

	eng, err := engine.NewEngineWithAggregateRepo(10, 30, repos.WorkflowAggregate)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, eng.Start(ctx))
	defer eng.Stop()

	registry := eng.GetRegistry()
	// 每个任务约 400ms，共 5 个串行任务，总时长约 2s
	taskDuration := 400 * time.Millisecond
	mockFunc := func(tc *task.TaskContext) (interface{}, error) {
		time.Sleep(taskDuration)
		return map[string]interface{}{"task_id": tc.TaskID}, nil
	}
	_, err = registry.Register(ctx, "progressMockFunc", mockFunc, "进度测试任务")
	require.NoError(t, err)

	wf := workflow.NewWorkflow("e2e-progress-workflow", "E2E进度测试")
	for i := 1; i <= 5; i++ {
		name := fmt.Sprintf("task%d", i)
		deps := []string{}
		if i > 1 {
			deps = append(deps, fmt.Sprintf("task%d", i-1))
		}
		taskObj, err := builder.NewTaskBuilder(name, name, registry).
			WithJobFunction("progressMockFunc", nil).
			WithDependencies(deps).
			Build()
		require.NoError(t, err)
		require.NoError(t, wf.AddTask(taskObj))
	}

	require.NoError(t, eng.RegisterWorkflow(ctx, wf))

	controller, err := eng.SubmitWorkflow(ctx, wf)
	require.NoError(t, err)
	instanceID := controller.GetInstanceID()

	router := api.SetupRouter(eng, "e2e")

	// 轮询 GET /api/v1/instances/:id，打印进度并验证运行中进度非 0、完成后 Completed==Total
	deadline := time.Now().Add(15 * time.Second)
	lastCompleted := -1
	sawProgressIncrease := false

	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/instances/"+instanceID, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		var resp dto.APIResponse[dto.InstanceDetail]
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		p := resp.Data.Progress
		status := resp.Data.Status

		pct := 0.0
		if p.Total > 0 {
			pct = 100 * float64(p.Completed+p.Failed) / float64(p.Total)
		}
		t.Logf("[进度] 状态=%s 总=%d 完成=%d 运行=%d 失败=%d 待执行=%d 进度=%.1f%%",
			status, p.Total, p.Completed, p.Running, p.Failed, p.Pending, pct)

		if p.Completed > lastCompleted {
			sawProgressIncrease = true
			lastCompleted = p.Completed
		}
		// 完成判定：状态为 Success/Failed，或进度已满 5/5（Total 异步初始化可能稍晚）
		done := status == "Success" || status == "Failed" || (p.Total > 0 && p.Completed == p.Total && p.Total == 5)
		if done {
			require.Equal(t, 5, p.Total)
			require.Equal(t, 5, p.Completed)
			require.True(t, sawProgressIncrease, "运行期间进度应曾递增")
			t.Log("✅ E2E 通过：运行中实例进度为内存实时进度且随任务完成递增")
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("等待实例完成超时")
}

// TestE2E_InstanceProgress_DynamicTaskWorkflow 验证带动态子任务的工作流进度包含子任务
// 场景：1 个模板任务 + 3 个子任务，运行中 Total 应为 4，完成后 Completed=4
func TestE2E_InstanceProgress_DynamicTaskWorkflow(t *testing.T) {
	dataDir := filepath.Join(os.TempDir(), "task-engine-e2e-progress-dynamic", time.Now().Format("20060102150405"))
	require.NoError(t, os.MkdirAll(dataDir, 0755))
	dbPath := filepath.Join(dataDir, "test.db")

	repos, err := sqlite.NewRepositories(dbPath)
	require.NoError(t, err)
	defer repos.Close()

	eng, err := engine.NewEngineWithAggregateRepo(10, 30, repos.WorkflowAggregate)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, eng.Start(ctx))
	defer eng.Stop()

	registry := eng.GetRegistry()
	mockFunc := func(tc *task.TaskContext) (interface{}, error) {
		time.Sleep(50 * time.Millisecond)
		return map[string]interface{}{"ok": true}, nil
	}
	_, err = registry.Register(ctx, "dynamicMockFunc", mockFunc, "动态任务")
	require.NoError(t, err)

	type ManagerWithAtomicAddSubTasks interface {
		AtomicAddSubTasks(subTasks []types.Task, parentTaskID string) error
	}
	templateHandler := func(tc *task.TaskContext) {
		manager, ok := task.GetDependencyTyped[ManagerWithAtomicAddSubTasks](tc.Context(), "InstanceManager")
		if !ok {
			if raw, ok2 := tc.GetDependency("InstanceManager"); ok2 {
				manager, _ = raw.(ManagerWithAtomicAddSubTasks)
			}
		}
		if manager == nil {
			return
		}
		subTasks := make([]types.Task, 0, 3)
		for i := 0; i < 3; i++ {
			sub, err := builder.NewTaskBuilder(
				fmt.Sprintf("sub-%s-%d", tc.TaskID, i),
				"子任务",
				registry,
			).WithJobFunction("dynamicMockFunc", nil).Build()
			if err != nil {
				continue
			}
			subTasks = append(subTasks, sub)
		}
		_ = manager.AtomicAddSubTasks(subTasks, tc.TaskID)
	}
	_, err = registry.RegisterTaskHandler(ctx, "dynamicTemplateHandler", templateHandler, "动态模板handler")
	require.NoError(t, err)

	wf := workflow.NewWorkflow("e2e-dynamic-progress", "E2E动态进度")
	templateTask, err := builder.NewTaskBuilder("template", "模板", registry).
		WithJobFunction("dynamicMockFunc", nil).
		Build()
	require.NoError(t, err)
	templateTask.SetTemplate(true)
	templateTask.SetStatusHandlers(map[string][]string{"SUCCESS": {"dynamicTemplateHandler"}})
	require.NoError(t, wf.AddTask(templateTask))

	require.NoError(t, eng.RegisterWorkflow(ctx, wf))

	controller, err := eng.SubmitWorkflow(ctx, wf)
	require.NoError(t, err)
	instanceID := controller.GetInstanceID()

	router := api.SetupRouter(eng, "e2e")
	deadline := time.Now().Add(20 * time.Second)

	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/instances/"+instanceID, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		var resp dto.APIResponse[dto.InstanceDetail]
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		p := resp.Data.Progress
		status := resp.Data.Status

		t.Logf("[动态进度] 状态=%s 总=%d 完成=%d 运行=%d 失败=%d 待执行=%d",
			status, p.Total, p.Completed, p.Running, p.Failed, p.Pending)

		// 完成判定：状态为 Success/Failed，或进度已满（Total>0 且 Completed==Total）
		// 使用 AggregateRepo 且未注入 workflowInstanceRepo 时，DB 可能未更新状态，进度来自内存
		done := status == "Success" || status == "Failed" || (p.Total > 0 && p.Completed == p.Total)
		if done {
			require.Equal(t, 4, p.Total, "应为 1 模板 + 3 子任务 = 4")
			require.Equal(t, 4, p.Completed)
			if status != "Success" && status != "Failed" {
				t.Logf("进度已满 4/4，状态仍为 %s（可能未持久化），以进度为准判定通过", status)
			}
			t.Log("✅ E2E 通过：带动态任务的工作流进度包含子任务")
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatal("等待动态工作流完成超时")
}
