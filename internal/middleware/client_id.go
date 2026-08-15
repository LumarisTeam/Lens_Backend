package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"lens-backend/internal/handler/response"
)

// ClientIDKey 是存储在 gin.Context 中的匿名身份键。
const ClientIDKey = "client_id"

// MaxClientIDLength 是 X-Client-ID 的最大长度。
const MaxClientIDLength = 64

// ClientID 校验匿名身份请求头，缺失返回 40002，超长返回 40001。
func ClientID() gin.HandlerFunc {
	return func(c *gin.Context) {
		cid := c.GetHeader("X-Client-ID")
		if cid == "" {
			response.Error(c, http.StatusBadRequest, response.CodeMissingClientID, "缺少 X-Client-ID")
			c.Abort()
			return
		}
		if len(cid) > MaxClientIDLength {
			response.Error(c, http.StatusBadRequest, response.CodeBadRequest, "X-Client-ID 超长")
			c.Abort()
			return
		}
		c.Set(ClientIDKey, cid)
		c.Next()
	}
}
