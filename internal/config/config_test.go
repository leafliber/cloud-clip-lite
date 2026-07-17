package config

import (
	"strings"
	"testing"
	"time"
)

// validConfig 返回一份可通过 Validate 的基准配置
func validConfig() *Config {
	return &Config{
		JWTSecret:            "test-secret-at-least-32-bytes-long!!!",
		AllowRegister:        "closed",
		BlobStore:            "local",
		AccessTTL:            15 * time.Minute,
		RefreshTTL:           720 * time.Hour,
		Argon2Memory:         65536,
		Argon2Iterations:     3,
		Argon2Parallelism:    2,
		DefaultMaxItemSize:   10485760,
		DefaultQuotaBytes:    1073741824,
		DefaultRetentionDays: 30,
		RateLimitPerMinute:   60,
	}
}

func TestValidate_Valid(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("基准配置应通过校验: %v", err)
	}
}

func TestValidate_NumericParams(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(c *Config)
		wantErr string
	}{
		{"AccessTTL 为零", func(c *Config) { c.AccessTTL = 0 }, "ACCESS_TTL"},
		{"AccessTTL 为负", func(c *Config) { c.AccessTTL = -time.Minute }, "ACCESS_TTL"},
		{"RefreshTTL 为零", func(c *Config) { c.RefreshTTL = 0 }, "REFRESH_TTL"},
		{"RefreshTTL 为负", func(c *Config) { c.RefreshTTL = -time.Hour }, "REFRESH_TTL"},
		{"Argon2Iterations 为零", func(c *Config) { c.Argon2Iterations = 0 }, "ARGON2_ITERATIONS"},
		// uint8 字段，256 在 Load 时已被截断为 0，这里直接验证截断后的值
		{"Argon2Parallelism 为零", func(c *Config) { c.Argon2Parallelism = 0 }, "ARGON2_PARALLELISM"},
		{"Argon2Memory 低于下限", func(c *Config) { c.Argon2Memory = 8*1024 - 1 }, "ARGON2_MEMORY"},
		{"Argon2Memory 高于上限", func(c *Config) { c.Argon2Memory = 4*1024*1024 + 1 }, "ARGON2_MEMORY"},
		// 负数 KiB 经 uint32 转换后约为 4TiB（^uint32(0) 即 uint32(-1)），必须被拦截
		{"Argon2Memory 负数转换后", func(c *Config) { c.Argon2Memory = ^uint32(0) }, "ARGON2_MEMORY"},
		{"RateLimitPerMinute 为零", func(c *Config) { c.RateLimitPerMinute = 0 }, "RATE_LIMIT_PER_MINUTE"},
		{"RateLimitPerMinute 为负", func(c *Config) { c.RateLimitPerMinute = -1 }, "RATE_LIMIT_PER_MINUTE"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			tc.mutate(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("应校验失败（%s），实际通过", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("错误信息应包含 %q，实际: %v", tc.wantErr, err)
			}
		})
	}
}

func TestValidate_NumericBoundaryOK(t *testing.T) {
	// 边界合法值应通过
	cfg := validConfig()
	cfg.Argon2Memory = 8 * 1024
	cfg.Argon2Iterations = 1
	cfg.Argon2Parallelism = 255
	cfg.RateLimitPerMinute = 1
	if err := cfg.Validate(); err != nil {
		t.Errorf("边界合法值应通过校验: %v", err)
	}

	cfg = validConfig()
	cfg.Argon2Memory = 4 * 1024 * 1024
	if err := cfg.Validate(); err != nil {
		t.Errorf("Argon2Memory 上限值应通过校验: %v", err)
	}
}
