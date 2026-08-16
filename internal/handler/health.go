package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"lens-backend/internal/handler/response"
)

// Health 健康检查：探测数据库连通性，DB 不可用时返回 503，
// 供负载均衡 / k8s 探活及时摘除异常节点（Docker HEALTHCHECK 同步受益）。
func (h *Handler) Health(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := h.repo.Ping(ctx); err != nil {
		response.Error(c, http.StatusServiceUnavailable, response.CodeInternal, "服务不可用")
		return
	}
	response.OK(c, nil)
}
