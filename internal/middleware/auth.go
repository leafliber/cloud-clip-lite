package middleware

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/leaf/cloud-clip-lite/internal/auth"
	"github.com/leaf/cloud-clip-lite/internal/store"
)

// AuthContext 鉴权上下文，存储在 request context 中
type AuthContext struct {
	UserID   int64
	Username string
	Role     string
	DeviceID int64 // API Token 鉴权时有值
	AuthType string // "jwt" | "api_token"
}

type authContextKey struct{}

// GetAuthContext 从请求上下文获取鉴权信息
func GetAuthContext(ctx context.Context) *AuthContext {
	if v, ok := ctx.Value(authContextKey{}).(*AuthContext); ok {
		return v
	}
	return nil
}

// RequireAuth 鉴权中间件：支持 JWT 与 API Token 双凭证
// 优先检查 Authorization: Bearer <token>
// 若 token 能被 JWT 解析 → JWT 鉴权
// 否则尝试作为 API Token 查库 → API Token 鉴权
func RequireAuth(jwtMgr *auth.JWTManager, st *store.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			if token == "" {
				writeAuthError(w, "MISSING_CREDENTIALS", "缺少认证凭证", http.StatusUnauthorized)
				return
			}

			// 尝试 JWT
			claims, err := jwtMgr.ParseAccessToken(token)
			if err == nil {
				ctx := context.WithValue(r.Context(), authContextKey{}, &AuthContext{
					UserID:   claims.UserID,
					Username: claims.Username,
					Role:     claims.Role,
					AuthType: "jwt",
				})
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// JWT 失败，尝试 API Token
			tokenHash := auth.HashToken(token)
			dev, user, err := st.GetDeviceByAPIToken(r.Context(), tokenHash)
			if err != nil {
				writeAuthError(w, "INVALID_CREDENTIALS", "认证凭证无效", http.StatusUnauthorized)
				return
			}

			// 更新设备最后活跃时间（异步不阻塞）：
			// 带超时与错误日志，避免 goroutine 泄漏或优雅关闭后静默失败；
			// 本次认证查询已带出 last_seen_at，距今不足 1 分钟则跳过更新，降低重连风暴写压力
			if !lastSeenRecent(dev.LastSeenAt) {
				go func(deviceID int64) {
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					defer cancel()
					if err := st.UpdateDeviceLastSeen(ctx, deviceID); err != nil {
						slog.Default().Error("更新设备最后活跃时间失败", "error", err, "device_id", deviceID)
					}
				}(dev.ID)
			}

			ctx := context.WithValue(r.Context(), authContextKey{}, &AuthContext{
				UserID:   user.ID,
				Username: user.Username,
				Role:     user.Role,
				DeviceID: dev.ID,
				AuthType: "api_token",
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole 角色校验中间件（如 RequireAdmin）
func RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ac := GetAuthContext(r.Context())
			if ac == nil {
				writeAuthError(w, "NOT_AUTHENTICATED", "未认证", http.StatusUnauthorized)
				return
			}
			if ac.Role != role {
				writeAuthError(w, "FORBIDDEN", "权限不足", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAdmin 管理员校验快捷方法
func RequireAdmin() func(http.Handler) http.Handler {
	return RequireRole("admin")
}

// extractToken 从请求中提取 Token
// 优先 Authorization: Bearer <token>，其次 X-API-Token 头
func extractToken(r *http.Request) string {
	// X-API-Token 头（快捷指令专用）
	if token := r.Header.Get("X-API-Token"); token != "" {
		return token
	}

	// Authorization: Bearer <token>
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// writeAuthError 写认证错误响应
func writeAuthError(w http.ResponseWriter, code, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

// lastSeenRecent 判断设备的 last_seen_at 是否距今不足 1 分钟
// 解析失败（含 NULL）时按不新鲜处理，照常触发更新
func lastSeenRecent(lastSeen sql.NullString) bool {
	if !lastSeen.Valid {
		return false
	}
	t, ok := parseDBTime(lastSeen.String)
	if !ok {
		return false
	}
	return time.Since(t) < time.Minute
}

// parseDBTime 解析数据库存储的时间字符串
// 兼容 SQLite datetime('now')（UTC，无时区）与 PostgreSQL TIMESTAMPTZ（RFC3339Nano）
func parseDBTime(s string) (time.Time, bool) {
	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
