package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/leaf/cloud-clip-lite/internal/blob"
	"github.com/leaf/cloud-clip-lite/internal/store"
)

// Scheduler 定时任务调度器
// 负责：TTL 清理、配额超额最旧优先删除、孤儿 Blob 回收
type Scheduler struct {
	store     *store.Store
	blobStore blob.BlobStore
	logger    *slog.Logger
	stop      chan struct{}

	// 执行间隔
	cleanupInterval time.Duration
	orphanInterval  time.Duration
}

// New 创建调度器
func New(st *store.Store, bs blob.BlobStore, log *slog.Logger) *Scheduler {
	return &Scheduler{
		store:           st,
		blobStore:       bs,
		logger:          log,
		stop:            make(chan struct{}),
		cleanupInterval: 10 * time.Minute,
		orphanInterval:  1 * time.Hour,
	}
}

// Start 启动调度器
func (s *Scheduler) Start() {
	go s.runCleanup()
	go s.runOrphanReclaim()
	s.logger.Info("清理调度器已启动",
		"cleanup_interval", s.cleanupInterval,
		"orphan_interval", s.orphanInterval,
	)
}

// Stop 停止调度器
func (s *Scheduler) Stop() {
	close(s.stop)
}

// runCleanup TTL 清理 + 配额超额删除
func (s *Scheduler) runCleanup() {
	ticker := time.NewTicker(s.cleanupInterval)
	defer ticker.Stop()

	// 启动后立即执行一次
	s.doCleanup()

	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.doCleanup()
		}
	}
}

// doCleanup 执行一轮清理
func (s *Scheduler) doCleanup() {
	ctx := context.Background()

	// 1. 清理过期条目
	deleted, err := s.store.DeleteExpiredClipItems(ctx, 500)
	if err != nil {
		s.logger.Error("清理过期条目失败", "error", err)
	} else if deleted > 0 {
		s.logger.Info("清理过期条目", "deleted", deleted)
	}

	// 2. 清理过期 refresh tokens
	rtDeleted, err := s.store.CleanExpiredRefreshTokens(ctx)
	if err != nil {
		s.logger.Error("清理过期 refresh token 失败", "error", err)
	} else if rtDeleted > 0 {
		s.logger.Info("清理过期 refresh token", "deleted", rtDeleted)
	}

	// 3. 配额超额最旧优先删除
	s.enforceQuotas(ctx)
}

// enforceQuotas 检查所有用户的配额，超出的从最旧条目开始删除
func (s *Scheduler) enforceQuotas(ctx context.Context) {
	// 获取所有用户（最多 1000 个）
	users, err := s.store.ListUsers(ctx, 1000, 0)
	if err != nil {
		s.logger.Error("查询用户列表失败（配额检查）", "error", err)
		return
	}

	for _, u := range users {
		usage, err := s.store.GetUserStorageUsage(ctx, u.ID)
		if err != nil {
			continue
		}
		if usage <= u.QuotaBytes {
			continue
		}

		excess := usage - u.QuotaBytes
		// 查询最旧的条目直到覆盖 excess
		items, err := s.store.GetOldestClipItemsForQuota(ctx, u.ID, excess)
		if err != nil {
			s.logger.Error("查询最旧条目失败", "error", err, "user_id", u.ID)
			continue
		}

		// 删除条目并清理 blob
		var ids []int64
		for _, item := range items {
			ids = append(ids, item.ID)
			// 异步删除 blob
			if item.BlobKey.Valid {
				go func(key string) {
					_ = s.blobStore.Delete(ctx, key)
				}(item.BlobKey.String)
			}
		}

		if len(ids) > 0 {
			deleted, err := s.store.BatchDeleteClipItems(ctx, ids)
			if err != nil {
				s.logger.Error("配额超额删除失败", "error", err, "user_id", u.ID)
			} else {
				s.logger.Info("配额超额清理",
					"user_id", u.ID,
					"username", u.Username,
					"excess_bytes", excess,
					"deleted", deleted,
				)
			}
		}
	}
}

// runOrphanReclaim 孤儿 Blob 回收
func (s *Scheduler) runOrphanReclaim() {
	ticker := time.NewTicker(s.orphanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.doOrphanReclaim()
		}
	}
}

// doOrphanReclaim 执行一轮孤儿 Blob 回收
func (s *Scheduler) doOrphanReclaim() {
	ctx := context.Background()

	// 获取数据库中所有 blob_key
	dbKeys, err := s.store.GetAllBlobKeys(ctx)
	if err != nil {
		s.logger.Error("查询 blob_key 失败", "error", err)
		return
	}

	// 获取本地文件系统中所有 blob 文件
	fsKeys, err := s.blobStore.List(ctx)
	if err != nil {
		s.logger.Error("列出 blob 文件失败", "error", err)
		return
	}

	// 找出文件系统中有但数据库中没有的孤儿
	reclaimed := 0
	for _, key := range fsKeys {
		if !dbKeys[key] {
			if err := s.blobStore.Delete(ctx, key); err != nil {
				s.logger.Error("删除孤儿 blob 失败", "error", err, "key", key)
			} else {
				reclaimed++
			}
		}
	}

	if reclaimed > 0 {
		s.logger.Info("孤儿 Blob 回收", "reclaimed", reclaimed)
	}
}
