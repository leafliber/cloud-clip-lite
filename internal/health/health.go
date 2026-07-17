package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/leaf/cloud-clip-lite/internal/store"
)

// Handler 健康检查处理器
type Handler struct {
	store     *store.Store
	startTime time.Time
	ready     *atomic.Bool
	version   string
}

// New 创建健康检查处理器（版本默认 "dev"，由 main 通过 NewWithVersion 注入构建版本）
func New(s *store.Store, ready *atomic.Bool) *Handler {
	return NewWithVersion(s, ready, "dev")
}

// NewWithVersion 创建健康检查处理器并指定版本号
func NewWithVersion(s *store.Store, ready *atomic.Bool, version string) *Handler {
	if version == "" {
		version = "dev"
	}
	return &Handler{
		store:     s,
		startTime: time.Now(),
		ready:     ready,
		version:   version,
	}
}

// healthResponse 健康检查响应
type healthResponse struct {
	Status    string `json:"status"`
	Uptime    string `json:"uptime"`
	Timestamp string `json:"timestamp"`
	Version   string `json:"version"`
}

// Liveness 存活探针 — 进程存活即返回 OK
func (h *Handler) Liveness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(healthResponse{
		Status:    "ok",
		Uptime:    time.Since(h.startTime).Round(time.Second).String(),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Version:   h.version,
	})
}

// Readiness 就绪探针 — 数据库可达且标记为就绪
func (h *Handler) Readiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if !h.ready.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "not_ready",
			"reason":  "服务未就绪",
			"version": h.version,
		})
		return
	}

	// 检查数据库连通性
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := h.store.HealthCheck(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "degraded",
			"reason":  "数据库不可达",
			"version": h.version,
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(healthResponse{
		Status:    "ok",
		Uptime:    time.Since(h.startTime).Round(time.Second).String(),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Version:   h.version,
	})
}

// RegisterRoutes 注册健康检查路由
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.Liveness)
	mux.HandleFunc("/readyz", h.Readiness)
}
