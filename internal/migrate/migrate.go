package migrate

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/leaf/cloud-clip-lite/internal/db"
)

// Migration 单个迁移
type Migration struct {
	Version int
	Name    string
	// UpFunc 接收 *sql.DB 与方言，返回 SQL 或在函数内执行
	UpFunc func(d *db.DB) (string, error)
}

// 迁移注册表
var migrations = []Migration{
	migration0001Initial,
	migration0002InviteCodes,
}

// Run 执行所有未应用的迁移
func Run(ctx context.Context, d *db.DB) error {
	if err := ensureSchemaMigrationsTable(ctx, d); err != nil {
		return fmt.Errorf("创建迁移跟踪表失败: %w", err)
	}

	applied, err := getAppliedVersions(ctx, d)
	if err != nil {
		return fmt.Errorf("读取已应用迁移失败: %w", err)
	}

	sorted := make([]Migration, len(migrations))
	copy(sorted, migrations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Version < sorted[j].Version })

	for _, m := range sorted {
		if applied[m.Version] {
			continue
		}
		if err := applyMigration(ctx, d, m); err != nil {
			return fmt.Errorf("应用迁移 %d (%s) 失败: %w", m.Version, m.Name, err)
		}
	}
	return nil
}

func ensureSchemaMigrationsTable(ctx context.Context, d *db.DB) error {
	var stmt string
	if d.Dialect == db.DialectSQLite {
		stmt = `CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name    TEXT NOT NULL,
			applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`
	} else {
		stmt = `CREATE TABLE IF NOT EXISTS schema_migrations (
			version BIGINT PRIMARY KEY,
			name    TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`
	}
	_, err := d.ExecContext(ctx, stmt)
	return err
}

func getAppliedVersions(ctx context.Context, d *db.DB) (map[int]bool, error) {
	rows, err := d.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

func applyMigration(ctx context.Context, d *db.DB, m Migration) error {
	sqlStr, err := m.UpFunc(d)
	if err != nil {
		return err
	}
	// 拆分多个语句逐条执行（SQLite 与 PG 的 DB.Exec 不支持多语句）
	stmts := splitStatements(sqlStr)
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, s := range stmts {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("执行语句失败 [%s]: %w", truncate(s, 80), err)
		}
	}

	var insStmt string
	if d.Dialect == db.DialectSQLite {
		insStmt = `INSERT INTO schema_migrations (version, name) VALUES (?, ?)`
	} else {
		insStmt = `INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`
	}
	if _, err := tx.ExecContext(ctx, insStmt, m.Version, m.Name); err != nil {
		return fmt.Errorf("记录迁移版本失败: %w", err)
	}

	return tx.Commit()
}

// splitStatements 简易 SQL 拆分（按分号，忽略字符串字面量内的分号）
func splitStatements(sqlStr string) []string {
	var stmts []string
	var cur strings.Builder
	inSingle := false
	for _, r := range sqlStr {
		switch r {
		case '\'':
			inSingle = !inSingle
			cur.WriteRune(r)
		case ';':
			if inSingle {
				cur.WriteRune(r)
			} else {
				stmts = append(stmts, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if strings.TrimSpace(cur.String()) != "" {
		stmts = append(stmts, cur.String())
	}
	return stmts
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
