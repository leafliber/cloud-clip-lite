package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// CreateUser 创建用户
func (s *Store) CreateUser(ctx context.Context, u *User) (*User, error) {
	query := fmt.Sprintf(`INSERT INTO users (username, email, password_hash, role, status,
		max_item_size, quota_bytes, retention_days)
		VALUES (%s, %s, %s, %s, %s, %s, %s, %s)`,
		s.ph(1), s.ph(2), s.ph(3), s.ph(4), s.ph(5), s.ph(6), s.ph(7), s.ph(8),
	)

	var email any
	if u.Email.Valid {
		email = u.Email.String
	} else {
		email = nil
	}

	role := u.Role
	if role == "" {
		role = "user"
	}
	status := u.Status
	if status == "" {
		status = "active"
	}
	maxSize := u.MaxItemSize
	if maxSize == 0 {
		maxSize = 10485760
	}
	quota := u.QuotaBytes
	if quota == 0 {
		quota = 1073741824
	}
	retention := u.RetentionDays
	if retention == 0 {
		retention = 30
	}

	res, err := s.db.ExecContext(ctx, query,
		u.Username, email, u.PasswordHash, role, status, maxSize, quota, retention,
	)
	if err != nil {
		return nil, fmt.Errorf("创建用户失败: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		// SQLite 在某些驱动下可能不支持 LastInsertId，回退查询
		return s.GetUserByUsername(ctx, u.Username)
	}
	u.ID = id
	u.Role = role
	u.Status = status
	u.MaxItemSize = maxSize
	u.QuotaBytes = quota
	u.RetentionDays = retention
	return u, nil
}

// GetUserByID 按 ID 查询用户
func (s *Store) GetUserByID(ctx context.Context, id int64) (*User, error) {
	var u User
	var email sql.NullString
	query := fmt.Sprintf(`SELECT id, username, email, password_hash, role, status,
		max_item_size, quota_bytes, retention_days, created_at, updated_at
		FROM users WHERE id = %s`, s.ph(1))
	row := s.db.QueryRowContext(ctx, query, id)
	if err := row.Scan(
		&u.ID, &u.Username, &email, &u.PasswordHash, &u.Role, &u.Status,
		&u.MaxItemSize, &u.QuotaBytes, &u.RetentionDays, &u.CreatedAt, &u.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}
	u.Email = email
	return &u, nil
}

// UpdateUserPassword 更新用户密码
func (s *Store) UpdateUserPassword(ctx context.Context, userID int64, passwordHash string) error {
	query := fmt.Sprintf(`UPDATE users SET password_hash = %s, updated_at = %s WHERE id = %s`,
		s.ph(1), s.now(), s.ph(2))
	res, err := s.db.ExecContext(ctx, query, passwordHash, userID)
	if err != nil {
		return fmt.Errorf("更新密码失败: %w", err)
	}
	return s.assertAffected(res)
}

// UpdateUserSettings 更新用户配额设置
func (s *Store) UpdateUserSettings(ctx context.Context, userID int64, maxItemSize, quotaBytes int64, retentionDays int) error {
	query := fmt.Sprintf(`UPDATE users SET max_item_size = %s, quota_bytes = %s, retention_days = %s, updated_at = %s WHERE id = %s`,
		s.ph(1), s.ph(2), s.ph(3), s.now(), s.ph(4))
	res, err := s.db.ExecContext(ctx, query, maxItemSize, quotaBytes, retentionDays, userID)
	if err != nil {
		return fmt.Errorf("更新用户设置失败: %w", err)
	}
	return s.assertAffected(res)
}

// UpdateUserRole 更新用户角色
func (s *Store) UpdateUserRole(ctx context.Context, userID int64, role string) error {
	query := fmt.Sprintf(`UPDATE users SET role = %s, updated_at = %s WHERE id = %s`,
		s.ph(1), s.now(), s.ph(2))
	res, err := s.db.ExecContext(ctx, query, role, userID)
	if err != nil {
		return fmt.Errorf("更新用户角色失败: %w", err)
	}
	return s.assertAffected(res)
}

// UpdateUserStatus 更新用户状态
func (s *Store) UpdateUserStatus(ctx context.Context, userID int64, status string) error {
	query := fmt.Sprintf(`UPDATE users SET status = %s, updated_at = %s WHERE id = %s`,
		s.ph(1), s.now(), s.ph(2))
	res, err := s.db.ExecContext(ctx, query, status, userID)
	if err != nil {
		return fmt.Errorf("更新用户状态失败: %w", err)
	}
	return s.assertAffected(res)
}

// ListUsers 分页查询用户列表（管理后台用）
func (s *Store) ListUsers(ctx context.Context, limit, offset int) ([]*User, error) {
	if limit <= 0 {
		limit = 20
	}
	query := fmt.Sprintf(`SELECT id, username, email, role, status,
		max_item_size, quota_bytes, retention_days, created_at, updated_at
		FROM users ORDER BY id DESC LIMIT %s OFFSET %s`,
		s.ph(1), s.ph(2))
	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("查询用户列表失败: %w", err)
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var u User
		var email sql.NullString
		if err := rows.Scan(
			&u.ID, &u.Username, &email, &u.Role, &u.Status,
			&u.MaxItemSize, &u.QuotaBytes, &u.RetentionDays, &u.CreatedAt, &u.UpdatedAt,
		); err != nil {
			return nil, err
		}
		u.Email = email
		users = append(users, &u)
	}
	return users, rows.Err()
}
