package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiter_Allow(t *testing.T) {
	rl := NewRateLimiter(60, 3) // 每分钟 60 次，突发 3

	// 突发 3 次应允许
	for i := 0; i < 3; i++ {
		if !rl.Allow("test-key") {
			t.Errorf("第 %d 次请求应被允许", i+1)
		}
	}

	// 第 4 次应被拒绝
	if rl.Allow("test-key") {
		t.Error("第 4 次请求应被拒绝")
	}
}

func TestRateLimiter_DifferentKeys(t *testing.T) {
	rl := NewRateLimiter(60, 2)

	// key1 用完
	rl.Allow("key1")
	rl.Allow("key1")

	// key2 应仍可用
	if !rl.Allow("key2") {
		t.Error("不同 key 应独立限流")
	}
}

func TestRateLimiter_Refill(t *testing.T) {
	rl := NewRateLimiter(6000, 1) // 每分钟 6000 次（每秒 100），突发 1

	// 用完突发额度
	rl.Allow("refill-key")

	// 等待一点时间让令牌恢复
	time.Sleep(20 * time.Millisecond)

	// 应该已经补充了令牌
	if !rl.Allow("refill-key") {
		t.Error("等待后应有新令牌")
	}
}

func TestRateLimit_Middleware(t *testing.T) {
	rl := NewRateLimiter(60, 2)

	handler := RateLimit(rl, func(r *http.Request) string {
		return "test-ip"
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// 前两次应 200
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/test", nil)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("第 %d 次请求状态码 = %d, 期望 200", i+1, rec.Code)
		}
	}

	// 第三次应 429
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("第 3 次请求状态码 = %d, 期望 429", rec.Code)
	}
}

func TestRateLimit_ByUser(t *testing.T) {
	rl := NewRateLimiter(60, 2)

	handler := RateLimitByUser(rl)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// 无 auth context 时按 IP 限流
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/test", nil)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("第 %d 次请求状态码 = %d, 期望 200", i+1, rec.Code)
		}
	}

	// 第三次应 429
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("第 3 次请求状态码 = %d, 期望 429", rec.Code)
	}
}
