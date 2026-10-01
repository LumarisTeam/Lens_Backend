package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"lens-backend/internal/model"
)

// InsertFeedbackTx 在事务内插入反馈，request_id 冲突时返回 ErrRequestIDConflict。
// 使用 ON CONFLICT (request_id) DO NOTHING + RETURNING 处理并发重复提交。
func (r *Repository) InsertFeedbackTx(ctx context.Context, tx pgx.Tx, f *model.Feedback) (int64, error) {
	const q = `INSERT INTO feedback (feedback_no, request_id, client_id, user_id, content, contact, image_count, extra)
	           VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)
	           ON CONFLICT (request_id) DO NOTHING
	           RETURNING id`
	var id int64
	err := tx.QueryRow(ctx, q, f.FeedbackNo, f.RequestID, f.ClientID, f.UserID, f.Content, f.Contact, f.ImageCount, f.Extra).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrRequestIDConflict
	}
	return id, err
}

// GetFeedbackByRequestID 按 request_id 返回首次成功的 feedback_no 与 client_id（幂等回查）。
func (r *Repository) GetFeedbackByRequestID(ctx context.Context, requestID string) (feedbackNo, clientID string, err error) {
	const q = `SELECT feedback_no, client_id FROM feedback WHERE request_id = $1`
	err = r.pool.QueryRow(ctx, q, requestID).Scan(&feedbackNo, &clientID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrFeedbackNotFound
	}
	return feedbackNo, clientID, err
}

// GetFeedbackByRequestIDTx 在事务内按 request_id 回查，用于插入前幂等判断。
func (r *Repository) GetFeedbackByRequestIDTx(
	ctx context.Context,
	tx pgx.Tx,
	requestID string,
) (feedbackNo, clientID string, err error) {
	const q = `SELECT feedback_no, client_id FROM feedback WHERE request_id = $1`
	err = tx.QueryRow(ctx, q, requestID).Scan(&feedbackNo, &clientID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrFeedbackNotFound
	}
	return feedbackNo, clientID, err
}

// CountFeedbackByClientLast24hTx 在事务内统计 client 近 24 小时的成功反馈数（日上限）。
// 命中 idx_feedback_client_time 索引。
// 配合事务级 advisory lock（pg_advisory_xact_lock）保证 count 与 insert 之间无并发窗口。
func (r *Repository) CountFeedbackByClientLast24hTx(ctx context.Context, tx pgx.Tx, clientID string) (int64, error) {
	const q = `SELECT count(*) FROM feedback WHERE client_id = $1 AND created_at > now() - interval '24 hours'`
	var n int64
	err := tx.QueryRow(ctx, q, clientID).Scan(&n)
	return n, err
}

// GetFeedbackByNo 按 feedback_no 查询反馈。
func (r *Repository) GetFeedbackByNo(ctx context.Context, feedbackNo string) (*model.Feedback, error) {
	const q = `SELECT id, feedback_no, request_id, client_id, user_id, content, contact, image_count, extra, created_at
	           FROM feedback WHERE feedback_no = $1`
	f := &model.Feedback{}
	var userID *int64
	var extra []byte
	err := r.pool.QueryRow(ctx, q, feedbackNo).Scan(
		&f.ID, &f.FeedbackNo, &f.RequestID, &f.ClientID, &userID, &f.Content, &f.Contact, &f.ImageCount, &extra, &f.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrFeedbackNotFound
	}
	if err != nil {
		return nil, err
	}
	f.UserID = userID
	f.Extra = extra
	return f, nil
}

// feedbackWhere 是列表查询复用的过滤条件。
// $1=contact 模糊、$2=app_name（extra->>'app_name'）、$3/$4=UTC 时间范围。
const feedbackWhere = `
	WHERE ($1 = '' OR contact ILIKE '%' || $1 || '%')
	  AND ($2 = '' OR extra ->> 'app_name' = $2)
	  AND ($3::timestamptz IS NULL OR created_at >= $3)
	  AND ($4::timestamptz IS NULL OR created_at < $4)`

// CountFeedbacks 统计满足过滤条件的反馈总数。
func (r *Repository) CountFeedbacks(ctx context.Context, p model.ListFeedbacksParams) (int64, error) {
	q := `SELECT count(*) FROM feedback ` + feedbackWhere
	var n int64
	err := r.pool.QueryRow(ctx, q, p.Contact, p.AppName, p.StartTime, p.EndTime).Scan(&n)
	return n, err
}

// ListFeedbacks 分页查询反馈列表（按 created_at 倒序）。
func (r *Repository) ListFeedbacks(ctx context.Context, p model.ListFeedbacksParams) ([]model.FeedbackListItem, error) {
	q := `SELECT feedback_no, client_id, content, contact, image_count, extra, created_at
	      FROM feedback ` + feedbackWhere + `
	      ORDER BY created_at DESC
	      LIMIT $5 OFFSET $6`
	rows, err := r.pool.Query(ctx, q, p.Contact, p.AppName, p.StartTime, p.EndTime, p.Limit, p.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.FeedbackListItem
	for rows.Next() {
		var it model.FeedbackListItem
		var extra []byte
		if err := rows.Scan(&it.FeedbackNo, &it.ClientID, &it.Content, &it.Contact, &it.ImageCount, &extra, &it.CreatedAt); err != nil {
			return nil, err
		}
		it.Extra = extra
		out = append(out, it)
	}
	return out, rows.Err()
}
