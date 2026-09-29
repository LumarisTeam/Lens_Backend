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

// ---- 反馈中心 DTO 与实体 ----

const (
	FeedbackCenterStatusEnabled  = "enabled"
	FeedbackCenterStatusDisabled = "disabled"

	FeedbackCenterSNModeWhitelist = "whitelist"
	FeedbackCenterSNModePrefix    = "prefix"
	FeedbackCenterSNModeAny       = "any"

	FeedbackCenterAuditCreate       = "create"
	FeedbackCenterAuditUpdate       = "update"
	FeedbackCenterAuditEnable       = "enable"
	FeedbackCenterAuditDisable      = "disable"
	FeedbackCenterAuditResetSecret  = "reset_secret"
	FeedbackCenterAuditGenerateCode = "generate_code"
)

// FeedbackCenter 对应 feedback_center 表。
type FeedbackCenter struct {
	ID            int64
	CenterID      string
	Name          string
	AppID         string
	Env           string
	SecretCipher  string
	SecretVersion int
	SNMode        string
	Status        string
	ExpireAt      *time.Time
	Contact       string
	Remark        string
	CreatedBy     string
	LastUsedAt    *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// FeedbackCenterSN 对应 feedback_center_sn 表。
type FeedbackCenterSN struct {
	ID        int64
	CenterID  string
	SN        string
	Status    string
	ExpireAt  *time.Time
	CreatedAt time.Time
}

// FeedbackCenterAudit 对应 feedback_center_audit 表。
type FeedbackCenterAudit struct {
	ID        int64
	CenterID  string
	Action    string
	Operator  string
	Detail    json.RawMessage
	IP        string
	CreatedAt time.Time
}

// CreateFeedbackCenterRequest 创建反馈中心请求。
type CreateFeedbackCenterRequest struct {
	Name     string     `json:"name"`
	AppID    string     `json:"appId"`
	Env      string     `json:"env"`
	SNMode   string     `json:"snMode"`
	SNList   []string   `json:"snList"`
	ExpireAt *time.Time `json:"expireAt"`
	Contact  string     `json:"contact"`
	Remark   string     `json:"remark"`
}

// UpdateFeedbackCenterRequest 更新反馈中心请求；仅传非 nil 字段生效。
type UpdateFeedbackCenterRequest struct {
	Name     *string    `json:"name"`
	AppID    *string    `json:"appId"`
	Env      *string    `json:"env"`
	SNMode   *string    `json:"snMode"`
	SNList   *[]string  `json:"snList"`
	ExpireAt *time.Time `json:"expireAt"`
	Contact  *string    `json:"contact"`
	Remark   *string    `json:"remark"`
}

// CreateFeedbackCenterResponse 仅在创建时返回一次完整 secret。
type CreateFeedbackCenterResponse struct {
	CenterID string `json:"centerId"`
	Secret   string `json:"secret"`
	Status   string `json:"status"`
}

// FeedbackCenterListItem 反馈中心列表项。
type FeedbackCenterListItem struct {
	CenterID      string     `json:"centerId"`
	Name          string     `json:"name"`
	AppID         string     `json:"appId"`
	Env           string     `json:"env"`
	SNMode        string     `json:"snMode"`
	Status        string     `json:"status"`
	SecretVersion int        `json:"secretVersion"`
	ExpireAt      *time.Time `json:"expireAt,omitempty"`
	Contact       string     `json:"contact"`
	LastUsedAt    *time.Time `json:"lastUsedAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

// FeedbackCenterListResponse 反馈中心列表响应。
type FeedbackCenterListResponse struct {
	Total int64                    `json:"total"`
	Items []FeedbackCenterListItem `json:"items"`
}

// FeedbackCenterDetail 反馈中心详情（不返回 secret 明文）。
type FeedbackCenterDetail struct {
	CenterID      string     `json:"centerId"`
	Name          string     `json:"name"`
	AppID         string     `json:"appId"`
	Env           string     `json:"env"`
	SecretMasked  string     `json:"secretMasked"`
	SecretVersion int        `json:"secretVersion"`
	SNMode        string     `json:"snMode"`
	Status        string     `json:"status"`
	SNList        []string   `json:"snList"`
	ExpireAt      *time.Time `json:"expireAt,omitempty"`
	Contact       string     `json:"contact"`
	Remark        string     `json:"remark"`
	CreatedBy     string     `json:"createdBy"`
	LastUsedAt    *time.Time `json:"lastUsedAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

// ListFeedbackCentersParams 反馈中心列表查询参数。
type ListFeedbackCentersParams struct {
	Keyword string
	Status  string
	Limit   int
	Offset  int
}

// FeedbackCenterStatusRequest 启停请求。
type FeedbackCenterStatusRequest struct {
	Status string `json:"status"`
}

// ResetFeedbackCenterSecretResponse 仅重置后返回一次完整 secret。
type ResetFeedbackCenterSecretResponse struct {
	CenterID      string `json:"centerId"`
	Secret        string `json:"secret"`
	SecretVersion int    `json:"secretVersion"`
}

// GenerateFeedbackCodeRequest 生成校验码请求。
type GenerateFeedbackCodeRequest struct {
	Timestamp int64  `json:"timestamp"`
	SN        string `json:"sn"`
	Nonce     string `json:"nonce"`
	TTL       int    `json:"ttl"`
}

// GenerateFeedbackCodeResponse 生成校验码响应。
type GenerateFeedbackCodeResponse struct {
	CenterID  string    `json:"centerId"`
	Timestamp int64     `json:"timestamp"`
	SN        string    `json:"sn"`
	Nonce     string    `json:"nonce"`
	Code      string    `json:"code"`
	ExpireAt  time.Time `json:"expireAt"`
}

// FeedbackCodeHeaders 反馈提交接口的校验头。
type FeedbackCodeHeaders struct {
	CenterID  string
	Timestamp string
	SN        string
	Nonce     string
	Code      string
}

// FeedbackCenterAuditListItem 审计日志列表项。
type FeedbackCenterAuditListItem struct {
	ID        int64           `json:"id"`
	CenterID  string          `json:"centerId"`
	Action    string          `json:"action"`
	Operator  string          `json:"operator"`
	Detail    json.RawMessage `json:"detail"`
	IP        string          `json:"ip"`
	CreatedAt time.Time       `json:"createdAt"`
}

// FeedbackCenterAuditListResponse 审计日志列表响应。
type FeedbackCenterAuditListResponse struct {
	Total int64                         `json:"total"`
	Items []FeedbackCenterAuditListItem `json:"items"`
}
