// Package model 定义数据库实体与接口 DTO。
package model

import (
	"encoding/json"
	"time"
)

// Feedback 对应 feedback 表。
type Feedback struct {
	ID         int64
	FeedbackNo string
	RequestID  string
	ClientID   string
	UserID     *int64
	Content    string
	Contact    string
	ImageCount int16
	Extra      json.RawMessage
	CreatedAt  time.Time
}

// FeedbackAttachment 对应 feedback_attachment 表。
type FeedbackAttachment struct {
	ID           int64
	FeedbackID   *int64
	ClientID     string
	FileKey      string
	OriginalName string
	MimeType     string
	Status       string
	SizeBytes    int64
	CreatedAt    time.Time
}

// 附件状态常量。
const (
	AttachmentStatusUnconfirmed = "unconfirmed"
	AttachmentStatusConfirmed   = "confirmed"
	AttachmentStatusInvalid     = "invalid"
)

// ---- 客户端上传链路 DTO ----

// PresignRequest 获取图片上传凭证请求。
type PresignRequest struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	MimeType string `json:"mime_type"`
}

// PresignResponse 获取图片上传凭证响应。
type PresignResponse struct {
	FileKey   string            `json:"file_key"`
	UploadURL string            `json:"upload_url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expires_at"`
}

// ConfirmRequest 确认图片上传请求。
type ConfirmRequest struct {
	FileKey string `json:"file_key"`
}

// ConfirmResponse 确认图片上传响应。
type ConfirmResponse struct {
	AttachmentID int64 `json:"attachment_id"`
}

// SubmitFeedbackRequest 提交反馈请求。
type SubmitFeedbackRequest struct {
	RequestID     string          `json:"request_id"`
	Content       string          `json:"content"`
	Contact       string          `json:"contact"`
	AttachmentIDs []int64         `json:"attachment_ids"`
	Extra         json.RawMessage `json:"extra"`
}

// SubmitFeedbackResponse 提交反馈响应。
type SubmitFeedbackResponse struct {
	FeedbackNo string `json:"feedback_no"`
}

// ---- 管理员读取链路 DTO ----

// ListFeedbacksParams 列表查询参数（时间已转换为 UTC）。
type ListFeedbacksParams struct {
	Contact   string
	AppName   string
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
	Offset    int
}

// FeedbackImage 反馈图片视图（含临时签名 URL 与过期时间）。
type FeedbackImage struct {
	AttachmentID int64     `json:"attachment_id"`
	FileKey      string    `json:"file_key"`
	URL          string    `json:"url"`
	ExpiresAt    time.Time `json:"expires_at"`
	MimeType     string    `json:"mime_type"`
	Size         int64     `json:"size"`
}

// FeedbackDetail 反馈详情视图。
type FeedbackDetail struct {
	FeedbackNo string          `json:"feedback_no"`
	Content    string          `json:"content"`
	Contact    string          `json:"contact"`
	ImageCount int16           `json:"image_count"`
	Extra      json.RawMessage `json:"extra"`
	CreatedAt  time.Time       `json:"created_at"`
	Images     []FeedbackImage `json:"images"`
}

// FeedbackListItem 列表项视图。
type FeedbackListItem struct {
	FeedbackNo string          `json:"feedback_no"`
	ClientID   string          `json:"client_id"`
	Content    string          `json:"content"`
	Contact    string          `json:"contact"`
	ImageCount int16           `json:"image_count"`
	Extra      json.RawMessage `json:"extra"`
	CreatedAt  time.Time       `json:"created_at"`
}

// FeedbackListResponse 列表响应。
type FeedbackListResponse struct {
	Total int64              `json:"total"`
	Items []FeedbackListItem `json:"items"`
}

// OrphanAttachment 孤儿清理使用的精简附件信息。
type OrphanAttachment struct {
	ID      int64
	FileKey string
}
