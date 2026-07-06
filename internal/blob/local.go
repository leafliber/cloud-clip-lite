package blob

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// LocalStore 本地文件系统 BlobStore 实现
type LocalStore struct {
	rootDir string
}

// NewLocalStore 创建本地 FS BlobStore
func NewLocalStore(rootDir string) (*LocalStore, error) {
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建 blob 根目录失败: %w", err)
	}
	return &LocalStore{rootDir: rootDir}, nil
}

// fullPath 返回 blobKey 对应的完整文件路径
func (s *LocalStore) fullPath(blobKey string) string {
	return filepath.Join(s.rootDir, filepath.FromSlash(blobKey))
}

// Save 存储 blob 到本地文件系统
// 流式写入，超过 maxBytes 立即中断并删除已写入部分
func (s *LocalStore) Save(ctx context.Context, reader io.Reader, blobKey string, maxBytes int64) (int64, error) {
	fullPath := s.fullPath(blobKey)

	// 确保目录存在
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("创建 blob 目录失败: %w", err)
	}

	// 先写入临时文件，成功后重命名
	tmpPath := fullPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return 0, fmt.Errorf("创建临时文件失败: %w", err)
	}

	// 限制读取器
	limitedReader := &maxBytesReader{reader: reader, max: maxBytes}

	written, err := io.Copy(f, limitedReader)
	closeErr := f.Close()

	if err != nil {
		_ = os.Remove(tmpPath)
		if written >= maxBytes {
			return written, ErrItemTooLarge
		}
		return 0, fmt.Errorf("写入 blob 失败: %w", err)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return 0, fmt.Errorf("关闭临时文件失败: %w", closeErr)
	}

	// 重命名到最终路径
	if err := os.Rename(tmpPath, fullPath); err != nil {
		_ = os.Remove(tmpPath)
		return 0, fmt.Errorf("重命名 blob 失败: %w", err)
	}

	return written, nil
}

// Open 打开 blob 文件
func (s *LocalStore) Open(ctx context.Context, blobKey string) (io.ReadCloser, error) {
	f, err := os.Open(s.fullPath(blobKey))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrBlobNotFound
		}
		return nil, fmt.Errorf("打开 blob 失败: %w", err)
	}
	return f, nil
}

// Delete 删除 blob 文件
func (s *LocalStore) Delete(ctx context.Context, blobKey string) error {
	err := os.Remove(s.fullPath(blobKey))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Exists 检查 blob 是否存在
func (s *LocalStore) Exists(ctx context.Context, blobKey string) (bool, error) {
	_, err := os.Stat(s.fullPath(blobKey))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// List 列出所有 blob 的 key（相对路径，用于孤儿回收）
func (s *LocalStore) List(ctx context.Context) ([]string, error) {
	var keys []string
	rootAbs, err := filepath.Abs(s.rootDir)
	if err != nil {
		return nil, fmt.Errorf("获取根目录绝对路径失败: %w", err)
	}

	err = filepath.Walk(rootAbs, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		// 跳过临时文件
		if filepath.Ext(path) == ".tmp" {
			return nil
		}
		// 转为相对 key（使用 forward slash）
		rel, err := filepath.Rel(rootAbs, path)
		if err != nil {
			return err
		}
		keys = append(keys, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("遍历 blob 目录失败: %w", err)
	}
	return keys, nil
}

// ---------- 错误定义 ----------

var (
	// ErrItemTooLarge 超过单条大小上限
	ErrItemTooLarge = fmt.Errorf("ITEM_TOO_LARGE")
	// ErrBlobNotFound blob 不存在
	ErrBlobNotFound = fmt.Errorf("BLOB_NOT_FOUND")
)

// maxBytesReader 限制最大读取字节数
type maxBytesReader struct {
	reader io.Reader
	max    int64
	read   int64
}

func (r *maxBytesReader) Read(p []byte) (int, error) {
	if r.read >= r.max {
		return 0, ErrItemTooLarge
	}
	// 限制本次读取量
	remaining := r.max - r.read
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := r.reader.Read(p)
	r.read += int64(n)
	return n, err
}
