package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/leaf/cloud-clip-lite/internal/blob"
	"github.com/leaf/cloud-clip-lite/internal/store"
)

const (
	// orphanGracePeriod 孤儿回收宽限期：新文件可能刚 rename 完成、尚未插入 DB，跳过避免误删
	orphanGracePeriod = 10 * time.Minute
	// staleTmpMaxAge 残留 .tmp 临时文件的清理阈值
	staleTmpMaxAge = 24 * time.Hour
	// blobDeleteTimeout 批量删除 blob 的超时时间
	blobDeleteTimeout = 30 * time.Second
)

// Scheduler 定时任务调度器
// 负责：TTL 清理、配额超额最旧优先删除、孤儿 Blob 回收
type Scheduler struct {
	store     *store.Store
	blobStore blob.BlobStore
	logger    *slog.Logger
	stop      chan struct{}
	wg        sync.WaitGroup

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
	s.wg.Add(2)
	go s.runCleanup()
	go s.runOrphanReclaim()
	s.logger.Info("清理调度器已启动",
		"cleanup_interval", s.cleanupInterval,
		"orphan_interval", s.orphanInterval,
	)
}

// Stop 停止调度器并等待在途任务结束
func (s *Scheduler) Stop() {
	close(s.stop)
	s.wg.Wait()
}

// runCleanup TTL 清理 + 配额超额删除
func (s *Scheduler) runCleanup() {
	defer s.wg.Done()
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

	// 1. 清理过期条目（先取 blob_key，删 DB 行后再删 blob）
	expired, err := s.store.GetExpiredClipItems(ctx, 500)
	if err != nil {
		s.logger.Error("查询过期条目失败", "error", err)
	} else if len(expired) > 0 {
		ids := make([]int64, 0, len(expired))
		var blobKeys []string
		for _, item := range expired {
			ids = append(ids, item.ID)
			if item.BlobKey.Valid {
				blobKeys = append(blobKeys, item.BlobKey.String)
			}
		}
		deleted, err := s.store.BatchDeleteClipItems(ctx, ids)
		if err != nil {
			s.logger.Error("清理过期条目失败", "error", err)
		} else {
			if deleted > 0 {
				s.logger.Info("清理过期条目", "deleted", deleted)
			}
			s.deleteBlobs(ctx, blobKeys)
		}
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
	// 分页遍历全部用户
	const pageSize = 1000
	for offset := 0; ; offset += pageSize {
		users, err := s.store.ListUsers(ctx, pageSize, offset)
		if err != nil {
			s.logger.Error("查询用户列表失败（配额检查）", "error", err)
			return
		}

		for _, u := range users {
			s.enforceUserQuota(ctx, u)
		}

		if len(users) < pageSize {
			break
		}
	}
}

// enforceUserQuota 检查单个用户的配额，超出部分从最旧条目开始删除
func (s *Scheduler) enforceUserQuota(ctx context.Context, u *store.User) {
	usage, err := s.store.GetUserStorageUsage(ctx, u.ID)
	if err != nil {
		return
	}
	if usage <= u.QuotaBytes {
		return
	}

	excess := usage - u.QuotaBytes
	// 查询最旧的条目直到覆盖 excess
	items, err := s.store.GetOldestClipItemsForQuota(ctx, u.ID, excess)
	if err != nil {
		s.logger.Error("查询最旧条目失败", "error", err, "user_id", u.ID)
		return
	}

	// 先删 DB 行成功后再删 blob，避免出现"有条目但下载不到内容"的悬空记录
	ids := make([]int64, 0, len(items))
	var blobKeys []string
	for _, item := range items {
		ids = append(ids, item.ID)
		if item.BlobKey.Valid {
			blobKeys = append(blobKeys, item.BlobKey.String)
		}
	}
	if len(ids) == 0 {
		return
	}

	deleted, err := s.store.BatchDeleteClipItems(ctx, ids)
	if err != nil {
		s.logger.Error("配额超额删除失败", "error", err, "user_id", u.ID)
		return
	}
	s.logger.Info("配额超额清理",
		"user_id", u.ID,
		"username", u.Username,
		"excess_bytes", excess,
		"deleted", deleted,
	)
	s.deleteBlobs(ctx, blobKeys)
}

// deleteBlobs 并发删除 blob：带超时 context，WaitGroup 等待完成，失败记日志
func (s *Scheduler) deleteBlobs(ctx context.Context, keys []string) {
	if len(keys) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, blobDeleteTimeout)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(len(keys))
	for _, key := range keys {
		go func(key string) {
			defer wg.Done()
			if err := s.blobStore.Delete(ctx, key); err != nil {
				s.logger.Error("删除 blob 失败", "error", err, "key", key)
			}
		}(key)
	}
	wg.Wait()
}

// runOrphanReclaim 孤儿 Blob 回收
func (s *Scheduler) runOrphanReclaim() {
	defer s.wg.Done()
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
		if dbKeys[key] {
			continue
		}
		// 宽限期：跳过最近新建的文件——上传先 rename 后插 DB，
		// 两快照之间刚落盘的文件可能被误判为孤儿
		mt, err := s.blobStore.ModTime(ctx, key)
		if err != nil {
			s.logger.Error("获取 blob 修改时间失败", "error", err, "key", key)
			continue
		}
		if time.Since(mt) < orphanGracePeriod {
			continue
		}
		if err := s.blobStore.Delete(ctx, key); err != nil {
			s.logger.Error("删除孤儿 blob 失败", "error", err, "key", key)
		} else {
			reclaimed++
		}
	}

	if reclaimed > 0 {
		s.logger.Info("孤儿 Blob 回收", "reclaimed", reclaimed)
	}

	// 顺便清理 Save 中途崩溃残留的陈旧 .tmp 文件（List 会跳过它们，需要专门清理）
	removed, err := s.blobStore.CleanStaleTmpFiles(ctx, staleTmpMaxAge)
	if err != nil {
		s.logger.Error("清理残留临时文件失败", "error", err)
	} else if removed > 0 {
		s.logger.Info("清理残留临时文件", "removed", removed)
	}
}
