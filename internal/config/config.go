package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config 应用配置
type Config struct {
	// 服务器
	Port            string        `env:"PORT"`
	Env             string        `env:"ENV"`             // development | production
	ReadTimeout     time.Duration `env:"READ_TIMEOUT"`    // 读超时
	WriteTimeout    time.Duration `env:"WRITE_TIMEOUT"`   // 写超时
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT"` // 优雅关闭超时
	AllowedOrigins  []string      `env:"ALLOWED_ORIGINS"`  // CORS 允许的源

	// 数据库
	DatabaseURL string `env:"DATABASE_URL"` // 空则使用 SQLite
	SQLitePath  string `env:"SQLITE_PATH"`  // SQLite 文件路径

	// 对象存储
	BlobStore     string `env:"BLOB_STORE"`      // local | s3
	BlobLocalDir  string `env:"BLOB_LOCAL_DIR"`  // 本地存储目录
	S3Endpoint    string `env:"S3_ENDPOINT"`
	S3Bucket      string `env:"S3_BUCKET"`
	S3Key         string `env:"S3_KEY"`
	S3Secret      string `env:"S3_SECRET"`
	S3Region      string `env:"S3_REGION"`
	S3UsePathStyle bool  `env:"S3_USE_PATH_STYLE"`

	// 鉴权
	JWTSecret         string        `env:"JWT_SECRET"`
	AccessTTL         time.Duration `env:"ACCESS_TTL"`
	RefreshTTL        time.Duration `env:"REFRESH_TTL"`
	Argon2Memory      uint32        `env:"ARGON2_MEMORY"`      // KiB
	Argon2Iterations  uint32        `env:"ARGON2_ITERATIONS"`
	Argon2Parallelism uint8         `env:"ARGON2_PARALLELISM"`

	// 注册策略：closed | invite | open
	AllowRegister string `env:"ALLOW_REGISTER"`

	// 默认用户配额
	DefaultMaxItemSize   int64 `env:"DEFAULT_MAX_ITEM_SIZE"`   // 字节
	DefaultQuotaBytes    int64 `env:"DEFAULT_QUOTA_BYTES"`     // 字节
	DefaultRetentionDays int   `env:"DEFAULT_RETENTION_DAYS"`

	// 限流
	RateLimitPerMinute int `env:"RATE_LIMIT_PER_MINUTE"`

	// 日志
	LogLevel  string `env:"LOG_LEVEL"`  // debug | info | warn | error
	LogFormat string `env:"LOG_FORMAT"` // json | text

	// 管理员引导（首次启动）
	AdminUsername string `env:"ADMIN_USERNAME"`
	AdminPassword string `env:"ADMIN_PASSWORD"`
}

// Load 加载配置：优先环境变量，缺失时回退 .env 文件
func Load() (*Config, error) {
	// .env 文件可选（生产环境用纯环境变量）
	_ = godotenv.Load()

	cfg := &Config{
		Port:            getEnv("PORT", "8080"),
		Env:             getEnv("ENV", "development"),
		ReadTimeout:     getEnvDuration("READ_TIMEOUT", 15*time.Second),
		WriteTimeout:    getEnvDuration("WRITE_TIMEOUT", 30*time.Second),
		ShutdownTimeout: getEnvDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
		AllowedOrigins:  getEnvSlice("ALLOWED_ORIGINS", []string{"*"}),

		DatabaseURL: getEnv("DATABASE_URL", ""),
		SQLitePath:  getEnv("SQLITE_PATH", "./data/clipboard.db"),

		BlobStore:      getEnv("BLOB_STORE", "local"),
		BlobLocalDir:   getEnv("BLOB_LOCAL_DIR", "./data/blobs"),
		S3Endpoint:     getEnv("S3_ENDPOINT", ""),
		S3Bucket:       getEnv("S3_BUCKET", ""),
		S3Key:          getEnv("S3_KEY", ""),
		S3Secret:       getEnv("S3_SECRET", ""),
		S3Region:       getEnv("S3_REGION", ""),
		S3UsePathStyle: getEnvBool("S3_USE_PATH_STYLE", true),

		JWTSecret:         getEnv("JWT_SECRET", ""),
		AccessTTL:         getEnvDuration("ACCESS_TTL", 15*time.Minute),
		RefreshTTL:        getEnvDuration("REFRESH_TTL", 720*time.Hour),
		Argon2Memory:      uint32(getEnvInt("ARGON2_MEMORY", 65536)),
		Argon2Iterations:  uint32(getEnvInt("ARGON2_ITERATIONS", 3)),
		Argon2Parallelism: uint8(getEnvInt("ARGON2_PARALLELISM", 2)),

		AllowRegister: getEnv("ALLOW_REGISTER", "closed"),

		DefaultMaxItemSize:   int64(getEnvInt("DEFAULT_MAX_ITEM_SIZE", 10485760)),
		DefaultQuotaBytes:    int64(getEnvInt("DEFAULT_QUOTA_BYTES", 1073741824)),
		DefaultRetentionDays: getEnvInt("DEFAULT_RETENTION_DAYS", 30),

		RateLimitPerMinute: getEnvInt("RATE_LIMIT_PER_MINUTE", 60),

		LogLevel:  getEnv("LOG_LEVEL", "info"),
		LogFormat: getEnv("LOG_FORMAT", "json"),

		AdminUsername: getEnv("ADMIN_USERNAME", ""),
		AdminPassword: getEnv("ADMIN_PASSWORD", ""),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate 校验必填项与取值合法性
func (c *Config) Validate() error {
	if c.JWTSecret == "" {
		return fmt.Errorf("JWT_SECRET 必填，请设置随机字符串（≥32 字节）")
	}
	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("JWT_SECRET 长度不足，建议 ≥32 字节")
	}
	switch c.AllowRegister {
	case "closed", "invite", "open":
	default:
		return fmt.Errorf("ALLOW_REGISTER 取值非法，应为 closed|invite|open，当前: %s", c.AllowRegister)
	}
	switch c.BlobStore {
	case "local", "s3":
	default:
		return fmt.Errorf("BLOB_STORE 取值非法，应为 local|s3，当前: %s", c.BlobStore)
	}
	if c.BlobStore == "s3" {
		if c.S3Bucket == "" || c.S3Key == "" || c.S3Secret == "" {
			return fmt.Errorf("BLOB_STORE=s3 时 S3_BUCKET/S3_KEY/S3_SECRET 必填")
		}
	}
	if c.DefaultMaxItemSize <= 0 {
		return fmt.Errorf("DEFAULT_MAX_ITEM_SIZE 必须 > 0")
	}
	if c.DefaultQuotaBytes <= 0 {
		return fmt.Errorf("DEFAULT_QUOTA_BYTES 必须 > 0")
	}
	if c.DefaultRetentionDays <= 0 {
		return fmt.Errorf("DEFAULT_RETENTION_DAYS 必须 > 0")
	}
	if c.AccessTTL <= 0 {
		return fmt.Errorf("ACCESS_TTL 必须 > 0")
	}
	if c.RefreshTTL <= 0 {
		return fmt.Errorf("REFRESH_TTL 必须 > 0")
	}
	// argon2.Key 对非法参数直接 panic，需在配置阶段拦截
	if c.Argon2Iterations < 1 {
		return fmt.Errorf("ARGON2_ITERATIONS 必须 ≥ 1，为 0 会导致 argon2 计算 panic")
	}
	if c.Argon2Parallelism < 1 {
		return fmt.Errorf("ARGON2_PARALLELISM 必须在 1..255 之间（0 或 >255 被截断为 0 会导致 argon2 计算 panic）")
	}
	// 单位 KiB：下限 8MiB（argon2 安全基线），上限 4GiB（防止误配导致单次哈希 OOM）
	if c.Argon2Memory < 8*1024 || c.Argon2Memory > 4*1024*1024 {
		return fmt.Errorf("ARGON2_MEMORY 必须在 8192..4194304 KiB（8MiB..4GiB）之间，当前: %d", c.Argon2Memory)
	}
	if c.RateLimitPerMinute <= 0 {
		return fmt.Errorf("RATE_LIMIT_PER_MINUTE 必须 > 0")
	}
	return nil
}

// IsProduction 是否生产环境
func (c *Config) IsProduction() bool { return c.Env == "production" }

// IsSQLite 是否使用 SQLite
func (c *Config) IsSQLite() bool { return c.DatabaseURL == "" }

// ---------- 辅助函数 ----------

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func getEnvSlice(key string, def []string) []string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		parts := strings.Split(v, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	}
	return def
}
