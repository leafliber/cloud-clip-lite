package api

import (
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
)

// DeviceHandler 设备路由处理器
type DeviceHandler struct {
	cfg   *config.Config
	store *store.Store
	logger *slog.Logger
}

// NewDeviceHandler 创建设备处理器
func NewDeviceHandler(cfg *config.Config, st *store.Store, log *slog.Logger) *DeviceHandler {
	return &DeviceHandler{cfg: cfg, store: st, logger: log}
}

// RegisterRoutes 注册设备路由（需鉴权）
func (h *DeviceHandler) RegisterRoutes(r chi.Router) {
	r.Get("/devices", h.ListDevices)
	r.Post("/devices", h.CreateDevice)
	r.Delete("/devices/{id}", h.DeleteDevice)
	r.Post("/devices/{id}/revoke", h.RevokeDeviceToken)
}

// deviceResponse 设备信息响应（不含 Token Hash）
func deviceResponse(d *store.Device) map[string]any {
	resp := map[string]any{
		"id":         d.ID,
		"name":       d.Name,
		"type":       d.Type,
		"created_at": d.CreatedAt,
		"has_token":  d.APITokenHash.Valid,
	}
	if d.LastSeenAt.Valid {
		resp["last_seen_at"] = d.LastSeenAt.String
	}
	return resp
}

// ListDevices 列出当前用户的所有设备
func (h *DeviceHandler) ListDevices(w http.ResponseWriter, r *http.Request) {
	ac := middleware.GetAuthContext(r.Context())
	if ac == nil {
		writeError(w, http.StatusUnauthorized, "NOT_AUTHENTICATED", "未认证")
		return
	}

	devices, err := h.store.ListDevicesByUser(r.Context(), ac.UserID)
	if err != nil {
		h.logger.Error("查询设备列表失败", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "查询设备列表失败")
		return
	}

	var list []map[string]any
	for _, d := range devices {
		list = append(list, deviceResponse(d))
	}
	if list == nil {
		list = []map[string]any{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"devices": list})
}

// createDeviceRequest 创建设备请求
type createDeviceRequest struct {
	Name string `json:"name"`
	Type string `json:"type"` // ios-shortcut | desktop | web | android
}

// CreateDevice 创建设备并生成 API Token（仅返回一次）
func (h *DeviceHandler) CreateDevice(w http.ResponseWriter, r *http.Request) {
	ac := middleware.GetAuthContext(r.Context())
	if ac == nil {
		writeError(w, http.StatusUnauthorized, "NOT_AUTHENTICATED", "未认证")
		return
	}

	var req createDeviceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "请求体格式错误")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "MISSING_NAME", "设备名称不能为空")
		return
	}
	if len(req.Name) > 64 {
		writeError(w, http.StatusBadRequest, "NAME_TOO_LONG", "设备名称不能超过 64 字符")
		return
	}

	validTypes := map[string]bool{
		"ios-shortcut": true,
		"desktop":      true,
		"web":          true,
		"android":      true,
	}
	if req.Type == "" {
		req.Type = "web"
	}
	if !validTypes[req.Type] {
		writeError(w, http.StatusBadRequest, "INVALID_TYPE", "设备类型非法")
		return
	}

	// 生成 API Token
	apiToken, err := auth.GenerateAPIToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "TOKEN_GEN_FAILED", "生成 Token 失败")
		return
	}
	tokenHash := auth.HashToken(apiToken)

	d := &store.Device{
		UserID:       ac.UserID,
		Name:         req.Name,
		Type:         req.Type,
		APITokenHash: sql.NullString{String: tokenHash, Valid: true},
	}

	created, err := h.store.CreateDevice(r.Context(), d)
	if err != nil {
		h.logger.Error("创建设备失败", "error", err)
		writeError(w, http.StatusInternalServerError, "CREATE_FAILED", "创建设备失败")
		return
	}

	resp := deviceResponse(created)
	resp["api_token"] = apiToken // 明文 Token 仅返回一次

	writeJSON(w, http.StatusCreated, resp)
}

// DeleteDevice 删除设备
func (h *DeviceHandler) DeleteDevice(w http.ResponseWriter, r *http.Request) {
	ac := middleware.GetAuthContext(r.Context())
	if ac == nil {
		writeError(w, http.StatusUnauthorized, "NOT_AUTHENTICATED", "未认证")
		return
	}

	deviceID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "设备 ID 非法")
		return
	}

	if err := h.store.DeleteDevice(r.Context(), deviceID, ac.UserID); err != nil {
		handleError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// RevokeDeviceToken 吊销设备的 API Token（不删除设备）
func (h *DeviceHandler) RevokeDeviceToken(w http.ResponseWriter, r *http.Request) {
	ac := middleware.GetAuthContext(r.Context())
	if ac == nil {
		writeError(w, http.StatusUnauthorized, "NOT_AUTHENTICATED", "未认证")
		return
	}

	deviceID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "设备 ID 非法")
		return
	}

	if err := h.store.RevokeDeviceAPIToken(r.Context(), deviceID, ac.UserID); err != nil {
		handleError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
