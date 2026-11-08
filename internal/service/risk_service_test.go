package service

import (
	"io"
	"log/slog"
	"net/http"
	"testing"

	"lens-backend/internal/captcha"
	"lens-backend/internal/config"
	"lens-backend/internal/pkg/apperr"
	"lens-backend/internal/repository"
)

func testRiskConfig() *config.Config {
	return &config.Config{
		RiskReviewThreshold:     40,
		RiskChallengeThreshold:  55,
		RiskBlockThreshold:      80,
		RiskSimilarityThreshold: 0.85,
	}
}

func TestRiskServiceChallengeErrorCarriesPublicMetadata(t *testing.T) {
	svc := NewRiskService(
		nil,
		nil,
		captcha.New(captcha.Config{
			Provider: "turnstile",
			SiteKey:  "site-key",
			Secret:   "server-secret",
		}),
		testRiskConfig(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	err := svc.ChallengeError()
	ae, ok := err.(*apperr.Error)
	if !ok {
		t.Fatalf("ChallengeError() = %T, want *apperr.Error", err)
	}
	if ae.HTTPStatus != http.StatusPreconditionRequired || ae.Code != riskChallengeCode {
		t.Fatalf("challenge status/code = %d/%d", ae.HTTPStatus, ae.Code)
	}
	if ae.Headers["X-Captcha-Required"] != "turnstile" {
		t.Fatalf("challenge header = %v", ae.Headers)
	}
	details, ok := ae.Details.(map[string]any)
	if !ok {
		t.Fatalf("challenge details = %T", ae.Details)
	}
	challenge := details["challenge"].(map[string]string)
	if challenge["site_key"] != "site-key" || challenge["header"] != "X-Captcha-Token" {
		t.Fatalf("challenge metadata = %v", challenge)
	}
}

func TestRiskServiceAssessDuplicateAndSimilarity(t *testing.T) {
	svc := NewRiskService(nil, nil, captcha.New(captcha.Config{Provider: "off"}), testRiskConfig(), nil)
	got := svc.Assess(
		"课表页面切换周次后闪退，重新打开仍然无法恢复。",
		repository.RiskSnapshot{
			ExactDuplicate24h: 1,
			RecentContents: []string{
				"课表页面切换周次后会闪退，重新打开仍然无法恢复",
			},
		},
		false,
	)
	if got.Score < 55 || !got.RequiresCaptcha {
		t.Fatalf("assessment = %+v, want challenge-level duplicate risk", got)
	}
}
