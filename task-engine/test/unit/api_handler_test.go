package unit

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stevelan1995/task-engine/pkg/api/dto"
	"github.com/stevelan1995/task-engine/pkg/api/handler"
	"github.com/stevelan1995/task-engine/pkg/api/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// TestHealthHandler 测试健康检查处理器
func TestHealthHandler(t *testing.T) {
	t.Run("健康检查返回正确响应", func(t *testing.T) {
		// 创建handler
		h := handler.NewHealthHandler("1.0.0-test")

		// 创建测试router
		router := gin.New()
		router.GET("/health", h.Health)

		// 发送请求
		req, _ := http.NewRequest("GET", "/health", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// 验证响应
		assert.Equal(t, http.StatusOK, w.Code)

		var resp dto.APIResponse[dto.HealthResponse]
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)

		assert.Equal(t, 0, resp.Code)
		assert.Equal(t, "success", resp.Message)
		assert.Equal(t, "healthy", resp.Data.Status)
		assert.Equal(t, "1.0.0-test", resp.Data.Version)
		assert.NotEmpty(t, resp.Data.Uptime)
		assert.NotEmpty(t, resp.Data.Timestamp)
	})

	t.Run("就绪检查返回正确响应", func(t *testing.T) {
		h := handler.NewHealthHandler("1.0.0-test")

		router := gin.New()
		router.GET("/ready", h.Ready)

		req, _ := http.NewRequest("GET", "/ready", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// TestMiddleware 测试中间件
func TestMiddleware(t *testing.T) {
	t.Run("Recovery中间件捕获panic", func(t *testing.T) {
		router := gin.New()
		router.Use(middleware.Recovery())

		// 添加一个会panic的处理器
		router.GET("/panic", func(c *gin.Context) {
			panic("test panic")
		})

		req, _ := http.NewRequest("GET", "/panic", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// 应该返回500而不是崩溃
		assert.Equal(t, http.StatusInternalServerError, w.Code)

		var resp dto.APIResponse[any]
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, 500, resp.Code)
	})

	t.Run("CORS中间件设置正确的头", func(t *testing.T) {
		router := gin.New()
		router.Use(middleware.CORS())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(200, gin.H{"status": "ok"})
		})

		req, _ := http.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
		assert.Contains(t, w.Header().Get("Access-Control-Allow-Methods"), "GET")
	})

	t.Run("CORS中间件处理OPTIONS请求", func(t *testing.T) {
		router := gin.New()
		router.Use(middleware.CORS())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(200, gin.H{"status": "ok"})
		})

		req, _ := http.NewRequest("OPTIONS", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, 204, w.Code)
	})
}

// TestDTOSerialization 测试DTO序列化
func TestDTOSerialization(t *testing.T) {
	t.Run("APIResponse序列化", func(t *testing.T) {
		resp := dto.NewSuccessResponse(map[string]string{"key": "value"})

		data, err := json.Marshal(resp)
		require.NoError(t, err)

		var parsed dto.APIResponse[map[string]string]
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)

		assert.Equal(t, 0, parsed.Code)
		assert.Equal(t, "success", parsed.Message)
		assert.Equal(t, "value", parsed.Data["key"])
	})

	t.Run("ErrorResponse序列化", func(t *testing.T) {
		resp := dto.NewErrorResponse(400, "Bad Request")

		data, err := json.Marshal(resp)
		require.NoError(t, err)

		var parsed dto.APIResponse[any]
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)

		assert.Equal(t, 400, parsed.Code)
		assert.Equal(t, "Bad Request", parsed.Message)
	})

	t.Run("HistoryQueryRequest默认值", func(t *testing.T) {
		req := dto.HistoryQueryRequest{}
		assert.Equal(t, 20, req.GetDefaultLimit())
		assert.Equal(t, "desc", req.GetDefaultOrder())

		req.Limit = 50
		req.Order = "asc"
		assert.Equal(t, 50, req.GetDefaultLimit())
		assert.Equal(t, "asc", req.GetDefaultOrder())
	})
}

// TestRequestValidation 测试请求验证
func TestRequestValidation(t *testing.T) {
	t.Run("ExecuteWorkflowRequest绑定", func(t *testing.T) {
		router := gin.New()
		router.POST("/test", func(c *gin.Context) {
			var req dto.ExecuteWorkflowRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(400, gin.H{"error": err.Error()})
				return
			}
			c.JSON(200, req)
		})

		// 测试有效请求
		body := `{"params": {"key": "value"}}`
		req, _ := http.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		// 测试空body也应该通过
		req2, _ := http.NewRequest("POST", "/test", bytes.NewBufferString("{}"))
		req2.Header.Set("Content-Type", "application/json")
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)

		assert.Equal(t, http.StatusOK, w2.Code)
	})

	t.Run("UploadWorkflowRequest需要content字段", func(t *testing.T) {
		router := gin.New()
		router.POST("/test", func(c *gin.Context) {
			var req dto.UploadWorkflowRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(400, gin.H{"error": err.Error()})
				return
			}
			c.JSON(200, req)
		})

		// 测试缺少content字段
		body := `{}`
		req, _ := http.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		// 测试有content字段
		body2 := `{"content": "yaml content"}`
		req2, _ := http.NewRequest("POST", "/test", bytes.NewBufferString(body2))
		req2.Header.Set("Content-Type", "application/json")
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)

		assert.Equal(t, http.StatusOK, w2.Code)
	})
}
