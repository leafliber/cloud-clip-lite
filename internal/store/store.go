package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/leaf/cloud-clip-lite/internal/db"
)

// ErrNotFound 通用未找到错误
var ErrNotFound = errors.New("记录不存在")

// Store 数据访问层：聚合各实体的查询方法
type Store struct {
	db *db.DB
}

// New 创建 Store
func New(d *db.DB) *Store {
	return &Store{db: d}
}

// DB 暴露底层连接（供高级用法使用）
func (s *Store) DB() *db.DB { return s.db }

// HealthCheck 简单 ping 数据库
func (s *Store) HealthCheck(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// ---------- 用户相关（占位，阶段 1 补充完整） ----------

// User 用户实体
type User struct {
	ID             int64
	Username       string
	Email          sql.NullString
	PasswordHash   string
	Role           string
	Status         string
	MaxItemSize    int64
	QuotaBytes     int64
	RetentionDays  int
	CreatedAt      string
	UpdatedAt      string
}

// GetUserByUsername 按用户名查询用户
func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	var u User
	var email sql.NullString
	query := fmt.Sprintf(`SELECT id, username, email, password_hash, role, status,
		max_item_size, quota_bytes, retention_days, created_at, updated_at
		FROM users WHERE username = %s`, s.db.Placeholder(1))
	row := s.db.QueryRowContext(ctx, query, username)
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

// CountUsers 统计用户总数（用于判断是否首次启动）
func (s *Store) CountUsers(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

// ---------- 辅助方法 ----------

// ph 返回指定索引的占位符
func (s *Store) ph(idx int) string {
	return s.db.Placeholder(idx)
}

// now 返回当前时间的 SQL 表达式（SQLite/PG 各不同）
func (s *Store) now() string {
	if s.db.Dialect == "sqlite" {
		return "datetime('now')"
	}
	return "now()"
}

// booleanTrue 返回当前方言的布尔真值
func (s *Store) booleanTrue() string {
	if s.db.Dialect == "sqlite" {
		return "1"
	}
	return "TRUE"
}

// assertAffected 确认 SQL 影响了至少一行
func (s *Store) assertAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
