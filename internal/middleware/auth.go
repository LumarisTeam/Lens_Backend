package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"lens-backend/internal/handler/response"
)

// OperatorKey 是管理员操作人存储在 gin.Context 中的键。
const OperatorKey = "admin_operator"

// AdminRoleKey 是管理员角色存储在 gin.Context 中的键。
const AdminRoleKey = "admin_role"

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
		operator := strings.TrimSpace(c.GetHeader("X-Operator"))
		if operator == "" {
			operator = "admin"
		}
		if len(operator) > 64 {
			response.Error(c, http.StatusBadRequest, response.CodeBadRequest, "X-Operator 超长")
			c.Abort()
			return
		}
		role := strings.TrimSpace(c.GetHeader("X-Admin-Role"))
		if role == "" {
			role = "admin"
		}
		if role != "admin" && role != "readonly" {
			response.Error(c, http.StatusUnauthorized, response.CodeUnauthorized, "管理员角色非法")
			c.Abort()
			return
		}
		c.Set(OperatorKey, operator)
		c.Set(AdminRoleKey, role)
		c.Next()
	}
}

// RequireAdminWrite 拒绝只读管理员写操作。
func RequireAdminWrite() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetString(AdminRoleKey) != "admin" {
			response.Error(c, http.StatusForbidden, response.CodeUnauthorized, "只读用户无操作权限")
			c.Abort()
			return
		}
		c.Next()
	}
}
