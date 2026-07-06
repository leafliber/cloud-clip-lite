package migrate

import (
	"fmt"

	"github.com/leaf/cloud-clip-lite/internal/db"
)

// migration0001Initial 初始 Schema：users / devices / clip_items / audit_logs / refresh_tokens
var migration0001Initial = Migration{
	Version: 1,
	Name:    "initial_schema",
	UpFunc: func(d *db.DB) (string, error) {
		switch d.Dialect {
		case db.DialectSQLite:
			return sqliteSchema, nil
		case db.DialectPostgres:
			return postgresSchema, nil
		default:
			return "", fmt.Errorf("不支持的数据库方言: %s", d.Dialect)
		}
	},
}

// SQLite Schema（INTEGER PRIMARY KEY 即自增；TEXT 代替 TIMESTAMPTZ）
const sqliteSchema = `
CREATE TABLE users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    email         TEXT UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'user',
    status        TEXT NOT NULL DEFAULT 'active',
    max_item_size INTEGER NOT NULL DEFAULT 10485760,
    quota_bytes   INTEGER NOT NULL DEFAULT 1073741824,
    retention_days INTEGER NOT NULL DEFAULT 30,
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE devices (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    type            TEXT NOT NULL,
    api_token_hash  TEXT UNIQUE,
    last_seen_at    TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_devices_user ON devices(user_id);

CREATE TABLE clip_items (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id    INTEGER REFERENCES devices(id) ON DELETE SET NULL,
    type         TEXT NOT NULL,
    mime_type    TEXT,
    size         INTEGER NOT NULL,
    blob_key     TEXT,
    text_content TEXT,
    sha256       TEXT,
    meta         TEXT NOT NULL DEFAULT '{}',
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at   TEXT
);
CREATE INDEX idx_clip_user_created ON clip_items(user_id, created_at DESC);
CREATE INDEX idx_clip_expires ON clip_items(expires_at) WHERE expires_at IS NOT NULL;

CREATE TABLE audit_logs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER REFERENCES users(id) ON DELETE CASCADE,
    device_id  INTEGER,
    action     TEXT NOT NULL,
    target     TEXT,
    ip         TEXT,
    ua         TEXT,
    meta       TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_audit_user_time ON audit_logs(user_id, created_at DESC);

CREATE TABLE refresh_tokens (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    device_id   INTEGER,
    expires_at  TEXT NOT NULL,
    revoked     INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_refresh_tokens_user ON refresh_tokens(user_id);
`

// PostgreSQL Schema（BIGSERIAL + TIMESTAMPTZ + JSONB）
const postgresSchema = `
CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    email         TEXT UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'user',
    status        TEXT NOT NULL DEFAULT 'active',
    max_item_size BIGINT NOT NULL DEFAULT 10485760,
    quota_bytes   BIGINT NOT NULL DEFAULT 1073741824,
    retention_days INT NOT NULL DEFAULT 30,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE devices (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    type            TEXT NOT NULL,
    api_token_hash  TEXT UNIQUE,
    last_seen_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_devices_user ON devices(user_id);

CREATE TABLE clip_items (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id    BIGINT REFERENCES devices(id) ON DELETE SET NULL,
    type         TEXT NOT NULL,
    mime_type    TEXT,
    size         BIGINT NOT NULL,
    blob_key     TEXT,
    text_content TEXT,
    sha256       TEXT,
    meta         JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ
);
CREATE INDEX idx_clip_user_created ON clip_items(user_id, created_at DESC);
CREATE INDEX idx_clip_expires ON clip_items(expires_at) WHERE expires_at IS NOT NULL;

CREATE TABLE audit_logs (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT REFERENCES users(id) ON DELETE CASCADE,
    device_id  BIGINT,
    action     TEXT NOT NULL,
    target     TEXT,
    ip         TEXT,
    ua         TEXT,
    meta       JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_user_time ON audit_logs(user_id, created_at DESC);

CREATE TABLE refresh_tokens (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    device_id   BIGINT,
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_refresh_tokens_user ON refresh_tokens(user_id);
`
