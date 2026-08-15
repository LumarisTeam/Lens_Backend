package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"unicode/utf8"

	"lens-backend/internal/config"
	"lens-backend/internal/model"
	"lens-backend/internal/pkg/apperr"
	"lens-backend/internal/pkg/idgen"
	"lens-backend/internal/repository"
)

// FeedbackService 处理反馈提交业务逻辑。
type FeedbackService struct {
	repo *repository.Repository
	cfg  *config.Config
}

// NewFeedbackService 构造 FeedbackService。
func NewFeedbackService(repo *repository.Repository, cfg *config.Config) *FeedbackService {
	return &FeedbackService{repo: repo, cfg: cfg}
}

// Submit 提交反馈：参数校验 -> 日上限 -> 事务内幂等插入 + 附件绑定。
func (s *FeedbackService) Submit(ctx context.Context, clientID string, req model.SubmitFeedbackRequest) (*model.SubmitFeedbackResponse, error) {
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

	// extra 校验：按 PRD 执行 json.Marshal 计算字节长度，不得超过 8192。
	extra := req.Extra
	if len(extra) == 0 || string(extra) == "null" {
		extra = json.RawMessage("{}")
	}
	var normalized any
	if err := json.Unmarshal(extra, &normalized); err != nil {
		return nil, apperr.New(http.StatusBadRequest, 40001, "extra 参数非法")
	}
	marshaled, err := json.Marshal(normalized)
	if err != nil {
		return nil, apperr.New(http.StatusBadRequest, 40001, "extra 参数非法")
	}
	if len(marshaled) > 8192 {
		return nil, apperr.New(http.StatusBadRequest, 40001, "extra 超过 8KB")
	}
	extra = json.RawMessage(marshaled)

	// 日上限：同一 client_id 24 小时内成功创建的反馈数不超过上限。
	count, err := s.repo.CountFeedbackByClientLast24h(ctx, clientID)
	if err != nil {
		return nil, err
	}
	if count >= int64(s.cfg.FeedbackDailyLimit) {
		return nil, apperr.New(http.StatusTooManyRequests, 42901, "今日提交数量已达上限")
	}

	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	f := &model.Feedback{
		FeedbackNo: idgen.FeedbackNo(),
		RequestID:  req.RequestID,
		ClientID:   clientID,
		Content:    req.Content,
		Contact:    req.Contact,
		ImageCount: int16(len(req.AttachmentIDs)),
		Extra:      extra,
	}
	feedbackID, err := s.repo.InsertFeedbackTx(ctx, tx, f)
	if errors.Is(err, repository.ErrRequestIDConflict) {
		no, existingClientID, qerr := s.repo.GetFeedbackByRequestID(ctx, req.RequestID)
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
