package store

import (
	"context"
	"fmt"
	"time"
)

// Group is stored WhatsApp group metadata.
type Group struct {
	JID              string    `json:"jid"`
	Name             string    `json:"name"`
	Topic            string    `json:"topic,omitempty"`
	OwnerJID         string    `json:"owner_jid,omitempty"`
	OwnerDisplayName string    `json:"owner_display_name,omitempty"`
	CreatedAt        time.Time `json:"created_at,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// GroupParticipant is a resolved group member.
type GroupParticipant struct {
	GroupJID    string `json:"group_jid"`
	ContactJID  string `json:"contact_jid"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	PhoneJID    string `json:"phone_jid,omitempty"`
	LIDJID      string `json:"lid_jid,omitempty"`
}

// GroupRepo persists group metadata and participants.
type GroupRepo struct {
	db *DB
}

// NewGroupRepo constructs a group repository.
func NewGroupRepo(db *DB) *GroupRepo {
	return &GroupRepo{db: db}
}

// Save stores group metadata and replaces its participant set.
func (r *GroupRepo) Save(ctx context.Context, group Group, participants []GroupParticipant) error {
	now := time.Now().UTC().Format(time.RFC3339)
	created := ""
	if !group.CreatedAt.IsZero() {
		created = group.CreatedAt.UTC().Format(time.RFC3339)
	}
	if group.Name == "" {
		group.Name = FormatJID(group.JID)
	}
	if _, err := r.db.Exec(ctx, `
INSERT INTO groups (jid, name, topic, owner_jid, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(jid) DO UPDATE SET name = excluded.name, topic = excluded.topic, owner_jid = excluded.owner_jid, created_at = excluded.created_at, updated_at = excluded.updated_at
`, group.JID, group.Name, group.Topic, group.OwnerJID, created, now); err != nil {
		return fmt.Errorf("save group: %w", err)
	}
	if _, err := r.db.Exec(ctx, `DELETE FROM group_participants WHERE group_jid = ?`, group.JID); err != nil {
		return fmt.Errorf("replace group participants: %w", err)
	}
	for _, participant := range participants {
		if participant.ContactJID == "" {
			continue
		}
		if _, err := r.db.Exec(ctx, `
INSERT INTO contacts (jid, push_name, updated_at)
VALUES (?, NULLIF(?, ''), ?)
ON CONFLICT(jid) DO UPDATE SET push_name = COALESCE(excluded.push_name, contacts.push_name), updated_at = excluded.updated_at
`, participant.ContactJID, participant.DisplayName, now); err != nil {
			return fmt.Errorf("ensure participant contact: %w", err)
		}
		if _, err := r.db.Exec(ctx, `
INSERT INTO group_participants (group_jid, contact_jid, role)
VALUES (?, ?, ?)
`, group.JID, participant.ContactJID, participant.Role); err != nil {
			return fmt.Errorf("save group participant: %w", err)
		}
	}
	return nil
}

// List returns known groups.
func (r *GroupRepo) List(ctx context.Context) ([]Group, error) {
	rows, err := r.db.Query(ctx, `
SELECT g.jid, g.name, COALESCE(g.topic, ''), COALESCE(g.owner_jid, ''), COALESCE(c.alias, ''), COALESCE(c.push_name, ''), COALESCE(c.agenda_name, ''), COALESCE(g.created_at, ''), g.updated_at
FROM groups g
LEFT JOIN contacts c ON c.jid = g.owner_jid
ORDER BY g.name COLLATE NOCASE
`)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()
	groups := make([]Group, 0)
	for rows.Next() {
		group, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate groups: %w", err)
	}
	return groups, nil
}

// Get returns one group.
func (r *GroupRepo) Get(ctx context.Context, jid string) (Group, error) {
	row := r.db.QueryRow(ctx, `
SELECT g.jid, g.name, COALESCE(g.topic, ''), COALESCE(g.owner_jid, ''), COALESCE(c.alias, ''), COALESCE(c.push_name, ''), COALESCE(c.agenda_name, ''), COALESCE(g.created_at, ''), g.updated_at
FROM groups g
LEFT JOIN contacts c ON c.jid = g.owner_jid
WHERE g.jid = ?
`, jid)
	group, err := scanGroup(row)
	if err != nil {
		return Group{}, err
	}
	return group, nil
}

// ListParticipants returns resolved group participants.
func (r *GroupRepo) ListParticipants(ctx context.Context, groupJID string) ([]GroupParticipant, error) {
	rows, err := r.db.Query(ctx, `
SELECT gp.group_jid, gp.contact_jid, COALESCE(c.alias, ''), COALESCE(c.push_name, ''), COALESCE(c.agenda_name, ''), gp.role
FROM group_participants gp
LEFT JOIN contacts c ON c.jid = gp.contact_jid
WHERE gp.group_jid = ?
ORDER BY COALESCE(NULLIF(c.alias, ''), NULLIF(c.agenda_name, ''), NULLIF(c.push_name, ''), gp.contact_jid) COLLATE NOCASE
`, groupJID)
	if err != nil {
		return nil, fmt.Errorf("list group participants: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()
	participants := make([]GroupParticipant, 0)
	for rows.Next() {
		var participant GroupParticipant
		var alias, pushName, agendaName string
		if err := rows.Scan(&participant.GroupJID, &participant.ContactJID, &alias, &pushName, &agendaName, &participant.Role); err != nil {
			return nil, fmt.Errorf("scan group participant: %w", err)
		}
		participant.DisplayName = ResolveDisplayName(participant.ContactJID, alias, agendaName, pushName)
		participants = append(participants, participant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate group participants: %w", err)
	}
	return participants, nil
}

type groupScanner interface {
	Scan(dest ...any) error
}

func scanGroup(row groupScanner) (Group, error) {
	var group Group
	var alias, pushName, agendaName, createdRaw, updatedRaw string
	if err := row.Scan(&group.JID, &group.Name, &group.Topic, &group.OwnerJID, &alias, &pushName, &agendaName, &createdRaw, &updatedRaw); err != nil {
		return Group{}, fmt.Errorf("scan group: %w", err)
	}
	group.OwnerDisplayName = ResolveDisplayName(group.OwnerJID, alias, agendaName, pushName)
	if createdRaw != "" {
		createdAt, err := time.Parse(time.RFC3339, createdRaw)
		if err != nil {
			return Group{}, fmt.Errorf("parse group created_at: %w", err)
		}
		group.CreatedAt = createdAt
	}
	updatedAt, err := time.Parse(time.RFC3339, updatedRaw)
	if err != nil {
		return Group{}, fmt.Errorf("parse group updated_at: %w", err)
	}
	group.UpdatedAt = updatedAt
	return group, nil
}
