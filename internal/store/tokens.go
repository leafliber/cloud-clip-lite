package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// RefreshToken Refresh Token 实体
type RefreshToken struct {
	ID        int64
	UserID    int64
	TokenHash string
	DeviceID  sql.NullInt64
	ExpiresAt string
	Revoked   bool
	CreatedAt string
}

// CreateRefreshToken 创建 Refresh Token 记录
func (s *Store) CreateRefreshToken(ctx context.Context, rt *RefreshToken) (*RefreshToken, error) {
	var deviceID any
	if rt.DeviceID.Valid {
		deviceID = rt.DeviceID.Int64
	} else {
		deviceID = nil
	}
	query := fmt.Sprintf(`INSERT INTO refresh_tokens (user_id, token_hash, device_id, expires_at)
		VALUES (%s, %s, %s, %s)`,
		s.ph(1), s.ph(2), s.ph(3), s.ph(4))
	res, err := s.db.ExecContext(ctx, query, rt.UserID, rt.TokenHash, deviceID, rt.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("创建 Refresh Token 失败: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("获取 Refresh Token ID 失败: %w", err)
	}
	rt.ID = id
	return rt, nil
}

// GetRefreshToken 按 Token 哈希查询（未吊销且未过期）
func (s *Store) GetRefreshToken(ctx context.Context, tokenHash string) (*RefreshToken, error) {
	var rt RefreshToken
	var deviceID sql.NullInt64
	revokedInt := 0 // SQLite 用 0/1，PG 用 bool

	// PG 用 revoked = FALSE，SQLite 用 revoked = 0
	var revokedCondition string
	if s.db.Dialect == "sqlite" {
		revokedCondition = "revoked = 0"
	} else {
		revokedCondition = "revoked = FALSE"
	}

	query := fmt.Sprintf(`SELECT id, user_id, token_hash, device_id, expires_at, revoked, created_at
		FROM refresh_tokens WHERE token_hash = %s AND %s`,
		s.ph(1), revokedCondition)

	row := s.db.QueryRowContext(ctx, query, tokenHash)
	if err := row.Scan(&rt.ID, &rt.UserID, &rt.TokenHash, &deviceID, &rt.ExpiresAt, &revokedInt, &rt.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("查询 Refresh Token 失败: %w", err)
	}
	rt.DeviceID = deviceID
	rt.Revoked = revokedInt != 0
	return &rt, nil
}

// RevokeRefreshToken 吊销单个 Refresh Token
func (s *Store) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	query := fmt.Sprintf(`UPDATE refresh_tokens SET revoked = %s WHERE token_hash = %s`,
		s.booleanTrue(), s.ph(1))
	_, err := s.db.ExecContext(ctx, query, tokenHash)
	return err
}

// RevokeAllRefreshTokensByUser 吊销用户的所有 Refresh Token（强制下线）
func (s *Store) RevokeAllRefreshTokensByUser(ctx context.Context, userID int64) error {
	query := fmt.Sprintf(`UPDATE refresh_tokens SET revoked = %s WHERE user_id = %s`,
		s.booleanTrue(), s.ph(1))
	_, err := s.db.ExecContext(ctx, query, userID)
	return err
}

// CleanExpiredRefreshTokens 清理过期的 Refresh Token
func (s *Store) CleanExpiredRefreshTokens(ctx context.Context) (int64, error) {
	query := fmt.Sprintf(`DELETE FROM refresh_tokens WHERE expires_at < %s`, s.now())
	res, err := s.db.ExecContext(ctx, query)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
