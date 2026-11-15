package config

import (
	"reflect"
	"strings"
	"testing"
)

func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("DB_DSN", "postgres://u:p@localhost:5432/db?sslmode=disable")
	t.Setenv("S3_ACCESS_KEY", "ak")
	t.Setenv("S3_SECRET_KEY", "sk")
	t.Setenv("S3_BUCKET", "test-1250000000")
	t.Setenv("ADMIN_API_TOKEN", "tok")
	t.Setenv("FEEDBACK_SECRET_KEY", "test-feedback-secret-key")
}

func TestLoad_MissingRequired(t *testing.T) {
	// 只设置 S3 与 ADMIN，缺 DB_DSN
	t.Setenv("S3_ACCESS_KEY", "ak")
	t.Setenv("S3_SECRET_KEY", "sk")
	t.Setenv("S3_BUCKET", "test-1250000000")
	t.Setenv("ADMIN_API_TOKEN", "tok")
	t.Setenv("DB_DSN", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() should fail when DB_DSN missing")
	}
	if !strings.Contains(err.Error(), "DB_DSN") {
		t.Fatalf("error should name missing var, got: %v", err)
	}
}

func TestLoad_AllRequiredSet(t *testing.T) {
	setRequired(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.ImageMaxSize != 5242880 {
		t.Errorf("ImageMaxSize = %d, want 5242880", cfg.ImageMaxSize)
	}
	if cfg.HTTPTimeoutSeconds != 90 {
		t.Errorf("HTTPTimeoutSeconds = %d, want 90", cfg.HTTPTimeoutSeconds)
	}
	if cfg.AdminRateLimitRPS != 10 || cfg.AdminRateLimitBurst != 30 {
		t.Errorf("admin rate limit defaults mismatch: %v/%v", cfg.AdminRateLimitRPS, cfg.AdminRateLimitBurst)
	}
	if cfg.IPRateLimitBurst != 20 || cfg.SubnetRateLimitBurst != 100 {
		t.Errorf("IP/subnet rate defaults mismatch: %d/%d", cfg.IPRateLimitBurst, cfg.SubnetRateLimitBurst)
	}
	if cfg.IPDailyFeedbackLimit != 30 || cfg.IPDailyCodeLimit != 100 || cfg.IPDailyImageLimit != 60 {
		t.Errorf("IP daily quota defaults mismatch: %d/%d/%d",
			cfg.IPDailyFeedbackLimit, cfg.IPDailyCodeLimit, cfg.IPDailyImageLimit)
	}
	if cfg.RiskReviewThreshold != 40 || cfg.RiskChallengeThreshold != 55 || cfg.RiskBlockThreshold != 80 {
		t.Errorf("risk threshold defaults mismatch: %d/%d/%d",
			cfg.RiskReviewThreshold, cfg.RiskChallengeThreshold, cfg.RiskBlockThreshold)
	}
	if cfg.CaptchaProvider != "off" {
		t.Errorf("CaptchaProvider = %q, want off", cfg.CaptchaProvider)
	}
	if cfg.RedisAddr != "127.0.0.1:6379" {
		t.Errorf("RedisAddr = %q, want 127.0.0.1:6379", cfg.RedisAddr)
	}
	if cfg.FeedbackCodeTTLSeconds != 300 {
		t.Errorf("FeedbackCodeTTLSeconds = %d, want 300", cfg.FeedbackCodeTTLSeconds)
	}
	if !cfg.FeedbackCodeRequired {
		t.Error("FeedbackCodeRequired should default to true")
	}
	if len(cfg.CORSAllowedOrigins) != 0 {
		t.Errorf("CORSAllowedOrigins = %v, want empty", cfg.CORSAllowedOrigins)
	}
	// 未设置 S3_CDN_URL 时按 bucket+region 推导
	if cfg.StorageBucketURL != "https://test-1250000000.cos.ap-guangzhou.myqcloud.com" {
		t.Errorf("StorageBucketURL = %q, want derived bucket URL", cfg.StorageBucketURL)
	}
}

func TestLoad_FeedbackCodeOptional(t *testing.T) {
	setRequired(t)
	t.Setenv("FEEDBACK_CODE_REQUIRED", "false")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.FeedbackCodeRequired {
		t.Fatal("FeedbackCodeRequired should be false when explicitly disabled")
	}
}

func TestLoad_TimeoutNegativeRejected(t *testing.T) {
	setRequired(t)
	t.Setenv("HTTP_TIMEOUT_SECONDS", "-1")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "HTTP_TIMEOUT_SECONDS") {
		t.Fatalf("negative timeout should be rejected, got: %v", err)
	}
}

func TestLoad_AdminRateLimitInvalidRejected(t *testing.T) {
	setRequired(t)
	t.Setenv("ADMIN_RATE_LIMIT_RPS", "0")
	_, err := Load()
	if err == nil {
		t.Fatal("zero admin rate limit should be rejected")
	}
}

func TestLoad_CaptchaRequiresKeys(t *testing.T) {
	setRequired(t)
	t.Setenv("CAPTCHA_PROVIDER", "turnstile")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "CAPTCHA_SITE_KEY") {
		t.Fatalf("enabled captcha without keys should fail, got: %v", err)
	}
}

func TestLoad_RiskThresholdsMustBeOrdered(t *testing.T) {
	setRequired(t)
	t.Setenv("RISK_REVIEW_THRESHOLD", "80")
	t.Setenv("RISK_CHALLENGE_THRESHOLD", "50")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "risk thresholds") {
		t.Fatalf("unordered risk thresholds should fail, got: %v", err)
	}
}

func TestGetEnvIntFallback(t *testing.T) {
	t.Setenv("CFG_TEST_INT_UNSET", "")
	t.Setenv("CFG_TEST_INT_BAD", "abc")
	if n := getEnvInt("CFG_TEST_INT_UNSET", 42); n != 42 {
		t.Errorf("empty value should fall back to default, got %d", n)
	}
	if n := getEnvInt("CFG_TEST_INT_BAD", 42); n != 42 {
		t.Errorf("bad value should fall back to default, got %d", n)
	}
	if n := getEnvInt("CFG_TEST_INT_BAD", 7); n != 7 {
		t.Errorf("bad value should fall back to default, got %d", n)
	}
}

func TestGetEnvFloatFallback(t *testing.T) {
	t.Setenv("CFG_TEST_FLOAT_BAD", "not-a-number")
	if n := getEnvFloat("CFG_TEST_FLOAT_BAD", 2.5); n != 2.5 {
		t.Errorf("bad float should fall back to default, got %v", n)
	}
}

func TestGetEnvBoolFallback(t *testing.T) {
	t.Setenv("CFG_TEST_BOOL_UNSET", "")
	t.Setenv("CFG_TEST_BOOL_BAD", "not-a-bool")
	t.Setenv("CFG_TEST_BOOL_FALSE", "false")
	t.Setenv("CFG_TEST_BOOL_TRUE", "true")
	if got := getEnvBool("CFG_TEST_BOOL_UNSET", true); got != true {
		t.Errorf("empty value should fall back to default, got %v", got)
	}
	if got := getEnvBool("CFG_TEST_BOOL_BAD", true); got != true {
		t.Errorf("bad value should fall back to default, got %v", got)
	}
	if got := getEnvBool("CFG_TEST_BOOL_FALSE", true); got != false {
		t.Errorf("false value should parse, got %v", got)
	}
	if got := getEnvBool("CFG_TEST_BOOL_TRUE", false); got != true {
		t.Errorf("true value should parse, got %v", got)
	}
}

func TestSplitCSV(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"a", []string{"a"}},
		{"a, b ,c,", []string{"a", "b", "c"}},
		{"127.0.0.1/32,10.0.0.0/8", []string{"127.0.0.1/32", "10.0.0.0/8"}},
	}
	for _, c := range cases {
		got := splitCSV(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitCSV(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
