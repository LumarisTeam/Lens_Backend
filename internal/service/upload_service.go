package service

import (
	"context"
	"errors"
	"net/http"
	"path"
	"time"

	cosclient "lens-backend/internal/cos"
	"lens-backend/internal/config"
	"lens-backend/internal/model"
	"lens-backend/internal/pkg/apperr"
	"lens-backend/internal/pkg/idgen"
	"lens-backend/internal/pkg/imagecheck"
	"lens-backend/internal/repository"
)

// UploadService 处理图片上传链路（presign -> confirm）。
type UploadService struct {
	repo *repository.Repository
	cos  *cosclient.Client
	cfg  *config.Config
}

// NewUploadService 构造 UploadService。
func NewUploadService(repo *repository.Repository, cos *cosclient.Client, cfg *config.Config) *UploadService {
	return &UploadService{repo: repo, cos: cos, cfg: cfg}
}

// Presign 生成上传凭证：先插入 unconfirmed 记录（防幽灵文件），再生成 PUT 预签名 URL。
func (s *UploadService) Presign(ctx context.Context, clientID string, req model.PresignRequest) (*model.PresignResponse, error) {
	ext, ok := imagecheck.ExtFromMime(req.MimeType)
	if !ok {
		return nil, apperr.New(http.StatusUnsupportedMediaType, 41501, "文件类型不支持")
	}
	if req.Size <= 0 {
		return nil, apperr.New(http.StatusBadRequest, 40001, "size 参数非法")
	}
	if req.Size > s.cfg.ImageMaxSize {
		return nil, apperr.New(http.StatusRequestEntityTooLarge, 41301, "文件过大")
	}
	// original_name 列长 255，超长直接拒绝（filename 仅作原始记录）。
	if len(req.Filename) > 255 {
		return nil, apperr.New(http.StatusBadRequest, 40001, "filename 超长")
	}

	fileKey := s.buildFileKey(ext)

	att := &model.FeedbackAttachment{
		ClientID:     clientID,
		FileKey:      fileKey,
		OriginalName: req.Filename,
		MimeType:     req.MimeType,
		SizeBytes:    req.Size,
	}
	if _, err := s.repo.InsertUnconfirmedAttachment(ctx, att); err != nil {
		return nil, err
	}

	uploadURL, err := s.cos.PresignPutObject(fileKey, req.MimeType, req.Size, s.cfg.UploadPresignExpireSeconds)
	if err != nil {
		return nil, err
	}

	return &model.PresignResponse{
		FileKey:   fileKey,
		UploadURL: uploadURL,
		Method:    "PUT",
		Headers:   map[string]string{"Content-Type": req.MimeType},
		ExpiresAt: time.Now().Add(time.Duration(s.cfg.UploadPresignExpireSeconds) * time.Second),
	}, nil
}

// Confirm 校验并确认图片上传：HeadObject 校验大小，读取文件头校验 Magic Number。
func (s *UploadService) Confirm(ctx context.Context, clientID string, req model.ConfirmRequest) (*model.ConfirmResponse, error) {
	if req.FileKey == "" {
		return nil, apperr.New(http.StatusBadRequest, 40001, "file_key 不能为空")
	}

	att, err := s.repo.GetAttachmentByFileKey(ctx, req.FileKey)
	if err != nil {
		if errors.Is(err, repository.ErrAttachmentNotFound) {
			return nil, apperr.New(http.StatusNotFound, 40401, "file_key 不存在")
		}
		return nil, err
	}
	if att.ClientID != clientID {
		return nil, apperr.New(http.StatusBadRequest, 40001, "附件归属冲突")
	}

	switch att.Status {
	case model.AttachmentStatusConfirmed:
		// 幂等：已确认直接返回。
		return &model.ConfirmResponse{AttachmentID: att.ID}, nil
	case model.AttachmentStatusInvalid:
		return nil, apperr.New(http.StatusUnsupportedMediaType, 41501, "文件类型不支持")
	}

	// 1) HeadObject：确认对象存在且不超过大小上限。
	size, err := s.cos.HeadObject(req.FileKey)
	if err != nil {
		return nil, apperr.New(http.StatusNotFound, 40401, "对象不存在，请先上传")
	}
	if size > s.cfg.ImageMaxSize {
		_ = s.repo.UpdateAttachmentStatus(ctx, att.ID, model.AttachmentStatusInvalid)
		return nil, apperr.New(http.StatusRequestEntityTooLarge, 41301, "文件过大")
	}

	// 2) GetObjectRange：读取文件头做 Magic Number 二次校验。
	header, err := s.cos.GetObjectRange(req.FileKey, 0, 15)
	if err != nil {
		return nil, apperr.New(http.StatusNotFound, 40401, "对象读取失败")
	}
	if !imagecheck.Check(att.MimeType, header) {
		// 验证失败不立即删 COS，交由孤儿清理任务处理。
		_ = s.repo.UpdateAttachmentStatus(ctx, att.ID, model.AttachmentStatusInvalid)
		return nil, apperr.New(http.StatusUnsupportedMediaType, 41501, "文件类型不支持")
	}

	if err := s.repo.UpdateAttachmentStatus(ctx, att.ID, model.AttachmentStatusConfirmed); err != nil {
		return nil, err
	}
	return &model.ConfirmResponse{AttachmentID: att.ID}, nil
}

// buildFileKey 根据 mime_type 严格生成 file_key：[S3_BASE_PREFIX/]feedback/YYYY/MM/DD/{ULID}{ext}。
// 使用 UTC 日期，路径分隔符统一为 "/"。
func (s *UploadService) buildFileKey(ext string) string {
	now := time.Now().UTC()
	key := path.Join("feedback", now.Format("2006"), now.Format("01"), now.Format("02"), idgen.NewULID()+ext)
	if s.cfg.StorageBasePrefix != "" {
		return path.Join(s.cfg.StorageBasePrefix, key)
	}
	return key
}
