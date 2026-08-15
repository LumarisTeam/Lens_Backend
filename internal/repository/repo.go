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
	ErrAttachmentNotFound = errors.New("attachment not found")
	ErrFeedbackNotFound   = errors.New("feedback not found")
	ErrRequestIDConflict  = errors.New("request_id conflict")
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
