package store

import (
	"context"
	"database/sql"
	"fmt"
)

// AuditLog 审计日志实体
type AuditLog struct {
	ID        int64
	UserID    sql.NullInt64
	DeviceID  sql.NullInt64
	Action    string // 如 user.login, clip.create, admin.user.update
	Target    sql.NullString
	IP        sql.NullString
	UA        sql.NullString
	Meta      sql.NullString // JSON
	CreatedAt string
}

// CreateAuditLog 写入审计日志
func (s *Store) CreateAuditLog(ctx context.Context, log *AuditLog) error {
	var userID, deviceID, target, ip, ua, meta any
	if log.UserID.Valid {
		userID = log.UserID.Int64
	}
	if log.DeviceID.Valid {
		deviceID = log.DeviceID.Int64
	}
	if log.Target.Valid {
		target = log.Target.String
	}
	if log.IP.Valid {
		ip = log.IP.String
	}
	if log.UA.Valid {
		ua = log.UA.String
	}
	if log.Meta.Valid {
		meta = log.Meta.String
	}

	query := fmt.Sprintf(`INSERT INTO audit_logs (user_id, device_id, action, target, ip, ua, meta)
		VALUES (%s, %s, %s, %s, %s, %s, %s)`,
		s.ph(1), s.ph(2), s.ph(3), s.ph(4), s.ph(5), s.ph(6), s.ph(7))
	_, err := s.db.ExecContext(ctx, query, userID, deviceID, log.Action, target, ip, ua, meta)
	return err
}

// ListAuditLogs 分页查询审计日志
// 支持按 user_id 和 action 过滤，按时间倒序
func (s *Store) ListAuditLogs(ctx context.Context, userID int64, action string, limit, offset int) ([]*AuditLog, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	where := "1=1"
	args := []any{}
	idx := 1
	if userID > 0 {
		where += fmt.Sprintf(" AND user_id = %s", s.ph(idx))
		args = append(args, userID)
		idx++
	}
	if action != "" {
		where += fmt.Sprintf(" AND action = %s", s.ph(idx))
		args = append(args, action)
		idx++
	}

	query := fmt.Sprintf(`SELECT id, user_id, device_id, action, target, ip, ua, meta, created_at
		FROM audit_logs WHERE %s ORDER BY created_at DESC, id DESC LIMIT %s OFFSET %s`,
		where, s.ph(idx), s.ph(idx+1))
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("查询审计日志失败: %w", err)
	}
	defer rows.Close()

	var logs []*AuditLog
	for rows.Next() {
		var log AuditLog
		if err := rows.Scan(
			&log.ID, &log.UserID, &log.DeviceID, &log.Action, &log.Target,
			&log.IP, &log.UA, &log.Meta, &log.CreatedAt,
		); err != nil {
			return nil, err
		}
		logs = append(logs, &log)
	}
	return logs, rows.Err()
}

// CountAuditLogs 统计审计日志总数（用于分页元数据）
func (s *Store) CountAuditLogs(ctx context.Context, userID int64, action string) (int64, error) {
	where := "1=1"
	args := []any{}
	idx := 1
	if userID > 0 {
		where += fmt.Sprintf(" AND user_id = %s", s.ph(idx))
		args = append(args, userID)
		idx++
	}
	if action != "" {
		where += fmt.Sprintf(" AND action = %s", s.ph(idx))
		args = append(args, action)
		idx++
	}

	query := fmt.Sprintf("SELECT COUNT(*) FROM audit_logs WHERE %s", where)
	var count int64
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}
