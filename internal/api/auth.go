package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
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
		// 检查过期
		if ic.ExpiresAt.Valid {
			expiresAt, err := time.Parse("2006-01-02 15:04:05", ic.ExpiresAt.String)
			if err == nil && time.Now().After(expiresAt) {
				writeError(w, http.StatusBadRequest, "INVITE_CODE_EXPIRED", "邀请码已过期")
				return
			}
		}
		// 延迟标记使用（用户创建成功后）
		defer func() {
			if err == nil {
				h.store.UseInviteCode(r.Context(), req.InviteCode, 0) // userID 在下面设置
			}
		}()
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

	// 邀请码模式下标记已使用
	if h.cfg.AllowRegister == "invite" && req.InviteCode != "" {
		_ = h.store.UseInviteCode(r.Context(), req.InviteCode, created.ID)
	}

	// 生成 Token
	accessToken, _ := h.jwtMgr.GenerateAccessToken(created.ID, created.Username, created.Role)
	refreshToken, _ := h.generateAndStoreRefreshToken(r.Context(), created.ID)

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
	accessToken, _ := h.jwtMgr.GenerateAccessToken(user.ID, user.Username, user.Role)
	refreshToken, _ := h.generateAndStoreRefreshToken(r.Context(), user.ID)

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
		writeError(w, http.StatusForbidden, "ACCOUNT_DISABLED", "账号已被禁用")
		return
	}

	// 吊销旧 Token，签发新的（轮转）
	_ = h.store.RevokeRefreshToken(r.Context(), tokenHash)
	accessToken, _ := h.jwtMgr.GenerateAccessToken(user.ID, user.Username, user.Role)
	newRefreshToken, _ := h.generateAndStoreRefreshToken(r.Context(), user.ID)

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
	expiresAt := time.Now().Add(h.cfg.RefreshTTL).Format("2006-01-02 15:04:05")

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
	if len(password) < 6 {
		return errors.New("密码长度至少 6 字符")
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
func isExpired(expiresAt string) bool {
	t, err := time.Parse("2006-01-02 15:04:05", strings.TrimSpace(expiresAt))
	if err != nil {
		// 尝试 RFC3339
		t, err = time.Parse(time.RFC3339, strings.TrimSpace(expiresAt))
		if err != nil {
			return false // 解析失败不视为过期
		}
	}
	return time.Now().After(t)
}
