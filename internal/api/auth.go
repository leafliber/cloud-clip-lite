package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/leaf/cloud-clip-lite/internal/auth"
	"github.com/leaf/cloud-clip-lite/internal/config"
	"github.com/leaf/cloud-clip-lite/internal/store"
)

// AuthHandler 认证路由处理器
type AuthHandler struct {
	cfg     *config.Config
	store   *store.Store
	jwtMgr  *auth.JWTManager
	hasher  *auth.PasswordHasher
	logger  *slog.Logger
	// dummy 哈希用于登录时用户不存在的兜底校验，惰性生成一次
	dummyOnce sync.Once
	dummyHash string
}

// NewAuthHandler 创建认证处理器
func NewAuthHandler(cfg *config.Config, st *store.Store, jwtMgr *auth.JWTManager, hasher *auth.PasswordHasher, log *slog.Logger) *AuthHandler {
	return &AuthHandler{cfg: cfg, store: st, jwtMgr: jwtMgr, hasher: hasher, logger: log}
}

// RegisterRoutes 注册认证路由
func (h *AuthHandler) RegisterRoutes(r chi.Router) {
	r.Post("/auth/register", h.Register)
	r.Post("/auth/login", h.Login)
	r.Post("/auth/refresh", h.Refresh)
	r.Post("/auth/logout", h.Logout)
}

// registerRequest 注册请求
type registerRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	Email      string `json:"email"`
	InviteCode string `json:"invite_code"`
}

// Register 注册新用户
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "请求体格式错误")
		return
	}

	// 校验输入
	if err := validateUsername(req.Username); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USERNAME", err.Error())
		return
	}
	if err := validatePassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PASSWORD", err.Error())
		return
	}

	// 根据注册模式校验
	switch h.cfg.AllowRegister {
	case "closed":
		writeError(w, http.StatusForbidden, "REGISTRATION_CLOSED", "注册已关闭，请联系管理员创建账号")
		return
	case "invite":
		if req.InviteCode == "" {
			writeError(w, http.StatusBadRequest, "INVITE_CODE_REQUIRED", "需要邀请码")
			return
		}
		ic, err := h.store.GetInviteCode(r.Context(), req.InviteCode)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_INVITE_CODE", "邀请码无效或已使用")
			return
		}
		// 检查过期（isExpired 解析失败按已过期处理，fail-closed）
		if ic.ExpiresAt.Valid && isExpired(ic.ExpiresAt.String) {
			writeError(w, http.StatusBadRequest, "INVITE_CODE_EXPIRED", "邀请码已过期")
			return
		}
	case "open":
		// 开放注册，无需校验
	default:
		writeError(w, http.StatusInternalServerError, "CONFIG_ERROR", "未知的注册模式")
		return
	}

	// 哈希密码
	hash, err := h.hasher.Hash(req.Password)
	if err != nil {
		h.logger.Error("密码哈希失败", "error", err)
		writeError(w, http.StatusInternalServerError, "HASH_FAILED", "密码处理失败")
		return
	}

	// 创建用户
	u := &store.User{
		Username:     req.Username,
		PasswordHash: hash,
	}
	if req.Email != "" {
		u.Email = sql.NullString{String: req.Email, Valid: true}
	}

	created, err := h.store.CreateUser(r.Context(), u)
	if err != nil {
		if isDuplicateError(err) {
			writeError(w, http.StatusConflict, "USERNAME_EXISTS", "用户名已存在")
			return
		}
		h.logger.Error("创建用户失败", "error", err)
		writeError(w, http.StatusInternalServerError, "CREATE_FAILED", "创建用户失败")
		return
	}

	// 邀请码模式下在用户创建成功后一次性标记已使用（带 used=0 原子守卫）
	if h.cfg.AllowRegister == "invite" && req.InviteCode != "" {
		if err := h.store.UseInviteCode(r.Context(), req.InviteCode, created.ID); err != nil {
			// 并发双花：邀请码被其他注册请求抢先使用，兜底删除已创建用户（best effort）
			h.logger.Error("标记邀请码使用失败，回滚新建用户", "error", err, "user_id", created.ID)
			if derr := h.store.DeleteUser(r.Context(), created.ID); derr != nil {
				h.logger.Error("回滚删除用户失败", "error", derr, "user_id", created.ID)
			}
			writeError(w, http.StatusBadRequest, "INVALID_INVITE_CODE", "邀请码无效或已使用")
			return
		}
	}

	// 生成 Token
	accessToken, err := h.jwtMgr.GenerateAccessToken(created.ID, created.Username, created.Role)
	if err != nil {
		h.logger.Error("生成 Access Token 失败", "error", err, "user_id", created.ID)
		writeError(w, http.StatusInternalServerError, "TOKEN_FAILED", "令牌生成失败")
		return
	}
	refreshToken, err := h.generateAndStoreRefreshToken(r.Context(), created.ID)
	if err != nil {
		h.logger.Error("生成 Refresh Token 失败", "error", err, "user_id", created.ID)
		writeError(w, http.StatusInternalServerError, "TOKEN_FAILED", "令牌生成失败")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"user":          h.userResponse(created),
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"token_type":    "Bearer",
		"expires_in":    int(h.jwtMgr.AccessTTL().Seconds()),
	})
}

// loginRequest 登录请求
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Login 登录
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "请求体格式错误")
		return
	}

	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "MISSING_FIELDS", "用户名和密码不能为空")
		return
	}

	user, err := h.store.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// 用户不存在时也执行一次完整 Verify，抹平响应时间差，防止用户名枚举
			_, _ = h.hasher.Verify(req.Password, h.dummyVerifyHash())
			writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "用户名或密码错误")
			return
		}
		h.logger.Error("查询用户失败", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误")
		return
	}

	// 检查状态
	if user.Status != "active" {
		writeError(w, http.StatusForbidden, "ACCOUNT_DISABLED", "账号已被禁用")
		return
	}

	// 验证密码
	ok, err := h.hasher.Verify(req.Password, user.PasswordHash)
	if err != nil || !ok {
		writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "用户名或密码错误")
		return
	}

	// 生成 Token
	accessToken, err := h.jwtMgr.GenerateAccessToken(user.ID, user.Username, user.Role)
	if err != nil {
		h.logger.Error("生成 Access Token 失败", "error", err, "user_id", user.ID)
		writeError(w, http.StatusInternalServerError, "TOKEN_FAILED", "令牌生成失败")
		return
	}
	refreshToken, err := h.generateAndStoreRefreshToken(r.Context(), user.ID)
	if err != nil {
		h.logger.Error("生成 Refresh Token 失败", "error", err, "user_id", user.ID)
		writeError(w, http.StatusInternalServerError, "TOKEN_FAILED", "令牌生成失败")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user":          h.userResponse(user),
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"token_type":    "Bearer",
		"expires_in":    int(h.jwtMgr.AccessTTL().Seconds()),
	})
}

// refreshRequest 刷新请求
type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// Refresh 刷新 Access Token
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "请求体格式错误")
		return
	}

	if req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "MISSING_TOKEN", "refresh_token 不能为空")
		return
	}

	// 哈希查询
	tokenHash := auth.HashToken(req.RefreshToken)
	rt, err := h.store.GetRefreshToken(r.Context(), tokenHash)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "INVALID_REFRESH_TOKEN", "刷新令牌无效或已吊销")
		return
	}

	// 检查过期
	if isExpired(rt.ExpiresAt) {
		_ = h.store.RevokeRefreshToken(r.Context(), tokenHash)
		writeError(w, http.StatusUnauthorized, "REFRESH_TOKEN_EXPIRED", "刷新令牌已过期")
		return
	}

	// 查用户
	user, err := h.store.GetUserByID(r.Context(), rt.UserID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "USER_NOT_FOUND", "用户不存在")
		return
	}
	if user.Status != "active" {
		// 被禁用用户吊销 refresh token，禁止在有效期内无限续期
		if err := h.store.RevokeRefreshToken(r.Context(), tokenHash); err != nil {
			h.logger.Error("吊销禁用用户的 Refresh Token 失败", "error", err, "user_id", user.ID)
		}
		writeError(w, http.StatusUnauthorized, "ACCOUNT_DISABLED", "账号已被禁用")
		return
	}

	// 轮转：先生成新 Token 成功后再吊销旧的，避免生成失败导致用户静默掉线
	accessToken, err := h.jwtMgr.GenerateAccessToken(user.ID, user.Username, user.Role)
	if err != nil {
		h.logger.Error("生成 Access Token 失败", "error", err, "user_id", user.ID)
		writeError(w, http.StatusInternalServerError, "TOKEN_FAILED", "令牌生成失败")
		return
	}
	newRefreshToken, err := h.generateAndStoreRefreshToken(r.Context(), user.ID)
	if err != nil {
		h.logger.Error("生成 Refresh Token 失败", "error", err, "user_id", user.ID)
		writeError(w, http.StatusInternalServerError, "TOKEN_FAILED", "令牌生成失败")
		return
	}
	// 吊销旧 Token（失败仅记日志，不影响新 Token 使用）
	if err := h.store.RevokeRefreshToken(r.Context(), tokenHash); err != nil {
		h.logger.Error("吊销旧 Refresh Token 失败", "error", err, "user_id", user.ID)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  accessToken,
		"refresh_token": newRefreshToken,
		"token_type":    "Bearer",
		"expires_in":    int(h.jwtMgr.AccessTTL().Seconds()),
	})
}

// Logout 登出（吊销当前 refresh token）
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// 即使 body 解析失败也返回成功（幂等）
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
		return
	}

	if req.RefreshToken != "" {
		tokenHash := auth.HashToken(req.RefreshToken)
		_ = h.store.RevokeRefreshToken(r.Context(), tokenHash)
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// generateAndStoreRefreshToken 生成并存储 Refresh Token
func (h *AuthHandler) generateAndStoreRefreshToken(ctx context.Context, userID int64) (string, error) {
	token, err := auth.GenerateRefreshToken()
	if err != nil {
		return "", err
	}
	tokenHash := auth.HashToken(token)
	// 统一 UTC 写入，与 store 的 datetime('now') 及 isExpired 的解析口径一致
	expiresAt := time.Now().UTC().Add(h.cfg.RefreshTTL).Format("2006-01-02 15:04:05")

	_, err = h.store.CreateRefreshToken(ctx, &store.RefreshToken{
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return "", err
	}
	return token, nil
}

// fallbackDummyHash 预生成的 Argon2id 哈希，仅在运行时生成 dummy 哈希失败时使用
const fallbackDummyHash = "$argon2id$v=19$m=65536,t=3,p=2$uFemaSjzba+lBVn4BA5eyg$fp68HikkbKJD9z9y1i0ghXWBCfEamC2qAtpvZwt+xVg"

// dummyVerifyHash 返回用于「用户不存在」登录路径的 dummy 哈希（惰性生成一次）
// 使该路径与真实用户的密码校验耗时一致，抹平用户名枚举的时序差
func (h *AuthHandler) dummyVerifyHash() string {
	h.dummyOnce.Do(func() {
		hash, err := h.hasher.Hash("timing-equalizer-dummy")
		if err != nil {
			h.logger.Error("生成 dummy 哈希失败", "error", err)
			hash = fallbackDummyHash
		}
		h.dummyHash = hash
	})
	return h.dummyHash
}

// userResponse 用户信息响应（不含敏感字段）
func (h *AuthHandler) userResponse(u *store.User) map[string]any {
	resp := map[string]any{
		"id":             u.ID,
		"username":       u.Username,
		"role":           u.Role,
		"status":         u.Status,
		"max_item_size":  u.MaxItemSize,
		"quota_bytes":    u.QuotaBytes,
		"retention_days": u.RetentionDays,
		"created_at":     u.CreatedAt,
	}
	if u.Email.Valid {
		resp["email"] = u.Email.String
	}
	return resp
}

// validateUsername 校验用户名
func validateUsername(username string) error {
	if len(username) < 3 || len(username) > 32 {
		return errors.New("用户名长度需 3-32 字符")
	}
	for _, c := range username {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
			return errors.New("用户名只能包含字母、数字、下划线和连字符")
		}
	}
	return nil
}

// validatePassword 校验密码
func validatePassword(password string) error {
	if len(password) < 8 {
		return errors.New("密码长度至少 8 字符")
	}
	if len(password) > 128 {
		return errors.New("密码长度不能超过 128 字符")
	}
	return nil
}

// isDuplicateError 判断是否为唯一约束冲突
func isDuplicateError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE") || strings.Contains(msg, "duplicate")
}

// isExpired 检查过期时间字符串是否已过期
// 时间字符串按 UTC 解析（与 store 的 datetime('now') 写入口径一致）；
// 解析失败按已过期处理（fail-closed）
func isExpired(expiresAt string) bool {
	t, err := time.Parse("2006-01-02 15:04:05", strings.TrimSpace(expiresAt))
	if err != nil {
		// 尝试 RFC3339
		t, err = time.Parse(time.RFC3339, strings.TrimSpace(expiresAt))
		if err != nil {
			return true // 解析失败视为已过期
		}
	}
	return time.Now().After(t)
}
