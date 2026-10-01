package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"lens-backend/internal/handler/response"
	"lens-backend/internal/middleware"
	"lens-backend/internal/model"
)

// CreateFeedbackCenter 创建反馈中心。
func (h *Handler) CreateFeedbackCenter(c *gin.Context) {
	var req model.CreateFeedbackCenterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeBadRequest, "请求参数错误")
		return
	}
	out, err := h.center.Create(c.Request.Context(), req, operator(c), c.ClientIP())
	if err != nil {
		response.WriteError(c, err)
		return
	}
	response.OK(c, out)
}

// ListFeedbackCenters 查询反馈中心列表。
func (h *Handler) ListFeedbackCenters(c *gin.Context) {
	page := parseIntDefault(c.Query("page"), 1)
	pageSize := parseIntDefault(c.Query("page_size"), 20)
	if page < 1 {
		page = 1
	}
	if page > 1000 {
		page = 1000
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	out, err := h.center.List(c.Request.Context(), model.ListFeedbackCentersParams{
		Keyword: c.Query("keyword"),
		Status:  c.Query("status"),
		Limit:   pageSize,
		Offset:  (page - 1) * pageSize,
	})
	if err != nil {
		response.WriteError(c, err)
		return
	}
	response.OK(c, out)
}

// GetFeedbackCenter 查询反馈中心详情。
func (h *Handler) GetFeedbackCenter(c *gin.Context) {
	out, err := h.center.Get(c.Request.Context(), c.Param("center_id"))
	if err != nil {
		response.WriteError(c, err)
		return
	}
	response.OK(c, out)
}

// UpdateFeedbackCenter 更新反馈中心。
func (h *Handler) UpdateFeedbackCenter(c *gin.Context) {
	var req model.UpdateFeedbackCenterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeBadRequest, "请求参数错误")
		return
	}
	out, err := h.center.Update(
		c.Request.Context(), c.Param("center_id"), req, operator(c), c.ClientIP(),
	)
	if err != nil {
		response.WriteError(c, err)
		return
	}
	response.OK(c, out)
}

// EnableFeedbackCenter 启用反馈中心。
func (h *Handler) EnableFeedbackCenter(c *gin.Context) {
	h.setFeedbackCenterStatus(c, model.FeedbackCenterStatusEnabled)
}

// DisableFeedbackCenter 禁用反馈中心。
func (h *Handler) DisableFeedbackCenter(c *gin.Context) {
	h.setFeedbackCenterStatus(c, model.FeedbackCenterStatusDisabled)
}

// UpdateFeedbackCenterStatus 按请求体更新反馈中心状态。
func (h *Handler) UpdateFeedbackCenterStatus(c *gin.Context) {
	var req model.FeedbackCenterStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeBadRequest, "请求参数错误")
		return
	}
	h.setFeedbackCenterStatus(c, req.Status)
}

// ResetFeedbackCenterSecret 重置反馈中心 secret。
func (h *Handler) ResetFeedbackCenterSecret(c *gin.Context) {
	out, err := h.center.ResetSecret(
		c.Request.Context(), c.Param("center_id"), operator(c), c.ClientIP(),
	)
	if err != nil {
		response.WriteError(c, err)
		return
	}
	response.OK(c, out)
}

// GenerateFeedbackCode 生成测试校验码。
func (h *Handler) GenerateFeedbackCode(c *gin.Context) {
	var req model.GenerateFeedbackCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeBadRequest, "请求参数错误")
		return
	}
	out, err := h.center.GenerateCode(
		c.Request.Context(), c.Param("center_id"), req, operator(c), c.ClientIP(),
	)
	if err != nil {
		response.WriteError(c, err)
		return
	}
	response.OK(c, out)
}

// IssueFeedbackCode 客户端取码；时间戳、nonce 与 TTL 由服务端生成。
func (h *Handler) IssueFeedbackCode(c *gin.Context) {
	var req model.IssueFeedbackCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeBadRequest, "请求参数错误")
		return
	}
	out, err := h.center.IssueCode(
		c.Request.Context(),
		c.Param("center_id"),
		c.GetString(middleware.ClientIDKey),
		req,
	)
	if err != nil {
		response.WriteError(c, err)
		return
	}
	response.OK(c, out)
}

// ListFeedbackCenterAudits 查询反馈中心审计日志。
func (h *Handler) ListFeedbackCenterAudits(c *gin.Context) {
	page := parseIntDefault(c.Query("page"), 1)
	pageSize := parseIntDefault(c.Query("page_size"), 20)
	if page < 1 {
		page = 1
	}
	if page > 1000 {
		page = 1000
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	out, err := h.center.ListAudits(
		c.Request.Context(),
		c.Param("center_id"),
		pageSize,
		(page-1)*pageSize,
	)
	if err != nil {
		response.WriteError(c, err)
		return
	}
	response.OK(c, out)
}

func (h *Handler) setFeedbackCenterStatus(c *gin.Context, status string) {
	if err := h.center.SetStatus(
		c.Request.Context(),
		c.Param("center_id"),
		status,
		operator(c),
		c.ClientIP(),
	); err != nil {
		response.WriteError(c, err)
		return
	}
	response.OK(c, struct {
		Status string `json:"status"`
	}{Status: status})
}

func operator(c *gin.Context) string {
	if v := c.GetString(middleware.OperatorKey); v != "" {
		return v
	}
	return "admin"
}
