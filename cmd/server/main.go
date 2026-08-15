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
	"lens-backend/internal/handler"
	"lens-backend/internal/middleware"
	"lens-backend/internal/repository"
	"lens-backend/internal/service"
	"lens-backend/migrations"
)

const shutdownTimeout = 10 * time.Second

// runMigrations 将 schema 按分号拆分后逐条执行（本项目的 schema 语句内不包含分号）。
func runMigrations(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	for _, stmt := range strings.Split(schema, ";") {
		if s := strings.TrimSpace(stmt); s != "" {
			if _, err := pool.Exec(ctx, s); err != nil {
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

	cosCli, err := cosclient.New(cfg.CosEndpoint, cfg.CosAccessKey, cfg.CosSecretKey)
	if err != nil {
		logger.Error("cos client create failed", "error", err.Error())
		os.Exit(1)
	}

	rateLimiter := service.NewRateLimiter(ctx, cfg.RateLimitRPS, cfg.RateLimitBurst)

	uploadSvc := service.NewUploadService(repo, cosCli, cfg)
	feedbackSvc := service.NewFeedbackService(repo, cfg)

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
	router.Use(middleware.Recovery(logger), middleware.Logger(logger), middleware.BodyLimit(middleware.MaxRequestBodyBytes))
	if err := router.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		logger.Error("set trusted proxies failed", "error", err.Error())
		os.Exit(1)
	}

	h := handler.New(uploadSvc, feedbackSvc, repo, cosCli, cfg, logger)

	api := router.Group("/api/v1")
	{
		api.GET("/healthz", h.Health)

		// 客户端接口：匿名身份 + 进程内限流
		clientGroup := api.Group("")
		clientGroup.Use(middleware.ClientID(), middleware.RateLimit(rateLimiter))
		{
			clientGroup.POST("/uploads/presign", h.Presign)
			clientGroup.POST("/uploads/confirm", h.Confirm)
			clientGroup.POST("/feedbacks", h.SubmitFeedback)
		}

		// 管理员接口：Bearer Token 鉴权
		adminGroup := api.Group("/admin")
		adminGroup.Use(middleware.AdminAuth(cfg.AdminAPIToken))
		{
			adminGroup.GET("/feedbacks", h.ListFeedbacks)
			adminGroup.GET("/feedbacks/:feedback_no", h.GetFeedback)
		}
	}

	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: router,
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
