// Package captcha 校验 Cloudflare Turnstile 与 hCaptcha token。
package captcha

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	ErrInvalidToken = errors.New("captcha token invalid")
	ErrUnavailable  = errors.New("captcha provider unavailable")
)

// Config 是验证码服务配置。
type Config struct {
	Provider  string
	SiteKey   string
	Secret    string
	VerifyURL string
	Timeout   time.Duration
}

// Verifier 提供验证码元数据与服务端校验能力。
type Verifier interface {
	Enabled() bool
	Provider() string
	SiteKey() string
	Verify(ctx context.Context, token, remoteIP string) error
}

// Service 是 HTTP 验证码校验器；provider=off 时 Enabled 返回 false。
type Service struct {
	provider  string
	siteKey   string
	secret    string
	verifyURL string
	client    *http.Client
}

// New 根据配置构造验证码校验器。
func New(cfg Config) *Service {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		provider = "off"
	}
	endpoint := strings.TrimSpace(cfg.VerifyURL)
	if endpoint == "" {
		switch provider {
		case "turnstile":
			endpoint = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
		case "hcaptcha":
			endpoint = "https://hcaptcha.com/siteverify"
		}
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Service{
		provider:  provider,
		siteKey:   strings.TrimSpace(cfg.SiteKey),
		secret:    strings.TrimSpace(cfg.Secret),
		verifyURL: endpoint,
		client:    &http.Client{Timeout: timeout},
	}
}

// Enabled 表示是否需要高风险请求执行人机验证。
func (s *Service) Enabled() bool {
	return s != nil && s.provider != "off" && s.verifyURL != "" && s.secret != ""
}

// Provider 返回 turnstile 或 hcaptcha。
func (s *Service) Provider() string {
	if s == nil {
		return "off"
	}
	return s.provider
}

// SiteKey 是允许返回给客户端的公开站点键。
func (s *Service) SiteKey() string {
	if s == nil {
		return ""
	}
	return s.siteKey
}

// Verify 调用验证码服务端接口。token 为空或校验失败返回 ErrInvalidToken，
// 网络/5xx/响应格式异常返回 ErrUnavailable。
func (s *Service) Verify(ctx context.Context, token, remoteIP string) error {
	if !s.Enabled() {
		return ErrUnavailable
	}
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 8192 {
		return ErrInvalidToken
	}
	form := url.Values{}
	form.Set("secret", s.secret)
	form.Set("response", token)
	if ip := strings.TrimSpace(remoteIP); ip != "" {
		form.Set("remoteip", ip)
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		s.verifyURL,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: status %d", ErrInvalidToken, resp.StatusCode)
	}
	var result struct {
		Success    bool     `json:"success"`
		ErrorCodes []string `json:"error-codes"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("%w: invalid json", ErrUnavailable)
	}
	if !result.Success {
		return fmt.Errorf("%w: %s", ErrInvalidToken, strings.Join(result.ErrorCodes, ","))
	}
	return nil
}
