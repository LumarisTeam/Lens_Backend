package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"lens-backend/internal/model"
)

// RiskSnapshot 是文本、频率与信誉规则所需的数据库聚合特征。
type RiskSnapshot struct {
	IPFeedback24h     int64
	ClientFeedback24h int64
	IPFeedback10m     int64
	ClientFeedback10m int64
	ExactDuplicate24h int64
	IPImages24h       int64
	IPBlockedEvents7d int64
	RecentContents    []string
}

// FeedbackContent 是哈希回填所需的最小反馈字段。
type FeedbackContent struct {
	ID      int64
	Content string
}

// ListFeedbackMissingContentHash 分批查询尚未回填哈希的历史反馈。
func (r *Repository) ListFeedbackMissingContentHash(
	ctx context.Context,
	limit int,
) ([]FeedbackContent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, content FROM feedback
		WHERE content_hash = ''
		ORDER BY id ASC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]FeedbackContent, 0, limit)
	for rows.Next() {
		var item FeedbackContent
		if err := rows.Scan(&item.ID, &item.Content); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// UpdateFeedbackContentHash 写入历史反馈的规范化文本哈希。
func (r *Repository) UpdateFeedbackContentHash(
	ctx context.Context,
	id int64,
	hash string,
) error {
	_, err := r.pool.Exec(ctx, `UPDATE feedback SET content_hash = $2 WHERE id = $1`, id, hash)
	return err
}

type riskQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// GetRiskSnapshot 在事务外读取风险评估特征。
func (r *Repository) GetRiskSnapshot(
	ctx context.Context,
	clientIP, clientID, contentHash string,
) (RiskSnapshot, error) {
	return getRiskSnapshot(ctx, r.pool, clientIP, clientID, contentHash)
}

// GetRiskSnapshotTx 在事务内读取风险评估特征。
func (r *Repository) GetRiskSnapshotTx(
	ctx context.Context,
	tx pgx.Tx,
	clientIP, clientID, contentHash string,
) (RiskSnapshot, error) {
	return getRiskSnapshot(ctx, tx, clientIP, clientID, contentHash)
}

func getRiskSnapshot(
	ctx context.Context,
	q riskQuerier,
	clientIP, clientID, contentHash string,
) (RiskSnapshot, error) {
	const aggregate = `
		SELECT
		  (SELECT count(*) FROM feedback WHERE client_ip = $1 AND created_at > now() - interval '24 hours'),
		  (SELECT count(*) FROM feedback WHERE client_id = $2 AND created_at > now() - interval '24 hours'),
		  (SELECT count(*) FROM feedback WHERE client_ip = $1 AND created_at > now() - interval '10 minutes'),
		  (SELECT count(*) FROM feedback WHERE client_id = $2 AND created_at > now() - interval '10 minutes'),
		  (SELECT count(*) FROM feedback WHERE content_hash = $3 AND created_at > now() - interval '24 hours'),
		  (SELECT count(*) FROM feedback_attachment WHERE client_ip = $1 AND created_at > now() - interval '24 hours'),
		  (SELECT count(*) FROM risk_event
		   WHERE client_ip = $1
		     AND (action = 'block' OR (action = 'manual' AND detail ->> 'new_status' = 'blocked'))
		     AND created_at > now() - interval '7 days')`
	var out RiskSnapshot
	if err := q.QueryRow(ctx, aggregate, clientIP, clientID, contentHash).Scan(
		&out.IPFeedback24h,
		&out.ClientFeedback24h,
		&out.IPFeedback10m,
		&out.ClientFeedback10m,
		&out.ExactDuplicate24h,
		&out.IPImages24h,
		&out.IPBlockedEvents7d,
	); err != nil {
		return out, err
	}

	const recent = `
		SELECT content FROM feedback
		WHERE (client_ip = $1 OR client_id = $2)
		  AND created_at > now() - interval '24 hours'
		  AND review_status != 'blocked'
		ORDER BY created_at DESC
		LIMIT 50`
	rows, err := q.Query(ctx, recent, clientIP, clientID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			return out, err
		}
		out.RecentContents = append(out.RecentContents, content)
	}
	return out, rows.Err()
}

// CountFeedbackByIPLast24hTx 统计单 IP 近 24 小时提交数。
func (r *Repository) CountFeedbackByIPLast24hTx(ctx context.Context, tx pgx.Tx, clientIP string) (int64, error) {
	const q = `SELECT count(*) FROM feedback
	           WHERE client_ip = $1 AND created_at > now() - interval '24 hours'`
	var n int64
	err := tx.QueryRow(ctx, q, clientIP).Scan(&n)
	return n, err
}

// InsertRiskEventTx 在事务内写入风控事件。
func (r *Repository) InsertRiskEventTx(ctx context.Context, tx pgx.Tx, event *model.RiskEvent) error {
	if event == nil {
		return nil
	}
	reasons := defaultJSON(event.RiskReasons, "[]")
	detail := defaultJSON(event.Detail, "{}")
	_, err := tx.Exec(ctx, `
		INSERT INTO risk_event
		  (feedback_id, center_id, client_id, client_ip, event_type, action,
		   risk_score, risk_reasons, detail, operator)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10)`,
		event.FeedbackID,
		event.CenterID,
		event.ClientID,
		event.ClientIP,
		event.EventType,
		event.Action,
		event.RiskScore,
		reasons,
		detail,
		event.Operator,
	)
	return err
}

// InsertRiskEvent 写入独立风控事件（限流、配额等无 feedback 记录的场景）。
func (r *Repository) InsertRiskEvent(ctx context.Context, event *model.RiskEvent) error {
	reasons := defaultJSON(event.RiskReasons, "[]")
	detail := defaultJSON(event.Detail, "{}")
	_, err := r.pool.Exec(ctx, `
		INSERT INTO risk_event
		  (feedback_id, center_id, client_id, client_ip, event_type, action,
		   risk_score, risk_reasons, detail, operator)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10)`,
		event.FeedbackID,
		event.CenterID,
		event.ClientID,
		event.ClientIP,
		event.EventType,
		event.Action,
		event.RiskScore,
		reasons,
		detail,
		event.Operator,
	)
	return err
}

// UpdateFeedbackReview 更新人工审核状态并写入一条 manual 风控事件。
func (r *Repository) UpdateFeedbackReview(
	ctx context.Context,
	feedbackNo, status, note, operator string,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const selectQ = `SELECT id, center_id, client_id, client_ip, risk_score, risk_reasons, review_status
	                 FROM feedback WHERE feedback_no = $1 FOR UPDATE`
	var (
		feedbackID int64
		centerID   string
		clientID   string
		clientIP   string
		riskScore  int
		reasons    []byte
		oldStatus  string
	)
	err = tx.QueryRow(ctx, selectQ, feedbackNo).Scan(
		&feedbackID, &centerID, &clientID, &clientIP, &riskScore, &reasons, &oldStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrFeedbackNotFound
	}
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE feedback
		SET review_status = $2, reviewed_by = $3, reviewed_at = now(),
		    review_note = $4, updated_at = now()
		WHERE feedback_no = $1`,
		feedbackNo, status, operator, note,
	); err != nil {
		return err
	}
	detail, _ := json.Marshal(map[string]any{
		"previous_status": oldStatus,
		"new_status":      status,
		"note":            note,
	})
	event := &model.RiskEvent{
		FeedbackID:  &feedbackID,
		CenterID:    centerID,
		ClientID:    clientID,
		ClientIP:    clientIP,
		EventType:   "manual_review",
		Action:      model.RiskActionManual,
		RiskScore:   riskScore,
		RiskReasons: reasons,
		Detail:      detail,
		Operator:    operator,
	}
	if err := r.InsertRiskEventTx(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ListRiskEvents 分页查询风控事件。
func (r *Repository) ListRiskEvents(
	ctx context.Context,
	p model.ListRiskEventsParams,
) ([]model.RiskEventListItem, int64, error) {
	const where = `
		WHERE ($1 = '' OR e.client_ip = $1)
		  AND ($2 = '' OR e.client_id = $2)
		  AND ($3 = '' OR e.action = $3)
		  AND ($4 = '' OR e.event_type = $4)
		  AND ($5 <= 0 OR e.risk_score >= $5)
		  AND ($6::timestamptz IS NULL OR e.created_at >= $6)
		  AND ($7::timestamptz IS NULL OR e.created_at < $7)
		  AND ($8 = '' OR e.center_id = $8)`
	var total int64
	if err := r.pool.QueryRow(
		ctx,
		`SELECT count(*) FROM risk_event e `+where,
		p.ClientIP, p.ClientID, p.Action, p.EventType, p.MinScore, p.StartTime, p.EndTime, p.CenterID,
	).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := `SELECT e.id, COALESCE(f.feedback_no, ''), e.center_id, e.client_id, e.client_ip,
	             e.event_type, e.action, e.risk_score, e.risk_reasons, e.detail, e.operator, e.created_at
	      FROM risk_event e
	      LEFT JOIN feedback f ON f.id = e.feedback_id ` + where + `
	      ORDER BY e.created_at DESC, e.id DESC
	      LIMIT $9 OFFSET $10`
	rows, err := r.pool.Query(
		ctx,
		q,
		p.ClientIP, p.ClientID, p.Action, p.EventType, p.MinScore, p.StartTime, p.EndTime, p.CenterID,
		p.Limit, p.Offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]model.RiskEventListItem, 0, p.Limit)
	for rows.Next() {
		var item model.RiskEventListItem
		var reasons, detail []byte
		if err := rows.Scan(
			&item.ID, &item.FeedbackNo, &item.CenterID, &item.ClientID, &item.ClientIP,
			&item.EventType, &item.Action, &item.RiskScore, &reasons, &detail,
			&item.Operator, &item.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		item.RiskReasons = reasons
		item.Detail = detail
		items = append(items, item)
	}
	return items, total, rows.Err()
}

// GetRiskStats 返回最近 days 天的风控趋势和来源 IP 排行。
func (r *Repository) GetRiskStats(
	ctx context.Context,
	days int,
	centerID ...string,
) (*model.RiskStatsResponse, error) {
	if days < 1 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	center := ""
	if len(centerID) > 0 {
		center = centerID[0]
	}
	out := &model.RiskStatsResponse{
		Days:   days,
		Daily:  make([]model.RiskTrendPoint, 0, days),
		TopIPs: make([]model.RiskIPStat, 0, 10),
	}
	rows, err := r.pool.Query(ctx, `
		SELECT to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD'),
		       count(*),
		       count(*) FILTER (
		         WHERE action = 'review'
		            OR (action = 'manual' AND detail ->> 'new_status' = 'review')
		       ),
		       count(*) FILTER (
		         WHERE action = 'block'
		            OR (action = 'manual' AND detail ->> 'new_status' = 'blocked')
		       ),
		       count(*) FILTER (WHERE action = 'challenge'),
		       count(*) FILTER (WHERE action = 'reject')
		FROM risk_event
		WHERE created_at >= now() - make_interval(days => $1)
		  AND ($2 = '' OR center_id = $2)
		GROUP BY 1
		ORDER BY 1 ASC`, days, center)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byDate := make(map[string]model.RiskTrendPoint, days)
	for rows.Next() {
		var p model.RiskTrendPoint
		if err := rows.Scan(&p.Date, &p.Total, &p.Review, &p.Blocked, &p.Challenged, &p.Rejected); err != nil {
			return nil, err
		}
		byDate[p.Date] = p
		out.Total += p.Total
		out.Review += p.Review
		out.Blocked += p.Blocked
		out.Challenged += p.Challenged
		out.Rejected += p.Rejected
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	for i := days - 1; i >= 0; i-- {
		date := now.AddDate(0, 0, -i).Format("2006-01-02")
		point, ok := byDate[date]
		if !ok {
			point = model.RiskTrendPoint{Date: date}
		}
		out.Daily = append(out.Daily, point)
	}

	ipRows, err := r.pool.Query(ctx, `
		SELECT client_ip, count(*) AS events,
		       count(*) FILTER (
		         WHERE action = 'block'
		            OR (action = 'manual' AND detail ->> 'new_status' = 'blocked')
		       ) AS blocked
		FROM risk_event
		WHERE created_at >= now() - make_interval(days => $1)
		  AND ($2 = '' OR center_id = $2)
		  AND client_ip != ''
		GROUP BY client_ip
		ORDER BY blocked DESC, events DESC
		LIMIT 10`, days, center)
	if err != nil {
		return nil, err
	}
	defer ipRows.Close()
	for ipRows.Next() {
		var stat model.RiskIPStat
		if err := ipRows.Scan(&stat.ClientIP, &stat.Events, &stat.Blocked); err != nil {
			return nil, err
		}
		out.TopIPs = append(out.TopIPs, stat)
	}
	return out, ipRows.Err()
}

func defaultJSON(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(fallback)
	}
	return raw
}
