package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"lens-backend/internal/handler/response"
	"lens-backend/internal/middleware"
	"lens-backend/internal/model"
)

// Presign 获取图片上传凭证。
func (h *Handler) Presign(c *gin.Context) {
	var req model.PresignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeBadRequest, "参数错误")
		return
	}
	resp, err := h.upload.Presign(c.Request.Context(), c.GetString(middleware.ClientIDKey), req)
	if err != nil {
		response.WriteError(c, err)
		return
	}
	response.OK(c, resp)
}

// Confirm 确认图片上传。
func (h *Handler) Confirm(c *gin.Context) {
	var req model.ConfirmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeBadRequest, "参数错误")
		return
	}
	resp, err := h.upload.Confirm(c.Request.Context(), c.GetString(middleware.ClientIDKey), req)
	if err != nil {
		response.WriteError(c, err)
		return
	}
	response.OK(c, resp)
}
