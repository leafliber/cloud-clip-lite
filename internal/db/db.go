package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/leaf/cloud-clip-lite/internal/config"
	_ "modernc.org/sqlite"        // 纯 Go SQLite 驱动，无需 CGO
	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL 驱动
)

// Dialect 数据库方言
type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectPostgres Dialect = "postgres"
)

// DB 封装数据库连接与方言信息
type DB struct {
	*sql.DB
	Dialect Dialect
}

// Open 根据配置打开数据库连接
// - DATABASE_URL 为空：使用 SQLite（默认）
// - DATABASE_URL 以 postgres:// 开头：使用 PostgreSQL
func Open(ctx context.Context, cfg *config.Config) (*DB, error) {
	if cfg.IsSQLite() {
		return openSQLite(ctx, cfg)
	}
	return openPostgres(ctx, cfg)
}

func openSQLite(ctx context.Context, cfg *config.Config) (*DB, error) {
	// 启用外键、WAL 模式以提升并发
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", cfg.SQLitePath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite 失败: %w", err)
	}
	// SQLite 单写多读，连接池不宜过大
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("连接 SQLite 失败: %w", err)
	}
	return &DB{DB: db, Dialect: DialectSQLite}, nil
}

func openPostgres(ctx context.Context, cfg *config.Config) (*DB, error) {
	url := cfg.DatabaseURL
	// 兼容标准 pg URL 与 pgx 占位符
	if strings.HasPrefix(url, "postgres://") || strings.HasPrefix(url, "postgresql://") {
		// pgx stdlib 直接支持标准 URL
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, fmt.Errorf("打开 PostgreSQL 失败: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("连接 PostgreSQL 失败: %w", err)
	}
	return &DB{DB: db, Dialect: DialectPostgres}, nil
}

// Placeholder 返回当前方言的占位符样式
// SQLite: ?    PostgreSQL: $1, $2, ...
// 使用 store 层的查询构建工具统一处理
func (d *DB) Placeholder(idx int) string {
	if d.Dialect == DialectPostgres {
		return fmt.Sprintf("$%d", idx)
	}
	return "?"
}

// DriverName 返回驱动名
func (d *DB) DriverName() string {
	if d.Dialect == DialectPostgres {
		return "pgx"
	}
	return "sqlite"
}
