package api

import (
	"github.com/gin-gonic/gin"
	"github.com/stevelan1995/task-engine/pkg/api/handler"
	"github.com/stevelan1995/task-engine/pkg/api/middleware"
	"github.com/stevelan1995/task-engine/pkg/core/engine"
)

// SetupRouter 设置路由
func SetupRouter(eng *engine.Engine, version string) *gin.Engine {
	// 设置gin模式
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()

	// 全局中间件
	router.Use(middleware.Recovery())
	router.Use(middleware.Logger())
	router.Use(middleware.CORS())

	// 创建handlers
	workflowHandler := handler.NewWorkflowHandler(eng)
	instanceHandler := handler.NewInstanceHandler(eng)
	healthHandler := handler.NewHealthHandler(version)

	// 健康检查路由（不带前缀）
	router.GET("/health", healthHandler.Health)
	router.GET("/ready", healthHandler.Ready)

	// API v1 路由组
	v1 := router.Group("/api/v1")
	{
		// Workflow路由
		workflows := v1.Group("/workflows")
		{
			workflows.GET("", workflowHandler.List)
			workflows.POST("", workflowHandler.Upload)
			workflows.GET("/:id", workflowHandler.Get)
			workflows.DELETE("/:id", workflowHandler.Delete)
			workflows.POST("/:id/execute", workflowHandler.Execute)
			workflows.GET("/:id/history", workflowHandler.History)
		}

		// Instance路由
		instances := v1.Group("/instances")
		{
			instances.GET("", instanceHandler.List)
			instances.GET("/:id", instanceHandler.Get)
			instances.GET("/:id/tasks", instanceHandler.GetTasks)
			instances.POST("/:id/pause", instanceHandler.Pause)
			instances.POST("/:id/resume", instanceHandler.Resume)
			instances.POST("/:id/cancel", instanceHandler.Cancel)
		}
	}

	return router
}
