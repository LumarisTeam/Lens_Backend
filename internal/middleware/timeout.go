package middleware

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
)

// Timeout 为每个请求设置上下文 deadline。
// 超时后依赖 ctx 的 DB/COS 操作立即取消，防止 DB 挂起时请求无限占用连接。
// d <= 0 时透传（由调用方按配置决定是否挂载）。
func Timeout(d time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d <= 0 {
			c.Next()
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
