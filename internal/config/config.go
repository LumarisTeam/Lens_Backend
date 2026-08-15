// Package config 通过 os 环境变量加载配置（禁止使用 Viper）。
// 必填项缺失时返回错误，由 main 输出明确日志后退出；
// 敏感信息（SecretKey、AdminToken）只在此处读取，绝不打印。
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config 汇总全部运行配置。
type Config struct {
	AppEnv         string
	HTTPAddr       string
	DBDSN          string
	TrustedProxies []string

	CosRegion    string
	CosBucket    string
	CosEndpoint  string
	CosAccessKey string
	CosSecretKey string

	PresignGetExpireSeconds    int
	UploadPresignExpireSeconds int
	ImageMaxSize               int64
	ImageMaxCount              int
	ContentMaxLength           int
	ContactMaxLength           int
	OrphanImageRetainHours     int
	AdminAPIToken              string

	RateLimitRPS       float64
	RateLimitBurst     int
	FeedbackDailyLimit int
}

// Load 读取并校验全部配置。
func Load() (*Config, error) {
	cfg := &Config{
		AppEnv:   getEnv("APP_ENV", "development"),
		HTTPAddr: getEnv("HTTP_ADDR", ":8080"),

		CosRegion:    getEnv("COS_REGION", "ap-guangzhou"),
		CosBucket:    getEnv("COS_BUCKET", ""),
		CosEndpoint:  getEnv("COS_ENDPOINT", ""),
		CosAccessKey: getEnv("COS_ACCESS_KEY", ""),
		CosSecretKey: getEnv("COS_SECRET_KEY", ""),

		PresignGetExpireSeconds:    getEnvInt("PRESIGN_GET_EXPIRE_SECONDS", 900),
		UploadPresignExpireSeconds: getEnvInt("UPLOAD_PRESIGN_EXPIRE_SECONDS", 900),
		ImageMaxSize:               getEnvInt64("IMAGE_MAX_SIZE", 5242880),
		ImageMaxCount:              getEnvInt("IMAGE_MAX_COUNT", 6),
		ContentMaxLength:           getEnvInt("CONTENT_MAX_LENGTH", 2000),
		ContactMaxLength:           getEnvInt("CONTACT_MAX_LENGTH", 128),
		OrphanImageRetainHours:     getEnvInt("ORPHAN_IMAGE_RETAIN_HOURS", 24),
		AdminAPIToken:              getEnv("ADMIN_API_TOKEN", ""),

		RateLimitRPS:       getEnvFloat("RATE_LIMIT_RPS", 5),
		RateLimitBurst:     getEnvInt("RATE_LIMIT_BURST", 10),
		FeedbackDailyLimit: getEnvInt("FEEDBACK_DAILY_LIMIT", 100),
	}

	cfg.TrustedProxies = splitCSV(getEnv("TRUSTED_PROXIES", ""))

	// 必填校验
	var missing []string
	if v := getEnv("DB_DSN", ""); v != "" {
		cfg.DBDSN = v
	} else {
		missing = append(missing, "DB_DSN")
	}
	if cfg.CosBucket == "" {
		missing = append(missing, "COS_BUCKET")
	}
	if cfg.CosAccessKey == "" {
		missing = append(missing, "COS_ACCESS_KEY")
	}
	if cfg.CosSecretKey == "" {
		missing = append(missing, "COS_SECRET_KEY")
	}
	if cfg.AdminAPIToken == "" {
		missing = append(missing, "ADMIN_API_TOKEN")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required env vars: %s", strings.Join(missing, ", "))
	}

	// COS endpoint：未显式配置时按 bucket+region 推导标准域名。
	if cfg.CosEndpoint == "" {
		cfg.CosEndpoint = fmt.Sprintf("https://%s.cos.%s.myqcloud.com", cfg.CosBucket, cfg.CosRegion)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// validate 校验数值型配置的合法性。
func (c *Config) validate() error {
	if c.PresignGetExpireSeconds <= 0 || c.UploadPresignExpireSeconds <= 0 {
		return fmt.Errorf("presign expire seconds must be positive")
	}
	if c.ImageMaxSize <= 0 {
		return fmt.Errorf("IMAGE_MAX_SIZE must be positive")
	}
	if c.ImageMaxCount <= 0 {
		return fmt.Errorf("IMAGE_MAX_COUNT must be positive")
	}
	if c.RateLimitRPS <= 0 || c.RateLimitBurst <= 0 {
		return fmt.Errorf("rate limit config must be positive")
	}
	if c.FeedbackDailyLimit <= 0 {
		return fmt.Errorf("FEEDBACK_DAILY_LIMIT must be positive")
	}
	return nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func getEnvFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return n
		}
	}
	return def
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
