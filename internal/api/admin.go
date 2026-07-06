package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/leaf/cloud-clip-lite/internal/auth"
	"github.com/leaf/cloud-clip-lite/internal/config"
	"github.com/leaf/cloud-clip-lite/internal/middleware"
	"github.com/leaf/cloud-clip-lite/internal/store"
	"github.com/leaf/cloud-clip-lite/internal/ws"
)

// AdminHandler 管理后台 API 处理器
type AdminHandler struct {
	cfg    *config.Config
	store  *store.Store
	hasher *auth.PasswordHasher
	logger *slog.Logger
	hub    *ws.Hub
}

// NewAdminHandler 创建管理后台处理器
func NewAdminHandler(cfg *config.Config, st *store.Store, hasher *auth.PasswordHasher, log *slog.Logger, hub *ws.Hub) *AdminHandler {
	return &AdminHandler{cfg: cfg, store: st, hasher: hasher, logger: log, hub: hub}
}

// RegisterRoutes 注册管理后台路由（需 admin 角色）
func (h *AdminHandler) RegisterRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAdmin())

		r.Get("/admin/stats", h.GetStats)
		r.Get("/admin/users", h.ListUsers)
		r.Patch("/admin/users/{id}", h.UpdateUser)
		r.Post("/admin/users/{id}/reset-password", h.ResetPassword)
		r.Post("/admin/users/{id}/force-logout", h.ForceLogout)
		r.Delete("/admin/users/{id}", h.DeleteUser)
		r.Get("/admin/audit-logs", h.ListAuditLogs)
	})
}

// GetStats GET /api/admin/stats — 系统统计
func (h *AdminHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.store.GetSystemStats(r.Context())
	if err != nil {
		h.logger.Error("查询系统统计失败", "error", err)
		writeError(w, http.StatusInternalServerError, "STATS_FAILED", "获取系统统计失败")
		return
	}

	// WS 在线连接数
	if h.hub != nil {
		stats.OnlineCount = h.hub.GetOnlineCount()
	}

	writeJSON(w, http.StatusOK, stats)
}

// ListUsers GET /api/admin/users?limit=&offset= — 用户列表
func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	users, err := h.store.ListUsers(r.Context(), limit, offset)
	if err != nil {
		h.logger.Error("查询用户列表失败", "error", err)
		writeError(w, http.StatusInternalServerError, "LIST_FAILED", "查询用户列表失败")
		return
	}

	// 转换为响应格式（不含密码哈希）
	items := make([]map[string]any, 0, len(users))
	for _, u := range users {
		items = append(items, adminUserResponse(u))
	}

	total, _ := h.store.CountUsers(r.Context())

	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// UpdateUser PATCH /api/admin/users/:id — 更新用户角色/状态/配额
func (h *AdminHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "用户 ID 无效")
		return
	}

	var req struct {
		Role          *string `json:"role"`
		Status        *string `json:"status"`
		MaxItemSize   *int64  `json:"max_item_size"`
		QuotaBytes    *int64  `json:"quota_bytes"`
		RetentionDays *int    `json:"retention_days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "请求体格式错误")
		return
	}

	// 检查目标用户存在
	target, err := h.store.GetUserByID(r.Context(), id)
	if err != nil {
		handleError(w, err)
		return
	}

	// 逐字段更新
	ac := middleware.GetAuthContext(r.Context())

	if req.Role != nil {
		if *req.Role != "user" && *req.Role != "admin" {
			writeError(w, http.StatusBadRequest, "INVALID_ROLE", "角色必须为 user 或 admin")
			return
		}
		if err := h.store.UpdateUserRole(r.Context(), id, *req.Role); err != nil {
			h.logger.Error("更新用户角色失败", "error", err)
			writeError(w, http.StatusInternalServerError, "UPDATE_FAILED", "更新角色失败")
			return
		}
		h.audit(r, ac, "admin.user.role_update", target.Username)
	}

	if req.Status != nil {
		if *req.Status != "active" && *req.Status != "disabled" {
			writeError(w, http.StatusBadRequest, "INVALID_STATUS", "状态必须为 active 或 disabled")
			return
		}
		if err := h.store.UpdateUserStatus(r.Context(), id, *req.Status); err != nil {
			h.logger.Error("更新用户状态失败", "error", err)
			writeError(w, http.StatusInternalServerError, "UPDATE_FAILED", "更新状态失败")
			return
		}
		h.audit(r, ac, "admin.user.status_update", target.Username)
	}

	if req.MaxItemSize != nil || req.QuotaBytes != nil || req.RetentionDays != nil {
		maxSize := target.MaxItemSize
		quota := target.QuotaBytes
		retention := target.RetentionDays
		if req.MaxItemSize != nil {
			maxSize = *req.MaxItemSize
		}
		if req.QuotaBytes != nil {
			quota = *req.QuotaBytes
		}
		if req.RetentionDays != nil {
			retention = *req.RetentionDays
		}
		if err := h.store.UpdateUserSettings(r.Context(), id, maxSize, quota, retention); err != nil {
			h.logger.Error("更新用户配额失败", "error", err)
			writeError(w, http.StatusInternalServerError, "UPDATE_FAILED", "更新配额失败")
			return
		}
		h.audit(r, ac, "admin.user.quota_update", target.Username)
	}

	// 返回更新后的用户
	updated, _ := h.store.GetUserByID(r.Context(), id)
	writeJSON(w, http.StatusOK, adminUserResponse(updated))
}

// ResetPassword POST /api/admin/users/:id/reset-password — 管理员重置密码
func (h *AdminHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "用户 ID 无效")
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "请求体格式错误")
		return
	}

	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "WEAK_PASSWORD", "密码至少 8 个字符")
		return
	}

	target, err := h.store.GetUserByID(r.Context(), id)
	if err != nil {
		handleError(w, err)
		return
	}

	hash, err := h.hasher.Hash(req.Password)
	if err != nil {
		h.logger.Error("哈希密码失败", "error", err)
		writeError(w, http.StatusInternalServerError, "HASH_FAILED", "密码处理失败")
		return
	}

	if err := h.store.UpdateUserPassword(r.Context(), id, hash); err != nil {
		h.logger.Error("重置密码失败", "error", err)
		writeError(w, http.StatusInternalServerError, "UPDATE_FAILED", "重置密码失败")
		return
	}

	// 强制下线：吊销所有 refresh token
	_ = h.store.RevokeAllRefreshTokensByUser(r.Context(), id)

	ac := middleware.GetAuthContext(r.Context())
	h.audit(r, ac, "admin.user.reset_password", target.Username)

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "message": "密码已重置，用户需重新登录"})
}

// ForceLogout POST /api/admin/users/:id/force-logout — 强制下线
func (h *AdminHandler) ForceLogout(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "用户 ID 无效")
		return
	}

	target, err := h.store.GetUserByID(r.Context(), id)
	if err != nil {
		handleError(w, err)
		return
	}

	// 吊销所有 refresh token
	if err := h.store.RevokeAllRefreshTokensByUser(r.Context(), id); err != nil {
		h.logger.Error("强制下线失败", "error", err)
		writeError(w, http.StatusInternalServerError, "FORCE_LOGOUT_FAILED", "强制下线失败")
		return
	}

	ac := middleware.GetAuthContext(r.Context())
	h.audit(r, ac, "admin.user.force_logout", target.Username)

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "message": "用户已被强制下线"})
}

// DeleteUser DELETE /api/admin/users/:id — 删除用户
func (h *AdminHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "用户 ID 无效")
		return
	}

	ac := middleware.GetAuthContext(r.Context())

	// 禁止删除自己
	if ac.UserID == id {
		writeError(w, http.StatusBadRequest, "SELF_DELETE", "不能删除当前登录账号")
		return
	}

	target, err := h.store.GetUserByID(r.Context(), id)
	if err != nil {
		handleError(w, err)
		return
	}

	if err := h.store.DeleteUser(r.Context(), id); err != nil {
		h.logger.Error("删除用户失败", "error", err)
		writeError(w, http.StatusInternalServerError, "DELETE_FAILED", "删除用户失败")
		return
	}

	h.audit(r, ac, "admin.user.delete", target.Username)

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "message": "用户已删除"})
}

// ListAuditLogs GET /api/admin/audit-logs?user_id=&action=&limit=&offset= — 审计日志
func (h *AdminHandler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	userID, _ := strconv.ParseInt(r.URL.Query().Get("user_id"), 10, 64)
	action := r.URL.Query().Get("action")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	logs, err := h.store.ListAuditLogs(r.Context(), userID, action, limit, offset)
	if err != nil {
		h.logger.Error("查询审计日志失败", "error", err)
		writeError(w, http.StatusInternalServerError, "LIST_FAILED", "查询审计日志失败")
		return
	}

	total, _ := h.store.CountAuditLogs(r.Context(), userID, action)

	items := make([]map[string]any, 0, len(logs))
	for _, log := range logs {
		item := map[string]any{
			"id":         log.ID,
			"action":     log.Action,
			"created_at": log.CreatedAt,
		}
		if log.UserID.Valid {
			item["user_id"] = log.UserID.Int64
		}
		if log.DeviceID.Valid {
			item["device_id"] = log.DeviceID.Int64
		}
		if log.Target.Valid {
			item["target"] = log.Target.String
		}
		if log.IP.Valid {
			item["ip"] = log.IP.String
		}
		if log.Meta.Valid && log.Meta.String != "" {
			var metaObj map[string]any
			if err := json.Unmarshal([]byte(log.Meta.String), &metaObj); err == nil {
				item["meta"] = metaObj
			}
		}
		items = append(items, item)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// audit 写入审计日志（异步）
func (h *AdminHandler) audit(r *http.Request, ac *middleware.AuthContext, action, target string) {
	if ac == nil {
		return
	}
	log := &store.AuditLog{
		Action: action,
		UserID: sql.NullInt64{Int64: ac.UserID, Valid: true},
		Target: sql.NullString{String: target, Valid: target != ""},
		IP:     sql.NullString{String: r.RemoteAddr, Valid: r.RemoteAddr != ""},
		UA:     sql.NullString{String: r.UserAgent(), Valid: r.UserAgent() != ""},
	}
	go h.store.CreateAuditLog(context.Background(), log)
}

// adminUserResponse 管理后台用户响应（不含密码哈希）
func adminUserResponse(u *store.User) map[string]any {
	resp := map[string]any{
		"id":             u.ID,
		"username":       u.Username,
		"role":           u.Role,
		"status":         u.Status,
		"max_item_size":  u.MaxItemSize,
		"quota_bytes":    u.QuotaBytes,
		"retention_days": u.RetentionDays,
		"created_at":     u.CreatedAt,
		"updated_at":     u.UpdatedAt,
	}
	if u.Email.Valid {
		resp["email"] = u.Email.String
	}
	return resp
}
