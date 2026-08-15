package service

import (
	"context"
	"log/slog"
	"time"

	cosclient "lens-backend/internal/cos"
	"lens-backend/internal/repository"
)

const (
	orphanCleanInterval = 10 * time.Minute
	orphanBatchSize     = 100
)

// OrphanCleaner 定期清理孤儿图片：先批量删除 COS 对象，成功后物理删除 DB 记录。
type OrphanCleaner struct {
	repo        *repository.Repository
	cos         *cosclient.Client
	retainHours int
	logger      *slog.Logger
}

// NewOrphanCleaner 构造 OrphanCleaner。
func NewOrphanCleaner(repo *repository.Repository, cos *cosclient.Client, retainHours int, logger *slog.Logger) *OrphanCleaner {
	return &OrphanCleaner{repo: repo, cos: cos, retainHours: retainHours, logger: logger}
}

// Run 启动清理循环，随 ctx 取消优雅退出。
func (c *OrphanCleaner) Run(ctx context.Context) {
	ticker := time.NewTicker(orphanCleanInterval)
	defer ticker.Stop()
	c.logger.Info("orphan cleaner started", "interval", orphanCleanInterval.String())
	for {
		select {
		case <-ctx.Done():
			c.logger.Info("orphan cleaner stopped")
			return
		case <-ticker.C:
			c.clean(ctx)
		}
	}
}

// clean 执行一轮清理。
func (c *OrphanCleaner) clean(ctx context.Context) {
	orphans, err := c.repo.ListOrphans(ctx, c.retainHours, orphanBatchSize)
	if err != nil {
		c.logger.Error("orphan cleaner: list failed", "error", err)
		return
	}
	if len(orphans) == 0 {
		return
	}

	// 循环调用 COS 删除接口，删除成功后才物理 DELETE 对应 DB 记录。
	deletedIDs := make([]int64, 0, len(orphans))
	for _, o := range orphans {
		if err := c.cos.DeleteObject(o.FileKey); err != nil {
			c.logger.Warn("orphan cleaner: cos delete failed", "file_key", o.FileKey, "error", err)
			continue
		}
		deletedIDs = append(deletedIDs, o.ID)
	}

	if len(deletedIDs) == 0 {
		return
	}
	if err := c.repo.DeleteAttachmentsByIDs(ctx, deletedIDs); err != nil {
		c.logger.Error("orphan cleaner: db delete failed", "error", err, "count", len(deletedIDs))
		return
	}
	c.logger.Info("orphan cleaner: cleaned", "count", len(deletedIDs))
}
