package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Device 设备实体
type Device struct {
	ID            int64
	UserID        int64
	Name          string
	Type          string // ios-shortcut | desktop | web | android
	APITokenHash  sql.NullString
	LastSeenAt    sql.NullString
	CreatedAt     string
}

// CreateDevice 创建设备，返回带 ID 的设备
func (s *Store) CreateDevice(ctx context.Context, d *Device) (*Device, error) {
	var tokenHash any
	if d.APITokenHash.Valid {
		tokenHash = d.APITokenHash.String
	} else {
		tokenHash = nil
	}
	query := fmt.Sprintf(`INSERT INTO devices (user_id, name, type, api_token_hash)
		VALUES (%s, %s, %s, %s)`,
		s.ph(1), s.ph(2), s.ph(3), s.ph(4))

	args := []any{d.UserID, d.Name, d.Type, tokenHash}

	if s.db.Dialect == "sqlite" {
		res, err := s.db.ExecContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("创建设备失败: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("获取设备 ID 失败: %w", err)
		}
		d.ID = id
		// created_at 由 DB 默认值生成，回填以保证创建响应携带时间戳
		if err := s.db.QueryRowContext(ctx, `SELECT created_at FROM devices WHERE id = `+s.ph(1), id).Scan(&d.CreatedAt); err != nil {
			return nil, fmt.Errorf("回填设备创建时间失败: %w", err)
		}
		return d, nil
	}

	// PostgreSQL: pgx 不支持 LastInsertId，改用 RETURNING 取回 ID 和创建时间
	query += " RETURNING id, created_at"
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&d.ID, &d.CreatedAt); err != nil {
		return nil, fmt.Errorf("创建设备失败: %w", err)
	}
	return d, nil
}

// GetDeviceByAPIToken 按 API Token 哈希查询设备（含关联用户）
func (s *Store) GetDeviceByAPIToken(ctx context.Context, tokenHash string) (*Device, *User, error) {
	query := fmt.Sprintf(`SELECT d.id, d.user_id, d.name, d.type, d.api_token_hash, d.last_seen_at, d.created_at,
		u.id, u.username, u.email, u.password_hash, u.role, u.status,
		u.max_item_size, u.quota_bytes, u.retention_days, u.created_at, u.updated_at
		FROM devices d
		JOIN users u ON d.user_id = u.id
		WHERE d.api_token_hash = %s AND u.status = 'active'`,
		s.ph(1))

	var d Device
	var u User
	var dTokenHash sql.NullString
	var dLastSeen sql.NullString
	var uEmail sql.NullString

	row := s.db.QueryRowContext(ctx, query, tokenHash)
	if err := row.Scan(
		&d.ID, &d.UserID, &d.Name, &d.Type, &dTokenHash, &dLastSeen, &d.CreatedAt,
		&u.ID, &u.Username, &uEmail, &u.PasswordHash, &u.Role, &u.Status,
		&u.MaxItemSize, &u.QuotaBytes, &u.RetentionDays, &u.CreatedAt, &u.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("按 Token 查询设备失败: %w", err)
	}
	d.APITokenHash = dTokenHash
	d.LastSeenAt = dLastSeen
	u.Email = uEmail
	return &d, &u, nil
}

// ListDevicesByUser 查询用户的所有设备
func (s *Store) ListDevicesByUser(ctx context.Context, userID int64) ([]*Device, error) {
	query := fmt.Sprintf(`SELECT id, user_id, name, type, api_token_hash, last_seen_at, created_at
		FROM devices WHERE user_id = %s ORDER BY id DESC`, s.ph(1))
	rows, err := s.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("查询设备列表失败: %w", err)
	}
	defer rows.Close()

	var devices []*Device
	for rows.Next() {
		var d Device
		var tokenHash sql.NullString
		var lastSeen sql.NullString
		if err := rows.Scan(&d.ID, &d.UserID, &d.Name, &d.Type, &tokenHash, &lastSeen, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.APITokenHash = tokenHash
		d.LastSeenAt = lastSeen
		devices = append(devices, &d)
	}
	return devices, rows.Err()
}

// UpdateDeviceLastSeen 更新设备最后活跃时间
func (s *Store) UpdateDeviceLastSeen(ctx context.Context, deviceID int64) error {
	query := fmt.Sprintf(`UPDATE devices SET last_seen_at = %s WHERE id = %s`, s.now(), s.ph(1))
	_, err := s.db.ExecContext(ctx, query, deviceID)
	return err
}

// RevokeDeviceAPIToken 吊销设备的 API Token（置空 hash）
func (s *Store) RevokeDeviceAPIToken(ctx context.Context, deviceID, userID int64) error {
	query := fmt.Sprintf(`UPDATE devices SET api_token_hash = NULL WHERE id = %s AND user_id = %s`,
		s.ph(1), s.ph(2))
	res, err := s.db.ExecContext(ctx, query, deviceID, userID)
	if err != nil {
		return fmt.Errorf("吊销 API Token 失败: %w", err)
	}
	return s.assertAffected(res)
}

// DeleteDevice 删除设备
func (s *Store) DeleteDevice(ctx context.Context, deviceID, userID int64) error {
	query := fmt.Sprintf(`DELETE FROM devices WHERE id = %s AND user_id = %s`, s.ph(1), s.ph(2))
	res, err := s.db.ExecContext(ctx, query, deviceID, userID)
	if err != nil {
		return fmt.Errorf("删除设备失败: %w", err)
	}
	return s.assertAffected(res)
}
