package migrate

import (
	"fmt"

	"github.com/leaf/cloud-clip-lite/internal/db"
)

// migration0002InviteCodes 邀请码表
var migration0002InviteCodes = Migration{
	Version: 2,
	Name:    "invite_codes",
	UpFunc: func(d *db.DB) (string, error) {
		switch d.Dialect {
		case db.DialectSQLite:
			return sqliteInviteCodes, nil
		case db.DialectPostgres:
			return postgresInviteCodes, nil
		default:
			return "", fmt.Errorf("不支持的数据库方言: %s", d.Dialect)
		}
	},
}

const sqliteInviteCodes = `
CREATE TABLE invite_codes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    code       TEXT NOT NULL UNIQUE,
    created_by INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    used_by    INTEGER REFERENCES users(id) ON DELETE SET NULL,
    expires_at TEXT,
    used       INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_invite_codes_code ON invite_codes(code);
`

const postgresInviteCodes = `
CREATE TABLE invite_codes (
    id         BIGSERIAL PRIMARY KEY,
    code       TEXT NOT NULL UNIQUE,
    created_by BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    used_by    BIGINT REFERENCES users(id) ON DELETE SET NULL,
    expires_at TIMESTAMPTZ,
    used       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_invite_codes_code ON invite_codes(code);
`
