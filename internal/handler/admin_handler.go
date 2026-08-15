package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"lens-backend/internal/handler/response"
	"lens-backend/internal/model"
	"lens-backend/internal/repository"
)

// ListFeedbacks 管理员分页查询反馈列表。
func (h *Handler) ListFeedbacks(c *gin.Context) {
	p, err := parseListParams(c)
	if err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeBadRequest, err.Error())
		return
	}
	total, err := h.repo.CountFeedbacks(c.Request.Context(), p)
	if err != nil {
		response.WriteError(c, err)
		return
	}
	items, err := h.repo.ListFeedbacks(c.Request.Context(), p)
	if err != nil {
		response.WriteError(c, err)
		return
	}
	response.OK(c, model.FeedbackListResponse{Total: total, Items: items})
}

// GetFeedback 管理员查询反馈详情，并实时生成附件的预签名 GET URL。
func (h *Handler) GetFeedback(c *gin.Context) {
	no := c.Param("feedback_no")
	f, err := h.repo.GetFeedbackByNo(c.Request.Context(), no)
	if err != nil {
		if errors.Is(err, repository.ErrFeedbackNotFound) {
			response.Error(c, http.StatusNotFound, response.CodeNotFound, "资源不存在")
			return
		}
		response.WriteError(c, err)
		return
	}

	atts, err := h.repo.ListAttachmentsByFeedbackID(c.Request.Context(), f.ID)
	if err != nil {
		response.WriteError(c, err)
		return
	}

	images := make([]model.FeedbackImage, 0, len(atts))
	for _, a := range atts {
		u, err := h.cos.PresignGetObject(a.FileKey, h.cfg.PresignGetExpireSeconds)
		if err != nil {
			response.WriteError(c, err)
			return
		}
		images = append(images, model.FeedbackImage{
			AttachmentID: a.ID,
			FileKey:      a.FileKey,
			URL:          u,
			ExpiresAt:    time.Now().Add(time.Duration(h.cfg.PresignGetExpireSeconds) * time.Second),
			MimeType:     a.MimeType,
			Size:         a.SizeBytes,
		})
	}

	response.OK(c, model.FeedbackDetail{
		FeedbackNo: f.FeedbackNo,
		Content:    f.Content,
		Contact:    f.Contact,
		ImageCount: f.ImageCount,
		Extra:      f.Extra,
		CreatedAt:  f.CreatedAt,
		Images:     images,
	})
}

// parseListParams 解析分页、过滤与时间参数。时间显式解析为 RFC3339 并转 UTC。
func parseListParams(c *gin.Context) (model.ListFeedbacksParams, error) {
	page := parseIntDefault(c.Query("page"), 1)
	pageSize := parseIntDefault(c.Query("page_size"), 20)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	start, err := parseRFC3339(c.Query("start_date"))
	if err != nil {
		return model.ListFeedbacksParams{}, err
	}
	end, err := parseRFC3339(c.Query("end_date"))
	if err != nil {
		return model.ListFeedbacksParams{}, err
	}

	return model.ListFeedbacksParams{
		Contact:   c.Query("contact"),
		AppName:   c.Query("app_name"),
		StartTime: start,
		EndTime:   end,
		Limit:     pageSize,
		Offset:    (page - 1) * pageSize,
	}, nil
}

// parseRFC3339 解析 RFC3339 时间并转换为 UTC。
func parseRFC3339(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, fmt.Errorf("时间格式必须为 RFC3339")
	}
	u := t.UTC()
	return &u, nil
}

func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}
