package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/gin-gonic/gin"
)

// RequestIDKey 是请求 ID 在 gin.Context 中的键。
const RequestIDKey = "request_id"

// RequestID 透传合法 X-Request-ID，否则生成随机请求 ID。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
		if requestID == "" || len(requestID) > 128 || !validRequestID(requestID) {
			requestID = newRequestID()
		}
		c.Set(RequestIDKey, requestID)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}

func validRequestID(value string) bool {
	for _, r := range value {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func newRequestID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(buf)
}
