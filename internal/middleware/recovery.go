package middleware

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"lens-backend/internal/handler/response"
)

// Recovery 捕获 panic，记录日志并返回 50001，避免进程崩溃。
func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("panic recovered", "panic", r)
				response.Error(c, http.StatusInternalServerError, response.CodeInternal, "服务内部错误")
				c.Abort()
			}
		}()
		c.Next()
	}
}
