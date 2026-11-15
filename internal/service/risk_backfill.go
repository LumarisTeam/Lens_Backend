package service

import (
	"context"
	"log/slog"
	"time"

	"lens-backend/internal/repository"
	"lens-backend/internal/risk"
)

const riskHashBackfillBatchSize = 500

// BackfillFeedbackContentHashes 为升级前旧反馈补算规范化文本哈希。
// 分批执行并随服务 ctx 退出；单批失败只记录日志，不影响请求链路。
func BackfillFeedbackContentHashes(
	ctx context.Context,
	repo *repository.Repository,
	logger *slog.Logger,
) {
	total := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		items, err := repo.ListFeedbackMissingContentHash(ctx, riskHashBackfillBatchSize)
		if err != nil {
			logger.Warn("list feedback for hash backfill failed", "error", err)
			return
		}
		if len(items) == 0 {
			if total > 0 {
				logger.Info("feedback content hash backfill completed", "updated", total)
			}
			return
		}
		for _, item := range items {
			if err := repo.UpdateFeedbackContentHash(ctx, item.ID, risk.ContentHash(item.Content)); err != nil {
				logger.Warn("update feedback content hash failed", "feedback_id", item.ID, "error", err)
				return
			}
		}
		total += len(items)
		if len(items) < riskHashBackfillBatchSize {
			logger.Info("feedback content hash backfill completed", "updated", total)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}
