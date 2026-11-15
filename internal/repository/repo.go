// Package repository 是基于 pgx/v5（pgxpool）的数据访问层，禁止使用 ORM。
// 所有 SQL 均参数化，杜绝拼接注入。
package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 仓储层哨兵错误，供 service 层翻译为统一业务错误。
var (
	ErrAttachmentNotFound     = errors.New("attachment not found")
	ErrFeedbackNotFound       = errors.New("feedback not found")
	ErrRequestIDConflict      = errors.New("request_id conflict")
	ErrFeedbackCenterNotFound = errors.New("feedback center not found")
	ErrSecretVersionConflict  = errors.New("feedback center secret version conflict")
)

// Repository 持有连接池，提供全部数据访问方法。
type Repository struct {
	pool *pgxpool.Pool
}

// New 构造 Repository。
func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Begin 开启事务。
func (r *Repository) Begin(ctx context.Context) (pgx.Tx, error) {
	return r.pool.Begin(ctx)
}

// Ping 探测数据库连通性（健康检查用）。
func (r *Repository) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

// TryAdvisoryLock 非阻塞尝试获取会话级 advisory lock；成功后在持有专用连接的期间执行 fn，
// 连接释放时锁自动释放。返回是否成功获取。用于多实例部署下互斥（如孤儿清理）。
func (r *Repository) TryAdvisoryLock(ctx context.Context, key int64, fn func() error) (bool, error) {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Release()

	var ok bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&ok); err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	return true, fn()
}
