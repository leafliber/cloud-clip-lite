package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/leaf/cloud-clip-lite/internal/api"
	"github.com/leaf/cloud-clip-lite/internal/auth"
	"github.com/leaf/cloud-clip-lite/internal/blob"
	"github.com/leaf/cloud-clip-lite/internal/config"
	"github.com/leaf/cloud-clip-lite/internal/db"
	"github.com/leaf/cloud-clip-lite/internal/health"
	"github.com/leaf/cloud-clip-lite/internal/logger"
	"github.com/leaf/cloud-clip-lite/internal/metrics"
	"github.com/leaf/cloud-clip-lite/internal/migrate"
	"github.com/leaf/cloud-clip-lite/internal/middleware"
	"github.com/leaf/cloud-clip-lite/internal/scheduler"
	"github.com/leaf/cloud-clip-lite/internal/store"
	"github.com/leaf/cloud-clip-lite/internal/ws"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "启动失败: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// 1. 加载配置
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("加载配置: %w", err)
	}

	// 2. 初始化日志
	log := logger.New(cfg.LogLevel, cfg.LogFormat)

	log.Info("启动 cloud-clip-lite",
		"env", cfg.Env,
		"port", cfg.Port,
		"database", dbDialectLabel(cfg),
		"blob_store", cfg.BlobStore,
		"allow_register", cfg.AllowRegister,
	)

	// 3. 确保数据目录存在（SQLite 与本地 Blob 共用）
	if err := ensureDirs(cfg); err != nil {
		return fmt.Errorf("创建数据目录: %w", err)
	}

	// 4. 打开数据库
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	database, err := db.Open(ctx, cfg)
	if err != nil {
		return fmt.Errorf("打开数据库: %w", err)
	}
	defer func() {
		if cerr := database.Close(); cerr != nil {
			log.Error("关闭数据库失败", "error", cerr)
		}
	}()
	log.Info("数据库连接成功", "dialect", database.Dialect)

	// 5. 执行迁移
	if err := migrate.Run(ctx, database); err != nil {
		return fmt.Errorf("执行迁移: %w", err)
	}
	log.Info("数据库迁移完成", "dialect", database.Dialect)

	// 6. 组装服务层
	st := store.New(database)
	ready := &atomic.Bool{}
	ready.Store(true)

	// 鉴权组件
	jwtMgr := auth.NewJWTManager(cfg.JWTSecret, cfg.AccessTTL)
	hasher := auth.NewPasswordHasher(auth.Argon2Params{
		Memory:      cfg.Argon2Memory,
		Iterations:  cfg.Argon2Iterations,
		Parallelism: cfg.Argon2Parallelism,
		SaltLength:  16,
		KeyLength:   32,
	})

	// 管理员引导（首次启动）
	if err := bootstrapAdmin(ctx, st, hasher, cfg, log); err != nil {
		log.Error("管理员引导失败", "error", err)
	}

	healthHandler := health.New(st, ready)

	// 对象存储
	var blobStore blob.BlobStore
	if cfg.BlobStore == "local" {
		ls, err := blob.NewLocalStore(cfg.BlobLocalDir)
		if err != nil {
			return fmt.Errorf("创建本地 BlobStore 失败: %w", err)
		}
		blobStore = ls
		log.Info("BlobStore 初始化", "type", "local", "dir", cfg.BlobLocalDir)
	} else {
		return fmt.Errorf("S3 BlobStore 将在阶段 5 实现，当前仅支持 local")
	}

	// WebSocket Hub
	hub := ws.NewHub(log)
	go hub.Run()

	// Prometheus 指标
	m := metrics.New()

	// 限流器（每分钟 60 请求，突发 10）
	rateLimiter := middleware.NewRateLimiter(cfg.RateLimitPerMinute, 10)

	// 清理调度器
	sched := scheduler.New(st, blobStore, log)
	sched.Start()
	defer sched.Stop()

	apiServer := api.New(cfg, st, blobStore, healthHandler, log, jwtMgr, hasher, hub, m, rateLimiter)

	// 7. 启动 HTTP 服务器
	httpServer := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      apiServer.Router(),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  60 * time.Second,
		ErrorLog:     slog.NewLogLogger(log.Handler(), slog.LevelError),
	}

	// 监听信号，优雅关闭
	serverErr := make(chan error, 1)
	go func() {
		log.Info("HTTP 服务监听", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	// 等待中断信号或启动错误
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		if err != nil {
			return fmt.Errorf("HTTP 服务错误: %w", err)
		}
	case sig := <-sigCh:
		log.Info("收到退出信号，开始优雅关闭", "signal", sig.String())
		ready.Store(false)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error("优雅关闭失败", "error", err)
		return fmt.Errorf("优雅关闭: %w", err)
	}

	// 停止 WebSocket Hub
	hub.Stop()

	log.Info("服务已停止")
	return nil
}

// ensureDirs 确保数据目录存在
func ensureDirs(cfg *config.Config) error {
	if cfg.IsSQLite() {
		if err := os.MkdirAll(filepathOf(cfg.SQLitePath), 0o755); err != nil {
			return err
		}
	}
	if cfg.BlobStore == "local" {
		if err := os.MkdirAll(cfg.BlobLocalDir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// filepathOf 从文件路径提取目录
func filepathOf(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[:i]
		}
	}
	return "."
}

// dbDialectLabel 数据库方言标签
func dbDialectLabel(cfg *config.Config) string {
	if cfg.IsSQLite() {
		return "sqlite"
	}
	return "postgres"
}

// bootstrapAdmin 首次启动时自动创建管理员账号
// 条件：数据库中无任何用户，且 ADMIN_USERNAME/ADMIN_PASSWORD 已配置
func bootstrapAdmin(ctx context.Context, st *store.Store, hasher *auth.PasswordHasher, cfg *config.Config, log *slog.Logger) error {
	count, err := st.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("查询用户数失败: %w", err)
	}
	if count > 0 {
		return nil // 已有用户，跳过
	}

	if cfg.AdminUsername == "" || cfg.AdminPassword == "" {
		log.Warn("首次启动但未配置 ADMIN_USERNAME/ADMIN_PASSWORD，跳过管理员创建")
		return nil
	}

	hash, err := hasher.Hash(cfg.AdminPassword)
	if err != nil {
		return fmt.Errorf("哈希管理员密码失败: %w", err)
	}

	_, err = st.CreateUser(ctx, &store.User{
		Username:     cfg.AdminUsername,
		PasswordHash: hash,
		Role:         "admin",
		Status:       "active",
	})
	if err != nil {
		return fmt.Errorf("创建管理员失败: %w", err)
	}

	log.Info("管理员账号创建成功", "username", cfg.AdminUsername)
	return nil
}
