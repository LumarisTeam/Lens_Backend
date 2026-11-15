package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"lens-backend/internal/captcha"
	"lens-backend/internal/config"
	"lens-backend/internal/feedbackcache"
	"lens-backend/internal/model"
	"lens-backend/internal/pkg/apperr"
	"lens-backend/internal/repository"
	"lens-backend/internal/risk"
)

const (
	riskChallengeCode      = 42801
	riskCaptchaInvalidCode = 40301
	riskCaptchaUnavailable = 50302
	riskEventWriteTimeout  = 2 * time.Second
	riskEventMaxKeyDetail  = 80
)

// RiskService 统一处理共享限流、每日配额、风险评分与 CAPTCHA。
type RiskService struct {
	repo    *repository.Repository
	cache   *feedbackcache.Client
	captcha captcha.Verifier
	cfg     *config.Config
	logger  *slog.Logger
}

// NewRiskService 构造风控服务。
func NewRiskService(
	repo *repository.Repository,
	cache *feedbackcache.Client,
	verifier captcha.Verifier,
	cfg *config.Config,
	logger *slog.Logger,
) *RiskService {
	if logger == nil {
		logger = slog.Default()
	}
	return &RiskService{repo: repo, cache: cache, captcha: verifier, cfg: cfg, logger: logger}
}

// CheckClientRate 对来源 IP 与 IPv4 /24（IPv6 /64）执行跨实例令牌桶限流。
func (s *RiskService) CheckClientRate(ctx context.Context, rawIP string) error {
	clientIP := risk.ClientKey(rawIP)
	subnet := risk.Subnet(rawIP)
	ok, err := s.cache.AllowTokenBucket(
		ctx,
		"ip",
		clientIP,
		s.cfg.IPRateLimitRPS,
		s.cfg.IPRateLimitBurst,
		10*time.Minute,
	)
	if err != nil {
		return redisError(err)
	}
	if !ok {
		s.recordRejection(
			"ip_rate_limited",
			clientIP,
			map[string]any{"scope": "ip", "limit_rps": s.cfg.IPRateLimitRPS, "burst": s.cfg.IPRateLimitBurst},
		)
		return apperr.New(http.StatusTooManyRequests, 42901, "请求过于频繁")
	}
	if subnet == "" {
		return nil
	}
	ok, err = s.cache.AllowTokenBucket(
		ctx,
		"subnet",
		subnet,
		s.cfg.SubnetRateLimitRPS,
		s.cfg.SubnetRateLimitBurst,
		10*time.Minute,
	)
	if err != nil {
		return redisError(err)
	}
	if !ok {
		s.recordRejection(
			"subnet_rate_limited",
			clientIP,
			map[string]any{
				"scope":     "subnet",
				"subnet":    subnet,
				"limit_rps": s.cfg.SubnetRateLimitRPS,
				"burst":     s.cfg.SubnetRateLimitBurst,
			},
		)
		return apperr.New(http.StatusTooManyRequests, 42901, "请求过于频繁")
	}
	return nil
}

// ConsumeCodeQuota 消耗单 IP 每日取码配额。
func (s *RiskService) ConsumeCodeQuota(
	ctx context.Context,
	centerID, clientIP, clientID string,
) error {
	return s.consumeDailyQuota(
		ctx,
		"code",
		centerID,
		clientIP,
		clientID,
		s.cfg.IPDailyCodeLimit,
		"code_daily_quota",
	)
}

// ConsumeImageQuota 消耗单 IP 每日图片 presign 配额。
func (s *RiskService) ConsumeImageQuota(ctx context.Context, clientIP, clientID string) error {
	return s.consumeDailyQuota(
		ctx,
		"image",
		"",
		clientIP,
		clientID,
		s.cfg.IPDailyImageLimit,
		"image_daily_quota",
	)
}

func (s *RiskService) consumeDailyQuota(
	ctx context.Context,
	scope, centerID, rawIP, clientID string,
	limit int,
	eventType string,
) error {
	clientIP := risk.ClientKey(rawIP)
	allowed, count, err := s.cache.ConsumeDailyQuota(ctx, scope, clientIP, limit, time.Now())
	if err != nil {
		return redisError(err)
	}
	if !allowed {
		s.recordRejectionWithClient(
			eventType,
			centerID,
			clientID,
			clientIP,
			map[string]any{"scope": scope, "limit": limit, "count": count},
		)
		return apperr.New(http.StatusTooManyRequests, 42901, "今日操作次数已达上限")
	}
	return nil
}

// Assess 将数据库聚合特征转换为评分，并计算相似度。
func (s *RiskService) Assess(content string, snapshot repository.RiskSnapshot, captchaPassed bool) risk.Assessment {
	signals := risk.Signals{
		IPFeedback24h:     snapshot.IPFeedback24h,
		ClientFeedback24h: snapshot.ClientFeedback24h,
		IPFeedback10m:     snapshot.IPFeedback10m,
		ClientFeedback10m: snapshot.ClientFeedback10m,
		ExactDuplicate24h: snapshot.ExactDuplicate24h,
		IPImages24h:       snapshot.IPImages24h,
		IPBlockedEvents7d: snapshot.IPBlockedEvents7d,
		CaptchaPassed:     captchaPassed,
	}
	for _, recent := range snapshot.RecentContents {
		score := risk.Similarity(content, recent)
		if score > signals.SimilarMax {
			signals.SimilarMax = score
		}
		if score >= s.cfg.RiskSimilarityThreshold {
			signals.SimilarCount++
		}
	}
	return risk.Evaluate(signals, s.Policy())
}

// Policy 返回当前风险评分策略。
func (s *RiskService) Policy() risk.Policy {
	return risk.Policy{
		ReviewThreshold:     s.cfg.RiskReviewThreshold,
		ChallengeThreshold:  s.cfg.RiskChallengeThreshold,
		BlockThreshold:      s.cfg.RiskBlockThreshold,
		SimilarityThreshold: s.cfg.RiskSimilarityThreshold,
	}
}

// CaptchaEnabled 表示是否配置了高风险挑战提供方。
func (s *RiskService) CaptchaEnabled() bool {
	return s.captcha != nil && s.captcha.Enabled()
}

// ChallengeError 返回 428 + 公开挑战元数据；token 通过 X-Captcha-Token 回传。
func (s *RiskService) ChallengeError() error {
	if !s.CaptchaEnabled() {
		return nil
	}
	provider := s.captcha.Provider()
	return apperr.WithDetails(
		http.StatusPreconditionRequired,
		riskChallengeCode,
		"需要完成人机验证",
		map[string]any{
			"challenge": map[string]string{
				"provider": provider,
				"site_key": s.captcha.SiteKey(),
				"header":   "X-Captcha-Token",
			},
		},
		map[string]string{"X-Captcha-Required": provider},
	)
}

// VerifyCaptcha 校验高风险请求的人机验证 token。
func (s *RiskService) VerifyCaptcha(
	ctx context.Context,
	centerID, clientID, rawIP, token string,
) error {
	if !s.CaptchaEnabled() {
		return nil
	}
	err := s.captcha.Verify(ctx, token, risk.ClientKey(rawIP))
	if err == nil {
		return nil
	}
	clientIP := risk.ClientKey(rawIP)
	if errors.Is(err, captcha.ErrInvalidToken) {
		s.recordRejectionWithClient("captcha_failed", centerID, clientID, clientIP, map[string]any{
			"center_id": centerID,
		})
		return apperr.New(http.StatusForbidden, riskCaptchaInvalidCode, "人机验证失败，请重试")
	}
	s.logger.Warn("captcha provider unavailable", "error", err, "center_id", centerID)
	s.recordRejectionWithClient("captcha_unavailable", centerID, clientID, clientIP, map[string]any{
		"center_id": centerID,
	})
	return apperr.New(http.StatusServiceUnavailable, riskCaptchaUnavailable, "人机验证服务暂不可用")
}

// RecordChallenge 记录一次高风险 CAPTCHA 触发；此时反馈尚未入库。
func (s *RiskService) RecordChallenge(
	ctx context.Context,
	centerID, clientID, clientIP string,
	assessment risk.Assessment,
) {
	if !s.claimRiskEvent("captcha_required", centerID+"|"+clientIP) {
		return
	}
	reasons, _ := json.Marshal(assessment.Reasons)
	detail, _ := json.Marshal(map[string]any{"provider": s.captcha.Provider()})
	writeCtx, cancel := context.WithTimeout(ctx, riskEventWriteTimeout)
	defer cancel()
	if err := s.repo.InsertRiskEvent(writeCtx, &model.RiskEvent{
		CenterID:    centerID,
		ClientID:    clientID,
		ClientIP:    clientIP,
		EventType:   "captcha_required",
		Action:      model.RiskActionChallenge,
		RiskScore:   assessment.Score,
		RiskReasons: reasons,
		Detail:      detail,
	}); err != nil {
		s.logger.Warn("write captcha challenge event failed", "center_id", centerID, "error", err)
	}
}

// RecordSubmissionQuotaRejection 记录提交配额拦截。
func (s *RiskService) RecordSubmissionQuotaRejection(
	ctx context.Context,
	centerID, clientID, clientIP, eventType string,
	detail map[string]any,
) {
	if !s.claimRiskEvent(eventType, centerID+"|"+clientIP) {
		return
	}
	reasons, _ := json.Marshal([]risk.Reason{{
		Code:   eventType,
		Score:  60,
		Detail: "24 小时提交配额已耗尽",
	}})
	rawDetail, _ := json.Marshal(detail)
	writeCtx, cancel := context.WithTimeout(ctx, riskEventWriteTimeout)
	defer cancel()
	if err := s.repo.InsertRiskEvent(writeCtx, &model.RiskEvent{
		CenterID:    centerID,
		ClientID:    clientID,
		ClientIP:    clientIP,
		EventType:   eventType,
		Action:      model.RiskActionReject,
		RiskScore:   60,
		RiskReasons: reasons,
		Detail:      rawDetail,
	}); err != nil {
		s.logger.Warn("write submission quota event failed", "event_type", eventType, "error", err)
	}
}

// RecordProtectionRejection 记录进程内限流、校验码频率等本地规则拦截。
func (s *RiskService) RecordProtectionRejection(
	centerID, clientID, clientIP, eventType string,
	detail map[string]any,
) {
	s.recordRejectionWithClient(eventType, centerID, clientID, clientIP, detail)
}

func (s *RiskService) recordRejection(
	eventType, clientIP string,
	detail map[string]any,
) {
	s.recordRejectionWithClient(eventType, "", "", clientIP, detail)
}

func (s *RiskService) recordRejectionWithClient(
	eventType, centerID, clientID, clientIP string,
	detail map[string]any,
) {
	if s.repo == nil {
		return
	}
	if !s.claimRiskEvent(eventType, centerID+"|"+clientIP) {
		return
	}
	reasons, _ := json.Marshal([]risk.Reason{{
		Code:   eventType,
		Score:  60,
		Detail: "已触发服务端防护规则",
	}})
	rawDetail, _ := json.Marshal(detail)
	ctx, cancel := context.WithTimeout(context.Background(), riskEventWriteTimeout)
	defer cancel()
	if err := s.repo.InsertRiskEvent(ctx, &model.RiskEvent{
		CenterID:    centerID,
		ClientID:    truncate(clientID, riskEventMaxKeyDetail),
		ClientIP:    truncate(clientIP, riskEventMaxKeyDetail),
		EventType:   eventType,
		Action:      model.RiskActionReject,
		RiskScore:   60,
		RiskReasons: reasons,
		Detail:      rawDetail,
	}); err != nil {
		s.logger.Warn("write risk event failed", "event_type", eventType, "error", err)
	}
}

func (s *RiskService) claimRiskEvent(eventType, identity string) bool {
	if s.cache == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ok, err := s.cache.ClaimRiskEvent(ctx, eventType, identity, time.Minute)
	if err != nil {
		s.logger.Warn("claim risk event failed", "event_type", eventType, "error", err)
		return false
	}
	return ok
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
