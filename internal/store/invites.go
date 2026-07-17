package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// InviteCode 邀请码实体
type InviteCode struct {
	ID        int64
	Code      string
	CreatedBy int64
	UsedBy    sql.NullInt64
	ExpiresAt sql.NullString
	Used      bool
	CreatedAt string
}

// CreateInviteCode 创建邀请码
func (s *Store) CreateInviteCode(ctx context.Context, code string, createdBy int64, expiresAt *string) (*InviteCode, error) {
	var exp any
	if expiresAt != nil {
		exp = *expiresAt
	} else {
		exp = nil
	}
	query := fmt.Sprintf(`INSERT INTO invite_codes (code, created_by, expires_at)
		VALUES (%s, %s, %s)`, s.ph(1), s.ph(2), s.ph(3))

	args := []any{code, createdBy, exp}

	if s.db.Dialect == "sqlite" {
		res, err := s.db.ExecContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("创建邀请码失败: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("获取邀请码 ID 失败: %w", err)
		}
		ic := &InviteCode{ID: id, Code: code, CreatedBy: createdBy}
		// created_at 由 DB 默认值生成，回填以保证创建响应携带时间戳
		if err := s.db.QueryRowContext(ctx, `SELECT created_at FROM invite_codes WHERE id = `+s.ph(1), id).Scan(&ic.CreatedAt); err != nil {
			return nil, fmt.Errorf("回填邀请码创建时间失败: %w", err)
		}
		return ic, nil
	}

	// PostgreSQL: pgx 不支持 LastInsertId，改用 RETURNING 取回 ID 和创建时间
	query += " RETURNING id, created_at"
	ic := &InviteCode{Code: code, CreatedBy: createdBy}
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&ic.ID, &ic.CreatedAt); err != nil {
		return nil, fmt.Errorf("创建邀请码失败: %w", err)
	}
	return ic, nil
}

// GetInviteCode 查询邀请码（未使用）
func (s *Store) GetInviteCode(ctx context.Context, code string) (*InviteCode, error) {
	var ic InviteCode
	var usedBy sql.NullInt64
	var expiresAt sql.NullString

	var usedCondition string
	if s.db.Dialect == "sqlite" {
		usedCondition = "used = 0"
	} else {
		usedCondition = "used = FALSE"
	}

	query := fmt.Sprintf(`SELECT id, code, created_by, used_by, expires_at, used, created_at
		FROM invite_codes WHERE code = %s AND %s`,
		s.ph(1), usedCondition)
	row := s.db.QueryRowContext(ctx, query, code)
	// used 扫进 bool：PG BOOLEAN 与 SQLite 0/1 均可由 database/sql 转换
	if err := row.Scan(&ic.ID, &ic.Code, &ic.CreatedBy, &usedBy, &expiresAt, &ic.Used, &ic.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("查询邀请码失败: %w", err)
	}
	ic.UsedBy = usedBy
	ic.ExpiresAt = expiresAt
	return &ic, nil
}

// UseInviteCode 标记邀请码已使用
// UPDATE 带 used 未使用守卫，并发下第二个使用者命中 0 行返回 ErrNotFound，防止一次性邀请码双花
func (s *Store) UseInviteCode(ctx context.Context, code string, userID int64) error {
	query := fmt.Sprintf(`UPDATE invite_codes SET used = %s, used_by = %s WHERE code = %s AND used = %s`,
		s.booleanTrue(), s.ph(1), s.ph(2), s.booleanFalse())
	res, err := s.db.ExecContext(ctx, query, userID, code)
	if err != nil {
		return fmt.Errorf("使用邀请码失败: %w", err)
	}
	return s.assertAffected(res)
}

// ListInviteCodes 查询邀请码列表（管理后台用）
func (s *Store) ListInviteCodes(ctx context.Context, limit, offset int) ([]*InviteCode, error) {
	if limit <= 0 {
		limit = 20
	}
	query := fmt.Sprintf(`SELECT id, code, created_by, used_by, expires_at, used, created_at
		FROM invite_codes ORDER BY id DESC LIMIT %s OFFSET %s`, s.ph(1), s.ph(2))
	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("查询邀请码列表失败: %w", err)
	}
	defer rows.Close()

	var codes []*InviteCode
	for rows.Next() {
		var ic InviteCode
		var usedBy sql.NullInt64
		var expiresAt sql.NullString
		if err := rows.Scan(&ic.ID, &ic.Code, &ic.CreatedBy, &usedBy, &expiresAt, &ic.Used, &ic.CreatedAt); err != nil {
			return nil, err
		}
		ic.UsedBy = usedBy
		ic.ExpiresAt = expiresAt
		codes = append(codes, &ic)
	}
	return codes, rows.Err()
}
