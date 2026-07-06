package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ClipItem 剪切板条目实体
type ClipItem struct {
	ID          int64
	UserID      int64
	DeviceID    sql.NullInt64
	Type        string // text | image | file
	MimeType    sql.NullString
	Size        int64
	BlobKey     sql.NullString
	TextContent sql.NullString
	SHA256      sql.NullString
	Meta        string // JSON 字符串
	CreatedAt   string
	ExpiresAt   sql.NullString
}

// CreateClipItem 创建剪切板条目
func (s *Store) CreateClipItem(ctx context.Context, item *ClipItem) (*ClipItem, error) {
	var deviceID, mimeType, blobKey, textContent, sha256, expiresAt any
	if item.DeviceID.Valid {
		deviceID = item.DeviceID.Int64
	}
	if item.MimeType.Valid {
		mimeType = item.MimeType.String
	}
	if item.BlobKey.Valid {
		blobKey = item.BlobKey.String
	}
	if item.TextContent.Valid {
		textContent = item.TextContent.String
	}
	if item.SHA256.Valid {
		sha256 = item.SHA256.String
	}
	if item.ExpiresAt.Valid {
		expiresAt = item.ExpiresAt.String
	}
	meta := item.Meta
	if meta == "" {
		meta = "{}"
	}

	query := fmt.Sprintf(`INSERT INTO clip_items (user_id, device_id, type, mime_type, size, blob_key, text_content, sha256, meta, expires_at)
		VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s)`,
		s.ph(1), s.ph(2), s.ph(3), s.ph(4), s.ph(5), s.ph(6), s.ph(7), s.ph(8), s.ph(9), s.ph(10))

	res, err := s.db.ExecContext(ctx, query,
		item.UserID, deviceID, item.Type, mimeType, item.Size, blobKey, textContent, sha256, meta, expiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("创建 clip_item 失败: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("获取 clip_item ID 失败: %w", err)
	}
	item.ID = id
	return item, nil
}

// GetClipItem 按 ID 查询单条（强制 user_id 隔离）
func (s *Store) GetClipItem(ctx context.Context, id, userID int64) (*ClipItem, error) {
	var item ClipItem
	var deviceID, mimeType, blobKey, textContent, sha256, expiresAt sql.NullString

	query := fmt.Sprintf(`SELECT id, user_id, device_id, type, mime_type, size, blob_key, text_content, sha256, meta, created_at, expires_at
		FROM clip_items WHERE id = %s AND user_id = %s`, s.ph(1), s.ph(2))

	row := s.db.QueryRowContext(ctx, query, id, userID)
	if err := row.Scan(
		&item.ID, &item.UserID, &deviceID, &item.Type, &mimeType, &item.Size,
		&blobKey, &textContent, &sha256, &item.Meta, &item.CreatedAt, &expiresAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("查询 clip_item 失败: %w", err)
	}
	item.DeviceID = toNullInt64(deviceID)
	item.MimeType = mimeType
	item.BlobKey = blobKey
	item.TextContent = textContent
	item.SHA256 = sha256
	item.ExpiresAt = expiresAt
	return &item, nil
}

// GetLatestClipItem 查询用户最新一条条目
func (s *Store) GetLatestClipItem(ctx context.Context, userID int64) (*ClipItem, error) {
	var item ClipItem
	var deviceID, mimeType, blobKey, textContent, sha256, expiresAt sql.NullString

	query := fmt.Sprintf(`SELECT id, user_id, device_id, type, mime_type, size, blob_key, text_content, sha256, meta, created_at, expires_at
		FROM clip_items WHERE user_id = %s ORDER BY id DESC LIMIT 1`, s.ph(1))

	row := s.db.QueryRowContext(ctx, query, userID)
	if err := row.Scan(
		&item.ID, &item.UserID, &deviceID, &item.Type, &mimeType, &item.Size,
		&blobKey, &textContent, &sha256, &item.Meta, &item.CreatedAt, &expiresAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("查询最新 clip_item 失败: %w", err)
	}
	item.DeviceID = toNullInt64(deviceID)
	item.MimeType = mimeType
	item.BlobKey = blobKey
	item.TextContent = textContent
	item.SHA256 = sha256
	item.ExpiresAt = expiresAt
	return &item, nil
}

// ListClipItems 分页查询用户条目列表（游标分页，按 id DESC）
// beforeID=0 表示从最新开始；typeFilter 为空表示不过滤
func (s *Store) ListClipItems(ctx context.Context, userID int64, beforeID int64, limit int, typeFilter string) ([]*ClipItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	var args []any
	args = append(args, userID)
	phIdx := 1

	whereClause := fmt.Sprintf("user_id = %s", s.ph(phIdx))
	phIdx++

	if beforeID > 0 {
		args = append(args, beforeID)
		whereClause += fmt.Sprintf(" AND id < %s", s.ph(phIdx))
		phIdx++
	}
	if typeFilter != "" {
		args = append(args, typeFilter)
		whereClause += fmt.Sprintf(" AND type = %s", s.ph(phIdx))
		phIdx++
	}

	args = append(args, limit)
	query := fmt.Sprintf(`SELECT id, user_id, device_id, type, mime_type, size, blob_key, text_content, sha256, meta, created_at, expires_at
		FROM clip_items WHERE %s ORDER BY id DESC LIMIT %s`, whereClause, s.ph(phIdx))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("查询 clip_items 列表失败: %w", err)
	}
	defer rows.Close()

	var items []*ClipItem
	for rows.Next() {
		var item ClipItem
		var deviceID, mimeType, blobKey, textContent, sha256, expiresAt sql.NullString
		if err := rows.Scan(
			&item.ID, &item.UserID, &deviceID, &item.Type, &mimeType, &item.Size,
			&blobKey, &textContent, &sha256, &item.Meta, &item.CreatedAt, &expiresAt,
		); err != nil {
			return nil, err
		}
		item.DeviceID = toNullInt64(deviceID)
		item.MimeType = mimeType
		item.BlobKey = blobKey
		item.TextContent = textContent
		item.SHA256 = sha256
		item.ExpiresAt = expiresAt
		items = append(items, &item)
	}
	return items, rows.Err()
}

// DeleteClipItem 删除单条（强制 user_id 隔离）
func (s *Store) DeleteClipItem(ctx context.Context, id, userID int64) (*ClipItem, error) {
	// 先查询（用于返回被删除条目的 blob_key 供清理）
	item, err := s.GetClipItem(ctx, id, userID)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`DELETE FROM clip_items WHERE id = %s AND user_id = %s`, s.ph(1), s.ph(2))
	_, err = s.db.ExecContext(ctx, query, id, userID)
	if err != nil {
		return nil, fmt.Errorf("删除 clip_item 失败: %w", err)
	}
	return item, nil
}

// GetUserStorageUsage 统计用户当前存储用量（所有条目 size 之和）
func (s *Store) GetUserStorageUsage(ctx context.Context, userID int64) (int64, error) {
	var total sql.NullInt64
	query := fmt.Sprintf(`SELECT COALESCE(SUM(size), 0) FROM clip_items WHERE user_id = %s`, s.ph(1))
	err := s.db.QueryRowContext(ctx, query, userID).Scan(&total)
	if err != nil {
		return 0, err
	}
	return total.Int64, nil
}

// ListClipItemsSince 增量同步：查询 sinceID 之后的新条目（按 id ASC）
// 用于 WebSocket 断线重连后的增量同步
func (s *Store) ListClipItemsSince(ctx context.Context, userID, sinceID int64, limit int) ([]*ClipItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}

	query := fmt.Sprintf(`SELECT id, user_id, device_id, type, mime_type, size, blob_key, text_content, sha256, meta, created_at, expires_at
		FROM clip_items WHERE user_id = %s AND id > %s ORDER BY id ASC LIMIT %s`,
		s.ph(1), s.ph(2), s.ph(3))

	rows, err := s.db.QueryContext(ctx, query, userID, sinceID, limit)
	if err != nil {
		return nil, fmt.Errorf("增量同步查询失败: %w", err)
	}
	defer rows.Close()

	var items []*ClipItem
	for rows.Next() {
		var item ClipItem
		var deviceID, mimeType, blobKey, textContent, sha256, expiresAt sql.NullString
		if err := rows.Scan(
			&item.ID, &item.UserID, &deviceID, &item.Type, &mimeType, &item.Size,
			&blobKey, &textContent, &sha256, &item.Meta, &item.CreatedAt, &expiresAt,
		); err != nil {
			return nil, err
		}
		item.DeviceID = toNullInt64(deviceID)
		item.MimeType = mimeType
		item.BlobKey = blobKey
		item.TextContent = textContent
		item.SHA256 = sha256
		item.ExpiresAt = expiresAt
		items = append(items, &item)
	}
	return items, rows.Err()
}

// toNullInt64 将 NullString（包含整数字符串）转为 NullInt64
// device_id 在 SQLite 中以 TEXT 存储时需要转换
func toNullInt64(ns sql.NullString) sql.NullInt64 {
	if !ns.Valid {
		return sql.NullInt64{}
	}
	var n int64
	if _, err := fmt.Sscanf(ns.String, "%d", &n); err != nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: n, Valid: true}
}
