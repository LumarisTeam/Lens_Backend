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

	// 对象存储（腾讯云 COS，按 S3 风格环境变量配置）
	StorageProvider   string // STORAGE_PROVIDER，固定 s3
	StorageAccessKey  string // S3_ACCESS_KEY，COS SecretId
	StorageSecretKey  string // S3_SECRET_KEY，COS SecretKey
	StorageBucket     string // S3_BUCKET，桶名（含 appid）
	StorageRegion     string // S3_REGION，地域
	StorageEndpoint   string // S3_ENDPOINT，服务端点（对象操作不使用）
	StorageCDNURL     string // S3_CDN_URL，桶访问域名（含桶名）
	StorageBasePrefix string // S3_BASE_PREFIX，对象 key 前缀
	StorageBucketURL  string // 解析后的桶访问域名，供 COS Client 使用

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

		StorageProvider:   strings.ToLower(getEnv("STORAGE_PROVIDER", "s3")),
		StorageAccessKey:  getEnv("S3_ACCESS_KEY", ""),
		StorageSecretKey:  getEnv("S3_SECRET_KEY", ""),
		StorageBucket:     getEnv("S3_BUCKET", ""),
		StorageRegion:     getEnv("S3_REGION", "ap-guangzhou"),
		StorageEndpoint:   getEnv("S3_ENDPOINT", ""),
		StorageCDNURL:     getEnv("S3_CDN_URL", ""),
		StorageBasePrefix: strings.Trim(getEnv("S3_BASE_PREFIX", ""), "/"),

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
	if cfg.StorageAccessKey == "" {
		missing = append(missing, "S3_ACCESS_KEY")
	}
	if cfg.StorageSecretKey == "" {
		missing = append(missing, "S3_SECRET_KEY")
	}
	if cfg.StorageBucket == "" {
		missing = append(missing, "S3_BUCKET")
	}
	if cfg.AdminAPIToken == "" {
		missing = append(missing, "ADMIN_API_TOKEN")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required env vars: %s", strings.Join(missing, ", "))
	}

	// 桶访问域名：优先使用 S3_CDN_URL（含桶名的虚拟主机式域名），
	// 否则按 S3_BUCKET + S3_REGION 推导。
	// 注意：S3_ENDPOINT 是不含桶名的服务端点，仅用于服务级 API，对象操作不使用。
	cfg.StorageBucketURL = cfg.StorageCDNURL
	if cfg.StorageBucketURL == "" {
		cfg.StorageBucketURL = fmt.Sprintf("https://%s.cos.%s.myqcloud.com", cfg.StorageBucket, cfg.StorageRegion)
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
