package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"lens-backend/internal/model"
)

// InsertUnconfirmedAttachment 预占位：插入一条 unconfirmed 附件记录，
// 确保后续 COS 中每一个文件都有 DB 记录兜底（防幽灵文件）。
func (r *Repository) InsertUnconfirmedAttachment(ctx context.Context, a *model.FeedbackAttachment) (int64, error) {
	const q = `INSERT INTO feedback_attachment (client_id, file_key, original_name, mime_type, status, size_bytes)
	           VALUES ($1, $2, $3, $4, 'unconfirmed', $5)
	           RETURNING id`
	var id int64
	err := r.pool.QueryRow(ctx, q, a.ClientID, a.FileKey, a.OriginalName, a.MimeType, a.SizeBytes).Scan(&id)
	return id, err
}

// GetAttachmentByFileKey 按 file_key 查询附件。
func (r *Repository) GetAttachmentByFileKey(ctx context.Context, fileKey string) (*model.FeedbackAttachment, error) {
	const q = `SELECT id, feedback_id, client_id, file_key, original_name, mime_type, status, size_bytes, created_at
	           FROM feedback_attachment WHERE file_key = $1`
	a := &model.FeedbackAttachment{}
	var feedbackID *int64
	err := r.pool.QueryRow(ctx, q, fileKey).Scan(
		&a.ID, &feedbackID, &a.ClientID, &a.FileKey, &a.OriginalName, &a.MimeType, &a.Status, &a.SizeBytes, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAttachmentNotFound
	}
	if err != nil {
		return nil, err
	}
	a.FeedbackID = feedbackID
	return a, nil
}

// UpdateAttachmentStatus 更新附件状态（unconfirmed/confirmed/invalid）。
func (r *Repository) UpdateAttachmentStatus(ctx context.Context, id int64, status string) error {
	_, err := r.pool.Exec(ctx, `UPDATE feedback_attachment SET status = $1 WHERE id = $2`, status, id)
	return err
}

// BindAttachmentTx 在事务内将附件绑定到 feedback。
// 仅当附件属于该 client、状态为 confirmed 且尚未绑定其它 feedback 时成功。
// 返回是否绑定成功（false 表示归属/状态/已绑定冲突）。
func (r *Repository) BindAttachmentTx(ctx context.Context, tx pgx.Tx, clientID string, id, feedbackID int64) (bool, error) {
	ct, err := tx.Exec(ctx, `UPDATE feedback_attachment SET feedback_id = $1
	           WHERE id = $2 AND client_id = $3 AND status = 'confirmed' AND feedback_id IS NULL`,
		feedbackID, id, clientID)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() == 1, nil
}

// ListAttachmentsByFeedbackID 返回某个反馈下的已确认附件。
func (r *Repository) ListAttachmentsByFeedbackID(ctx context.Context, feedbackID int64) ([]model.FeedbackAttachment, error) {
	const q = `SELECT id, feedback_id, client_id, file_key, original_name, mime_type, status, size_bytes, created_at
	           FROM feedback_attachment WHERE feedback_id = $1 AND status = 'confirmed' ORDER BY id ASC`
	rows, err := r.pool.Query(ctx, q, feedbackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.FeedbackAttachment
	for rows.Next() {
		a := model.FeedbackAttachment{}
		var fid *int64
		if err := rows.Scan(&a.ID, &fid, &a.ClientID, &a.FileKey, &a.OriginalName, &a.MimeType, &a.Status, &a.SizeBytes, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.FeedbackID = fid
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListOrphans 查询孤儿附件（未绑定或未确认，且超过保留时长），单批最多 limit 条。
func (r *Repository) ListOrphans(ctx context.Context, retainHours, limit int) ([]model.OrphanAttachment, error) {
	const q = `SELECT id, file_key FROM feedback_attachment
	           WHERE (feedback_id IS NULL OR status != 'confirmed')
	             AND created_at < now() - make_interval(hours => $1)
	           ORDER BY created_at ASC
	           LIMIT $2`
	rows, err := r.pool.Query(ctx, q, retainHours, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.OrphanAttachment
	for rows.Next() {
		var o model.OrphanAttachment
		if err := rows.Scan(&o.ID, &o.FileKey); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// DeleteAttachmentsByIDs 物理删除附件记录。
func (r *Repository) DeleteAttachmentsByIDs(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx, `DELETE FROM feedback_attachment WHERE id = ANY($1)`, ids)
	return err
}
