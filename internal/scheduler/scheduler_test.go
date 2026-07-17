package scheduler

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/leaf/cloud-clip-lite/internal/blob"
	"github.com/leaf/cloud-clip-lite/internal/config"
	"github.com/leaf/cloud-clip-lite/internal/db"
	"github.com/leaf/cloud-clip-lite/internal/migrate"
	"github.com/leaf/cloud-clip-lite/internal/store"
)

// newTestEnv 创建内存 SQLite Store + 临时目录 BlobStore + Scheduler，返回 blob 根目录便于构造文件
func newTestEnv(t *testing.T) (*store.Store, blob.BlobStore, *Scheduler, string) {
	t.Helper()
	ctx := context.Background()

	cfg := &config.Config{
		SQLitePath: ":memory:",
		JWTSecret:  "test-secret-at-least-32-bytes-long!!!",
	}
	database, err := db.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if err := migrate.Run(ctx, database); err != nil {
		t.Fatalf("运行迁移失败: %v", err)
	}

	st := store.New(database)
	blobDir := t.TempDir()
	bs, err := blob.NewLocalStore(blobDir)
	if err != nil {
		t.Fatalf("创建测试 BlobStore 失败: %v", err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return st, bs, New(st, bs, log), blobDir
}

// createUser 创建测试用户
func createUser(t *testing.T, st *store.Store, username string) *store.User {
	t.Helper()
	u, err := st.CreateUser(context.Background(), &store.User{
		Username:     username,
		PasswordHash: "hash",
	})
	if err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	return u
}

func TestDoOrphanReclaim_GracePeriod(t *testing.T) {
	st, bs, s, blobDir := newTestEnv(t)
	ctx := context.Background()
	user := createUser(t, st, "orphan-user")

	// 1. DB 中有记录的 blob：不应被回收
	trackedKey := "blobs/1/2026/07/tracked"
	if _, err := bs.Save(ctx, bytes.NewReader([]byte("tracked")), trackedKey, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateClipItem(ctx, &store.ClipItem{
		UserID:  user.ID,
		Type:    "file",
		Size:    7,
		BlobKey: sql.NullString{String: trackedKey, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}

	// 2. 无 DB 记录但 mtime 很近（模拟刚 rename、尚未插库）：宽限期内不应被删
	freshKey := "blobs/1/2026/07/fresh-orphan"
	if _, err := bs.Save(ctx, bytes.NewReader([]byte("fresh")), freshKey, 100); err != nil {
		t.Fatal(err)
	}

	// 3. 无 DB 记录且 mtime 超过宽限期：应被回收
	staleKey := "blobs/1/2026/07/stale-orphan"
	if _, err := bs.Save(ctx, bytes.NewReader([]byte("stale")), staleKey, 100); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-orphanGracePeriod - time.Minute)
	if err := os.Chtimes(filepath.Join(blobDir, filepath.FromSlash(staleKey)), old, old); err != nil {
		t.Fatal(err)
	}

	s.doOrphanReclaim()

	if exists, _ := bs.Exists(ctx, trackedKey); !exists {
		t.Error("DB 有记录的 blob 不应被回收")
	}
	if exists, _ := bs.Exists(ctx, freshKey); !exists {
		t.Error("宽限期内的 blob 不应被回收")
	}
	if exists, _ := bs.Exists(ctx, staleKey); exists {
		t.Error("超过宽限期的孤儿 blob 应被回收")
	}
}

func TestDoOrphanReclaim_CleansStaleTmp(t *testing.T) {
	_, _, s, blobDir := newTestEnv(t)

	// 制造一个超过 24h 的残留 .tmp 文件
	sub := filepath.Join(blobDir, "blobs", "1", "2026", "07")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	tmpFile := filepath.Join(sub, "crash.tmp")
	if err := os.WriteFile(tmpFile, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(tmpFile, old, old); err != nil {
		t.Fatal(err)
	}

	s.doOrphanReclaim()

	if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
		t.Error("超过 24h 的 .tmp 文件应被清理")
	}
}

func TestDoCleanup_ExpiredItemDeletesBlob(t *testing.T) {
	st, bs, s, _ := newTestEnv(t)
	ctx := context.Background()
	user := createUser(t, st, "expire-user")

	// 保存 blob 并创建一条已过期的条目
	blobKey := "blobs/1/2026/07/expired"
	if _, err := bs.Save(ctx, bytes.NewReader([]byte("expired-content")), blobKey, 1024); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour).UTC().Format("2006-01-02 15:04:05")
	if _, err := st.CreateClipItem(ctx, &store.ClipItem{
		UserID:    user.ID,
		Type:      "file",
		Size:      15,
		BlobKey:   sql.NullString{String: blobKey, Valid: true},
		ExpiresAt: sql.NullString{String: past, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}

	s.doCleanup()

	// DB 行应已删除
	keys, err := st.GetAllBlobKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if keys[blobKey] {
		t.Error("过期条目的 DB 行应被删除")
	}
	// 对应 blob 也应被删除
	if exists, _ := bs.Exists(ctx, blobKey); exists {
		t.Error("过期条目的 blob 应被删除")
	}
}

func TestScheduler_StopWaits(t *testing.T) {
	_, _, s, _ := newTestEnv(t)

	// 缩短间隔以便测试
	s.cleanupInterval = time.Hour
	s.orphanInterval = time.Hour

	s.Start()
	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Stop 正常返回（含等待 run 循环退出）
	case <-time.After(5 * time.Second):
		t.Fatal("Stop 应在 5 秒内返回")
	}
}
