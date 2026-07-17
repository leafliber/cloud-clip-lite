package blob

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLocalStore_SaveAndOpen(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore 失败: %v", err)
	}

	ctx := context.Background()
	content := []byte("hello clipboard blob!")
	blobKey := "blobs/1/2026/07/test-uuid-123"

	// Save
	written, err := store.Save(ctx, bytes.NewReader(content), blobKey, 1024)
	if err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	if written != int64(len(content)) {
		t.Errorf("写入字节数 = %d, 期望 %d", written, len(content))
	}

	// 验证文件存在
	exists, _ := store.Exists(ctx, blobKey)
	if !exists {
		t.Fatal("文件应存在")
	}

	// Open
	rc, err := store.Open(ctx, blobKey)
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	defer rc.Close()

	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, content) {
		t.Errorf("读取内容不匹配")
	}
}

func TestLocalStore_Save_TooLarge(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewLocalStore(dir)
	ctx := context.Background()

	content := make([]byte, 100)
	for i := range content {
		content[i] = 'x'
	}

	// maxBytes=50，但内容 100 字节
	_, err := store.Save(ctx, bytes.NewReader(content), "blobs/1/2026/07/big", 50)
	if err != ErrItemTooLarge {
		t.Fatalf("期望 ErrItemTooLarge, 实际 %v", err)
	}

	// 超限时临时文件应已清理
	_, err = os.Stat(filepath.Join(dir, "blobs", "1", "2026", "07", "big"))
	if !os.IsNotExist(err) {
		t.Error("超限时应删除临时文件")
	}
}

func TestLocalStore_Open_NotFound(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewLocalStore(dir)
	ctx := context.Background()

	_, err := store.Open(ctx, "nonexistent")
	if err != ErrBlobNotFound {
		t.Errorf("期望 ErrBlobNotFound, 实际 %v", err)
	}
}

func TestLocalStore_Delete(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewLocalStore(dir)
	ctx := context.Background()

	blobKey := "blobs/1/2026/07/to-delete"
	store.Save(ctx, bytes.NewReader([]byte("data")), blobKey, 100)

	// 删除
	err := store.Delete(ctx, blobKey)
	if err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}

	// 删除后不存在
	exists, _ := store.Exists(ctx, blobKey)
	if exists {
		t.Error("删除后应不存在")
	}

	// 重复删除不报错
	err = store.Delete(ctx, blobKey)
	if err != nil {
		t.Errorf("重复删除不应报错: %v", err)
	}
}

func TestLocalStore_Exists(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewLocalStore(dir)
	ctx := context.Background()

	// 不存在
	exists, _ := store.Exists(ctx, "nope")
	if exists {
		t.Error("应不存在")
	}

	// 存在
	store.Save(ctx, bytes.NewReader([]byte("x")), "blobs/1/2026/07/e", 100)
	exists, _ = store.Exists(ctx, "blobs/1/2026/07/e")
	if !exists {
		t.Error("应存在")
	}
}

func TestLocalStore_Save_CreatesNestedDirs(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewLocalStore(dir)
	ctx := context.Background()

	deepKey := "blobs/999/2026/07/deep/nested/path/uuid"
	_, err := store.Save(ctx, bytes.NewReader([]byte("nested")), deepKey, 100)
	if err != nil {
		t.Fatalf("Save 深层路径失败: %v", err)
	}

	// 验证嵌套目录已创建
	fullPath := filepath.Join(dir, "blobs", "999", "2026", "07", "deep", "nested", "path", "uuid")
	_, err = os.Stat(fullPath)
	if err != nil {
		t.Errorf("嵌套文件应存在: %v", err)
	}
}

func TestGenerateBlobKey(t *testing.T) {
	key := GenerateBlobKey(42, 2026, 7, "abc-123")
	expected := "blobs/42/2026/07/abc-123"
	if key != expected {
		t.Errorf("blobKey = %s, 期望 %s", key, expected)
	}

	// 验证路径结构
	parts := strings.Split(key, "/")
	if len(parts) != 5 {
		t.Errorf("blobKey 应有 5 段, 实际 %d", len(parts))
	}
	if parts[0] != "blobs" {
		t.Errorf("第一段应为 blobs, 实际 %s", parts[0])
	}
}

func TestLocalStore_Save_ExactBoundary(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewLocalStore(dir)
	ctx := context.Background()

	// 文件大小恰好等于 maxBytes，应正常保存（与文本路径 size > MaxItemSize 判定一致）
	content := bytes.Repeat([]byte("a"), 100)
	written, err := store.Save(ctx, bytes.NewReader(content), "blobs/1/2026/07/exact", 100)
	if err != nil {
		t.Fatalf("恰好 maxBytes 的合法文件不应被拒: %v", err)
	}
	if written != 100 {
		t.Errorf("written = %d, 期望 100", written)
	}

	// 验证内容完整
	rc, err := store.Open(ctx, "blobs/1/2026/07/exact")
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, content) {
		t.Error("内容不完整")
	}
}

func TestLocalStore_Save_OneByteOver(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewLocalStore(dir)
	ctx := context.Background()

	// 超出 1 字节应被拒
	content := bytes.Repeat([]byte("b"), 101)
	_, err := store.Save(ctx, bytes.NewReader(content), "blobs/1/2026/07/over", 100)
	if err != ErrItemTooLarge {
		t.Fatalf("期望 ErrItemTooLarge, 实际 %v", err)
	}

	// 超限后不应留下任何文件（含 .tmp）
	matches, _ := filepath.Glob(filepath.Join(dir, "blobs", "1", "2026", "07", "over*"))
	if len(matches) != 0 {
		t.Errorf("超限后不应残留文件, 实际: %v", matches)
	}
}

func TestLocalStore_Save_FilePermission(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不支持 Unix 权限位")
	}
	dir := t.TempDir()
	store, _ := NewLocalStore(dir)
	ctx := context.Background()

	blobKey := "blobs/1/2026/07/perm"
	if _, err := store.Save(ctx, bytes.NewReader([]byte("secret")), blobKey, 100); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	// 剪切板内容可能含敏感信息，文件应仅属主可读写
	info, err := os.Stat(filepath.Join(dir, "blobs", "1", "2026", "07", "perm"))
	if err != nil {
		t.Fatalf("Stat 失败: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("文件权限 = %o, 期望 600", perm)
	}
}

func TestLocalStore_ModTime(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewLocalStore(dir)
	ctx := context.Background()

	blobKey := "blobs/1/2026/07/mt"
	if _, err := store.Save(ctx, bytes.NewReader([]byte("data")), blobKey, 100); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	mt, err := store.ModTime(ctx, blobKey)
	if err != nil {
		t.Fatalf("ModTime 失败: %v", err)
	}
	if time.Since(mt) > time.Minute {
		t.Error("刚保存的文件 ModTime 应在最近")
	}

	// 不存在应返回 ErrBlobNotFound
	_, err = store.ModTime(ctx, "nonexistent")
	if err != ErrBlobNotFound {
		t.Errorf("期望 ErrBlobNotFound, 实际 %v", err)
	}
}

func TestLocalStore_CleanStaleTmpFiles(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewLocalStore(dir)
	ctx := context.Background()

	// 制造一个陈旧 .tmp 和一个新 .tmp
	sub := filepath.Join(dir, "blobs", "1", "2026", "07")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	staleTmp := filepath.Join(sub, "stale.tmp")
	freshTmp := filepath.Join(sub, "fresh.tmp")
	for _, p := range []string{staleTmp, freshTmp} {
		if err := os.WriteFile(p, []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(staleTmp, old, old); err != nil {
		t.Fatal(err)
	}

	removed, err := store.CleanStaleTmpFiles(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("CleanStaleTmpFiles 失败: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, 期望 1", removed)
	}
	if _, err := os.Stat(staleTmp); !os.IsNotExist(err) {
		t.Error("陈旧 .tmp 应被删除")
	}
	if _, err := os.Stat(freshTmp); err != nil {
		t.Error("新 .tmp 不应被删除")
	}
}
