package store

import (
	"context"
	"fmt"
)

// SystemStats 系统统计数据
type SystemStats struct {
	UserCount     int64 `json:"user_count"`
	ActiveCount   int64 `json:"active_count"`
	TotalClips    int64 `json:"total_clips"`
	TotalStorage  int64 `json:"total_storage"`
	OnlineCount   int64 `json:"online_count"` // 由 Hub 提供
}

// GetSystemStats 获取系统统计数据
func (s *Store) GetSystemStats(ctx context.Context) (*SystemStats, error) {
	stats := &SystemStats{}

	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&stats.UserCount); err != nil {
		return nil, fmt.Errorf("查询用户数失败: %w", err)
	}

	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE status = 'active'").Scan(&stats.ActiveCount); err != nil {
		return nil, fmt.Errorf("查询活跃用户数失败: %w", err)
	}

	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM clip_items").Scan(&stats.TotalClips); err != nil {
		return nil, fmt.Errorf("查询剪切板数失败: %w", err)
	}

	if err := s.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(size), 0) FROM clip_items").Scan(&stats.TotalStorage); err != nil {
		return nil, fmt.Errorf("查询存储用量失败: %w", err)
	}

	return stats, nil
}

// DeleteUser 删除用户（级联删除 devices, clip_items, refresh_tokens）
func (s *Store) DeleteUser(ctx context.Context, userID int64) error {
	query := fmt.Sprintf("DELETE FROM users WHERE id = %s", s.ph(1))
	res, err := s.db.ExecContext(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("删除用户失败: %w", err)
	}
	return s.assertAffected(res)
}

// GetExpiredClipItems 查询已过期的剪切板条目（TTL 到期）
// 返回 id, user_id, blob_key 供清理调度器使用
func (s *Store) GetExpiredClipItems(ctx context.Context, limit int) ([]*ClipItem, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := fmt.Sprintf(`SELECT id, user_id, blob_key FROM clip_items
		WHERE expires_at IS NOT NULL AND expires_at < %s
		ORDER BY expires_at ASC LIMIT %s`,
		s.now(), s.ph(1))
	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("查询过期条目失败: %w", err)
	}
	defer rows.Close()

	var items []*ClipItem
	for rows.Next() {
		var item ClipItem
		if err := rows.Scan(&item.ID, &item.UserID, &item.BlobKey); err != nil {
			return nil, err
		}
		items = append(items, &item)
	}
	return items, rows.Err()
}

// DeleteExpiredClipItems 批量删除过期条目
func (s *Store) DeleteExpiredClipItems(ctx context.Context, limit int) (int64, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := fmt.Sprintf(`DELETE FROM clip_items WHERE id IN (
		SELECT id FROM clip_items WHERE expires_at IS NOT NULL AND expires_at < %s LIMIT %s)`,
		s.now(), s.ph(1))
	res, err := s.db.ExecContext(ctx, query, limit)
	if err != nil {
		return 0, fmt.Errorf("删除过期条目失败: %w", err)
	}
	return res.RowsAffected()
}

// GetOldestClipItemsForQuota 查询用户最旧的条目（配额超额时优先删除）
// 返回需要删除的条目，使总存储量降到 quota 以内
func (s *Store) GetOldestClipItemsForQuota(ctx context.Context, userID int64, excessBytes int64) ([]*ClipItem, error) {
	// 从最旧开始累计，直到覆盖 excessBytes
	query := fmt.Sprintf(`SELECT id, user_id, blob_key, size FROM clip_items
		WHERE user_id = %s ORDER BY created_at ASC`,
		s.ph(1))
	rows, err := s.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("查询用户最旧条目失败: %w", err)
	}
	defer rows.Close()

	var items []*ClipItem
	accumulated := int64(0)
	for rows.Next() {
		if accumulated >= excessBytes {
			break
		}
		var item ClipItem
		if err := rows.Scan(&item.ID, &item.UserID, &item.BlobKey, &item.Size); err != nil {
			return nil, err
		}
		items = append(items, &item)
		accumulated += item.Size
	}
	return items, rows.Err()
}

// BatchDeleteClipItems 批量删除剪切板条目
func (s *Store) BatchDeleteClipItems(ctx context.Context, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	// 构建占位符列表
	placeholders := ""
	args := make([]any, len(ids))
	for i, id := range ids {
		if i > 0 {
			placeholders += ","
		}
		placeholders += s.ph(i + 1)
		args[i] = id
	}
	query := fmt.Sprintf("DELETE FROM clip_items WHERE id IN (%s)", placeholders)
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("批量删除条目失败: %w", err)
	}
	return res.RowsAffected()
}

// GetAllBlobKeys 获取所有 blob_key（用于孤儿 blob 回收比对）
func (s *Store) GetAllBlobKeys(ctx context.Context) (map[string]bool, error) {
	query := "SELECT blob_key FROM clip_items WHERE blob_key IS NOT NULL"
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("查询所有 blob_key 失败: %w", err)
	}
	defer rows.Close()

	keys := make(map[string]bool)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys[key] = true
	}
	return keys, rows.Err()
}
