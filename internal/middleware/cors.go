package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CORS 可选跨域中间件：allowed 为空时完全不输出 CORS 头（Flutter 原生客户端不需要）。
// 支持精确匹配与 "*"（允许任意来源）。预检请求直接返回 204。
func CORS(allowed []string) gin.HandlerFunc {
	if len(allowed) == 0 {
		return func(c *gin.Context) { c.Next() }
	}
	allowAll := false
	set := make(map[string]bool, len(allowed))
	for _, o := range allowed {
		if o == "*" {
			allowAll = true
		}
		set[o] = true
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		matched := allowAll || set[origin]
		if matched {
			if allowAll {
				c.Header("Access-Control-Allow-Origin", "*")
			} else {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Vary", "Origin")
			}
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Client-ID, X-Request-ID")
			c.Header("Access-Control-Max-Age", "3600")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
