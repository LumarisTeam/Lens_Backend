package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"lens-backend/internal/handler/response"
	"lens-backend/internal/middleware"
	"lens-backend/internal/model"
)

// SubmitFeedback 提交反馈。
func (h *Handler) SubmitFeedback(c *gin.Context) {
	var req model.SubmitFeedbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeBadRequest, "参数错误")
		return
	}
	resp, err := h.feedback.Submit(
		c.Request.Context(),
		c.GetString(middleware.ClientIDKey),
		c.GetString(middleware.FeedbackAppIDKey),
		req,
	)
	if err != nil {
		response.WriteError(c, err)
		return
	}
	response.OK(c, resp)
}
