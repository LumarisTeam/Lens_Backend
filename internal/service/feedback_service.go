package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"lens-backend/internal/config"
	"lens-backend/internal/model"
	"lens-backend/internal/pkg/apperr"
	"lens-backend/internal/pkg/idgen"
	"lens-backend/internal/repository"
	"lens-backend/internal/risk"
)

// FeedbackService 处理反馈提交业务逻辑。
type FeedbackService struct {
	repo    *repository.Repository
	cfg     *config.Config
	riskSvc *RiskService
}

// NewFeedbackService 构造 FeedbackService。
func NewFeedbackService(
	repo *repository.Repository,
	cfg *config.Config,
	riskSvc ...*RiskService,
) *FeedbackService {
	s := &FeedbackService{repo: repo, cfg: cfg}
	if len(riskSvc) > 0 {
		s.riskSvc = riskSvc[0]
	}
	return s
}

// Submit 提交反馈：参数校验 -> 风险预判/CAPTCHA -> 事务内幂等、配额、复评 -> 插入。
func (s *FeedbackService) Submit(
	ctx context.Context,
	clientID, centerID, appID, clientIP, captchaToken string,
	req model.SubmitFeedbackRequest,
) (*model.SubmitFeedbackResponse, error) {
	if req.RequestID == "" || len(req.RequestID) > 64 {
		return nil, apperr.New(http.StatusBadRequest, 40001, "request_id 参数非法")
	}
	if n := utf8.RuneCountInString(req.Content); n < 1 || n > s.cfg.ContentMaxLength {
		return nil, apperr.New(http.StatusBadRequest, 40001, "content 长度非法")
	}
	if n := utf8.RuneCountInString(req.Contact); n < 1 || n > s.cfg.ContactMaxLength {
		return nil, apperr.New(http.StatusBadRequest, 40001, "contact 长度非法")
	}
	if len(req.AttachmentIDs) > s.cfg.ImageMaxCount {
		return nil, apperr.New(http.StatusBadRequest, 40001, "附件数量超限")
	}

	// extra 校验：必须是 JSON 对象，按 PRD 的 8KB 字节上限校验；
	// app_name 以反馈中心注册的 appId 为准，忽略客户端传入值。
	extra, err := normalizeExtra(req.Extra, appID)
	if err != nil {
		return nil, err
	}

	// request_id 幂等回查必须先于风险挑战，避免网络重试被误判为新请求。
	no, existingClientID, err := s.repo.GetFeedbackByRequestID(ctx, req.RequestID)
	if err == nil {
		if existingClientID != clientID {
			return nil, apperr.New(http.StatusConflict, 40901, "重复提交")
		}
		return &model.SubmitFeedbackResponse{FeedbackNo: no}, nil
	}
	if !errors.Is(err, repository.ErrFeedbackNotFound) {
		return nil, err
	}

	clientIP = risk.ClientKey(clientIP)
	contentHash := risk.ContentHash(req.Content)
	captchaPassed := false
	if s.riskSvc != nil {
		snapshot, err := s.repo.GetRiskSnapshot(ctx, clientIP, clientID, contentHash)
		if err != nil {
			return nil, internalError(err)
		}
		assessment := s.riskSvc.Assess(req.Content, snapshot, false)
		if assessment.RequiresCaptcha && s.riskSvc.CaptchaEnabled() {
			if captchaToken == "" {
				s.riskSvc.RecordChallenge(ctx, centerID, clientID, clientIP, assessment)
				return nil, s.riskSvc.ChallengeError()
			}
			if err := s.riskSvc.VerifyCaptcha(ctx, centerID, clientID, clientIP, captchaToken); err != nil {
				return nil, err
			}
			captchaPassed = true
		}
	}

	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 同一 IP 与 Client 串行化；排序后加锁，避免不同请求的锁顺序造成死锁。
	if err := lockFeedbackRiskKeysTx(ctx, tx, clientID, clientIP); err != nil {
		return nil, err
	}

	// 先按 request_id 回查：同一 client 的重试直接回显，避免在首次提交刚好
	// 触达日上限时被 42901 拦截而拿不回原 feedback_no。
	no, existingClientID, err = s.repo.GetFeedbackByRequestIDTx(ctx, tx, req.RequestID)
	if err == nil {
		if existingClientID != clientID {
			return nil, apperr.New(http.StatusConflict, 40901, "重复提交")
		}
		return &model.SubmitFeedbackResponse{FeedbackNo: no}, nil
	}
	if !errors.Is(err, repository.ErrFeedbackNotFound) {
		return nil, err
	}

	// 日上限：同一 client_id 24 小时内成功创建的反馈数不超过上限（事务内统计）。
	count, err := s.repo.CountFeedbackByClientLast24hTx(ctx, tx, clientID)
	if err != nil {
		return nil, err
	}
	if count >= int64(s.cfg.FeedbackDailyLimit) {
		if s.riskSvc != nil {
			_ = tx.Rollback(ctx)
			s.riskSvc.RecordSubmissionQuotaRejection(
				ctx,
				centerID,
				clientID,
				clientIP,
				"client_daily_quota",
				map[string]any{"limit": s.cfg.FeedbackDailyLimit, "count": count},
			)
		}
		return nil, apperr.New(http.StatusTooManyRequests, 42901, "今日提交数量已达上限")
	}
	ipCount, err := s.repo.CountFeedbackByIPLast24hTx(ctx, tx, clientIP)
	if err != nil {
		return nil, err
	}
	if ipCount >= int64(s.cfg.IPDailyFeedbackLimit) {
		if s.riskSvc != nil {
			_ = tx.Rollback(ctx)
			s.riskSvc.RecordSubmissionQuotaRejection(
				ctx,
				centerID,
				clientID,
				clientIP,
				"ip_daily_quota",
				map[string]any{"limit": s.cfg.IPDailyFeedbackLimit, "count": ipCount},
			)
		}
		return nil, apperr.New(http.StatusTooManyRequests, 42901, "当前网络今日提交数量已达上限")
	}

	assessment := risk.Assessment{
		ReviewStatus: model.FeedbackReviewNormal,
		Action:       model.RiskActionAllow,
		Reasons:      []risk.Reason{},
	}
	if s.riskSvc != nil {
		snapshot, err := s.repo.GetRiskSnapshotTx(ctx, tx, clientIP, clientID, contentHash)
		if err != nil {
			return nil, err
		}
		assessment = s.riskSvc.Assess(req.Content, snapshot, captchaPassed)
		if assessment.RequiresCaptcha && !captchaPassed && s.riskSvc.CaptchaEnabled() {
			_ = tx.Rollback(ctx)
			s.riskSvc.RecordChallenge(ctx, centerID, clientID, clientIP, assessment)
			return nil, s.riskSvc.ChallengeError()
		}
	}
	reasons, err := json.Marshal(assessment.Reasons)
	if err != nil {
		return nil, err
	}

	f := &model.Feedback{
		FeedbackNo:      idgen.FeedbackNo(),
		RequestID:       req.RequestID,
		ClientID:        clientID,
		ClientIP:        clientIP,
		CenterID:        centerID,
		Content:         req.Content,
		ContentHash:     contentHash,
		Contact:         req.Contact,
		ImageCount:      int16(len(req.AttachmentIDs)),
		Extra:           extra,
		RiskScore:       assessment.Score,
		RiskReasons:     reasons,
		ReviewStatus:    assessment.ReviewStatus,
		CaptchaVerified: captchaPassed,
	}
	feedbackID, err := s.repo.InsertFeedbackTx(ctx, tx, f)
	if errors.Is(err, repository.ErrRequestIDConflict) {
		no, existingClientID, qerr := s.repo.GetFeedbackByRequestIDTx(ctx, tx, req.RequestID)
		if qerr != nil {
			return nil, qerr
		}
		// 幂等仅在属于同一 client 时回显，避免向其它 client 泄露 feedback_no。
		if existingClientID != clientID {
			return nil, apperr.New(http.StatusConflict, 40901, "重复提交")
		}
		return &model.SubmitFeedbackResponse{FeedbackNo: no}, nil
	}
	if err != nil {
		return nil, err
	}

	if assessment.Score > 0 || captchaPassed || assessment.ReviewStatus != model.FeedbackReviewNormal {
		detail, _ := json.Marshal(map[string]any{
			"center_id": centerID,
			"captcha":   captchaPassed,
		})
		if err := s.repo.InsertRiskEventTx(ctx, tx, &model.RiskEvent{
			FeedbackID:  &feedbackID,
			CenterID:    centerID,
			ClientID:    clientID,
			ClientIP:    clientIP,
			EventType:   "feedback_assessment",
			Action:      assessment.Action,
			RiskScore:   assessment.Score,
			RiskReasons: reasons,
			Detail:      detail,
		}); err != nil {
			return nil, err
		}
	}

	// 绑定附件：归属/状态/未绑定校验 + 绑定在同一事务内原子完成，任一失败则整体回滚。
	for _, attID := range req.AttachmentIDs {
		ok, err := s.repo.BindAttachmentTx(ctx, tx, clientID, attID, feedbackID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, apperr.New(http.StatusBadRequest, 40001, "附件校验失败")
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &model.SubmitFeedbackResponse{FeedbackNo: f.FeedbackNo}, nil
}

func lockFeedbackRiskKeysTx(ctx context.Context, tx pgx.Tx, clientID, clientIP string) error {
	keys := []string{"feedback:client:" + clientID, "feedback:ip:" + clientIP}
	sort.Strings(keys)
	for _, key := range keys {
		if _, err := tx.Exec(
			ctx,
			"SELECT pg_advisory_xact_lock(hashtext($1)::bigint)",
			key,
		); err != nil {
			return err
		}
	}
	return nil
}

func normalizeExtra(raw json.RawMessage, appID string) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage("{}")
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, apperr.New(http.StatusBadRequest, 40001, "extra 参数非法")
	}
	if appID != "" {
		obj["app_name"] = appID
	}
	marshaled, err := json.Marshal(obj)
	if err != nil {
		return nil, apperr.New(http.StatusBadRequest, 40001, "extra 参数非法")
	}
	if len(marshaled) > 8192 {
		return nil, apperr.New(http.StatusBadRequest, 40001, "extra 超过 8KB")
	}
	return json.RawMessage(marshaled), nil
}
