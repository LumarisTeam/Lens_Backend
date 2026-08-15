package handler

import (
	"github.com/gin-gonic/gin"

	"lens-backend/internal/handler/response"
)

// Health 健康检查。
func (h *Handler) Health(c *gin.Context) {
	response.OK(c, nil)
}
