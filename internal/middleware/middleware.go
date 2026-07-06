package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// contextKey 上下文键类型
type contextKey string

const (
	// CtxKeyRequestID 请求 ID 上下文键
	CtxKeyRequestID contextKey = "request_id"
)

// RequestID 中间件：为每个请求生成唯一 ID 并注入上下文
func RequestID(next http.Handler) http.Handler {
	return middleware.RequestID(next)
}

// RealIP 解析真实客户端 IP（代理后）
func RealIP(next http.Handler) http.Handler {
	return middleware.RealIP(next)
}

// Logger 结构化请求日志中间件
func Logger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic 恢复",
						"error", rec,
						"stack", string(debug.Stack()),
						"path", r.URL.Path,
					)
					http.Error(ww, "内部服务器错误", http.StatusInternalServerError)
				}

				reqID := middleware.GetReqID(r.Context())
				logger.Info("请求",
					"request_id", reqID,
					"method", r.Method,
					"path", r.URL.Path,
					"status", ww.Status(),
					"bytes", ww.BytesWritten(),
					"duration_ms", time.Since(start).Milliseconds(),
					"remote_addr", r.RemoteAddr,
				)
			}()

			next.ServeHTTP(ww, r)
		}
		return http.HandlerFunc(fn)
	}
}

// Recover panic 恢复中间件（独立版，与 Logger 内置的重复保护互补）
func Recover(next http.Handler) http.Handler {
	return middleware.Recoverer(next)
}

// CORS 跨域中间件
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	allowAll := false
	originsSet := make(map[string]bool)
	for _, o := range allowedOrigins {
		if o == "*" {
			allowAll = true
			break
		}
		originsSet[o] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				origin = "*"
			}

			if allowAll || originsSet[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Token, X-Requested-With")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Max-Age", "3600")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Timeout 请求超时中间件
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, d, `{"error":{"code":"REQUEST_TIMEOUT","message":"请求超时"}}`)
	}
}

// RequestIDFromContext 从上下文提取请求 ID
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(CtxKeyRequestID).(string); ok {
		return v
	}
	// chi 的 middleware.GetReqID
	return middleware.GetReqID(ctx)
}
