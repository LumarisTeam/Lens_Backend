package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"lens-backend/internal/handler/response"
)

// AdminAuth 校验管理员 Bearer Token，用于 /admin 路由。
func AdminAuth(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		provided := ""
		if strings.HasPrefix(h, "Bearer ") {
			provided = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
		// 常数时间比较，降低 token 时序侧信道泄露风险。
		if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			response.Error(c, http.StatusUnauthorized, response.CodeUnauthorized, "未授权")
			c.Abort()
			return
		}
		c.Next()
	}
}
