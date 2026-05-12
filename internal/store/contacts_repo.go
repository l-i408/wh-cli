package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrInvalidContact reports invalid contact input.
var ErrInvalidContact = errors.New("invalid contact")

// Contact is a resolved WhatsApp contact.
type Contact struct {
	JID         string    `json:"jid"`
	PushName    string    `json:"push_name,omitempty"`
	AgendaName  string    `json:"agenda_name,omitempty"`
	Alias       string    `json:"alias,omitempty"`
	AvatarPath  string    `json:"avatar_path,omitempty"`
	DisplayName string    `json:"display_name"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// JIDMapping links WhatsApp's modern LID JID to the legacy phone-number JID.
type JIDMapping struct {
	LIDJID   string
	PhoneJID string
}

// ContactRepo persists and resolves contacts.
type ContactRepo struct {
	db *DB
}

// NewContactRepo constructs a contact repository.
func NewContactRepo(db *DB) *ContactRepo {
	return &ContactRepo{db: db}
}

// UpsertPushName stores the latest PushName seen for a JID.
func (r *ContactRepo) UpsertPushName(ctx context.Context, jid string, pushName string) error {
	if jid == "" || pushName == "" {
		return nil
	}
	if _, err := r.db.Exec(ctx, `
INSERT INTO contacts (jid, push_name, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(jid) DO UPDATE SET push_name = excluded.push_name, updated_at = excluded.updated_at
`, jid, pushName, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("upsert contact push name: %w", err)
	}
	return nil
}

// UpsertAgendaName stores the local address-book name seen for a JID.
func (r *ContactRepo) UpsertAgendaName(ctx context.Context, jid string, agendaName string) error {
	if jid == "" || agendaName == "" {
		return nil
	}
	if _, err := r.db.Exec(ctx, `
INSERT INTO contacts (jid, agenda_name, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(jid) DO UPDATE SET agenda_name = excluded.agenda_name, updated_at = excluded.updated_at
`, jid, agendaName, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("upsert contact agenda name: %w", err)
	}
	return nil
}

// BulkUpsertPushNames stores push names for multiple JIDs in one transaction.
func (r *ContactRepo) BulkUpsertPushNames(ctx context.Context, names map[string]string) error {
	if len(names) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin bulk upsert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for jid, name := range names {
		if jid == "" || name == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO contacts (jid, push_name, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(jid) DO UPDATE SET push_name = excluded.push_name, updated_at = excluded.updated_at
`, jid, name, now); err != nil {
			return fmt.Errorf("bulk upsert contact %s: %w", jid, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit bulk upsert: %w", err)
	}
	return nil
}

// BulkUpsertAgendaNames stores local address-book names for multiple JIDs in one transaction.
func (r *ContactRepo) BulkUpsertAgendaNames(ctx context.Context, names map[string]string) error {
	if len(names) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin bulk agenda upsert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for jid, name := range names {
		if jid == "" || name == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO contacts (jid, agenda_name, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(jid) DO UPDATE SET agenda_name = excluded.agenda_name, updated_at = excluded.updated_at
`, jid, name, now); err != nil {
			return fmt.Errorf("bulk upsert agenda contact %s: %w", jid, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit bulk agenda upsert: %w", err)
	}
	return nil
}

// BulkUpsertJIDMappings stores LID <-> phone-number mappings discovered by whatsmeow.
func (r *ContactRepo) BulkUpsertJIDMappings(ctx context.Context, mappings []JIDMapping) error {
	if len(mappings) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin bulk jid mapping upsert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, mapping := range mappings {
		if mapping.LIDJID == "" || mapping.PhoneJID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
DELETE FROM jid_mappings
WHERE phone_jid = ? AND lid_jid <> ?
`, mapping.PhoneJID, mapping.LIDJID); err != nil {
			return fmt.Errorf("delete stale jid mapping %s: %w", mapping.PhoneJID, err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO jid_mappings (lid_jid, phone_jid, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(lid_jid) DO UPDATE SET phone_jid = excluded.phone_jid, updated_at = excluded.updated_at
`, mapping.LIDJID, mapping.PhoneJID, now); err != nil {
			return fmt.Errorf("bulk upsert jid mapping %s: %w", mapping.LIDJID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit bulk jid mapping upsert: %w", err)
	}
	return nil
}

// SetAlias stores a user-controlled alias for a contact.
func (r *ContactRepo) SetAlias(ctx context.Context, jid string, alias string) (Contact, error) {
	if jid == "" {
		return Contact{}, fmt.Errorf("%w: empty jid", ErrInvalidContact)
	}
	if _, err := r.db.Exec(ctx, `
INSERT INTO contacts (jid, alias, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(jid) DO UPDATE SET alias = excluded.alias, updated_at = excluded.updated_at
`, jid, alias, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return Contact{}, fmt.Errorf("set contact alias: %w", err)
	}
	return r.Get(ctx, jid)
}

// Get returns one contact or a formatted fallback if it has no row yet.
func (r *ContactRepo) Get(ctx context.Context, jid string) (Contact, error) {
	contacts, err := r.list(ctx, "WHERE c.jid = ?", jid)
	if err != nil {
		return Contact{}, err
	}
	if len(contacts) == 0 {
		return Contact{JID: jid, DisplayName: FormatJID(jid), UpdatedAt: time.Now().UTC()}, nil
	}
	return contacts[0], nil
}

// List returns known contacts ordered by resolved display name.
func (r *ContactRepo) List(ctx context.Context) ([]Contact, error) {
	return r.list(ctx, "")
}

func (r *ContactRepo) list(ctx context.Context, where string, args ...any) ([]Contact, error) {
	query := `
SELECT c.jid,
       COALESCE(NULLIF(pc.push_name, ''), NULLIF(c.push_name, ''), ''),
       COALESCE(NULLIF(c.agenda_name, ''), NULLIF(pc.agenda_name, ''), ''),
       COALESCE(NULLIF(c.alias, ''), NULLIF(pc.alias, ''), ''),
       COALESCE(NULLIF(c.avatar_path, ''), NULLIF(pc.avatar_path, ''), ''),
       c.updated_at
FROM contacts c
LEFT JOIN jid_mappings jm ON jm.lid_jid = c.jid
LEFT JOIN contacts pc ON pc.jid = jm.phone_jid
` + where + `
ORDER BY COALESCE(NULLIF(c.alias, ''), NULLIF(pc.alias, ''), NULLIF(c.agenda_name, ''), NULLIF(pc.agenda_name, ''), NULLIF(pc.push_name, ''), NULLIF(c.push_name, ''), c.jid) COLLATE NOCASE
`
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list contacts: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()
	contacts := make([]Contact, 0)
	for rows.Next() {
		var contact Contact
		var updatedRaw string
		if err := rows.Scan(&contact.JID, &contact.PushName, &contact.AgendaName, &contact.Alias, &contact.AvatarPath, &updatedRaw); err != nil {
			return nil, fmt.Errorf("scan contact: %w", err)
		}
		updatedAt, err := time.Parse(time.RFC3339, updatedRaw)
		if err != nil {
			return nil, fmt.Errorf("parse contact updated_at: %w", err)
		}
		contact.UpdatedAt = updatedAt
		contact.DisplayName = ResolveDisplayName(contact.JID, contact.Alias, contact.AgendaName, contact.PushName)
		contacts = append(contacts, contact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate contacts: %w", err)
	}
	return contacts, nil
}

// ResolveDisplayName applies wh-cli's contact priority rule.
func ResolveDisplayName(jid string, alias string, agendaName string, pushName string) string {
	for _, candidate := range []string{alias, agendaName, pushName} {
		if strings.TrimSpace(candidate) != "" {
			return candidate
		}
	}
	return FormatJID(jid)
}

// FormatJID returns a readable fallback that avoids exposing the raw WhatsApp suffix.
func FormatJID(jid string) string {
	user, _, ok := strings.Cut(jid, "@")
	if !ok || user == "" {
		return "WhatsApp user"
	}
	if len(user) <= 4 {
		return "WhatsApp user " + user
	}
	return "WhatsApp user ..." + user[len(user)-4:]
}
