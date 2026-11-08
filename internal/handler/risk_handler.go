package handler

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"lens-backend/internal/handler/response"
	"lens-backend/internal/model"
	"lens-backend/internal/repository"
)

// ReviewFeedback 人工将反馈状态调整为 normal/review/blocked。
func (h *Handler) ReviewFeedback(c *gin.Context) {
	var req model.FeedbackReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeBadRequest, "请求参数错误")
		return
	}
	req.Status = strings.TrimSpace(req.Status)
	req.Note = strings.TrimSpace(req.Note)
	if req.Status != model.FeedbackReviewNormal &&
		req.Status != model.FeedbackReviewReview &&
		req.Status != model.FeedbackReviewBlocked {
		response.Error(c, http.StatusBadRequest, response.CodeBadRequest, "review_status 参数非法")
		return
	}
	if utf8.RuneCountInString(req.Note) > 500 {
		response.Error(c, http.StatusBadRequest, response.CodeBadRequest, "note 过长")
		return
	}
	err := h.repo.UpdateFeedbackReview(
		c.Request.Context(),
		c.Param("feedback_no"),
		req.Status,
		req.Note,
		operator(c),
	)
	if err != nil {
		if errors.Is(err, repository.ErrFeedbackNotFound) {
			response.Error(c, http.StatusNotFound, response.CodeNotFound, "资源不存在")
			return
		}
		response.WriteError(c, err)
		return
	}
	response.OK(c, struct {
		FeedbackNo string `json:"feedback_no"`
		Status     string `json:"review_status"`
	}{
		FeedbackNo: c.Param("feedback_no"),
		Status:     req.Status,
	})
}

// ListRiskEvents 查询风控事件与拦截记录。
func (h *Handler) ListRiskEvents(c *gin.Context) {
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
	start, err := parseRFC3339(c.Query("start_date"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeBadRequest, err.Error())
		return
	}
	end, err := parseRFC3339(c.Query("end_date"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeBadRequest, err.Error())
		return
	}
	items, total, err := h.repo.ListRiskEvents(c.Request.Context(), model.ListRiskEventsParams{
		ClientIP:  strings.TrimSpace(c.Query("client_ip")),
		ClientID:  strings.TrimSpace(c.Query("client_id")),
		CenterID:  strings.TrimSpace(c.Query("center_id")),
		Action:    strings.TrimSpace(c.Query("action")),
		EventType: strings.TrimSpace(c.Query("event_type")),
		MinScore:  parseIntDefault(c.Query("min_risk_score"), 0),
		StartTime: start,
		EndTime:   end,
		Limit:     pageSize,
		Offset:    (page - 1) * pageSize,
	})
	if err != nil {
		response.WriteError(c, err)
		return
	}
	response.OK(c, model.RiskEventListResponse{Total: total, Items: items})
}

// GetRiskStats 查询最近 N 天风控趋势与高风险 IP。
func (h *Handler) GetRiskStats(c *gin.Context) {
	days := parseIntDefault(c.Query("days"), 7)
	out, err := h.repo.GetRiskStats(c.Request.Context(), days, c.Query("center_id"))
	if err != nil {
		response.WriteError(c, err)
		return
	}
	response.OK(c, out)
}
