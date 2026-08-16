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

// orphanCleanLockKey 是孤儿清理的 advisory lock 键：多实例部署时同一时刻仅一个实例执行清理。
const orphanCleanLockKey int64 = 7452013102

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

// clean 执行一轮清理；通过会话级 advisory lock 保证多实例互斥。
func (c *OrphanCleaner) clean(ctx context.Context) {
	acquired, err := c.repo.TryAdvisoryLock(ctx, orphanCleanLockKey, func() error {
		return c.cleanLocked(ctx)
	})
	if err != nil {
		c.logger.Error("orphan cleaner: lock failed", "error", err)
		return
	}
	if !acquired {
		c.logger.Info("orphan cleaner: another instance is cleaning, skip")
	}
}

// cleanLocked 在持有锁的情况下执行一轮清理（批量删除 + 成功后才删 DB 记录）。
func (c *OrphanCleaner) cleanLocked(ctx context.Context) error {
	orphans, err := c.repo.ListOrphans(ctx, c.retainHours, orphanBatchSize)
	if err != nil {
		c.logger.Error("orphan cleaner: list failed", "error", err)
		return nil
	}
	if len(orphans) == 0 {
		return nil
	}

	keys := make([]string, 0, len(orphans))
	keyToID := make(map[string]int64, len(orphans))
	for _, o := range orphans {
		keys = append(keys, o.FileKey)
		keyToID[o.FileKey] = o.ID
	}

	// COS 批量删除（单次 ≤1000），仅对删除成功的对象删除 DB 记录；失败对象下一轮重试。
	_, failed, err := c.cos.DeleteObjects(ctx, keys)
	if err != nil {
		c.logger.Error("orphan cleaner: cos batch delete failed", "error", err, "count", len(keys))
		return nil
	}
	failedSet := make(map[string]bool, len(failed))
	for _, k := range failed {
		failedSet[k] = true
		c.logger.Warn("orphan cleaner: cos delete failed", "file_key", k)
	}

	deletedIDs := make([]int64, 0, len(orphans))
	for _, k := range keys {
		if !failedSet[k] {
			deletedIDs = append(deletedIDs, keyToID[k])
		}
	}
	if len(deletedIDs) == 0 {
		return nil
	}

	if err := c.repo.DeleteAttachmentsByIDs(ctx, deletedIDs); err != nil {
		c.logger.Error("orphan cleaner: db delete failed", "error", err, "count", len(deletedIDs))
		return nil
	}
	c.logger.Info("orphan cleaner: cleaned", "count", len(deletedIDs))
	return nil
}
