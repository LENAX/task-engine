package middleware

import (
	"log"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"github.com/LENAX/task-engine/pkg/api/dto"
)

// Recovery panic恢复中间件
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				// 打印堆栈信息
				log.Printf("[Recovery] panic recovered: %v\n%s", err, debug.Stack())

				// 返回500错误
				c.JSON(http.StatusInternalServerError, dto.NewErrorResponse(
					500,
					"Internal Server Error",
				))
				c.Abort()
			}
		}()
		c.Next()
	}
}
