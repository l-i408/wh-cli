// Package store owns SQLite persistence for wh-cli.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	// Register the sqlite3 driver used by database/sql.
	_ "github.com/mattn/go-sqlite3"
)

// ErrEmptyDBPath reports an attempt to open storage without a path.
var ErrEmptyDBPath = errors.New("empty database path")

// DB wraps the SQLite handle owned by the store package.
type DB struct {
	sql *sql.DB
}

// Open opens and verifies a SQLite database handle.
func Open(ctx context.Context, path string) (*DB, error) {
	if path == "" {
		return nil, ErrEmptyDBPath
	}

	conn, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := conn.PingContext(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return &DB{sql: conn}, nil
}

// Exec runs a statement against the database.
func (db *DB) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return db.sql.ExecContext(ctx, query, args...)
}

// QueryRow runs a query expected to return one row.
func (db *DB) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return db.sql.QueryRowContext(ctx, query, args...)
}

// Query runs a query that returns rows.
func (db *DB) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return db.sql.QueryContext(ctx, query, args...)
}

// ApplyInitialSchema creates the A0 schema when it is not present.
func (db *DB) ApplyInitialSchema(ctx context.Context) error {
	if _, err := db.Exec(ctx, initialSchema); err != nil {
		return fmt.Errorf("apply initial schema: %w", err)
	}
	if err := db.applyCompatMigrations(ctx); err != nil {
		return err
	}
	return nil
}

func (db *DB) applyCompatMigrations(ctx context.Context) error {
	if _, err := db.Exec(ctx, contactsGroupsSchema); err != nil {
		return fmt.Errorf("apply contacts/groups schema: %w", err)
	}
	if err := db.ensureColumn(ctx, "chats", "display_name", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := db.ensureColumn(ctx, "messages", "media_path", "TEXT"); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, jidMappingsSchema); err != nil {
		return fmt.Errorf("apply jid mappings schema: %w", err)
	}
	return nil
}

func (db *DB) ensureColumn(ctx context.Context, table string, column string, definition string) error {
	rows, err := db.Query(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return fmt.Errorf("read table info %s: %w", table, err)
	}
	defer func() {
		_ = rows.Close()
	}()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return fmt.Errorf("scan table info %s: %w", table, err)
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate table info %s: %w", table, err)
	}
	if _, err := db.Exec(ctx, "ALTER TABLE "+table+" ADD COLUMN "+column+" "+definition); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}

// Begin starts a new database transaction.
func (db *DB) Begin(ctx context.Context) (*sql.Tx, error) {
	return db.sql.BeginTx(ctx, nil)
}

// Close releases the database handle.
func (db *DB) Close() error {
	if db == nil || db.sql == nil {
		return nil
	}
	return db.sql.Close()
}

const initialSchema = `
CREATE TABLE IF NOT EXISTS auth_tokens (
    jti TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('access', 'refresh')),
    client_label TEXT NOT NULL,
    issued_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    revoked INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_auth_tokens_expires ON auth_tokens(expires_at);

` + contactsGroupsSchema + `

` + jidMappingsSchema + `

CREATE TABLE IF NOT EXISTS audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ts TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    target TEXT NOT NULL,
    result TEXT NOT NULL,
    severity TEXT NOT NULL DEFAULT 'info'
);

CREATE TABLE IF NOT EXISTS chats (
    jid TEXT PRIMARY KEY,
    type TEXT NOT NULL CHECK (type IN ('dm', 'group')),
    display_name TEXT NOT NULL DEFAULT '',
    last_message_id TEXT,
    unread_count INTEGER NOT NULL DEFAULT 0,
    pinned INTEGER NOT NULL DEFAULT 0,
    muted_until TEXT,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS messages (
    id TEXT PRIMARY KEY,
    chat_jid TEXT NOT NULL,
    sender_jid TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('text', 'image', 'document', 'audio')),
    body TEXT,
    media_path TEXT,
    reply_to_id TEXT,
    reactions_json TEXT NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'server_ack',
    timestamp TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_messages_chat_timestamp ON messages(chat_jid, timestamp DESC);

CREATE TABLE IF NOT EXISTS media_blobs (
    id TEXT PRIMARY KEY,
    message_id TEXT,
    mime TEXT NOT NULL,
    size INTEGER NOT NULL,
    sha256 TEXT NOT NULL UNIQUE,
    local_path TEXT NOT NULL,
    downloaded INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

const jidMappingsSchema = `
CREATE TABLE IF NOT EXISTS jid_mappings (
    lid_jid TEXT PRIMARY KEY,
    phone_jid TEXT NOT NULL UNIQUE,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

const contactsGroupsSchema = `
CREATE TABLE IF NOT EXISTS contacts (
    jid TEXT PRIMARY KEY,
    push_name TEXT,
    agenda_name TEXT,
    alias TEXT,
    avatar_path TEXT,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS groups (
    jid TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    topic TEXT,
    owner_jid TEXT,
    created_at TEXT,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS group_participants (
    group_jid TEXT NOT NULL REFERENCES groups(jid) ON DELETE CASCADE,
    contact_jid TEXT NOT NULL REFERENCES contacts(jid) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'member',
    joined_at TEXT,
    PRIMARY KEY (group_jid, contact_jid)
);
`
