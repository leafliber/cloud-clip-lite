package middleware

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RateLimiter 令牌桶限流器
// 按用户 ID + IP 维度分别限流，支持不同操作类型独立配额
type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*tokenBucket
	rate     int           // 每分钟请求数
	burst    int           // 突发上限
	cleanupT time.Duration // 清理间隔
}

type tokenBucket struct {
	tokens   float64
	lastTime time.Time
}

// NewRateLimiter 创建限流器
// rate: 每分钟允许的请求数, burst: 突发上限
func NewRateLimiter(rate, burst int) *RateLimiter {
	rl := &RateLimiter{
		buckets:  make(map[string]*tokenBucket),
		rate:     rate,
		burst:    burst,
		cleanupT: 5 * time.Minute,
	}
	go rl.cleanupLoop()
	return rl
}

// Allow 检查是否允许请求
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, exists := rl.buckets[key]
	if !exists {
		b = &tokenBucket{
			tokens:   float64(rl.burst),
			lastTime: now,
		}
		rl.buckets[key] = b
	}

	// 补充令牌（按速率）
	elapsed := now.Sub(b.lastTime).Seconds()
	refill := elapsed * float64(rl.rate) / 60.0
	b.tokens += refill
	if b.tokens > float64(rl.burst) {
		b.tokens = float64(rl.burst)
	}
	b.lastTime = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// cleanupLoop 定期清理过期的桶
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.cleanupT)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		cutoff := time.Now().Add(-rl.cleanupT)
		for k, b := range rl.buckets {
			if b.lastTime.Before(cutoff) {
				delete(rl.buckets, k)
			}
		}
		rl.mu.Unlock()
	}
}

// RateLimit 限流中间件
// keyFn 从请求中提取限流键（如 user_id 或 IP）
func RateLimit(limiter *RateLimiter, keyFn func(r *http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFn(r)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}

			if !limiter.Allow(key) {
				w.Header().Set("Retry-After", strconv.Itoa(60))
				writeAuthError(w, "RATE_LIMITED", "请求过于频繁，请稍后再试", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RateLimitByUser 按用户 ID 限流
func RateLimitByUser(limiter *RateLimiter) func(http.Handler) http.Handler {
	return RateLimit(limiter, func(r *http.Request) string {
		ac := GetAuthContext(r.Context())
		if ac != nil {
			return "user:" + strconv.FormatInt(ac.UserID, 10)
		}
		return "ip:" + r.RemoteAddr
	})
}

// RateLimitByIP 按 IP 限流
func RateLimitByIP(limiter *RateLimiter) func(http.Handler) http.Handler {
	return RateLimit(limiter, func(r *http.Request) string {
		return "ip:" + r.RemoteAddr
	})
}
