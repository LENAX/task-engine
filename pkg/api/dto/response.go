package dto

import "time"

// APIResponse 通用API响应结构
type APIResponse[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data,omitempty"`
}

// NewSuccessResponse 创建成功响应
func NewSuccessResponse[T any](data T) APIResponse[T] {
	return APIResponse[T]{
		Code:    0,
		Message: "success",
		Data:    data,
	}
}

// NewErrorResponse 创建错误响应
func NewErrorResponse(code int, message string) APIResponse[any] {
	return APIResponse[any]{
		Code:    code,
		Message: message,
	}
}

// WorkflowSummary Workflow摘要信息
type WorkflowSummary struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	TaskCount   int       `json:"task_count"`
	Status      string    `json:"status"`
	CronExpr    string    `json:"cron_expr,omitempty"`
	CronEnabled bool      `json:"cron_enabled"`
	CreatedAt   time.Time `json:"created_at"`
}

// WorkflowDetail Workflow详细信息
type WorkflowDetail struct {
	WorkflowSummary
	Tasks        []TaskSummary `json:"tasks"`
	Dependencies map[string][]string `json:"dependencies"`
}

// TaskSummary Task摘要信息
type TaskSummary struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Dependencies []string `json:"dependencies,omitempty"`
	Timeout      int      `json:"timeout,omitempty"`
	RetryCount   int      `json:"retry_count,omitempty"`
}

// InstanceDetail Instance详细信息
type InstanceDetail struct {
	ID           string       `json:"id"`
	WorkflowID   string       `json:"workflow_id"`
	WorkflowName string       `json:"workflow_name,omitempty"`
	Status       string       `json:"status"`
	Progress     ProgressInfo `json:"progress"`
	StartedAt    time.Time    `json:"started_at"`
	FinishedAt   *time.Time   `json:"finished_at,omitempty"`
	ErrorMessage string       `json:"error_message,omitempty"`
}

// ProgressInfo 进度信息
type ProgressInfo struct {
	Total          int      `json:"total"`
	Completed      int      `json:"completed"`
	Running        int      `json:"running"`
	Failed         int      `json:"failed"`
	Pending        int      `json:"pending"`
	RunningTaskIDs []string `json:"running_task_ids,omitempty"`
	PendingTaskIDs []string `json:"pending_task_ids,omitempty"`
}

// TaskInstanceDetail Task实例详细信息
type TaskInstanceDetail struct {
	ID           string     `json:"id"`
	TaskID       string     `json:"task_id"`
	TaskName     string     `json:"task_name"`
	Status       string     `json:"status"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	Duration     string     `json:"duration,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
	RetryCount   int        `json:"retry_count"`
}

// HistoryResponse 执行历史响应
type HistoryResponse struct {
	Total   int               `json:"total"`
	Items   []InstanceSummary `json:"items"`
	HasMore bool              `json:"has_more"`
}

// InstanceSummary Instance摘要信息
type InstanceSummary struct {
	ID           string     `json:"id"`
	WorkflowID   string     `json:"workflow_id"`
	WorkflowName string     `json:"workflow_name"`
	Status       string     `json:"status"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	Duration     string     `json:"duration,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
}

// ExecuteResponse 执行响应
type ExecuteResponse struct {
	InstanceID string `json:"instance_id"`
	Message    string `json:"message"`
}

// HealthResponse 健康检查响应
type HealthResponse struct {
	Status    string `json:"status"`
	Version   string `json:"version"`
	Uptime    string `json:"uptime"`
	Timestamp string `json:"timestamp"`
}

// ListResponse 列表响应
type ListResponse[T any] struct {
	Total   int  `json:"total"`
	Items   []T  `json:"items"`
	HasMore bool `json:"has_more"`
}
