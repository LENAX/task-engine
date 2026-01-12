package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/LENAX/task-engine/pkg/api"
	"github.com/LENAX/task-engine/pkg/api/dto"
	"github.com/LENAX/task-engine/pkg/core/builder"
	"github.com/LENAX/task-engine/pkg/core/engine"
	"github.com/LENAX/task-engine/pkg/core/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPIIntegration API集成测试
func TestAPIIntegration(t *testing.T) {
	// 跳过如果没有配置文件
	configPath := "./test_configs/api_test_engine.yaml"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// 创建临时配置
		configPath = createTempConfig(t)
	}

	// 创建Engine
	eng, err := engine.NewEngineBuilder(configPath).Build()
	require.NoError(t, err)

	ctx := context.Background()
	err = eng.Start(ctx)
	require.NoError(t, err)
	defer eng.Stop()

	// 注册测试函数
	registry := eng.GetRegistry()
	_, err = registry.Register(ctx, "test_job", func(ctx *task.TaskContext) (interface{}, error) {
		return "test result", nil
	}, "测试Job函数")
	require.NoError(t, err)

	// 创建测试路由
	router := api.SetupRouter(eng, "test-version")

	t.Run("健康检查", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/health", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp dto.APIResponse[dto.HealthResponse]
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)

		assert.Equal(t, 0, resp.Code)
		assert.Equal(t, "healthy", resp.Data.Status)
	})

	t.Run("列出Workflow-空列表", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/v1/workflows", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp dto.APIResponse[dto.ListResponse[dto.WorkflowSummary]]
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)

		assert.Equal(t, 0, resp.Code)
		assert.Equal(t, 0, resp.Data.Total)
	})

	t.Run("列出Instance-空列表", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/v1/instances", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp dto.APIResponse[dto.ListResponse[dto.InstanceSummary]]
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)

		assert.Equal(t, 0, resp.Code)
	})

	t.Run("获取不存在的Workflow", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/v1/workflows/non-existent-id", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// 不存在时返回404
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("获取不存在的Instance", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/v1/instances/non-existent-id", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// 不存在时返回404
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("执行不存在的Workflow", func(t *testing.T) {
		req, _ := http.NewRequest("POST", "/api/v1/workflows/non-existent-id/execute", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// 不存在时应返回404或400
		assert.True(t, w.Code == http.StatusNotFound || w.Code == http.StatusBadRequest)
	})

	// 注册Workflow并测试
	t.Run("注册并获取Workflow", func(t *testing.T) {
		// 使用Builder创建Workflow
		taskDef, err := builder.NewTaskBuilder("测试任务", "测试任务描述", registry).
			WithJobFunction("test_job", nil).
			WithTimeout(30).
			Build()
		require.NoError(t, err)

		wf, err := builder.NewWorkflowBuilder("测试工作流", "测试工作流描述").
			WithTask(taskDef).
			Build()
		require.NoError(t, err)

		// 注册Workflow
		err = eng.RegisterWorkflow(ctx, wf)
		require.NoError(t, err)

		// 测试列出Workflow
		req, _ := http.NewRequest("GET", "/api/v1/workflows", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var listResp dto.APIResponse[dto.ListResponse[dto.WorkflowSummary]]
		err = json.Unmarshal(w.Body.Bytes(), &listResp)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, listResp.Data.Total, 1)

		// 测试获取Workflow详情
		req2, _ := http.NewRequest("GET", "/api/v1/workflows/"+wf.ID, nil)
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)

		assert.Equal(t, http.StatusOK, w2.Code)

		var detailResp dto.APIResponse[dto.WorkflowDetail]
		err = json.Unmarshal(w2.Body.Bytes(), &detailResp)
		require.NoError(t, err)
		assert.Equal(t, wf.ID, detailResp.Data.ID)
		assert.Equal(t, "测试工作流", detailResp.Data.Name)

		// 测试查询执行历史（应为空）
		req3, _ := http.NewRequest("GET", "/api/v1/workflows/"+wf.ID+"/history", nil)
		w3 := httptest.NewRecorder()
		router.ServeHTTP(w3, req3)

		assert.Equal(t, http.StatusOK, w3.Code)
	})
}

// TestAPIErrorHandling API错误处理测试
func TestAPIErrorHandling(t *testing.T) {
	configPath := createTempConfig(t)

	eng, err := engine.NewEngineBuilder(configPath).Build()
	require.NoError(t, err)

	ctx := context.Background()
	err = eng.Start(ctx)
	require.NoError(t, err)
	defer eng.Stop()

	router := api.SetupRouter(eng, "test-version")

	t.Run("暂停不存在的Instance", func(t *testing.T) {
		req, _ := http.NewRequest("POST", "/api/v1/instances/non-existent/pause", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Engine返回错误时API返回500
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("恢复不存在的Instance", func(t *testing.T) {
		req, _ := http.NewRequest("POST", "/api/v1/instances/non-existent/resume", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("取消不存在的Instance", func(t *testing.T) {
		req, _ := http.NewRequest("POST", "/api/v1/instances/non-existent/cancel", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// createTempConfig 创建临时配置文件
func createTempConfig(t *testing.T) string {
	content := `
task-engine:
  storage:
    database:
      type: "sqlite"
      dsn: ":memory:"
  execution:
    worker_concurrency: 5
    default_task_timeout: "30s"
`
	tmpFile, err := os.CreateTemp("", "api_test_config_*.yaml")
	require.NoError(t, err)

	_, err = tmpFile.WriteString(content)
	require.NoError(t, err)
	tmpFile.Close()

	t.Cleanup(func() {
		os.Remove(tmpFile.Name())
	})

	return tmpFile.Name()
}
