package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// MaxRequestBodyBytes 限制请求体大小，防止超大 JSON 导致内存耗尽（防 OOM）。
// 最大合法请求体远小于 1MB（content 2000 字符 + contact 128 + extra 8KB + 附件 ID 列表）。
const MaxRequestBodyBytes = 1 << 20 // 1MB

// BodyLimit 限制请求体大小；超过时 JSON 解析失败，Handler 返回 40001。
func BodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}
