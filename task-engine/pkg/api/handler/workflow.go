package handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/LENAX/task-engine/pkg/api/dto"
	"github.com/LENAX/task-engine/pkg/core/engine"
	"github.com/LENAX/task-engine/pkg/core/workflow"
)

// WorkflowHandler Workflow API处理器
type WorkflowHandler struct {
	engine *engine.Engine
}

// NewWorkflowHandler 创建WorkflowHandler
func NewWorkflowHandler(eng *engine.Engine) *WorkflowHandler {
	return &WorkflowHandler{engine: eng}
}

// List 列出所有Workflow
// GET /api/v1/workflows
func (h *WorkflowHandler) List(c *gin.Context) {
	ctx := c.Request.Context()

	repo := h.engine.GetAggregateRepo()
	if repo == nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(500, "存储未配置"))
		return
	}

	workflows, err := repo.ListWorkflows(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(500, fmt.Sprintf("查询Workflow失败: %v", err)))
		return
	}

	items := make([]dto.WorkflowSummary, 0, len(workflows))
	for _, wf := range workflows {
		taskCount := 0
		wf.Tasks.Range(func(_, _ interface{}) bool {
			taskCount++
			return true
		})

		items = append(items, dto.WorkflowSummary{
			ID:          wf.ID,
			Name:        wf.Name,
			Description: wf.Description,
			TaskCount:   taskCount,
			Status:      wf.GetStatus(),
			CronExpr:    wf.GetCronExpr(),
			CronEnabled: wf.IsCronEnabled(),
			CreatedAt:   wf.CreateTime,
		})
	}

	c.JSON(http.StatusOK, dto.NewSuccessResponse(dto.ListResponse[dto.WorkflowSummary]{
		Total:   len(items),
		Items:   items,
		HasMore: false,
	}))
}

// Get 获取Workflow详情
// GET /api/v1/workflows/:id
func (h *WorkflowHandler) Get(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	repo := h.engine.GetAggregateRepo()
	if repo == nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(500, "存储未配置"))
		return
	}

	wf, err := repo.GetWorkflowWithTasks(ctx, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(500, fmt.Sprintf("查询Workflow失败: %v", err)))
		return
	}
	if wf == nil {
		c.JSON(http.StatusNotFound, dto.NewErrorResponse(404, "Workflow不存在"))
		return
	}

	// 构建Task列表
	tasks := make([]dto.TaskSummary, 0)
	wf.Tasks.Range(func(_, value interface{}) bool {
		t := value.(workflow.Task)
		tasks = append(tasks, dto.TaskSummary{
			ID:           t.GetID(),
			Name:         t.GetName(),
			Description:  t.GetDescription(),
			Dependencies: t.GetDependencies(),
		})
		return true
	})

	detail := dto.WorkflowDetail{
		WorkflowSummary: dto.WorkflowSummary{
			ID:          wf.ID,
			Name:        wf.Name,
			Description: wf.Description,
			TaskCount:   len(tasks),
			Status:      wf.GetStatus(),
			CronExpr:    wf.GetCronExpr(),
			CronEnabled: wf.IsCronEnabled(),
			CreatedAt:   wf.CreateTime,
		},
		Tasks:        tasks,
		Dependencies: wf.GetDependencies(),
	}

	c.JSON(http.StatusOK, dto.NewSuccessResponse(detail))
}

// Upload 上传Workflow定义
// POST /api/v1/workflows
func (h *WorkflowHandler) Upload(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.UploadWorkflowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.NewErrorResponse(400, fmt.Sprintf("请求参数错误: %v", err)))
		return
	}

	// 解析YAML内容并创建Workflow
	wfDef, err := h.engine.LoadWorkflowFromYAML(req.Content)
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.NewErrorResponse(400, fmt.Sprintf("解析Workflow定义失败: %v", err)))
		return
	}

	// 注册Workflow
	if err := h.engine.RegisterWorkflow(ctx, wfDef.Workflow); err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(500, fmt.Sprintf("注册Workflow失败: %v", err)))
		return
	}

	c.JSON(http.StatusOK, dto.NewSuccessResponse(dto.WorkflowSummary{
		ID:          wfDef.Workflow.ID,
		Name:        wfDef.Workflow.Name,
		Description: wfDef.Workflow.Description,
		CreatedAt:   wfDef.Workflow.CreateTime,
	}))
}

// Delete 删除Workflow
// DELETE /api/v1/workflows/:id
func (h *WorkflowHandler) Delete(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	repo := h.engine.GetAggregateRepo()
	if repo == nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(500, "存储未配置"))
		return
	}

	if err := repo.DeleteWorkflow(ctx, id); err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(500, fmt.Sprintf("删除Workflow失败: %v", err)))
		return
	}

	c.JSON(http.StatusOK, dto.NewSuccessResponse(map[string]string{
		"message": "删除成功",
		"id":      id,
	}))
}

// Execute 执行Workflow
// POST /api/v1/workflows/:id/execute
func (h *WorkflowHandler) Execute(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var req dto.ExecuteWorkflowRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		c.JSON(http.StatusBadRequest, dto.NewErrorResponse(400, fmt.Sprintf("请求参数错误: %v", err)))
		return
	}

	repo := h.engine.GetAggregateRepo()
	if repo == nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(500, "存储未配置"))
		return
	}

	// 获取Workflow
	wf, err := repo.GetWorkflowWithTasks(ctx, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(500, fmt.Sprintf("查询Workflow失败: %v", err)))
		return
	}
	if wf == nil {
		c.JSON(http.StatusNotFound, dto.NewErrorResponse(404, "Workflow不存在"))
		return
	}

	// 设置参数
	if req.Params != nil {
		for k, v := range req.Params {
			if strVal, ok := v.(string); ok {
				wf.SetParam(k, strVal)
			}
		}
	}

	// 提交执行
	controller, err := h.engine.SubmitWorkflow(ctx, wf)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(500, fmt.Sprintf("执行Workflow失败: %v", err)))
		return
	}

	c.JSON(http.StatusOK, dto.NewSuccessResponse(dto.ExecuteResponse{
		InstanceID: controller.GetInstanceID(),
		Message:    "Workflow已提交执行",
	}))
}

// History 查询Workflow执行历史
// GET /api/v1/workflows/:id/history
func (h *WorkflowHandler) History(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var query dto.HistoryQueryRequest
	if err := c.ShouldBindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, dto.NewErrorResponse(400, fmt.Sprintf("查询参数错误: %v", err)))
		return
	}

	repo := h.engine.GetAggregateRepo()
	if repo == nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(500, "存储未配置"))
		return
	}

	// 获取Workflow信息
	wf, err := repo.GetWorkflow(ctx, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(500, fmt.Sprintf("查询Workflow失败: %v", err)))
		return
	}
	if wf == nil {
		c.JSON(http.StatusNotFound, dto.NewErrorResponse(404, "Workflow不存在"))
		return
	}

	// 获取所有Instance
	instances, err := repo.ListWorkflowInstances(ctx, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(500, fmt.Sprintf("查询执行历史失败: %v", err)))
		return
	}

	// 过滤和排序
	filtered := filterInstances(instances, query.Status)

	// 排序（按开始时间）
	sortInstances(filtered, query.GetDefaultOrder())

	// 分页
	limit := query.GetDefaultLimit()
	offset := query.Offset
	total := len(filtered)

	if offset >= total {
		filtered = []*workflow.WorkflowInstance{}
	} else {
		end := offset + limit
		if end > total {
			end = total
		}
		filtered = filtered[offset:end]
	}

	// 构建响应
	items := make([]dto.InstanceSummary, 0, len(filtered))
	for _, inst := range filtered {
		summary := dto.InstanceSummary{
			ID:           inst.ID,
			WorkflowID:   inst.WorkflowID,
			WorkflowName: wf.Name,
			Status:       inst.Status,
			StartedAt:    inst.StartTime,
			FinishedAt:   inst.EndTime,
			ErrorMessage: inst.ErrorMessage,
		}
		if inst.EndTime != nil {
			duration := inst.EndTime.Sub(inst.StartTime)
			summary.Duration = formatDuration(duration)
		}
		items = append(items, summary)
	}

	c.JSON(http.StatusOK, dto.NewSuccessResponse(dto.HistoryResponse{
		Total:   total,
		Items:   items,
		HasMore: offset+limit < total,
	}))
}

// filterInstances 按状态过滤Instance
func filterInstances(instances []*workflow.WorkflowInstance, status string) []*workflow.WorkflowInstance {
	if status == "" {
		return instances
	}
	filtered := make([]*workflow.WorkflowInstance, 0)
	for _, inst := range instances {
		if inst.Status == status {
			filtered = append(filtered, inst)
		}
	}
	return filtered
}

// sortInstances 按时间排序Instance
func sortInstances(instances []*workflow.WorkflowInstance, order string) {
	// 简单冒泡排序
	n := len(instances)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			shouldSwap := false
			if order == "desc" {
				shouldSwap = instances[j].StartTime.Before(instances[j+1].StartTime)
			} else {
				shouldSwap = instances[j].StartTime.After(instances[j+1].StartTime)
			}
			if shouldSwap {
				instances[j], instances[j+1] = instances[j+1], instances[j]
			}
		}
	}
}

// formatDuration 格式化时长
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}
