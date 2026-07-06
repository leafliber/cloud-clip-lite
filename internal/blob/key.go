package blob

import "fmt"

// formatBlobKey 生成 blob 存储键
// 规则：blobs/<user_id>/<yyyy>/<mm>/<uuid>
func formatBlobKey(userID int64, year, month int, uuid string) string {
	return fmt.Sprintf("blobs/%d/%04d/%02d/%s", userID, year, month, uuid)
}
