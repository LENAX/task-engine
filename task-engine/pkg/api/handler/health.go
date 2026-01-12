package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/LENAX/task-engine/pkg/api/dto"
)

// HealthHandler 健康检查处理器
type HealthHandler struct {
	version   string
	startTime time.Time
}

// NewHealthHandler 创建HealthHandler
func NewHealthHandler(version string) *HealthHandler {
	return &HealthHandler{
		version:   version,
		startTime: time.Now(),
	}
}

// Health 健康检查
// GET /health
func (h *HealthHandler) Health(c *gin.Context) {
	uptime := time.Since(h.startTime)

	c.JSON(http.StatusOK, dto.NewSuccessResponse(dto.HealthResponse{
		Status:    "healthy",
		Version:   h.version,
		Uptime:    formatDuration(uptime),
		Timestamp: time.Now().Format(time.RFC3339),
	}))
}

// Ready 就绪检查
// GET /ready
func (h *HealthHandler) Ready(c *gin.Context) {
	c.JSON(http.StatusOK, dto.NewSuccessResponse(map[string]string{
		"status": "ready",
	}))
}
