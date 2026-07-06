package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/leaf/cloud-clip-lite/internal/store"
)

// APIError 统一错误结构
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// 可选额外字段
	Extra map[string]any `json:"extra,omitempty"`
}

// writeJSON 写入 JSON 响应
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// writeError 写入错误响应
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": APIError{Code: code, Message: message},
	})
}

// writeErrorWithExtra 写入带额外字段的错误响应
func writeErrorWithExtra(w http.ResponseWriter, status int, code, message string, extra map[string]any) {
	writeJSON(w, status, map[string]any{
		"error": APIError{Code: code, Message: message, Extra: extra},
	})
}

// handleError 根据 store 错误类型自动映射 HTTP 状态码
func handleError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在")
		return
	}
	writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
}
