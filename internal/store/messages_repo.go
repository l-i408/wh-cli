package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrMessageNotFound reports a requested message that is not present locally.
var ErrMessageNotFound = errors.New("message not found")

// Message is a stored WhatsApp message.
type Message struct {
	ID            string    `json:"id"`
	ChatJID       string    `json:"chat_jid"`
	SenderJID     string    `json:"sender_jid"`
	Type          string    `json:"type"`
	Body          string    `json:"body,omitempty"`
	MediaPath     string    `json:"media_path,omitempty"`
	ReplyToID     string    `json:"reply_to_id,omitempty"`
	ReactionsJSON string    `json:"reactions_json,omitempty"`
	Status        string    `json:"status"`
	Timestamp     time.Time `json:"timestamp"`
}

// Chat is a chat list item.
type Chat struct {
	JID           string    `json:"jid"`
	Type          string    `json:"type"`
	DisplayName   string    `json:"display_name"`
	LastMessageID string    `json:"last_message_id,omitempty"`
	UnreadCount   int       `json:"unread_count"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type chatRow struct {
	Chat
	StoredDisplayName string
	Alias             string
	PushName          string
	AgendaName        string
	GroupName         string
	MappedLIDJID      string
}

// MessageRepo persists messages and chat list metadata.
type MessageRepo struct {
	db *DB
}

// NewMessageRepo constructs a message repository.
func NewMessageRepo(db *DB) *MessageRepo {
	return &MessageRepo{db: db}
}

// SaveSentText upserts a sent text message and its chat metadata.
func (r *MessageRepo) SaveSentText(ctx context.Context, msg Message) error {
	return r.SaveText(ctx, msg, "")
}

// SaveText upserts a text message and its chat metadata.
func (r *MessageRepo) SaveText(ctx context.Context, msg Message, displayName string) error {
	chatType := "dm"
	if len(msg.ChatJID) >= 5 && msg.ChatJID[len(msg.ChatJID)-5:] == "@g.us" {
		chatType = "group"
	}
	contactPushName := displayName
	chatDisplayName := displayName
	if chatType == "group" {
		resolved, err := r.groupChatDisplayName(ctx, msg.ChatJID)
		if err != nil {
			return err
		}
		chatDisplayName = resolved
	}
	if chatDisplayName == "" {
		chatDisplayName = msg.ChatJID
	}
	if msg.SenderJID != "" && contactPushName != "" {
		if _, err := r.db.Exec(ctx, `
INSERT INTO contacts (jid, push_name, updated_at)
VALUES (?, NULLIF(?, ''), ?)
ON CONFLICT(jid) DO UPDATE SET push_name = COALESCE(excluded.push_name, contacts.push_name), updated_at = excluded.updated_at
`, msg.SenderJID, contactPushName, msg.Timestamp.UTC().Format(time.RFC3339)); err != nil {
			return fmt.Errorf("upsert message sender contact: %w", err)
		}
	}
	if _, err := r.db.Exec(ctx, `
INSERT INTO chats (jid, type, display_name, last_message_id, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(jid) DO UPDATE SET display_name = excluded.display_name, last_message_id = excluded.last_message_id, updated_at = excluded.updated_at
`, msg.ChatJID, chatType, chatDisplayName, msg.ID, msg.Timestamp.UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("upsert chat: %w", err)
	}
	if _, err := r.db.Exec(ctx, `
INSERT INTO messages (id, chat_jid, sender_jid, type, body, media_path, status, timestamp)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET status = excluded.status
`, msg.ID, msg.ChatJID, msg.SenderJID, msg.Type, msg.Body, msg.MediaPath, msg.Status, msg.Timestamp.UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("save message: %w", err)
	}
	return nil
}

func (r *MessageRepo) groupChatDisplayName(ctx context.Context, chatJID string) (string, error) {
	var name string
	if err := r.db.QueryRow(ctx, `
SELECT COALESCE(NULLIF(g.name, ''), '')
FROM groups g
WHERE g.jid = ?
`, chatJID).Scan(&name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("resolve group chat display name: %w", err)
	}
	return name, nil
}

// SaveReply upserts a sent reply and links it to the quoted message.
func (r *MessageRepo) SaveReply(ctx context.Context, msg Message, replyToID string) error {
	msg.ReplyToID = replyToID
	if err := r.SaveText(ctx, msg, ""); err != nil {
		return err
	}
	if _, err := r.db.Exec(ctx, `
UPDATE messages
SET reply_to_id = ?
WHERE id = ?
`, replyToID, msg.ID); err != nil {
		return fmt.Errorf("save reply link: %w", err)
	}
	return nil
}

// GetMessage returns a single stored message by ID.
func (r *MessageRepo) GetMessage(ctx context.Context, messageID string) (Message, error) {
	var msg Message
	var tsRaw string
	if err := r.db.QueryRow(ctx, `
SELECT id, chat_jid, sender_jid, type, COALESCE(body, ''), COALESCE(media_path, ''), COALESCE(reply_to_id, ''), reactions_json, status, timestamp
FROM messages
WHERE id = ?
`, messageID).Scan(&msg.ID, &msg.ChatJID, &msg.SenderJID, &msg.Type, &msg.Body, &msg.MediaPath, &msg.ReplyToID, &msg.ReactionsJSON, &msg.Status, &tsRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Message{}, ErrMessageNotFound
		}
		return Message{}, fmt.Errorf("get message: %w", err)
	}
	ts, err := time.Parse(time.RFC3339, tsRaw)
	if err != nil {
		return Message{}, fmt.Errorf("parse message timestamp: %w", err)
	}
	msg.Timestamp = ts
	return msg, nil
}

// GetOldestMessage returns the oldest locally stored message for a chat.
func (r *MessageRepo) GetOldestMessage(ctx context.Context, chatJID string) (Message, error) {
	chatJIDs, err := r.chatJIDAliases(ctx, chatJID)
	if err != nil {
		return Message{}, err
	}
	var msg Message
	var tsRaw string
	if err := r.db.QueryRow(ctx, `
SELECT id, chat_jid, sender_jid, type, COALESCE(body, ''), COALESCE(media_path, ''), COALESCE(reply_to_id, ''), reactions_json, status, timestamp
FROM messages
WHERE chat_jid IN (?, ?)
ORDER BY timestamp ASC
LIMIT 1
`, chatJIDs[0], chatJIDs[1]).Scan(&msg.ID, &msg.ChatJID, &msg.SenderJID, &msg.Type, &msg.Body, &msg.MediaPath, &msg.ReplyToID, &msg.ReactionsJSON, &msg.Status, &tsRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Message{}, ErrMessageNotFound
		}
		return Message{}, fmt.Errorf("get oldest message: %w", err)
	}
	ts, err := time.Parse(time.RFC3339, tsRaw)
	if err != nil {
		return Message{}, fmt.Errorf("parse oldest message timestamp: %w", err)
	}
	msg.Timestamp = ts
	return msg, nil
}

// SaveReaction records the local user's latest reaction for a target message.
func (r *MessageRepo) SaveReaction(ctx context.Context, messageID string, actorJID string, emoji string) error {
	msg, err := r.GetMessage(ctx, messageID)
	if err != nil {
		return err
	}
	reactions := map[string]string{}
	if msg.ReactionsJSON != "" {
		if err := json.Unmarshal([]byte(msg.ReactionsJSON), &reactions); err != nil {
			return fmt.Errorf("decode reactions: %w", err)
		}
	}
	if emoji == "" {
		delete(reactions, actorJID)
	} else {
		reactions[actorJID] = emoji
	}
	encoded, err := json.Marshal(reactions)
	if err != nil {
		return fmt.Errorf("encode reactions: %w", err)
	}
	if _, err := r.db.Exec(ctx, `
UPDATE messages
SET reactions_json = ?
WHERE id = ?
`, string(encoded), messageID); err != nil {
		return fmt.Errorf("save reaction: %w", err)
	}
	return nil
}

// UpdateStatus records delivery/read/server-error receipts for an existing message.
func (r *MessageRepo) UpdateStatus(ctx context.Context, messageID string, status string) error {
	result, err := r.db.Exec(ctx, `
UPDATE messages
SET status = ?
WHERE id = ?
`, status, messageID)
	if err != nil {
		return fmt.Errorf("update message status: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read updated status rows: %w", err)
	}
	if affected == 0 {
		return nil
	}
	return nil
}

// BackfillMissingSenderJID repairs old message rows created before sender_jid was persisted.
func (r *MessageRepo) BackfillMissingSenderJID(ctx context.Context, ownJID string) error {
	if ownJID != "" {
		if _, err := r.db.Exec(ctx, `
UPDATE messages
SET sender_jid = ?
WHERE COALESCE(sender_jid, '') = ''
  AND status IN ('sent', 'server_ack', 'delivered', 'read', 'played', 'retry', 'server_error', 'failed', 'pending')
`, ownJID); err != nil {
			return fmt.Errorf("backfill own sender jid: %w", err)
		}
	}
	if _, err := r.db.Exec(ctx, `
UPDATE messages
SET sender_jid = chat_jid
WHERE COALESCE(sender_jid, '') = ''
  AND status = 'received'
  AND chat_jid NOT LIKE '%@g.us'
`); err != nil {
		return fmt.Errorf("backfill dm sender jid: %w", err)
	}
	return nil
}

// ListChats returns recent chats.
func (r *MessageRepo) ListChats(ctx context.Context, limit int) ([]Chat, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
SELECT ch.jid, ch.type, ch.display_name, COALESCE(c.alias, ''), COALESCE(c.push_name, ''), COALESCE(c.agenda_name, ''), COALESCE(g.name, ''), COALESCE(ch.last_message_id, ''), ch.unread_count, ch.updated_at, COALESCE(jm.lid_jid, '')
FROM chats ch
LEFT JOIN contacts c ON c.jid = ch.jid
LEFT JOIN groups g ON g.jid = ch.jid
LEFT JOIN jid_mappings jm ON jm.phone_jid = ch.jid
ORDER BY ch.updated_at DESC
LIMIT ?
`, limit)
	if err != nil {
		return nil, fmt.Errorf("list chats: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	chatsByJID := make(map[string]chatRow)
	order := make([]string, 0)
	for rows.Next() {
		var row chatRow
		var updatedRaw string
		if err := rows.Scan(&row.JID, &row.Type, &row.StoredDisplayName, &row.Alias, &row.PushName, &row.AgendaName, &row.GroupName, &row.LastMessageID, &row.UnreadCount, &updatedRaw, &row.MappedLIDJID); err != nil {
			return nil, fmt.Errorf("scan chat: %w", err)
		}
		updatedAt, err := time.Parse(time.RFC3339, updatedRaw)
		if err != nil {
			return nil, fmt.Errorf("parse chat updated_at: %w", err)
		}
		row.UpdatedAt = updatedAt
		key := row.JID
		if row.MappedLIDJID != "" {
			key = row.MappedLIDJID
		}
		existing, ok := chatsByJID[key]
		if !ok {
			order = append(order, key)
			chatsByJID[key] = row
			continue
		}
		chatsByJID[key] = mergeChatRows(existing, row, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chats: %w", err)
	}
	chats := make([]Chat, 0, len(order))
	for _, key := range order {
		row := chatsByJID[key]
		row.JID = key
		if row.Type == "group" {
			row.DisplayName = firstReadableName(row.GroupName, row.StoredDisplayName, row.JID)
		} else {
			row.DisplayName = ResolveDisplayName(row.JID, row.Alias, row.AgendaName, row.PushName)
		}
		if row.Type != "group" && (row.MappedLIDJID != "" || isLIDUserJID(row.JID)) {
			if canonical, err := r.contactForChat(ctx, row.JID); err == nil {
				row.DisplayName = reliableDMDisplayName(row.JID, canonical)
			}
		}
		chats = append(chats, row.Chat)
	}
	return chats, nil
}

func reliableDMDisplayName(jid string, contact Contact) string {
	if strings.TrimSpace(contact.Alias) != "" || strings.TrimSpace(contact.AgendaName) != "" {
		return contact.DisplayName
	}
	if isLIDUserJID(jid) {
		return FormatJID(jid)
	}
	if strings.TrimSpace(contact.PushName) != "" {
		return contact.DisplayName
	}
	return FormatJID(jid)
}

func mergeChatRows(existing chatRow, candidate chatRow, key string) chatRow {
	if candidate.UpdatedAt.After(existing.UpdatedAt) {
		candidate.JID = key
		if existing.Alias != "" || existing.AgendaName != "" || existing.PushName != "" || isLIDUserJID(existing.JID) {
			candidate.Alias = existing.Alias
			candidate.AgendaName = existing.AgendaName
			candidate.PushName = existing.PushName
			candidate.StoredDisplayName = existing.StoredDisplayName
			candidate.GroupName = existing.GroupName
		}
		return candidate
	}
	if existing.Alias == "" {
		existing.Alias = candidate.Alias
	}
	if existing.AgendaName == "" {
		existing.AgendaName = candidate.AgendaName
	}
	if existing.PushName == "" {
		existing.PushName = candidate.PushName
	}
	if existing.GroupName == "" {
		existing.GroupName = candidate.GroupName
	}
	return existing
}

func isLegacyUserJID(jid string) bool {
	return strings.HasSuffix(jid, "@s.whatsapp.net")
}

func isLIDUserJID(jid string) bool {
	return strings.HasSuffix(jid, "@lid")
}

func firstReadableName(values ...string) string {
	for _, value := range values {
		if value != "" && !looksLikeJID(value) {
			return value
		}
	}
	return ""
}

func looksLikeJID(value string) bool {
	return len(value) > 3 && strings.Contains(value, "@")
}

// ListMessages returns recent messages for a chat.
func (r *MessageRepo) ListMessages(ctx context.Context, chatJID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 5000
	}
	if limit > 5000 {
		limit = 5000
	}
	chatJIDs, err := r.chatJIDAliases(ctx, chatJID)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `
SELECT id, chat_jid, sender_jid, type, COALESCE(body, ''), COALESCE(media_path, ''), COALESCE(reply_to_id, ''), reactions_json, status, timestamp
FROM messages
WHERE chat_jid IN (?, ?)
ORDER BY timestamp DESC
LIMIT ?
`, chatJIDs[0], chatJIDs[1], limit)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	messages := make([]Message, 0)
	for rows.Next() {
		var msg Message
		var tsRaw string
		if err := rows.Scan(&msg.ID, &msg.ChatJID, &msg.SenderJID, &msg.Type, &msg.Body, &msg.MediaPath, &msg.ReplyToID, &msg.ReactionsJSON, &msg.Status, &tsRaw); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		ts, err := time.Parse(time.RFC3339, tsRaw)
		if err != nil {
			return nil, fmt.Errorf("parse message timestamp: %w", err)
		}
		msg.Timestamp = ts
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}
	return messages, nil
}

func (r *MessageRepo) canonicalChatJID(ctx context.Context, chatJID string) (string, error) {
	if !isLegacyUserJID(chatJID) {
		return chatJID, nil
	}
	var lidJID string
	if err := r.db.QueryRow(ctx, `
SELECT lid_jid
FROM jid_mappings
WHERE phone_jid = ?
`, chatJID).Scan(&lidJID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return chatJID, nil
		}
		return "", fmt.Errorf("resolve canonical chat jid: %w", err)
	}
	return lidJID, nil
}

// CanonicalChatJID returns the modern LID JID for a legacy phone JID when a mapping is known.
func (r *MessageRepo) CanonicalChatJID(ctx context.Context, chatJID string) (string, error) {
	return r.canonicalChatJID(ctx, chatJID)
}

func (r *MessageRepo) chatJIDAliases(ctx context.Context, chatJID string) ([2]string, error) {
	canonicalJID, err := r.canonicalChatJID(ctx, chatJID)
	if err != nil {
		return [2]string{}, err
	}
	aliases := [2]string{canonicalJID, canonicalJID}
	var phoneJID string
	if isLIDUserJID(canonicalJID) {
		if err := r.db.QueryRow(ctx, `
SELECT phone_jid
FROM jid_mappings
WHERE lid_jid = ?
`, canonicalJID).Scan(&phoneJID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return aliases, nil
			}
			return [2]string{}, fmt.Errorf("resolve chat jid aliases: %w", err)
		}
		aliases[1] = phoneJID
	}
	return aliases, nil
}

func (r *MessageRepo) contactForChat(ctx context.Context, jid string) (Contact, error) {
	contacts := NewContactRepo(r.db)
	return contacts.Get(ctx, jid)
}
