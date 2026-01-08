package taskengine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/stevelan1995/task-engine/pkg/api/dto"
)

// TaskEngine HTTP API客户端
type TaskEngine struct {
	baseURL    string
	httpClient *http.Client
}

// New 创建TaskEngine客户端
func New(baseURL string) *TaskEngine {
	return &TaskEngine{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ========== Workflow API ==========

// ListWorkflows 列出所有Workflow
func (t *TaskEngine) ListWorkflows() (*dto.ListResponse[dto.WorkflowSummary], error) {
	var resp dto.APIResponse[dto.ListResponse[dto.WorkflowSummary]]
	if err := t.get("/api/v1/workflows", &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf(resp.Message)
	}
	return &resp.Data, nil
}

// GetWorkflow 获取Workflow详情
func (t *TaskEngine) GetWorkflow(id string) (*dto.WorkflowDetail, error) {
	var resp dto.APIResponse[dto.WorkflowDetail]
	if err := t.get("/api/v1/workflows/"+id, &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf(resp.Message)
	}
	return &resp.Data, nil
}

// UploadWorkflow 上传Workflow定义
func (t *TaskEngine) UploadWorkflow(yamlContent string) (*dto.WorkflowSummary, error) {
	req := dto.UploadWorkflowRequest{Content: yamlContent}
	var resp dto.APIResponse[dto.WorkflowSummary]
	if err := t.post("/api/v1/workflows", req, &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf(resp.Message)
	}
	return &resp.Data, nil
}

// DeleteWorkflow 删除Workflow
func (t *TaskEngine) DeleteWorkflow(id string) error {
	var resp dto.APIResponse[any]
	if err := t.delete("/api/v1/workflows/"+id, &resp); err != nil {
		return err
	}
	if resp.Code != 0 {
		return fmt.Errorf(resp.Message)
	}
	return nil
}

// ExecuteWorkflow 执行Workflow
func (t *TaskEngine) ExecuteWorkflow(id string, params map[string]interface{}) (*dto.ExecuteResponse, error) {
	req := dto.ExecuteWorkflowRequest{Params: params}
	var resp dto.APIResponse[dto.ExecuteResponse]
	if err := t.post("/api/v1/workflows/"+id+"/execute", req, &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf(resp.Message)
	}
	return &resp.Data, nil
}

// GetWorkflowHistory 查询Workflow执行历史
func (t *TaskEngine) GetWorkflowHistory(id string, status string, limit, offset int) (*dto.HistoryResponse, error) {
	params := url.Values{}
	if status != "" {
		params.Set("status", status)
	}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	if offset > 0 {
		params.Set("offset", fmt.Sprintf("%d", offset))
	}

	path := "/api/v1/workflows/" + id + "/history"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var resp dto.APIResponse[dto.HistoryResponse]
	if err := t.get(path, &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf(resp.Message)
	}
	return &resp.Data, nil
}

// ========== Instance API ==========

// ListInstances 列出所有Instance
func (t *TaskEngine) ListInstances(status string, limit, offset int) (*dto.ListResponse[dto.InstanceSummary], error) {
	params := url.Values{}
	if status != "" {
		params.Set("status", status)
	}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	if offset > 0 {
		params.Set("offset", fmt.Sprintf("%d", offset))
	}

	path := "/api/v1/instances"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var resp dto.APIResponse[dto.ListResponse[dto.InstanceSummary]]
	if err := t.get(path, &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf(resp.Message)
	}
	return &resp.Data, nil
}

// GetInstance 获取Instance详情
func (t *TaskEngine) GetInstance(id string) (*dto.InstanceDetail, error) {
	var resp dto.APIResponse[dto.InstanceDetail]
	if err := t.get("/api/v1/instances/"+id, &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf(resp.Message)
	}
	return &resp.Data, nil
}

// GetInstanceTasks 获取Instance的所有Task
func (t *TaskEngine) GetInstanceTasks(id string) ([]dto.TaskInstanceDetail, error) {
	var resp dto.APIResponse[[]dto.TaskInstanceDetail]
	if err := t.get("/api/v1/instances/"+id+"/tasks", &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf(resp.Message)
	}
	return resp.Data, nil
}

// PauseInstance 暂停Instance
func (t *TaskEngine) PauseInstance(id string) error {
	var resp dto.APIResponse[any]
	if err := t.post("/api/v1/instances/"+id+"/pause", nil, &resp); err != nil {
		return err
	}
	if resp.Code != 0 {
		return fmt.Errorf(resp.Message)
	}
	return nil
}

// ResumeInstance 恢复Instance
func (t *TaskEngine) ResumeInstance(id string) error {
	var resp dto.APIResponse[any]
	if err := t.post("/api/v1/instances/"+id+"/resume", nil, &resp); err != nil {
		return err
	}
	if resp.Code != 0 {
		return fmt.Errorf(resp.Message)
	}
	return nil
}

// CancelInstance 取消Instance
func (t *TaskEngine) CancelInstance(id string) error {
	var resp dto.APIResponse[any]
	if err := t.post("/api/v1/instances/"+id+"/cancel", nil, &resp); err != nil {
		return err
	}
	if resp.Code != 0 {
		return fmt.Errorf(resp.Message)
	}
	return nil
}

// ========== Health API ==========

// Health 健康检查
func (t *TaskEngine) Health() (*dto.HealthResponse, error) {
	var resp dto.APIResponse[dto.HealthResponse]
	if err := t.get("/health", &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf(resp.Message)
	}
	return &resp.Data, nil
}

// ========== HTTP Methods ==========

func (t *TaskEngine) get(path string, result interface{}) error {
	resp, err := t.httpClient.Get(t.baseURL + path)
	if err != nil {
		return fmt.Errorf("HTTP请求失败: %w", err)
	}
	defer resp.Body.Close()

	return t.parseResponse(resp, result)
}

func (t *TaskEngine) post(path string, body interface{}, result interface{}) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("序列化请求体失败: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	resp, err := t.httpClient.Post(t.baseURL+path, "application/json", reqBody)
	if err != nil {
		return fmt.Errorf("HTTP请求失败: %w", err)
	}
	defer resp.Body.Close()

	return t.parseResponse(resp, result)
}

func (t *TaskEngine) delete(path string, result interface{}) error {
	req, err := http.NewRequest(http.MethodDelete, t.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP请求失败: %w", err)
	}
	defer resp.Body.Close()

	return t.parseResponse(resp, result)
}

func (t *TaskEngine) parseResponse(resp *http.Response, result interface{}) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应体失败: %w", err)
	}

	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("解析响应失败: %w, body: %s", err, string(body))
	}

	return nil
}
