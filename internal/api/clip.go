package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/leaf/cloud-clip-lite/internal/blob"
	"github.com/leaf/cloud-clip-lite/internal/config"
	"github.com/leaf/cloud-clip-lite/internal/middleware"
	"github.com/leaf/cloud-clip-lite/internal/store"
	"github.com/leaf/cloud-clip-lite/internal/ws"
)

// ClipHandler 剪切板路由处理器
type ClipHandler struct {
	cfg      *config.Config
	store    *store.Store
	blob     blob.BlobStore
	logger   *slog.Logger
	hub      *ws.Hub
}

// NewClipHandler 创建剪切板处理器
func NewClipHandler(cfg *config.Config, st *store.Store, bs blob.BlobStore, log *slog.Logger, hub *ws.Hub) *ClipHandler {
	return &ClipHandler{cfg: cfg, store: st, blob: bs, logger: log, hub: hub}
}

// RegisterRoutes 注册剪切板路由（需鉴权）
func (h *ClipHandler) RegisterRoutes(r chi.Router) {
	r.Post("/clip", h.Create)
	r.Get("/clip", h.List)
	r.Get("/clip/latest", h.GetLatest)
	r.Get("/clip/{id}", h.GetByID)
	r.Get("/clip/{id}/content", h.GetContent)
	r.Delete("/clip/{id}", h.Delete)
}

// clipItemResponse 条目响应
func clipItemResponse(item *store.ClipItem) map[string]any {
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
	// meta 解析为对象
	if item.Meta != "" && item.Meta != "{}" {
		var metaObj map[string]any
		if err := json.Unmarshal([]byte(item.Meta), &metaObj); err == nil {
			resp["meta"] = metaObj
		}
	}
	return resp
}

// ---------- 上传 ----------

// createTextRequest 文本上传请求
type createTextRequest struct {
	Type       string            `json:"type"`        // text
	Text       string            `json:"text"`
	Meta       map[string]any    `json:"meta,omitempty"`
	ExpiresIn  *int              `json:"expires_in,omitempty"` // 自定义过期秒数
}

// Create 创建剪切板条目
// - Content-Type: application/json → 文本上传
// - Content-Type: multipart/form-data → 文件/图片上传
func (h *ClipHandler) Create(w http.ResponseWriter, r *http.Request) {
	ac := middleware.GetAuthContext(r.Context())
	if ac == nil {
		writeError(w, http.StatusUnauthorized, "NOT_AUTHENTICATED", "未认证")
		return
	}

	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		h.createText(w, r, ac)
		return
	}
	if strings.HasPrefix(contentType, "multipart/form-data") {
		h.createFile(w, r, ac)
		return
	}
	writeError(w, http.StatusBadRequest, "INVALID_CONTENT_TYPE", "Content-Type 必须为 application/json 或 multipart/form-data")
}

// createText 文本上传
func (h *ClipHandler) createText(w http.ResponseWriter, r *http.Request, ac *middleware.AuthContext) {
	var req createTextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "请求体格式错误")
		return
	}

	if req.Text == "" {
		writeError(w, http.StatusBadRequest, "EMPTY_TEXT", "文本内容不能为空")
		return
	}

	// 获取用户配置
	user, err := h.store.GetUserByID(r.Context(), ac.UserID)
	if err != nil {
		handleError(w, err)
		return
	}

	// 大小校验
	size := int64(len(req.Text))
	if size > user.MaxItemSize {
		writeErrorWithExtra(w, http.StatusRequestEntityTooLarge, "ITEM_TOO_LARGE",
			"文本超过单条上限", map[string]any{"limit": user.MaxItemSize})
		return
	}

	// 配额校验
	if err := h.checkQuota(r.Context(), ac.UserID, size, user); err != nil {
		writeError(w, http.StatusForbidden, "QUOTA_EXCEEDED", err.Error())
		return
	}

	// SHA256
	hash := sha256.Sum256([]byte(req.Text))
	sha := hex.EncodeToString(hash[:])

	// meta
	metaJSON := "{}"
	if req.Meta != nil {
		if b, err := json.Marshal(req.Meta); err == nil {
			metaJSON = string(b)
		}
	}

	// TTL
	expiresAt := h.calculateExpiry(user.RetentionDays, req.ExpiresIn)

	item := &store.ClipItem{
		UserID:      ac.UserID,
		Type:        "text",
		Size:        size,
		MimeType:    sql.NullString{String: "text/plain", Valid: true},
		TextContent: sql.NullString{String: req.Text, Valid: true},
		SHA256:      sql.NullString{String: sha, Valid: true},
		Meta:        metaJSON,
		ExpiresAt:   expiresAt,
	}
	if ac.DeviceID > 0 {
		item.DeviceID = sql.NullInt64{Int64: ac.DeviceID, Valid: true}
	}

	created, err := h.store.CreateClipItem(r.Context(), item)
	if err != nil {
		h.logger.Error("创建文本条目失败", "error", err)
		writeError(w, http.StatusInternalServerError, "CREATE_FAILED", "创建条目失败")
		return
	}

	resp := clipItemResponse(created)
	writeJSON(w, http.StatusCreated, resp)

	// 广播新条目到用户的其他在线连接
	if h.hub != nil {
		h.hub.BroadcastClipCreated(ac.UserID, resp, nil)
	}
}

// createFile 文件/图片上传（multipart）
func (h *ClipHandler) createFile(w http.ResponseWriter, r *http.Request, ac *middleware.AuthContext) {
	// 限制 multipart 解析的内存
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "PARSE_FAILED", "解析 multipart 表单失败")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "NO_FILE", "未找到 file 字段")
		return
	}
	defer file.Close()

	// 获取用户配置
	user, err := h.store.GetUserByID(r.Context(), ac.UserID)
	if err != nil {
		handleError(w, err)
		return
	}

	// 大小校验（先检查 Content-Length，实际以流式为准）
	maxSize := user.MaxItemSize

	// 配额校验（预估）
	if err := h.checkQuota(r.Context(), ac.UserID, header.Size, user); err != nil {
		writeError(w, http.StatusForbidden, "QUOTA_EXCEEDED", err.Error())
		return
	}

	// 生成 blobKey
	now := time.Now()
	blobUUID := uuid.New().String()
	blobKey := blob.GenerateBlobKey(ac.UserID, now.Year(), int(now.Month()), blobUUID)

	// 流式保存到 BlobStore（同时计算 SHA256）
	hasher := sha256.New()
	teeReader := io.TeeReader(file, hasher)

	written, err := h.blob.Save(r.Context(), teeReader, blobKey, maxSize)
	if err != nil {
		if errors.Is(err, blob.ErrItemTooLarge) {
			writeErrorWithExtra(w, http.StatusRequestEntityTooLarge, "ITEM_TOO_LARGE",
				"文件超过单条上限", map[string]any{"limit": maxSize})
			return
		}
		h.logger.Error("保存 blob 失败", "error", err)
		writeError(w, http.StatusInternalServerError, "BLOB_SAVE_FAILED", "文件存储失败")
		return
	}

	sha := hex.EncodeToString(hasher.Sum(nil))

	// MIME 类型校验：优先使用 magic bytes 检测，对比声明的类型
	declaredMIME := header.Header.Get("Content-Type")
	if declaredMIME == "" {
		declaredMIME = blob.MIMEByExtension(header.Filename)
	}

	// 读取已保存 blob 的头部进行 magic bytes 校验
	detectedMIME := declaredMIME
	blobReader, err := h.blob.Open(r.Context(), blobKey)
	if err == nil {
		headerBytes, _ := blob.ReadHeader(blobReader)
		_ = blobReader.Close()
		if len(headerBytes) > 0 {
			detected, ok := blob.ValidateMIME(declaredMIME, headerBytes)
			if !ok {
				h.logger.Warn("MIME 类型不匹配",
					"declared", declaredMIME,
					"detected", detected,
					"filename", header.Filename,
				)
			}
			detectedMIME = detected
		}
	}

	// 检查 MIME 类型是否在允许列表
	if !blob.IsMIMEAllowed(detectedMIME) {
		_ = h.blob.Delete(r.Context(), blobKey)
		writeError(w, http.StatusBadRequest, "MIME_NOT_ALLOWED", "不支持的文件类型: "+detectedMIME)
		return
	}

	// 判断类型
	clipType := "file"
	if blob.IsImageType(detectedMIME) {
		clipType = "image"
	}
	mimeType := detectedMIME

	// meta
	metaMap := map[string]any{"filename": header.Filename}
	metaJSON, _ := json.Marshal(metaMap)

	// TTL
	expiresAt := h.calculateExpiry(user.RetentionDays, nil)

	item := &store.ClipItem{
		UserID:   ac.UserID,
		Type:     clipType,
		Size:     written,
		MimeType: sql.NullString{String: mimeType, Valid: true},
		BlobKey:  sql.NullString{String: blobKey, Valid: true},
		SHA256:   sql.NullString{String: sha, Valid: true},
		Meta:     string(metaJSON),
		ExpiresAt: expiresAt,
	}
	if ac.DeviceID > 0 {
		item.DeviceID = sql.NullInt64{Int64: ac.DeviceID, Valid: true}
	}

	created, err := h.store.CreateClipItem(r.Context(), item)
	if err != nil {
		// DB 失败时清理已写入的 blob
		_ = h.blob.Delete(r.Context(), blobKey)
		h.logger.Error("创建文件条目失败", "error", err)
		writeError(w, http.StatusInternalServerError, "CREATE_FAILED", "创建条目失败")
		return
	}

	resp := clipItemResponse(created)
	writeJSON(w, http.StatusCreated, resp)

	// 广播新条目到用户的其他在线连接
	if h.hub != nil {
		h.hub.BroadcastClipCreated(ac.UserID, resp, nil)
	}
}

// ---------- 列表 ----------

// List 分页查询历史列表
func (h *ClipHandler) List(w http.ResponseWriter, r *http.Request) {
	ac := middleware.GetAuthContext(r.Context())
	if ac == nil {
		writeError(w, http.StatusUnauthorized, "NOT_AUTHENTICATED", "未认证")
		return
	}

	q := r.URL.Query()
	limit := 20
	if l := q.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	beforeID, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	typeFilter := q.Get("type")

	items, err := h.store.ListClipItems(r.Context(), ac.UserID, beforeID, limit, typeFilter)
	if err != nil {
		h.logger.Error("查询列表失败", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "查询失败")
		return
	}

	var list []map[string]any
	for _, item := range items {
		list = append(list, clipItemResponse(item))
	}
	if list == nil {
		list = []map[string]any{}
	}

	// 返回游标（最后一条 ID）供下一页使用
	var cursor int64
	if len(items) > 0 {
		cursor = items[len(items)-1].ID
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":  list,
		"cursor": cursor,
		"limit":  limit,
	})
}

// ---------- 详情 ----------

// GetByID 获取单条详情
func (h *ClipHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	ac := middleware.GetAuthContext(r.Context())
	if ac == nil {
		writeError(w, http.StatusUnauthorized, "NOT_AUTHENTICATED", "未认证")
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "ID 非法")
		return
	}

	item, err := h.store.GetClipItem(r.Context(), id, ac.UserID)
	if err != nil {
		handleError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, clipItemResponse(item))
}

// ---------- 下载内容 ----------

// GetContent 下载条目实际内容
func (h *ClipHandler) GetContent(w http.ResponseWriter, r *http.Request) {
	ac := middleware.GetAuthContext(r.Context())
	if ac == nil {
		writeError(w, http.StatusUnauthorized, "NOT_AUTHENTICATED", "未认证")
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "ID 非法")
		return
	}

	item, err := h.store.GetClipItem(r.Context(), id, ac.UserID)
	if err != nil {
		handleError(w, err)
		return
	}

	// 文本类型：直接返回文本
	if item.Type == "text" && item.TextContent.Valid {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(item.TextContent.String))
		return
	}

	// 二进制类型：从 BlobStore 读取
	if !item.BlobKey.Valid {
		writeError(w, http.StatusNotFound, "NO_CONTENT", "条目无二进制内容")
		return
	}

	reader, err := h.blob.Open(r.Context(), item.BlobKey.String)
	if err != nil {
		if errors.Is(err, blob.ErrBlobNotFound) {
			writeError(w, http.StatusNotFound, "BLOB_NOT_FOUND", "文件不存在")
			return
		}
		h.logger.Error("读取 blob 失败", "error", err)
		writeError(w, http.StatusInternalServerError, "BLOB_READ_FAILED", "读取文件失败")
		return
	}
	defer reader.Close()

	contentType := "application/octet-stream"
	if item.MimeType.Valid {
		contentType = item.MimeType.String
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "attachment")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, reader)
}

// ---------- 最新一条 ----------

// GetLatest 获取最新一条条目（快捷指令「下载」常用）
func (h *ClipHandler) GetLatest(w http.ResponseWriter, r *http.Request) {
	ac := middleware.GetAuthContext(r.Context())
	if ac == nil {
		writeError(w, http.StatusUnauthorized, "NOT_AUTHENTICATED", "未认证")
		return
	}

	item, err := h.store.GetLatestClipItem(r.Context(), ac.UserID)
	if err != nil {
		handleError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, clipItemResponse(item))
}

// ---------- 删除 ----------

// Delete 删除单条
func (h *ClipHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ac := middleware.GetAuthContext(r.Context())
	if ac == nil {
		writeError(w, http.StatusUnauthorized, "NOT_AUTHENTICATED", "未认证")
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "ID 非法")
		return
	}

	deleted, err := h.store.DeleteClipItem(r.Context(), id, ac.UserID)
	if err != nil {
		handleError(w, err)
		return
	}

	// 异步清理 blob
	if deleted.BlobKey.Valid {
		go func(key string) {
			_ = h.blob.Delete(context.Background(), key)
		}(deleted.BlobKey.String)
	}

	// 广播删除事件到用户的所有在线连接
	if h.hub != nil {
		h.hub.BroadcastClipDeleted(ac.UserID, id, nil)
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "id": id})
}

// ---------- 辅助方法 ----------

// checkQuota 检查用户配额
func (h *ClipHandler) checkQuota(ctx context.Context, userID int64, newSize int64, user *store.User) error {
	usage, err := h.store.GetUserStorageUsage(ctx, userID)
	if err != nil {
		return err
	}
	if usage+newSize > user.QuotaBytes {
		return errors.New("用户存储配额已超")
	}
	return nil
}

// calculateExpiry 计算过期时间
// customSeconds 非空时使用自定义过期，否则使用 retentionDays
func (h *ClipHandler) calculateExpiry(retentionDays int, customSeconds *int) sql.NullString {
	var dur time.Duration
	if customSeconds != nil && *customSeconds > 0 {
		dur = time.Duration(*customSeconds) * time.Second
	} else {
		dur = time.Duration(retentionDays) * 24 * time.Hour
	}
	expires := time.Now().Add(dur)
	return sql.NullString{
		String: expires.Format("2006-01-02 15:04:05"),
		Valid:  true,
	}
}
