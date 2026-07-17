package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/leaf/cloud-clip-lite/internal/auth"
	"github.com/leaf/cloud-clip-lite/internal/config"
	"github.com/leaf/cloud-clip-lite/internal/middleware"
	"github.com/leaf/cloud-clip-lite/internal/store"
)

// MeHandler 个人信息路由处理器
type MeHandler struct {
	cfg   *config.Config
	store *store.Store
	hasher *auth.PasswordHasher
	logger *slog.Logger
}

// NewMeHandler 创建个人信息处理器
func NewMeHandler(cfg *config.Config, st *store.Store, hasher *auth.PasswordHasher, log *slog.Logger) *MeHandler {
	return &MeHandler{cfg: cfg, store: st, hasher: hasher, logger: log}
}

// RegisterRoutes 注册个人信息路由（需鉴权）
func (h *MeHandler) RegisterRoutes(r chi.Router) {
	r.Get("/me", h.GetMe)
	r.Patch("/me", h.UpdateMe)
}

// GetMe 获取当前账号信息与配额用量
func (h *MeHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	ac := middleware.GetAuthContext(r.Context())
	if ac == nil {
		writeError(w, http.StatusUnauthorized, "NOT_AUTHENTICATED", "未认证")
		return
	}

	user, err := h.store.GetUserByID(r.Context(), ac.UserID)
	if err != nil {
		handleError(w, err)
		return
	}

	resp := map[string]any{
		"id":             user.ID,
		"username":       user.Username,
		"role":           user.Role,
		"status":         user.Status,
		"max_item_size":  user.MaxItemSize,
		"quota_bytes":    user.QuotaBytes,
		"retention_days": user.RetentionDays,
		"created_at":     user.CreatedAt,
	}
	if user.Email.Valid {
		resp["email"] = user.Email.String
	}

	writeJSON(w, http.StatusOK, resp)
}

// updateMeRequest 更新个人信息请求
type updateMeRequest struct {
	Password      *string `json:"password,omitempty"`
	OldPassword   *string `json:"old_password,omitempty"`
	Email         *string `json:"email,omitempty"`
	MaxItemSize   *int64  `json:"max_item_size,omitempty"`
	QuotaBytes    *int64  `json:"quota_bytes,omitempty"`
	RetentionDays *int    `json:"retention_days,omitempty"`
}

// UpdateMe 修改密码或个人设置
func (h *MeHandler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	ac := middleware.GetAuthContext(r.Context())
	if ac == nil {
		writeError(w, http.StatusUnauthorized, "NOT_AUTHENTICATED", "未认证")
		return
	}

	var req updateMeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "请求体格式错误")
		return
	}

	user, err := h.store.GetUserByID(r.Context(), ac.UserID)
	if err != nil {
		handleError(w, err)
		return
	}

	// 第一阶段：完成全部字段校验，不做任何写入，避免部分生效
	var newHash string
	if req.Password != nil {
		if req.OldPassword == nil {
			writeError(w, http.StatusBadRequest, "OLD_PASSWORD_REQUIRED", "修改密码需要提供旧密码")
			return
		}
		// 验证旧密码
		ok, err := h.hasher.Verify(*req.OldPassword, user.PasswordHash)
		if err != nil || !ok {
			writeError(w, http.StatusUnauthorized, "INVALID_OLD_PASSWORD", "旧密码错误")
			return
		}
		// 校验新密码
		if err := validatePassword(*req.Password); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_PASSWORD", err.Error())
			return
		}
		newHash, err = h.hasher.Hash(*req.Password)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "HASH_FAILED", "密码处理失败")
			return
		}
	}

	// 修改配额设置（仅管理员可改自己的配额，普通用户可改 retention_days）
	needsSettingsUpdate := false
	maxSize := user.MaxItemSize
	quota := user.QuotaBytes
	retention := user.RetentionDays

	if req.MaxItemSize != nil {
		if ac.Role != "admin" {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "仅管理员可修改单条上限")
			return
		}
		if *req.MaxItemSize <= 0 || *req.MaxItemSize > h.cfg.DefaultMaxItemSize*10 {
			writeError(w, http.StatusBadRequest, "INVALID_VALUE", "单条上限取值非法")
			return
		}
		maxSize = *req.MaxItemSize
		needsSettingsUpdate = true
	}

	if req.QuotaBytes != nil {
		if ac.Role != "admin" {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "仅管理员可修改总配额")
			return
		}
		if *req.QuotaBytes <= 0 {
			writeError(w, http.StatusBadRequest, "INVALID_VALUE", "总配额取值非法")
			return
		}
		quota = *req.QuotaBytes
		needsSettingsUpdate = true
	}

	if req.RetentionDays != nil {
		if *req.RetentionDays < 1 || *req.RetentionDays > 3650 {
			writeError(w, http.StatusBadRequest, "INVALID_VALUE", "保留天数需在 1-3650 之间")
			return
		}
		retention = *req.RetentionDays
		needsSettingsUpdate = true
	}

	// 第二阶段：校验全部通过，开始落库
	if req.Password != nil {
		if err := h.store.UpdateUserPassword(r.Context(), ac.UserID, newHash); err != nil {
			handleError(w, err)
			return
		}
		// 吊销所有 refresh token（强制重新登录）
		if err := h.store.RevokeAllRefreshTokensByUser(r.Context(), ac.UserID); err != nil {
			h.logger.Error("吊销用户 Refresh Token 失败", "error", err, "user_id", ac.UserID)
		}
	}

	// 修改邮箱（空串清除；唯一约束冲突由 handleError 映射为 409）
	if req.Email != nil {
		if err := h.store.UpdateUserEmail(r.Context(), ac.UserID, *req.Email); err != nil {
			handleError(w, err)
			return
		}
	}

	if needsSettingsUpdate {
		if err := h.store.UpdateUserSettings(r.Context(), ac.UserID, maxSize, quota, retention); err != nil {
			handleError(w, err)
			return
		}
	}

	// 返回更新后的用户
	updated, _ := h.store.GetUserByID(r.Context(), ac.UserID)
	resp := map[string]any{
		"id":             updated.ID,
		"username":       updated.Username,
		"role":           updated.Role,
		"status":         updated.Status,
		"max_item_size":  updated.MaxItemSize,
		"quota_bytes":    updated.QuotaBytes,
		"retention_days": updated.RetentionDays,
	}
	if updated.Email.Valid {
		resp["email"] = updated.Email.String
	}

	writeJSON(w, http.StatusOK, resp)
}
