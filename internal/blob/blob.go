package blob

import (
	"context"
	"io"
	"time"
)

// BlobStore 对象存储抽象接口
// 实现：local（本地 FS）、s3（S3 兼容，阶段 5）
type BlobStore interface {
	// Save 存储 blob，返回 blobKey。reader 流式读取，maxBytes 限制大小
	Save(ctx context.Context, reader io.Reader, blobKey string, maxBytes int64) (int64, error)

	// Open 读取 blob，返回 reader，调用方负责关闭
	Open(ctx context.Context, blobKey string) (io.ReadCloser, error)

	// Delete 删除 blob，不存在不报错
	Delete(ctx context.Context, blobKey string) error

	// Exists 检查 blob 是否存在
	Exists(ctx context.Context, blobKey string) (bool, error)

	// List 列出所有 blob 的 key（用于孤儿回收）
	List(ctx context.Context) ([]string, error)

	// ModTime 返回 blob 的修改时间（孤儿回收宽限期判断）
	ModTime(ctx context.Context, blobKey string) (time.Time, error)

	// CleanStaleTmpFiles 清理超过 maxAge 的残留 .tmp 临时文件，返回删除数量
	CleanStaleTmpFiles(ctx context.Context, maxAge time.Duration) (int, error)
}

// GenerateBlobKey 生成 blob 存储键
// 规则：blobs/<user_id>/<yyyy>/<mm>/<uuid>
func GenerateBlobKey(userID int64, year, month int, uuid string) string {
	return formatBlobKey(userID, year, month, uuid)
}
