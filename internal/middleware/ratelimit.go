package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"lens-backend/internal/handler/response"
	"lens-backend/internal/service"
)

// RateLimit 以 "IP|X-Client-ID" 为维度做进程内限流，触发返回 42901 + Retry-After。
func RateLimit(rl *service.RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.ClientIP() + "|" + c.GetString(ClientIDKey)
		if !rl.Allow(key) {
			c.Header("Retry-After", "60")
			response.Error(c, http.StatusTooManyRequests, response.CodeTooManyRequests, "请求过于频繁")
			c.Abort()
			return
		}
		c.Next()
	}
}
