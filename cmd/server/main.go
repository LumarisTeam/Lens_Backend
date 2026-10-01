// cmd/server 是服务入口：加载配置、建立连接池、执行迁移、组装路由，
// 并支持 HTTP Server 与定时任务随信号优雅退出。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"lens-backend/internal/config"
	cosclient "lens-backend/internal/cos"
	"lens-backend/internal/feedbackcache"
	"lens-backend/internal/handler"
	"lens-backend/internal/middleware"
	"lens-backend/internal/pkg/secretbox"
	"lens-backend/internal/repository"
	"lens-backend/internal/service"
	"lens-backend/migrations"
)

const (
	shutdownTimeout  = 10 * time.Second
	migrationLockKey = int64(7452013101) // 迁移 advisory lock：多实例并发启动时串行化执行
)

// runMigrations 将 schema 按分号拆分后逐条执行（本项目的 schema 语句内不包含分号）。
// 先获取会话级 advisory lock：多实例同时启动时仅一个实例执行迁移，其余等待，
// 避免 IF NOT EXISTS 下并发执行引发的潜在竞争。
func runMigrations(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockKey); err != nil {
		return err
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", migrationLockKey)
	}()

	for _, stmt := range strings.Split(schema, ";") {
		if s := strings.TrimSpace(stmt); s != "" {
			if _, err := conn.Exec(ctx, s); err != nil {
				return err
			}
		}
	}
	return nil
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config load failed", "error", err.Error())
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 数据库连接池
	pool, err := pgxpool.New(ctx, cfg.DBDSN)
	if err != nil {
		logger.Error("pgxpool create failed", "error", err.Error())
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		logger.Error("db ping failed", "error", err.Error())
		os.Exit(1)
	}

	// 启动时幂等执行 SQL 迁移（全部语句 IF NOT EXISTS）。
	// pgx 扩展协议单次 Exec 仅支持一条语句，故按分号拆分逐条执行。
	if err := runMigrations(ctx, pool, migrations.Schema); err != nil {
		logger.Error("migration failed", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("migration applied")

	repo := repository.New(pool)

	// Redis：反馈中心缓存、校验码、防重放、限流与分布式锁。
	cache := feedbackcache.New(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	defer func() { _ = cache.Close() }()
	if err := cache.Ping(ctx); err != nil {
		logger.Error("redis ping failed", "error", err.Error())
		os.Exit(1)
	}

	secretCipher, err := secretbox.New(cfg.FeedbackSecretKey)
	if err != nil {
		logger.Error("feedback secret cipher init failed", "error", err.Error())
		os.Exit(1)
	}

	cosCli, err := cosclient.New(cfg.StorageBucketURL, cfg.StorageAccessKey, cfg.StorageSecretKey)
	if err != nil {
		logger.Error("cos client create failed", "error", err.Error())
		os.Exit(1)
	}

	rateLimiter := service.NewRateLimiter(ctx, cfg.RateLimitRPS, cfg.RateLimitBurst)

	uploadSvc := service.NewUploadService(repo, cosCli, cfg)
	feedbackSvc := service.NewFeedbackService(repo, cfg)
	feedbackCenterSvc := service.NewFeedbackCenterService(repo, cache, secretCipher, cfg, logger)

	// 孤儿图片清理（随 ctx 退出）
	cleaner := service.NewOrphanCleaner(repo, cosCli, cfg.OrphanImageRetainHours, logger)
	go cleaner.Run(ctx)

	// 路由与中间件
	ginMode := gin.ReleaseMode
	if cfg.AppEnv == "development" {
		ginMode = gin.DebugMode
	}
	gin.SetMode(ginMode)

	router := gin.New()
	router.Use(
		middleware.RequestID(),
		middleware.Recovery(logger),
		middleware.Logger(logger),
		middleware.BodyLimit(middleware.MaxRequestBodyBytes),
	)
	// 请求超时：DB/COS 操作均派生自请求 ctx，超时后立即取消（0 表示禁用）
	if cfg.HTTPTimeoutSeconds > 0 {
		router.Use(middleware.Timeout(time.Duration(cfg.HTTPTimeoutSeconds) * time.Second))
	}
	// 可选 CORS（为空时不输出任何 CORS 头）
	router.Use(middleware.CORS(cfg.CORSAllowedOrigins))
	if err := router.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		logger.Error("set trusted proxies failed", "error", err.Error())
		os.Exit(1)
	}

	h := handler.New(uploadSvc, feedbackSvc, repo, cosCli, cfg, logger, feedbackCenterSvc)

	// 健康检查：独立于 /api/v1 前缀，供负载均衡 / k8s 探活。
	router.GET("/healthz", h.Health)

	api := router.Group("/api/v1")
	{
		// 客户端接口：匿名身份 + 进程内限流
		clientGroup := api.Group("")
		clientGroup.Use(middleware.ClientID(), middleware.RateLimit(rateLimiter))
		{
			clientGroup.POST("/uploads/presign", h.Presign)
			clientGroup.POST("/uploads/confirm", h.Confirm)
			clientGroup.POST("/feedback-centers/:center_id/codes", h.IssueFeedbackCode)
			clientGroup.POST("/feedbacks", middleware.FeedbackCodeAuth(feedbackCenterSvc), h.SubmitFeedback)
			// PRD 使用单数路径；保留现有复数路径兼容 SDK 历史版本。
			clientGroup.POST("/feedback", middleware.FeedbackCodeAuth(feedbackCenterSvc), h.SubmitFeedback)
		}

		// 管理员接口：独立限流（防 token 爆破/误用拖垮服务，维度为来源 IP）+ Bearer Token 鉴权
		adminLimiter := service.NewRateLimiter(ctx, cfg.AdminRateLimitRPS, cfg.AdminRateLimitBurst)
		adminGroup := api.Group("/admin")
		adminGroup.Use(middleware.RateLimit(adminLimiter), middleware.AdminAuth(cfg.AdminAPIToken))
		{
			adminGroup.GET("/feedbacks", h.ListFeedbacks)
			adminGroup.GET("/feedbacks/:feedback_no", h.GetFeedback)
		}

		registerFeedbackCenterRoutes(adminGroup, h)

		// PRD 中管理接口使用 /api/admin；同时提供 /api/v1/admin 版本以兼容现有 API 前缀。
		adminAlias := router.Group("/api/admin")
		adminAlias.Use(middleware.RateLimit(adminLimiter), middleware.AdminAuth(cfg.AdminAPIToken))
		registerFeedbackCenterRoutes(adminAlias, h)
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	// 读/写超时跟随请求超时（+10s 缓冲），同时防 slowloris 类慢速攻击
	if cfg.HTTPTimeoutSeconds > 0 {
		t := time.Duration(cfg.HTTPTimeoutSeconds+10) * time.Second
		srv.ReadTimeout = t
		srv.WriteTimeout = t
	}

	go func() {
		logger.Info("server starting", "addr", cfg.HTTPAddr, "env", cfg.AppEnv)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err.Error())
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown failed", "error", err.Error())
	}
	logger.Info("server stopped")
}

func registerFeedbackCenterRoutes(group *gin.RouterGroup, h *handler.Handler) {
	group.POST("/feedback-centers", middleware.RequireAdminWrite(), h.CreateFeedbackCenter)
	group.GET("/feedback-centers", h.ListFeedbackCenters)
	group.GET("/feedback-centers/:center_id", h.GetFeedbackCenter)
	group.PUT("/feedback-centers/:center_id", middleware.RequireAdminWrite(), h.UpdateFeedbackCenter)
	group.PATCH("/feedback-centers/:center_id", middleware.RequireAdminWrite(), h.UpdateFeedbackCenter)
	group.PATCH("/feedback-centers/:center_id/status", middleware.RequireAdminWrite(), h.UpdateFeedbackCenterStatus)
	group.POST("/feedback-centers/:center_id/status", middleware.RequireAdminWrite(), h.UpdateFeedbackCenterStatus)
	group.POST("/feedback-centers/:center_id/enable", middleware.RequireAdminWrite(), h.EnableFeedbackCenter)
	group.POST("/feedback-centers/:center_id/disable", middleware.RequireAdminWrite(), h.DisableFeedbackCenter)
	group.POST("/feedback-centers/:center_id/secret/reset", middleware.RequireAdminWrite(), h.ResetFeedbackCenterSecret)
	group.POST("/feedback-centers/:center_id/reset-secret", middleware.RequireAdminWrite(), h.ResetFeedbackCenterSecret)
	group.POST("/feedback-centers/:center_id/codes/generate", middleware.RequireAdminWrite(), h.GenerateFeedbackCode)
	group.GET("/feedback-centers/:center_id/audit-logs", h.ListFeedbackCenterAudits)
	group.GET("/feedback-centers/:center_id/audits", h.ListFeedbackCenterAudits)
}
