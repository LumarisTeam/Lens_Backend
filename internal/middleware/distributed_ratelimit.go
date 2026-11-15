package middleware

import (
	"github.com/gin-gonic/gin"

	"lens-backend/internal/handler/response"
	"lens-backend/internal/service"
)

// DistributedIPRateLimit 使用 Redis 对来源 IP 与网段做多实例共享限流。
func DistributedIPRateLimit(riskSvc *service.RiskService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := riskSvc.CheckClientRate(c.Request.Context(), c.ClientIP()); err != nil {
			c.Header("Retry-After", "1")
			response.WriteError(c, err)
			c.Abort()
			return
		}
		c.Next()
	}
}
