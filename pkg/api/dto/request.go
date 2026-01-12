package dto

// ExecuteWorkflowRequest 执行Workflow请求
type ExecuteWorkflowRequest struct {
	Params map[string]interface{} `json:"params" binding:"omitempty"`
}

// UploadWorkflowRequest 上传Workflow请求（YAML内容）
type UploadWorkflowRequest struct {
	Content string `json:"content" binding:"required"`
}

// HistoryQueryRequest 执行历史查询请求
type HistoryQueryRequest struct {
	Status string `form:"status" binding:"omitempty,oneof=Success Failed Running Paused Ready Terminated"`
	Limit  int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Offset int    `form:"offset" binding:"omitempty,min=0"`
	Order  string `form:"order" binding:"omitempty,oneof=asc desc"`
}

// ListQueryRequest 通用列表查询请求
type ListQueryRequest struct {
	Limit  int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Offset int    `form:"offset" binding:"omitempty,min=0"`
	Status string `form:"status" binding:"omitempty"`
}

// GetDefaultLimit 获取默认limit
func (r *HistoryQueryRequest) GetDefaultLimit() int {
	if r.Limit <= 0 {
		return 20
	}
	return r.Limit
}

// GetDefaultOrder 获取默认排序
func (r *HistoryQueryRequest) GetDefaultOrder() string {
	if r.Order == "" {
		return "desc"
	}
	return r.Order
}

// GetDefaultLimit 获取默认limit
func (r *ListQueryRequest) GetDefaultLimit() int {
	if r.Limit <= 0 {
		return 20
	}
	return r.Limit
}
