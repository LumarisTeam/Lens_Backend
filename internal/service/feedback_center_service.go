package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"lens-backend/internal/config"
	"lens-backend/internal/feedbackcache"
	"lens-backend/internal/model"
	"lens-backend/internal/pkg/apperr"
	"lens-backend/internal/pkg/feedbackcode"
	"lens-backend/internal/pkg/idgen"
	"lens-backend/internal/pkg/secretbox"
	"lens-backend/internal/repository"
)

const (
	maxFeedbackCodeTTLSeconds = 3600
	maxNonceLength            = 128
	maxSNLength               = 128
)

// FeedbackCenterCache 抽象 Redis 操作，便于服务层测试与替换。
type FeedbackCenterCache interface {
	StoreCenter(ctx context.Context, center *model.FeedbackCenter, ttl time.Duration) error
	GetCenter(ctx context.Context, centerID string) (*model.FeedbackCenter, error)
	DeleteCenter(ctx context.Context, centerID string) error
	StoreCode(ctx context.Context, centerID, sn string, timestamp int64, nonce, code, clientID string, secretVersion int, ttl time.Duration) error
	ConsumeCode(ctx context.Context, centerID, sn string, timestamp int64, nonce, code, clientID string, secretVersion int, ttl time.Duration) (feedbackcache.ConsumeCodeResult, error)
	DeleteCodes(ctx context.Context, centerID string) error
	Allow(ctx context.Context, centerID, sn string, limit int, minute time.Time) (bool, error)
	AcquireLock(ctx context.Context, key, token string, ttl time.Duration) (bool, error)
	ReleaseLock(ctx context.Context, key, token string) error
}

// FeedbackCenterService 处理反馈中心管理与校验码业务。
type FeedbackCenterService struct {
	repo   *repository.Repository
	cache  FeedbackCenterCache
	cipher *secretbox.Cipher
	cfg    *config.Config
	logger *slog.Logger
}

// NewFeedbackCenterService 构造 FeedbackCenterService。
func NewFeedbackCenterService(
	repo *repository.Repository,
	cache FeedbackCenterCache,
	cipher *secretbox.Cipher,
	cfg *config.Config,
	logger *slog.Logger,
) *FeedbackCenterService {
	return &FeedbackCenterService{
		repo:   repo,
		cache:  cache,
		cipher: cipher,
		cfg:    cfg,
		logger: logger,
	}
}

// Create 创建反馈中心，完整 secret 仅在本响应返回。
func (s *FeedbackCenterService) Create(
	ctx context.Context,
	req model.CreateFeedbackCenterRequest,
	operator, ip string,
) (*model.CreateFeedbackCenterResponse, error) {
	name, err := validateText("name", req.Name, 128, true)
	if err != nil {
		return nil, err
	}
	appID, err := validateText("appId", req.AppID, 64, true)
	if err != nil {
		return nil, err
	}
	env, err := validateText("env", defaultString(req.Env, "prod"), 16, true)
	if err != nil {
		return nil, err
	}
	snMode, sns, err := validateSNConfig(req.SNMode, req.SNList)
	if err != nil {
		return nil, err
	}
	if err := validateFutureExpire(req.ExpireAt); err != nil {
		return nil, err
	}
	contact, err := validateText("contact", req.Contact, 128, false)
	if err != nil {
		return nil, err
	}
	remark, err := validateText("remark", req.Remark, 255, false)
	if err != nil {
		return nil, err
	}

	plainSecret := newSecret()
	secretCipher, err := s.cipher.Encrypt(plainSecret)
	if err != nil {
		return nil, internalError(err)
	}
	center := &model.FeedbackCenter{
		CenterID:      "fc_" + strings.ToLower(idgen.NewULID()),
		Name:          name,
		AppID:         appID,
		Env:           env,
		SecretCipher:  secretCipher,
		SecretVersion: 1,
		SNMode:        snMode,
		Status:        model.FeedbackCenterStatusEnabled,
		ExpireAt:      req.ExpireAt,
		Contact:       contact,
		Remark:        remark,
		CreatedBy:     operator,
	}
	audit := newAudit(
		center.CenterID,
		model.FeedbackCenterAuditCreate,
		operator,
		ip,
		map[string]any{"appId": appID, "env": env},
	)
	if err := s.repo.CreateFeedbackCenter(ctx, center, sns, audit); err != nil {
		return nil, internalError(err)
	}
	s.cacheCenterBestEffort(ctx, center)

	return &model.CreateFeedbackCenterResponse{
		CenterID: center.CenterID,
		Secret:   plainSecret,
		Status:   center.Status,
	}, nil
}

// List 查询反馈中心列表。
func (s *FeedbackCenterService) List(
	ctx context.Context,
	p model.ListFeedbackCentersParams,
) (*model.FeedbackCenterListResponse, error) {
	if p.Status != "" &&
		p.Status != model.FeedbackCenterStatusEnabled &&
		p.Status != model.FeedbackCenterStatusDisabled {
		return nil, apperr.New(http.StatusBadRequest, 40001, "status 参数非法")
	}
	items, total, err := s.repo.ListFeedbackCenters(ctx, p)
	if err != nil {
		return nil, internalError(err)
	}
	return &model.FeedbackCenterListResponse{Total: total, Items: items}, nil
}

// Get 查询反馈中心详情；secret 永久脱敏。
func (s *FeedbackCenterService) Get(ctx context.Context, centerID string) (*model.FeedbackCenterDetail, error) {
	center, err := s.repo.GetFeedbackCenter(ctx, centerID)
	if err != nil {
		return nil, s.mapRepoError(err)
	}
	sns, err := s.repo.ListFeedbackCenterSNs(ctx, centerID)
	if err != nil {
		return nil, internalError(err)
	}
	snList := make([]string, 0, len(sns))
	for _, sn := range sns {
		if sn.Status == model.FeedbackCenterStatusEnabled {
			snList = append(snList, sn.SN)
		}
	}
	return &model.FeedbackCenterDetail{
		CenterID:      center.CenterID,
		Name:          center.Name,
		AppID:         center.AppID,
		Env:           center.Env,
		SecretMasked:  "sk_****",
		SecretVersion: center.SecretVersion,
		SNMode:        center.SNMode,
		Status:        center.Status,
		SNList:        snList,
		ExpireAt:      center.ExpireAt,
		Contact:       center.Contact,
		Remark:        center.Remark,
		CreatedBy:     center.CreatedBy,
		LastUsedAt:    center.LastUsedAt,
		CreatedAt:     center.CreatedAt,
		UpdatedAt:     center.UpdatedAt,
	}, nil
}

// Update 更新反馈中心配置。
func (s *FeedbackCenterService) Update(
	ctx context.Context,
	centerID string,
	req model.UpdateFeedbackCenterRequest,
	operator, ip string,
) (*model.FeedbackCenterDetail, error) {
	center, err := s.repo.GetFeedbackCenter(ctx, centerID)
	if err != nil {
		return nil, s.mapRepoError(err)
	}

	changed := make([]string, 0, 8)
	if req.Name != nil {
		v, err := validateText("name", *req.Name, 128, true)
		if err != nil {
			return nil, err
		}
		center.Name = v
		changed = append(changed, "name")
	}
	if req.AppID != nil {
		v, err := validateText("appId", *req.AppID, 64, true)
		if err != nil {
			return nil, err
		}
		center.AppID = v
		changed = append(changed, "appId")
	}
	if req.Env != nil {
		v, err := validateText("env", *req.Env, 16, true)
		if err != nil {
			return nil, err
		}
		center.Env = v
		changed = append(changed, "env")
	}
	if req.Contact != nil {
		v, err := validateText("contact", *req.Contact, 128, false)
		if err != nil {
			return nil, err
		}
		center.Contact = v
		changed = append(changed, "contact")
	}
	if req.Remark != nil {
		v, err := validateText("remark", *req.Remark, 255, false)
		if err != nil {
			return nil, err
		}
		center.Remark = v
		changed = append(changed, "remark")
	}
	if req.ExpireAt != nil {
		if err := validateFutureExpire(req.ExpireAt); err != nil {
			return nil, err
		}
		center.ExpireAt = req.ExpireAt
		changed = append(changed, "expireAt")
	}

	replaceSNs := false
	var sns []model.FeedbackCenterSN
	if req.SNMode != nil || req.SNList != nil {
		mode := center.SNMode
		if req.SNMode != nil {
			mode = *req.SNMode
		}
		list := []string(nil)
		if req.SNList != nil {
			list = *req.SNList
		} else {
			existing, err := s.repo.ListFeedbackCenterSNs(ctx, centerID)
			if err != nil {
				return nil, internalError(err)
			}
			for _, sn := range existing {
				list = append(list, sn.SN)
			}
		}
		center.SNMode, sns, err = validateSNConfig(mode, list)
		if err != nil {
			return nil, err
		}
		replaceSNs = true
		changed = append(changed, "snMode", "snList")
	}

	audit := newAudit(centerID, model.FeedbackCenterAuditUpdate, operator, ip, map[string]any{"changed": changed})
	if err := s.repo.UpdateFeedbackCenter(ctx, center, sns, replaceSNs, audit); err != nil {
		return nil, s.mapRepoError(err)
	}
	if err := s.refreshCenterCache(ctx, center); err != nil {
		return nil, redisError(err)
	}
	return s.Get(ctx, centerID)
}

// SetStatus 启用或禁用反馈中心。
func (s *FeedbackCenterService) SetStatus(
	ctx context.Context,
	centerID, status, operator, ip string,
) error {
	if status != model.FeedbackCenterStatusEnabled && status != model.FeedbackCenterStatusDisabled {
		return apperr.New(http.StatusBadRequest, 40001, "status 参数非法")
	}
	action := model.FeedbackCenterAuditEnable
	if status == model.FeedbackCenterStatusDisabled {
		action = model.FeedbackCenterAuditDisable
	}
	audit := newAudit(centerID, action, operator, ip, nil)
	if err := s.repo.UpdateFeedbackCenterStatus(ctx, centerID, status, audit); err != nil {
		return s.mapRepoError(err)
	}

	center, err := s.repo.GetFeedbackCenter(ctx, centerID)
	if err != nil {
		return s.mapRepoError(err)
	}
	if err := s.cache.StoreCenter(
		ctx, center, time.Duration(s.cfg.FeedbackCenterCacheTTLSeconds)*time.Second,
	); err != nil {
		return redisError(err)
	}
	if err := s.cache.DeleteCodes(ctx, centerID); err != nil {
		s.logger.Warn("delete feedback codes failed", "center_id", centerID, "error", err)
	}
	return nil
}

// ResetSecret 重置中心 secret，旧版本校验码立即失效。
func (s *FeedbackCenterService) ResetSecret(
	ctx context.Context,
	centerID, operator, ip string,
) (*model.ResetFeedbackCenterSecretResponse, error) {
	token := randomHex(16)
	lockKey := "reset:" + centerID
	locked, err := s.cache.AcquireLock(ctx, lockKey, token, 10*time.Second)
	if err != nil {
		return nil, redisError(err)
	}
	if !locked {
		return nil, apperr.New(http.StatusTooManyRequests, 42901, "操作过于频繁，请稍后再试")
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := s.cache.ReleaseLock(releaseCtx, lockKey, token); err != nil {
			s.logger.Warn("release feedback center reset lock failed", "center_id", centerID, "error", err)
		}
	}()

	center, err := s.repo.GetFeedbackCenter(ctx, centerID)
	if err != nil {
		return nil, s.mapRepoError(err)
	}
	plainSecret := newSecret()
	secretCipher, err := s.cipher.Encrypt(plainSecret)
	if err != nil {
		return nil, internalError(err)
	}
	audit := newAudit(centerID, model.FeedbackCenterAuditResetSecret, operator, ip, nil)
	newVersion, err := s.repo.ResetFeedbackCenterSecret(
		ctx, centerID, secretCipher, center.SecretVersion, audit,
	)
	if errors.Is(err, repository.ErrSecretVersionConflict) {
		return nil, apperr.New(http.StatusConflict, 40901, "secret 已被并发重置，请刷新后重试")
	}
	if err != nil {
		return nil, internalError(err)
	}

	center.SecretVersion = newVersion
	if err := s.cache.StoreCenter(
		ctx, center, time.Duration(s.cfg.FeedbackCenterCacheTTLSeconds)*time.Second,
	); err != nil {
		return nil, redisError(err)
	}

	return &model.ResetFeedbackCenterSecretResponse{
		CenterID:      centerID,
		Secret:        plainSecret,
		SecretVersion: newVersion,
	}, nil
}

// GenerateCode 生成校验码并写入 Redis。
func (s *FeedbackCenterService) GenerateCode(
	ctx context.Context,
	centerID string,
	req model.GenerateFeedbackCodeRequest,
	operator, ip string,
) (*model.GenerateFeedbackCodeResponse, error) {
	center, err := s.repo.GetFeedbackCenter(ctx, centerID)
	if err != nil {
		return nil, s.mapRepoError(err)
	}
	if err := s.ensureCenterUsable(ctx, center); err != nil {
		return nil, err
	}
	sn := strings.TrimSpace(req.SN)
	if err := s.validateSN(ctx, center, sn); err != nil {
		return nil, err
	}

	timestamp := req.Timestamp
	if timestamp == 0 {
		timestamp = time.Now().Unix()
	}
	nonce := strings.TrimSpace(req.Nonce)
	if nonce == "" {
		nonce = randomHex(16)
	}
	if err := validateKeyComponent("nonce", nonce, maxNonceLength); err != nil {
		return nil, err
	}

	ttl := req.TTL
	if ttl == 0 {
		ttl = s.cfg.FeedbackCodeTTLSeconds
	}
	if ttl < 1 || ttl > maxFeedbackCodeTTLSeconds {
		return nil, apperr.New(http.StatusBadRequest, 40001, "ttl 参数非法")
	}

	resp, err := s.issueCode(ctx, center, sn, timestamp, nonce, "", ttl)
	if err != nil {
		return nil, err
	}

	audit := newAudit(centerID, model.FeedbackCenterAuditGenerateCode, operator, ip, map[string]any{
		"sn":        sn,
		"timestamp": timestamp,
		"ttl":       ttl,
	})
	if err := s.repo.CreateFeedbackCenterAudit(ctx, audit); err != nil {
		s.logger.Warn("write feedback code audit failed", "center_id", centerID, "sn", req.SN, "error", err)
	}

	return resp, nil
}

// IssueCode 为客户端签发一次性反馈校验码，secret 始终保留在服务端。
func (s *FeedbackCenterService) IssueCode(
	ctx context.Context,
	centerID, clientID string,
	req model.IssueFeedbackCodeRequest,
) (*model.GenerateFeedbackCodeResponse, error) {
	center, err := s.repo.GetFeedbackCenter(ctx, centerID)
	if err != nil {
		return nil, s.mapRepoError(err)
	}
	if err := s.ensureCenterUsable(ctx, center); err != nil {
		return nil, err
	}
	sn := strings.TrimSpace(req.SN)
	if err := s.validateSN(ctx, center, sn); err != nil {
		return nil, err
	}
	ttl := s.cfg.FeedbackCodeTTLSeconds
	if ttl < 1 || ttl > maxFeedbackCodeTTLSeconds {
		return nil, apperr.New(http.StatusInternalServerError, 50002, "系统繁忙，请稍后重试")
	}

	return s.issueCode(
		ctx,
		center,
		sn,
		time.Now().Unix(),
		randomHex(16),
		clientID,
		ttl,
	)
}

func (s *FeedbackCenterService) issueCode(
	ctx context.Context,
	center *model.FeedbackCenter,
	sn string,
	timestamp int64,
	nonce string,
	clientID string,
	ttl int,
) (*model.GenerateFeedbackCodeResponse, error) {
	plainSecret, err := s.cipher.Decrypt(center.SecretCipher)
	if err != nil {
		return nil, internalError(err)
	}
	code := feedbackcode.Sign(plainSecret, center.CenterID, timestamp, sn, nonce)
	codeTTL := time.Duration(ttl) * time.Second
	if err := s.cache.StoreCode(
		ctx, center.CenterID, sn, timestamp, nonce, code, clientID, center.SecretVersion, codeTTL,
	); err != nil {
		return nil, redisError(err)
	}

	return &model.GenerateFeedbackCodeResponse{
		CenterID:  center.CenterID,
		Timestamp: timestamp,
		SN:        sn,
		Nonce:     nonce,
		Code:      code,
		ExpireAt:  time.Now().Add(codeTTL).UTC(),
	}, nil
}

// VerifyAndConsume 校验反馈提交头并一次性消费校验码。
func (s *FeedbackCenterService) VerifyAndConsume(
	ctx context.Context,
	h model.FeedbackCodeHeaders,
) (*model.FeedbackCenter, error) {
	if h.CenterID == "" || h.Timestamp == "" || h.SN == "" || h.Nonce == "" || h.Code == "" {
		return nil, apperr.New(http.StatusBadRequest, 40001, "请求参数不完整")
	}
	if err := validateIdentifier("centerId", h.CenterID, 64); err != nil {
		return nil, err
	}
	if err := validateKeyComponent("sn", h.SN, maxSNLength); err != nil {
		return nil, err
	}
	if err := validateKeyComponent("nonce", h.Nonce, maxNonceLength); err != nil {
		return nil, err
	}
	if len(h.Code) > 128 {
		return nil, apperr.New(http.StatusBadRequest, 40001, "code 参数非法")
	}
	timestamp, err := strconv.ParseInt(h.Timestamp, 10, 64)
	if err != nil {
		return nil, apperr.New(http.StatusBadRequest, 40002, "请求时间格式错误")
	}
	if err := validateTimestamp(timestamp, s.cfg.FeedbackTimestampWindowSeconds, time.Now()); err != nil {
		return nil, err
	}

	center, err := s.getCenterMeta(ctx, h.CenterID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureCenterUsable(ctx, center); err != nil {
		return nil, err
	}
	if err := s.validateSN(ctx, center, h.SN); err != nil {
		return nil, err
	}
	allowed, err := s.cache.Allow(
		ctx, h.CenterID, h.SN, s.cfg.FeedbackRateLimitPerMinute, time.Now(),
	)
	if err != nil {
		return nil, redisError(err)
	}
	if !allowed {
		return nil, apperr.New(http.StatusTooManyRequests, 42901, "请求过于频繁")
	}

	result, err := s.cache.ConsumeCode(
		ctx,
		h.CenterID,
		h.SN,
		timestamp,
		h.Nonce,
		h.Code,
		h.ClientID,
		center.SecretVersion,
		time.Duration(s.cfg.FeedbackCodeTTLSeconds)*time.Second,
	)
	if err != nil {
		return nil, redisError(err)
	}
	switch result {
	case feedbackcache.ConsumeCodeOK:
		if err := s.repo.TouchFeedbackCenterLastUsed(ctx, h.CenterID); err != nil {
			s.logger.Warn("touch feedback center last_used_at failed", "center_id", h.CenterID, "error", err)
		}
		return center, nil
	case feedbackcache.ConsumeCodeUsed, feedbackcache.ConsumeCodeNonceReused:
		return nil, apperr.New(http.StatusConflict, 40006, "请勿重复提交")
	case feedbackcache.ConsumeCodeVersionMismatch:
		return nil, apperr.New(http.StatusUnauthorized, 40103, "密钥已更新，请重新获取")
	default:
		return nil, apperr.New(http.StatusUnauthorized, 40005, "校验失败")
	}
}

// ListAudits 查询反馈中心审计日志。
func (s *FeedbackCenterService) ListAudits(
	ctx context.Context,
	centerID string,
	limit, offset int,
) (*model.FeedbackCenterAuditListResponse, error) {
	if _, err := s.repo.GetFeedbackCenter(ctx, centerID); err != nil {
		return nil, s.mapRepoError(err)
	}
	items, total, err := s.repo.ListFeedbackCenterAudits(ctx, centerID, limit, offset)
	if err != nil {
		return nil, internalError(err)
	}
	return &model.FeedbackCenterAuditListResponse{Total: total, Items: items}, nil
}

func (s *FeedbackCenterService) getCenterMeta(
	ctx context.Context,
	centerID string,
) (*model.FeedbackCenter, error) {
	center, err := s.cache.GetCenter(ctx, centerID)
	if err == nil {
		return center, nil
	}
	if !errors.Is(err, feedbackcache.ErrCacheMiss) {
		return nil, redisError(err)
	}
	center, err = s.repo.GetFeedbackCenter(ctx, centerID)
	if err != nil {
		return nil, s.mapRepoError(err)
	}
	s.cacheCenterBestEffort(ctx, center)
	return center, nil
}

func (s *FeedbackCenterService) ensureCenterUsable(ctx context.Context, center *model.FeedbackCenter) error {
	if center.Status != model.FeedbackCenterStatusEnabled {
		return apperr.New(http.StatusForbidden, 40102, "反馈中心已停用")
	}
	if center.ExpireAt != nil && !center.ExpireAt.After(time.Now()) {
		return apperr.New(http.StatusForbidden, 40102, "反馈中心已过期")
	}
	return nil
}

func (s *FeedbackCenterService) validateSN(
	ctx context.Context,
	center *model.FeedbackCenter,
	sn string,
) error {
	if err := validateKeyComponent("sn", sn, maxSNLength); err != nil {
		return err
	}
	if center.SNMode == model.FeedbackCenterSNModeAny {
		return nil
	}
	sns, err := s.repo.ListFeedbackCenterSNs(ctx, center.CenterID)
	if err != nil {
		return internalError(err)
	}
	for _, item := range sns {
		if item.Status != model.FeedbackCenterStatusEnabled {
			continue
		}
		if item.ExpireAt != nil && !item.ExpireAt.After(time.Now()) {
			continue
		}
		if center.SNMode == model.FeedbackCenterSNModePrefix && strings.HasPrefix(sn, item.SN) {
			return nil
		}
		if center.SNMode == model.FeedbackCenterSNModeWhitelist && item.SN == sn {
			return nil
		}
	}
	return apperr.New(http.StatusBadRequest, 40004, "设备标识无效")
}

func (s *FeedbackCenterService) cacheCenterBestEffort(ctx context.Context, center *model.FeedbackCenter) {
	if err := s.cache.StoreCenter(
		ctx, center, time.Duration(s.cfg.FeedbackCenterCacheTTLSeconds)*time.Second,
	); err != nil {
		s.logger.Warn("cache feedback center failed", "center_id", center.CenterID, "error", err)
	}
}

func (s *FeedbackCenterService) refreshCenterCache(ctx context.Context, center *model.FeedbackCenter) error {
	return s.cache.StoreCenter(
		ctx, center, time.Duration(s.cfg.FeedbackCenterCacheTTLSeconds)*time.Second,
	)
}

func (s *FeedbackCenterService) mapRepoError(err error) error {
	if errors.Is(err, repository.ErrFeedbackCenterNotFound) {
		return apperr.New(http.StatusNotFound, 40101, "反馈中心不存在")
	}
	return internalError(err)
}

func validateSNConfig(mode string, raw []string) (string, []model.FeedbackCenterSN, error) {
	mode = strings.TrimSpace(mode)
	if mode != model.FeedbackCenterSNModeWhitelist &&
		mode != model.FeedbackCenterSNModePrefix &&
		mode != model.FeedbackCenterSNModeAny {
		return "", nil, apperr.New(http.StatusBadRequest, 40001, "snMode 参数非法")
	}
	sns := normalizeSNs(raw)
	if mode != model.FeedbackCenterSNModeAny && len(sns) == 0 {
		return "", nil, apperr.New(http.StatusBadRequest, 40001, "snList 不能为空")
	}
	out := make([]model.FeedbackCenterSN, 0, len(sns))
	for _, sn := range sns {
		if err := validateKeyComponent("sn", sn, maxSNLength); err != nil {
			return "", nil, err
		}
		out = append(out, model.FeedbackCenterSN{
			SN:     sn,
			Status: model.FeedbackCenterStatusEnabled,
		})
	}
	return mode, out, nil
}

func normalizeSNs(raw []string) []string {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		sn := strings.TrimSpace(item)
		if sn == "" {
			continue
		}
		if _, ok := seen[sn]; ok {
			continue
		}
		seen[sn] = struct{}{}
		out = append(out, sn)
	}
	return out
}

func validateIdentifier(name, value string, max int) error {
	if value == "" || len(value) > max {
		return apperr.New(http.StatusBadRequest, 40001, name+" 参数非法")
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '.' || r == '_' || r == '-' {
			continue
		}
		return apperr.New(http.StatusBadRequest, 40001, name+" 参数非法")
	}
	return nil
}

func validateKeyComponent(name, value string, max int) error {
	if value == "" || utf8.RuneCountInString(value) > max {
		return apperr.New(http.StatusBadRequest, 40001, name+" 参数非法")
	}
	for _, r := range value {
		if r == utf8.RuneError || r < 0x20 || r == 0x7f || strings.ContainsRune(" \t\r\n", r) {
			return apperr.New(http.StatusBadRequest, 40001, name+" 参数非法")
		}
	}
	return nil
}

func validateText(name, value string, max int, required bool) (string, error) {
	value = strings.TrimSpace(value)
	n := utf8.RuneCountInString(value)
	if required && n == 0 {
		return "", apperr.New(http.StatusBadRequest, 40001, name+" 不能为空")
	}
	if n > max {
		return "", apperr.New(http.StatusBadRequest, 40001, name+" 过长")
	}
	return value, nil
}

func validateFutureExpire(expireAt *time.Time) error {
	if expireAt != nil && !expireAt.After(time.Now()) {
		return apperr.New(http.StatusBadRequest, 40001, "expireAt 必须晚于当前时间")
	}
	return nil
}

func validateTimestamp(timestamp int64, windowSeconds int, now time.Time) error {
	delta := now.Unix() - timestamp
	if delta < 0 {
		delta = -delta
	}
	if delta > int64(windowSeconds) {
		return apperr.New(http.StatusBadRequest, 40003, "请求已过期，请重试")
	}
	return nil
}

func newAudit(
	centerID, action, operator, ip string,
	detail map[string]any,
) *model.FeedbackCenterAudit {
	if detail == nil {
		detail = map[string]any{}
	}
	raw, _ := json.Marshal(detail)
	return &model.FeedbackCenterAudit{
		CenterID: centerID,
		Action:   action,
		Operator: operator,
		Detail:   raw,
		IP:       ip,
	}
}

func newSecret() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic("feedback center secret generation failed: " + err.Error())
	}
	return "sk_" + base64.RawURLEncoding.EncodeToString(buf)
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic("random generation failed: " + err.Error())
	}
	return hex.EncodeToString(buf)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func redisError(err error) error {
	return apperr.New(http.StatusServiceUnavailable, 50001, "校验服务暂不可用")
}

func internalError(err error) error {
	return apperr.New(http.StatusInternalServerError, 50002, "系统繁忙，请稍后重试")
}
