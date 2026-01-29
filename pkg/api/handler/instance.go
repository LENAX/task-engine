package handler

import (
	"fmt"
	"net/http"

	"github.com/LENAX/task-engine/pkg/api/dto"
	"github.com/LENAX/task-engine/pkg/core/engine"
	"github.com/LENAX/task-engine/pkg/storage"
	"github.com/gin-gonic/gin"
)

// InstanceHandler Instance API处理器
type InstanceHandler struct {
	engine *engine.Engine
}

// NewInstanceHandler 创建InstanceHandler
func NewInstanceHandler(eng *engine.Engine) *InstanceHandler {
	return &InstanceHandler{engine: eng}
}

// List 列出所有Instance
// GET /api/v1/instances
func (h *InstanceHandler) List(c *gin.Context) {
	ctx := c.Request.Context()

	var query dto.ListQueryRequest
	if err := c.ShouldBindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, dto.NewErrorResponse(400, fmt.Sprintf("查询参数错误: %v", err)))
		return
	}

	repo := h.engine.GetAggregateRepo()
	if repo == nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(500, "存储未配置"))
		return
	}

	// 获取所有Workflow
	workflows, err := repo.ListWorkflows(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(500, fmt.Sprintf("查询Workflow失败: %v", err)))
		return
	}

	// 收集所有Instance
	var allInstances []dto.InstanceSummary
	for _, wf := range workflows {
		instances, err := repo.ListWorkflowInstances(ctx, wf.ID)
		if err != nil {
			continue
		}
		for _, inst := range instances {
			// 按状态过滤
			if query.Status != "" && inst.Status != query.Status {
				continue
			}

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
			allInstances = append(allInstances, summary)
		}
	}

	// 分页
	limit := query.GetDefaultLimit()
	offset := query.Offset
	total := len(allInstances)

	if offset >= total {
		allInstances = []dto.InstanceSummary{}
	} else {
		end := offset + limit
		if end > total {
			end = total
		}
		allInstances = allInstances[offset:end]
	}

	c.JSON(http.StatusOK, dto.NewSuccessResponse(dto.ListResponse[dto.InstanceSummary]{
		Total:   total,
		Items:   allInstances,
		HasMore: offset+limit < total,
	}))
}

// Get 获取Instance详情
// GET /api/v1/instances/:id
func (h *InstanceHandler) Get(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	repo := h.engine.GetAggregateRepo()
	if repo == nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(500, "存储未配置"))
		return
	}

	inst, tasks, err := repo.GetWorkflowInstanceWithTasks(ctx, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(500, fmt.Sprintf("查询Instance失败: %v", err)))
		return
	}
	if inst == nil {
		c.JSON(http.StatusNotFound, dto.NewErrorResponse(404, "Instance不存在"))
		return
	}

	// 获取Workflow名称
	wfName := ""
	wf, _ := repo.GetWorkflow(ctx, inst.WorkflowID)
	if wf != nil {
		wfName = wf.Name
	}

	// 进度：运行中实例优先使用内存中的实时进度（含动态子任务），否则用入库任务计算
	var progress dto.ProgressInfo
	if snapshot, ok := h.engine.GetInstanceProgress(id); ok {
		progress = dto.ProgressInfo{
			Total:     snapshot.Total,
			Completed: snapshot.Completed,
			Running:   snapshot.Running,
			Failed:    snapshot.Failed,
			Pending:   snapshot.Pending,
		}
	} else {
		progress = calculateProgress(tasks)
	}

	detail := dto.InstanceDetail{
		ID:           inst.ID,
		WorkflowID:   inst.WorkflowID,
		WorkflowName: wfName,
		Status:       inst.Status,
		Progress:     progress,
		StartedAt:    inst.StartTime,
		FinishedAt:   inst.EndTime,
		ErrorMessage: inst.ErrorMessage,
	}

	c.JSON(http.StatusOK, dto.NewSuccessResponse(detail))
}

// GetTasks 获取Instance的所有Task详情
// GET /api/v1/instances/:id/tasks
func (h *InstanceHandler) GetTasks(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	repo := h.engine.GetAggregateRepo()
	if repo == nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(500, "存储未配置"))
		return
	}

	tasks, err := repo.GetTaskInstancesByWorkflowInstance(ctx, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(500, fmt.Sprintf("查询Task失败: %v", err)))
		return
	}

	items := make([]dto.TaskInstanceDetail, 0, len(tasks))
	for _, t := range tasks {
		item := dto.TaskInstanceDetail{
			ID:           t.ID,
			TaskID:       t.ID,   // TaskInstance的ID就是TaskID
			TaskName:     t.Name, // 使用Name字段
			Status:       t.Status,
			ErrorMessage: t.ErrorMessage,
			RetryCount:   t.RetryCount,
		}
		if t.StartTime != nil {
			item.StartedAt = t.StartTime
		}
		if t.EndTime != nil {
			item.FinishedAt = t.EndTime
			if t.StartTime != nil {
				duration := t.EndTime.Sub(*t.StartTime)
				item.Duration = formatDuration(duration)
			}
		}
		items = append(items, item)
	}

	c.JSON(http.StatusOK, dto.NewSuccessResponse(items))
}

// Pause 暂停Instance
// POST /api/v1/instances/:id/pause
func (h *InstanceHandler) Pause(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	if err := h.engine.PauseWorkflowInstance(ctx, id); err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(500, fmt.Sprintf("暂停失败: %v", err)))
		return
	}

	c.JSON(http.StatusOK, dto.NewSuccessResponse(map[string]string{
		"message": "Instance已暂停",
		"id":      id,
	}))
}

// Resume 恢复Instance
// POST /api/v1/instances/:id/resume
func (h *InstanceHandler) Resume(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	if err := h.engine.ResumeWorkflowInstance(ctx, id); err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(500, fmt.Sprintf("恢复失败: %v", err)))
		return
	}

	c.JSON(http.StatusOK, dto.NewSuccessResponse(map[string]string{
		"message": "Instance已恢复",
		"id":      id,
	}))
}

// Cancel 取消Instance
// POST /api/v1/instances/:id/cancel
func (h *InstanceHandler) Cancel(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	if err := h.engine.TerminateWorkflowInstance(ctx, id, "用户取消"); err != nil {
		c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(500, fmt.Sprintf("取消失败: %v", err)))
		return
	}

	c.JSON(http.StatusOK, dto.NewSuccessResponse(map[string]string{
		"message": "Instance已取消",
		"id":      id,
	}))
}

// calculateProgress 计算执行进度
func calculateProgress(tasks []*storage.TaskInstance) dto.ProgressInfo {
	progress := dto.ProgressInfo{
		Total: len(tasks),
	}

	for _, t := range tasks {
		switch t.Status {
		case "Success":
			progress.Completed++
		case "Running":
			progress.Running++
		case "Failed":
			progress.Failed++
		default:
			progress.Pending++
		}
	}

	return progress
}
