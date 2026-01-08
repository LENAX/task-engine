package unit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stevelan1995/task-engine/pkg/api/dto"
	"github.com/stevelan1995/task-engine/pkg/cli/output"
	"github.com/stevelan1995/task-engine/pkg/cli/taskengine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTaskEngineClient 测试TaskEngine客户端
func TestTaskEngineClient(t *testing.T) {
	t.Run("ListWorkflows成功", func(t *testing.T) {
		// 创建mock服务器
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v1/workflows", r.URL.Path)
			assert.Equal(t, "GET", r.Method)

			resp := dto.APIResponse[dto.ListResponse[dto.WorkflowSummary]]{
				Code:    0,
				Message: "success",
				Data: dto.ListResponse[dto.WorkflowSummary]{
					Total: 1,
					Items: []dto.WorkflowSummary{
						{ID: "wf-1", Name: "测试工作流", TaskCount: 3},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := taskengine.New(server.URL)
		result, err := client.ListWorkflows()

		require.NoError(t, err)
		assert.Equal(t, 1, result.Total)
		assert.Len(t, result.Items, 1)
		assert.Equal(t, "wf-1", result.Items[0].ID)
	})

	t.Run("GetWorkflow成功", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v1/workflows/wf-123", r.URL.Path)

			resp := dto.APIResponse[dto.WorkflowDetail]{
				Code:    0,
				Message: "success",
				Data: dto.WorkflowDetail{
					WorkflowSummary: dto.WorkflowSummary{
						ID:   "wf-123",
						Name: "测试工作流",
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := taskengine.New(server.URL)
		result, err := client.GetWorkflow("wf-123")

		require.NoError(t, err)
		assert.Equal(t, "wf-123", result.ID)
	})

	t.Run("ExecuteWorkflow成功", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v1/workflows/wf-123/execute", r.URL.Path)
			assert.Equal(t, "POST", r.Method)

			resp := dto.APIResponse[dto.ExecuteResponse]{
				Code:    0,
				Message: "success",
				Data: dto.ExecuteResponse{
					InstanceID: "inst-456",
					Message:    "已提交执行",
				},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := taskengine.New(server.URL)
		result, err := client.ExecuteWorkflow("wf-123", nil)

		require.NoError(t, err)
		assert.Equal(t, "inst-456", result.InstanceID)
	})

	t.Run("DeleteWorkflow成功", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v1/workflows/wf-123", r.URL.Path)
			assert.Equal(t, "DELETE", r.Method)

			resp := dto.APIResponse[any]{
				Code:    0,
				Message: "success",
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := taskengine.New(server.URL)
		err := client.DeleteWorkflow("wf-123")

		require.NoError(t, err)
	})

	t.Run("ListInstances成功", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v1/instances", r.URL.Path)

			resp := dto.APIResponse[dto.ListResponse[dto.InstanceSummary]]{
				Code:    0,
				Message: "success",
				Data: dto.ListResponse[dto.InstanceSummary]{
					Total: 1,
					Items: []dto.InstanceSummary{
						{ID: "inst-1", WorkflowID: "wf-1", Status: "Running"},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := taskengine.New(server.URL)
		result, err := client.ListInstances("", 20, 0)

		require.NoError(t, err)
		assert.Equal(t, 1, result.Total)
	})

	t.Run("GetInstance成功", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v1/instances/inst-123", r.URL.Path)

			resp := dto.APIResponse[dto.InstanceDetail]{
				Code:    0,
				Message: "success",
				Data: dto.InstanceDetail{
					ID:     "inst-123",
					Status: "Running",
				},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := taskengine.New(server.URL)
		result, err := client.GetInstance("inst-123")

		require.NoError(t, err)
		assert.Equal(t, "inst-123", result.ID)
	})

	t.Run("PauseInstance成功", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v1/instances/inst-123/pause", r.URL.Path)
			assert.Equal(t, "POST", r.Method)

			resp := dto.APIResponse[any]{Code: 0, Message: "success"}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := taskengine.New(server.URL)
		err := client.PauseInstance("inst-123")

		require.NoError(t, err)
	})

	t.Run("ResumeInstance成功", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v1/instances/inst-123/resume", r.URL.Path)

			resp := dto.APIResponse[any]{Code: 0, Message: "success"}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := taskengine.New(server.URL)
		err := client.ResumeInstance("inst-123")

		require.NoError(t, err)
	})

	t.Run("CancelInstance成功", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v1/instances/inst-123/cancel", r.URL.Path)

			resp := dto.APIResponse[any]{Code: 0, Message: "success"}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := taskengine.New(server.URL)
		err := client.CancelInstance("inst-123")

		require.NoError(t, err)
	})

	t.Run("Health成功", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/health", r.URL.Path)

			resp := dto.APIResponse[dto.HealthResponse]{
				Code:    0,
				Message: "success",
				Data: dto.HealthResponse{
					Status:  "healthy",
					Version: "1.0.0",
				},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := taskengine.New(server.URL)
		result, err := client.Health()

		require.NoError(t, err)
		assert.Equal(t, "healthy", result.Status)
	})

	t.Run("API错误响应", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := dto.APIResponse[any]{
				Code:    404,
				Message: "Workflow不存在",
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := taskengine.New(server.URL)
		_, err := client.GetWorkflow("non-existent")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "Workflow不存在")
	})

	t.Run("GetWorkflowHistory成功", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v1/workflows/wf-123/history", r.URL.Path)
			assert.Equal(t, "10", r.URL.Query().Get("limit"))

			resp := dto.APIResponse[dto.HistoryResponse]{
				Code:    0,
				Message: "success",
				Data: dto.HistoryResponse{
					Total: 2,
					Items: []dto.InstanceSummary{
						{ID: "inst-1", Status: "Success"},
						{ID: "inst-2", Status: "Failed"},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := taskengine.New(server.URL)
		result, err := client.GetWorkflowHistory("wf-123", "", 10, 0)

		require.NoError(t, err)
		assert.Equal(t, 2, result.Total)
	})
}

// TestOutputTable 测试表格输出
func TestOutputTable(t *testing.T) {
	t.Run("表格创建和行添加", func(t *testing.T) {
		// 测试表格创建不会panic
		table := output.NewTable([]string{"ID", "NAME", "STATUS"})
		assert.NotNil(t, table)

		// 测试添加行不会panic
		table.AddRow([]string{"1", "Test", "OK"})
		table.AddRow([]string{"2", "Test2", "Failed"})

		// 测试渲染不会panic（输出到stdout）
		assert.NotPanics(t, func() {
			table.Render()
		})
	})

	t.Run("空表格渲染", func(t *testing.T) {
		table := output.NewTable([]string{"COL1", "COL2"})
		assert.NotPanics(t, func() {
			table.Render()
		})
	})
}

// TestOutputJSON 测试JSON输出
func TestOutputJSON(t *testing.T) {
	t.Run("PrintJSONString", func(t *testing.T) {
		data := map[string]string{"key": "value"}
		result, err := output.PrintJSONString(data)

		require.NoError(t, err)
		assert.Contains(t, result, "key")
		assert.Contains(t, result, "value")
	})

	t.Run("PrintJSONString复杂对象", func(t *testing.T) {
		data := dto.WorkflowSummary{
			ID:        "wf-123",
			Name:      "测试",
			TaskCount: 5,
		}
		result, err := output.PrintJSONString(data)

		require.NoError(t, err)
		assert.Contains(t, result, "wf-123")
		assert.Contains(t, result, "测试")
	})
}
