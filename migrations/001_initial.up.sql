CREATE TABLE device_session (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    device_jid TEXT,
    session_blob BLOB NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE contacts (
    jid TEXT PRIMARY KEY,
    push_name TEXT,
    agenda_name TEXT,
    alias TEXT,
    avatar_path TEXT,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE groups (
    jid TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    topic TEXT,
    owner_jid TEXT,
    created_at TEXT,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE group_participants (
    group_jid TEXT NOT NULL REFERENCES groups(jid) ON DELETE CASCADE,
    contact_jid TEXT NOT NULL REFERENCES contacts(jid) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'member',
    joined_at TEXT,
    PRIMARY KEY (group_jid, contact_jid)
);

CREATE TABLE chats (
    jid TEXT PRIMARY KEY,
    type TEXT NOT NULL CHECK (type IN ('dm', 'group')),
    last_message_id TEXT,
    unread_count INTEGER NOT NULL DEFAULT 0,
    pinned INTEGER NOT NULL DEFAULT 0,
    muted_until TEXT,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE messages (
    id TEXT PRIMARY KEY,
    chat_jid TEXT NOT NULL REFERENCES chats(jid) ON DELETE CASCADE,
    sender_jid TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('text', 'image', 'document', 'audio')),
    body TEXT,
    media_path TEXT,
    reply_to_id TEXT REFERENCES messages(id) ON DELETE SET NULL,
    reactions_json TEXT NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'received',
    timestamp TEXT NOT NULL
);

CREATE INDEX idx_messages_chat_timestamp ON messages(chat_jid, timestamp DESC);

CREATE TABLE media_blobs (
    id TEXT PRIMARY KEY,
    message_id TEXT REFERENCES messages(id) ON DELETE SET NULL,
    mime TEXT NOT NULL,
    size INTEGER NOT NULL,
    sha256 TEXT NOT NULL UNIQUE,
    local_path TEXT NOT NULL,
    downloaded INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE auth_tokens (
    jti TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('access', 'refresh')),
    client_label TEXT NOT NULL,
    issued_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    revoked INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_auth_tokens_expires ON auth_tokens(expires_at);

CREATE TABLE audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ts TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    target TEXT NOT NULL,
    result TEXT NOT NULL,
    severity TEXT NOT NULL DEFAULT 'info'
);

CREATE INDEX idx_audit_log_ts ON audit_log(ts DESC);
