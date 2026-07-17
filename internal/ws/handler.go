package ws

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/leaf/cloud-clip-lite/internal/auth"
	"github.com/leaf/cloud-clip-lite/internal/store"
)

// Handler WebSocket HTTP 处理器
// 负责：提取 token → 鉴权 → 升级 WebSocket → 创建 Connection
type Handler struct {
	hub      *Hub
	jwtMgr   *auth.JWTManager
	store    *store.Store
	upgrader websocket.Upgrader
	logger   *slog.Logger

	// syncFunc 增量同步回调
	syncFunc SyncFunc
}

// NewHandler 创建 WebSocket 处理器
func NewHandler(hub *Hub, jwtMgr *auth.JWTManager, st *store.Store, allowedOrigins []string, logger *slog.Logger) *Handler {
	allowAll := false
	originSet := make(map[string]bool)
	for _, o := range allowedOrigins {
		if o == "*" {
			allowAll = true
			break
		}
		originSet[o] = true
	}

	h := &Handler{
		hub:    hub,
		jwtMgr: jwtMgr,
		store:  st,
		logger: logger,
	}

	h.upgrader = websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin: func(r *http.Request) bool {
			if allowAll {
				return true
			}
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true // 非浏览器客户端
			}
			return originSet[origin]
		},
	}

	// 设置增量同步回调
	h.syncFunc = h.syncClipItems

	return h
}

// ServeHTTP 处理 WebSocket 升级请求
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 1. 提取 token
	token := h.extractToken(r)
	if token == "" {
		h.writeWSError(w, http.StatusUnauthorized, "MISSING_CREDENTIALS", "缺少认证凭证")
		return
	}

	// 2. 鉴权（优先 JWT，其次 API Token）
	userID, username, deviceID, err := h.authenticate(r.Context(), token)
	if err != nil {
		h.writeWSError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "认证凭证无效")
		return
	}

	// 3. 升级 WebSocket
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Debug("WS 升级失败", "error", err, "user_id", userID)
		return
	}

	// 4. 创建并启动连接（阻塞）
	wsConn := NewConnection(conn, h.hub, userID, username, deviceID, h.logger, h.syncFunc)
	wsConn.Start()
}

// extractToken 从请求中提取 token
// 优先级：Authorization: Bearer > X-API-Token > ?token= 查询参数
func (h *Handler) extractToken(r *http.Request) string {
	// Authorization: Bearer <token>
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return strings.TrimSpace(parts[1])
		}
	}

	// X-API-Token 头
	if token := r.Header.Get("X-API-Token"); token != "" {
		return token
	}

	// ?token= 查询参数（浏览器 WS 无法设置 Authorization 头时的兜底）
	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}

	return ""
}

// authenticate 鉴权：先尝试 JWT，再尝试 API Token
func (h *Handler) authenticate(ctx context.Context, token string) (userID int64, username string, deviceID int64, err error) {
	// 尝试 JWT
	claims, jwtErr := h.jwtMgr.ParseAccessToken(token)
	if jwtErr == nil {
		return claims.UserID, claims.Username, 0, nil
	}

	// JWT 失败，尝试 API Token
	tokenHash := auth.HashToken(token)
	dev, user, apiErr := h.store.GetDeviceByAPIToken(ctx, tokenHash)
	if apiErr != nil {
		return 0, "", 0, apiErr
	}

	// 异步更新设备最后活跃时间：带超时与错误日志，避免 goroutine 泄漏
	// 或优雅关闭后静默失败；last_seen_at 距今不足 1 分钟则跳过，降低重连风暴写压力
	if !lastSeenRecent(dev.LastSeenAt) {
		go func(deviceID int64) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := h.store.UpdateDeviceLastSeen(ctx, deviceID); err != nil {
				h.logger.Error("更新设备最后活跃时间失败", "error", err, "device_id", deviceID)
			}
		}(dev.ID)
	}

	return user.ID, user.Username, dev.ID, nil
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

// syncClipItems 增量同步：查询 sinceID 之后的新条目
func (h *Handler) syncClipItems(userID, sinceID int64) ([]map[string]any, error) {
	ctx := context.Background()
	items, err := h.store.ListClipItemsSince(ctx, userID, sinceID, syncLimit)
	if err != nil {
		return nil, err
	}

	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, clipItemToMap(item))
	}
	return result, nil
}

// clipItemToMap 将 store.ClipItem 转为响应 map
func clipItemToMap(item *store.ClipItem) map[string]any {
	resp := map[string]any{
		"id":         item.ID,
		"type":       item.Type,
		"size":       item.Size,
		"created_at": item.CreatedAt,
	}
	if item.MimeType.Valid {
		resp["mime_type"] = item.MimeType.String
	}
	if item.BlobKey.Valid {
		resp["has_blob"] = true
	}
	if item.TextContent.Valid {
		resp["text"] = item.TextContent.String
	}
	if item.SHA256.Valid {
		resp["sha256"] = item.SHA256.String
	}
	if item.ExpiresAt.Valid {
		resp["expires_at"] = item.ExpiresAt.String
	}
	if item.Meta != "" && item.Meta != "{}" {
		var metaObj map[string]any
		if err := json.Unmarshal([]byte(item.Meta), &metaObj); err == nil {
			resp["meta"] = metaObj
		}
	}
	return resp
}

// writeWSError 写 WebSocket 升级前的错误响应（HTTP JSON）
func (h *Handler) writeWSError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

// GenerateConnID 生成连接 ID（用于日志追踪）
func GenerateConnID() string {
	return uuid.New().String()[:8]
}
