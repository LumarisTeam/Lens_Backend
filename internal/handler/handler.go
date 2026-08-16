// Package handler 是 Gin HTTP 处理器层，负责请求绑定与响应，
// 业务逻辑委托给 service 层。
package handler

import (
	"log/slog"

	"lens-backend/internal/config"
	cosclient "lens-backend/internal/cos"
	"lens-backend/internal/repository"
	"lens-backend/internal/service"
)

// Handler 聚合全部依赖，提供路由处理方法。
type Handler struct {
	upload   *service.UploadService
	feedback *service.FeedbackService
	repo     *repository.Repository
	cos      *cosclient.Client
	cfg      *config.Config
	logger   *slog.Logger
}

// New 构造 Handler。
func New(
	upload *service.UploadService,
	feedback *service.FeedbackService,
	repo *repository.Repository,
	cos *cosclient.Client,
	cfg *config.Config,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		upload:   upload,
		feedback: feedback,
		repo:     repo,
		cos:      cos,
		cfg:      cfg,
		logger:   logger,
	}
}
