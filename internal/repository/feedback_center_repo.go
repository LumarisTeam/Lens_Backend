package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"lens-backend/internal/model"
)

const feedbackCenterColumns = `
	id, center_id, name, app_id, env, secret_cipher, secret_version,
	sn_mode, status, expire_at, contact, remark, created_by,
	last_used_at, created_at, updated_at`

// CreateFeedbackCenter 在同一事务内创建中心、SN 白名单和审计日志。
func (r *Repository) CreateFeedbackCenter(
	ctx context.Context,
	center *model.FeedbackCenter,
	sns []model.FeedbackCenterSN,
	audit *model.FeedbackCenterAudit,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const q = `INSERT INTO feedback_center
		(center_id, name, app_id, env, secret_cipher, secret_version,
		 sn_mode, status, expire_at, contact, remark, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, created_at, updated_at`
	err = tx.QueryRow(ctx, q,
		center.CenterID, center.Name, center.AppID, center.Env, center.SecretCipher,
		center.SecretVersion, center.SNMode, center.Status, center.ExpireAt,
		center.Contact, center.Remark, center.CreatedBy,
	).Scan(&center.ID, &center.CreatedAt, &center.UpdatedAt)
	if err != nil {
		return err
	}
	if err := insertFeedbackCenterSNsTx(ctx, tx, center.CenterID, sns); err != nil {
		return err
	}
	if err := insertFeedbackCenterAuditTx(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// GetFeedbackCenter 按 center_id 查询反馈中心。
func (r *Repository) GetFeedbackCenter(ctx context.Context, centerID string) (*model.FeedbackCenter, error) {
	q := `SELECT ` + feedbackCenterColumns + ` FROM feedback_center WHERE center_id = $1`
	center := &model.FeedbackCenter{}
	err := scanFeedbackCenter(r.pool.QueryRow(ctx, q, centerID), center)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrFeedbackCenterNotFound
	}
	if err != nil {
		return nil, err
	}
	return center, nil
}

// ListFeedbackCenters 分页查询反馈中心。
func (r *Repository) ListFeedbackCenters(
	ctx context.Context,
	p model.ListFeedbackCentersParams,
) ([]model.FeedbackCenterListItem, int64, error) {
	const where = `
		WHERE ($1 = '' OR center_id ILIKE '%' || $1 || '%'
			OR name ILIKE '%' || $1 || '%'
			OR app_id ILIKE '%' || $1 || '%')
		  AND ($2 = '' OR status = $2)`

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM feedback_center `+where, p.Keyword, p.Status).Scan(&total); err != nil {
		return nil, 0, err
	}

	q := `SELECT center_id, name, app_id, env, sn_mode, status, secret_version,
			expire_at, contact, last_used_at, created_at, updated_at
	      FROM feedback_center ` + where + `
	      ORDER BY created_at DESC
	      LIMIT $3 OFFSET $4`
	rows, err := r.pool.Query(ctx, q, p.Keyword, p.Status, p.Limit, p.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]model.FeedbackCenterListItem, 0, p.Limit)
	for rows.Next() {
		var it model.FeedbackCenterListItem
		if err := rows.Scan(
			&it.CenterID, &it.Name, &it.AppID, &it.Env, &it.SNMode, &it.Status,
			&it.SecretVersion, &it.ExpireAt, &it.Contact, &it.LastUsedAt,
			&it.CreatedAt, &it.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		items = append(items, it)
	}
	return items, total, rows.Err()
}

// ListFeedbackCenterSNs 返回中心下的全部 SN。
func (r *Repository) ListFeedbackCenterSNs(ctx context.Context, centerID string) ([]model.FeedbackCenterSN, error) {
	const q = `SELECT id, center_id, sn, status, expire_at, created_at
	           FROM feedback_center_sn
	           WHERE center_id = $1
	           ORDER BY id ASC`
	rows, err := r.pool.Query(ctx, q, centerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]model.FeedbackCenterSN, 0)
	for rows.Next() {
		var sn model.FeedbackCenterSN
		if err := rows.Scan(&sn.ID, &sn.CenterID, &sn.SN, &sn.Status, &sn.ExpireAt, &sn.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, sn)
	}
	return out, rows.Err()
}

// UpdateFeedbackCenter 更新中心配置，并以 replaceSNs 控制是否重建 SN 列表。
func (r *Repository) UpdateFeedbackCenter(
	ctx context.Context,
	center *model.FeedbackCenter,
	sns []model.FeedbackCenterSN,
	replaceSNs bool,
	audit *model.FeedbackCenterAudit,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const q = `UPDATE feedback_center
	           SET name = $2,
	               app_id = $3,
	               env = $4,
	               sn_mode = $5,
	               expire_at = $6,
	               contact = $7,
	               remark = $8,
	               updated_at = now()
	           WHERE center_id = $1`
	ct, err := tx.Exec(ctx, q,
		center.CenterID, center.Name, center.AppID, center.Env, center.SNMode,
		center.ExpireAt, center.Contact, center.Remark,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrFeedbackCenterNotFound
	}

	if replaceSNs {
		if _, err := tx.Exec(ctx, `DELETE FROM feedback_center_sn WHERE center_id = $1`, center.CenterID); err != nil {
			return err
		}
		if err := insertFeedbackCenterSNsTx(ctx, tx, center.CenterID, sns); err != nil {
			return err
		}
	}
	if err := insertFeedbackCenterAuditTx(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// UpdateFeedbackCenterStatus 更新启停状态并记录审计。
func (r *Repository) UpdateFeedbackCenterStatus(
	ctx context.Context,
	centerID, status string,
	audit *model.FeedbackCenterAudit,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	ct, err := tx.Exec(ctx,
		`UPDATE feedback_center SET status = $2, updated_at = now() WHERE center_id = $1`,
		centerID, status,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrFeedbackCenterNotFound
	}
	if err := insertFeedbackCenterAuditTx(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ResetFeedbackCenterSecret 以旧版本做 CAS 更新密钥，防止并发重置互相覆盖。
func (r *Repository) ResetFeedbackCenterSecret(
	ctx context.Context,
	centerID, secretCipher string,
	oldVersion int,
	audit *model.FeedbackCenterAudit,
) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const q = `UPDATE feedback_center
	           SET secret_cipher = $3,
	               secret_version = secret_version + 1,
	               updated_at = now()
	           WHERE center_id = $1 AND secret_version = $2
	           RETURNING secret_version`
	var newVersion int
	err = tx.QueryRow(ctx, q, centerID, oldVersion, secretCipher).Scan(&newVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrSecretVersionConflict
	}
	if err != nil {
		return 0, err
	}
	if err := insertFeedbackCenterAuditTx(ctx, tx, audit); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return newVersion, nil
}

// ListFeedbackCenterAudits 分页查询审计日志。
func (r *Repository) ListFeedbackCenterAudits(
	ctx context.Context,
	centerID string,
	limit, offset int,
) ([]model.FeedbackCenterAuditListItem, int64, error) {
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM feedback_center_audit WHERE center_id = $1`,
		centerID,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	const q = `SELECT id, center_id, action, operator, detail, ip, created_at
	           FROM feedback_center_audit
	           WHERE center_id = $1
	           ORDER BY created_at DESC, id DESC
	           LIMIT $2 OFFSET $3`
	rows, err := r.pool.Query(ctx, q, centerID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]model.FeedbackCenterAuditListItem, 0, limit)
	for rows.Next() {
		var item model.FeedbackCenterAuditListItem
		var detail []byte
		if err := rows.Scan(
			&item.ID, &item.CenterID, &item.Action, &item.Operator,
			&detail, &item.IP, &item.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		item.Detail = detail
		items = append(items, item)
	}
	return items, total, rows.Err()
}

// CreateFeedbackCenterAudit 单独写入审计日志，用于生成测试校验码等非事务操作。
func (r *Repository) CreateFeedbackCenterAudit(ctx context.Context, audit *model.FeedbackCenterAudit) error {
	if audit == nil {
		return nil
	}
	detail := audit.Detail
	if len(detail) == 0 {
		detail = json.RawMessage("{}")
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO feedback_center_audit (center_id, action, operator, detail, ip)
		 VALUES ($1, $2, $3, $4::jsonb, $5)`,
		audit.CenterID, audit.Action, audit.Operator, detail, audit.IP,
	)
	return err
}

// TouchFeedbackCenterLastUsed 更新最近调用时间。
func (r *Repository) TouchFeedbackCenterLastUsed(ctx context.Context, centerID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE feedback_center SET last_used_at = now() WHERE center_id = $1`,
		centerID,
	)
	return err
}

func insertFeedbackCenterSNsTx(ctx context.Context, tx pgx.Tx, centerID string, sns []model.FeedbackCenterSN) error {
	const q = `INSERT INTO feedback_center_sn (center_id, sn, status, expire_at)
	           VALUES ($1, $2, $3, $4)
	           ON CONFLICT (center_id, sn) DO NOTHING`
	for _, sn := range sns {
		if _, err := tx.Exec(ctx, q, centerID, sn.SN, sn.Status, sn.ExpireAt); err != nil {
			return err
		}
	}
	return nil
}

func insertFeedbackCenterAuditTx(ctx context.Context, tx pgx.Tx, audit *model.FeedbackCenterAudit) error {
	if audit == nil {
		return nil
	}
	detail := audit.Detail
	if len(detail) == 0 {
		detail = json.RawMessage("{}")
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO feedback_center_audit (center_id, action, operator, detail, ip)
		 VALUES ($1, $2, $3, $4::jsonb, $5)`,
		audit.CenterID, audit.Action, audit.Operator, detail, audit.IP,
	)
	return err
}

func scanFeedbackCenter(row pgx.Row, center *model.FeedbackCenter) error {
	return row.Scan(
		&center.ID, &center.CenterID, &center.Name, &center.AppID, &center.Env,
		&center.SecretCipher, &center.SecretVersion, &center.SNMode, &center.Status,
		&center.ExpireAt, &center.Contact, &center.Remark, &center.CreatedBy,
		&center.LastUsedAt, &center.CreatedAt, &center.UpdatedAt,
	)
}
