package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/leaf/cloud-clip-lite/internal/auth"
	"github.com/leaf/cloud-clip-lite/internal/blob"
	"github.com/leaf/cloud-clip-lite/internal/config"
	"github.com/leaf/cloud-clip-lite/internal/health"
	"github.com/leaf/cloud-clip-lite/internal/metrics"
	"github.com/leaf/cloud-clip-lite/internal/middleware"
	"github.com/leaf/cloud-clip-lite/internal/store"
	"github.com/leaf/cloud-clip-lite/internal/ws"
)

// Server API 服务器：组装路由与中间件
type Server struct {
	cfg        *config.Config
	store      *store.Store
	blob       blob.BlobStore
	health     *health.Handler
	logger     *slog.Logger
	jwtMgr     *auth.JWTManager
	hasher     *auth.PasswordHasher
	hub        *ws.Hub
	metrics    *metrics.Metrics
	rateLimiter *middleware.RateLimiter
}

// New 创建 API 服务器
func New(cfg *config.Config, s *store.Store, bs blob.BlobStore, h *health.Handler, log *slog.Logger, jwtMgr *auth.JWTManager, hasher *auth.PasswordHasher, hub *ws.Hub, m *metrics.Metrics, rl *middleware.RateLimiter) *Server {
	return &Server{
		cfg:         cfg,
		store:       s,
		blob:        bs,
		health:      h,
		logger:      log,
		jwtMgr:      jwtMgr,
		hasher:      hasher,
		hub:         hub,
		metrics:     m,
		rateLimiter: rl,
	}
}

// Router 构建路由树
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	// 全局中间件
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger(s.logger))
	r.Use(middleware.Recover)
	r.Use(middleware.CORS(s.cfg.AllowedOrigins))

	// 健康检查（无需鉴权）
	r.Get("/healthz", s.health.Liveness)
	r.Get("/readyz", s.health.Readiness)

	// Prometheus 指标端点
	if s.metrics != nil {
		r.Get("/metrics", s.metrics.Handler())
	}

	// API 路由组
	r.Route("/api", func(r chi.Router) {
		// 认证路由（无需鉴权，但限流防暴力破解）
		authHandler := NewAuthHandler(s.cfg, s.store, s.jwtMgr, s.hasher, s.logger)
		authHandler.RegisterRoutes(r)

		// WebSocket 路由（独立鉴权，不走 RequireAuth 中间件）
		if s.hub != nil {
			wsHandler := ws.NewHandler(s.hub, s.jwtMgr, s.store, s.cfg.AllowedOrigins, s.logger)
			r.Handle("/ws", wsHandler)
		}

		// 受保护路由（需鉴权）
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(s.jwtMgr, s.store))

			// 用户级限流
			if s.rateLimiter != nil {
				r.Use(middleware.RateLimitByUser(s.rateLimiter))
			}

			// 个人信息
			meHandler := NewMeHandler(s.cfg, s.store, s.hasher, s.logger)
			meHandler.RegisterRoutes(r)

			// 设备管理
			deviceHandler := NewDeviceHandler(s.cfg, s.store, s.logger)
			deviceHandler.RegisterRoutes(r)

			// 剪切板（传入 Hub 用于广播）
			clipHandler := NewClipHandler(s.cfg, s.store, s.blob, s.logger, s.hub)
			clipHandler.RegisterRoutes(r)

			// 管理后台（需 admin 角色）
			adminHandler := NewAdminHandler(s.cfg, s.store, s.hasher, s.logger, s.hub)
			adminHandler.RegisterRoutes(r)
		})
	})

	return r
}

// ping 简单连通性测试
func (s *Server) ping(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok","message":"cloud-clip-lite API"}`))
}
