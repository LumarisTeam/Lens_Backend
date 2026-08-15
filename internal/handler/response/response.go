// Package response 提供统一响应格式与错误码字典，
// 避免 Handler 中散落重复的 JSON 响应逻辑。
package response

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"lens-backend/internal/pkg/apperr"
)

// 业务错误码字典（与 PRD 保持一致）。
const (
	CodeOK              = 0
	CodeBadRequest      = 40001
	CodeMissingClientID = 40002
	CodeUnauthorized    = 40101
	CodeNotFound        = 40401
	CodeConflict        = 40901
	CodeTooLarge        = 41301
	CodeUnsupportedType = 41501
	CodeTooManyRequests = 42901
	CodeInternal        = 50001
)

// Body 是统一响应体。成功携带 data，失败只携带 code/message。
type Body struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// OK 输出成功响应：{"code":0,"message":"ok","data":...}。
func OK(c *gin.Context, data interface{}) {
	if data == nil {
		data = struct{}{}
	}
	c.JSON(http.StatusOK, Body{Code: CodeOK, Message: "ok", Data: data})
}

// Error 输出失败响应：{"code":..., "message":...}。
func Error(c *gin.Context, httpStatus, code int, message string) {
	c.JSON(httpStatus, Body{Code: code, Message: message})
}

// WriteError 将 error 映射为响应：*apperr.Error 直接透传，其余视为 50001。
func WriteError(c *gin.Context, err error) {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		Error(c, ae.HTTPStatus, ae.Code, ae.Message)
		return
	}
	slog.Error("unhandled error", "error", err)
	Error(c, http.StatusInternalServerError, CodeInternal, "服务内部错误")
}
