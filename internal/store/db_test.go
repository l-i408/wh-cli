package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenSQLiteFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "wh-cli.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})
}

func TestOpenRejectsEmptyPath(t *testing.T) {
	t.Parallel()

	_, err := Open(context.Background(), "")
	if !errors.Is(err, ErrEmptyDBPath) {
		t.Fatalf("expected ErrEmptyDBPath, got %v", err)
	}
}

func TestMessageRepoSaveSentText(t *testing.T) {
	t.Parallel()

	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "messages.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})
	if err := db.ApplyInitialSchema(context.Background()); err != nil {
		t.Fatalf("ApplyInitialSchema returned error: %v", err)
	}

	repo := NewMessageRepo(db)
	err = repo.SaveSentText(context.Background(), Message{
		ID:        "msg-1",
		ChatJID:   "chat@s.whatsapp.net",
		SenderJID: "me@s.whatsapp.net",
		Type:      "text",
		Body:      "hello",
		Status:    "sent",
		Timestamp: time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("SaveSentText returned error: %v", err)
	}
}

func TestMessageRepoUpdateStatus(t *testing.T) {
	t.Parallel()

	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "status.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})
	if err := db.ApplyInitialSchema(context.Background()); err != nil {
		t.Fatalf("ApplyInitialSchema returned error: %v", err)
	}

	repo := NewMessageRepo(db)
	err = repo.SaveText(context.Background(), Message{
		ID:        "msg-status",
		ChatJID:   "chat@s.whatsapp.net",
		SenderJID: "me@s.whatsapp.net",
		Type:      "audio",
		Status:    "server_ack",
		Timestamp: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	}, "Chat")
	if err != nil {
		t.Fatalf("SaveText returned error: %v", err)
	}
	if err := repo.UpdateStatus(context.Background(), "msg-status", "delivered"); err != nil {
		t.Fatalf("UpdateStatus returned error: %v", err)
	}
	messages, err := repo.ListMessages(context.Background(), "chat@s.whatsapp.net", 1)
	if err != nil {
		t.Fatalf("ListMessages returned error: %v", err)
	}
	if messages[0].Status != "delivered" {
		t.Fatalf("status = %q, want delivered", messages[0].Status)
	}
}

func TestMessageRepoBackfillMissingSenderJID(t *testing.T) {
	t.Parallel()

	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "sender-backfill.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})
	if err := db.ApplyInitialSchema(context.Background()); err != nil {
		t.Fatalf("ApplyInitialSchema returned error: %v", err)
	}

	repo := NewMessageRepo(db)
	for _, msg := range []Message{
		{
			ID:        "old-sent",
			ChatJID:   "contact@lid",
			Type:      "text",
			Body:      "sent",
			Status:    "server_ack",
			Timestamp: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
		},
		{
			ID:        "old-received-dm",
			ChatJID:   "contact@lid",
			Type:      "text",
			Body:      "received",
			Status:    "received",
			Timestamp: time.Date(2026, 5, 10, 12, 1, 0, 0, time.UTC),
		},
		{
			ID:        "old-received-group",
			ChatJID:   "group@g.us",
			Type:      "text",
			Body:      "group",
			Status:    "received",
			Timestamp: time.Date(2026, 5, 10, 12, 2, 0, 0, time.UTC),
		},
	} {
		if err := repo.SaveText(context.Background(), msg, ""); err != nil {
			t.Fatalf("SaveText(%s) returned error: %v", msg.ID, err)
		}
	}
	if err := repo.BackfillMissingSenderJID(context.Background(), "me@s.whatsapp.net"); err != nil {
		t.Fatalf("BackfillMissingSenderJID returned error: %v", err)
	}

	sent, err := repo.GetMessage(context.Background(), "old-sent")
	if err != nil {
		t.Fatalf("GetMessage(old-sent): %v", err)
	}
	if sent.SenderJID != "me@s.whatsapp.net" {
		t.Fatalf("sent sender = %q, want own jid", sent.SenderJID)
	}
	received, err := repo.GetMessage(context.Background(), "old-received-dm")
	if err != nil {
		t.Fatalf("GetMessage(old-received-dm): %v", err)
	}
	if received.SenderJID != "contact@lid" {
		t.Fatalf("received sender = %q, want chat jid", received.SenderJID)
	}
	group, err := repo.GetMessage(context.Background(), "old-received-group")
	if err != nil {
		t.Fatalf("GetMessage(old-received-group): %v", err)
	}
	if group.SenderJID != "" {
		t.Fatalf("group sender = %q, want empty because sender is unknown", group.SenderJID)
	}
}

func TestResolveDisplayNamePrefersAgendaOverPushName(t *testing.T) {
	t.Parallel()

	got := ResolveDisplayName("user@lid", "", "Ivan", "Oliver")
	if got != "Ivan" {
		t.Fatalf("ResolveDisplayName = %q, want Ivan", got)
	}

	got = ResolveDisplayName("user@lid", "Manual", "Ivan", "Oliver")
	if got != "Manual" {
		t.Fatalf("ResolveDisplayName with alias = %q, want Manual", got)
	}
}

func TestContactRepoStoresAgendaNameSeparately(t *testing.T) {
	t.Parallel()

	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "contacts.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})
	if err := db.ApplyInitialSchema(context.Background()); err != nil {
		t.Fatalf("ApplyInitialSchema returned error: %v", err)
	}

	repo := NewContactRepo(db)
	if err := repo.UpsertPushName(context.Background(), "contact@lid", "Oliver"); err != nil {
		t.Fatalf("UpsertPushName returned error: %v", err)
	}
	if err := repo.UpsertAgendaName(context.Background(), "contact@lid", "Ivan"); err != nil {
		t.Fatalf("UpsertAgendaName returned error: %v", err)
	}
	contact, err := repo.Get(context.Background(), "contact@lid")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if contact.PushName != "Oliver" || contact.AgendaName != "Ivan" || contact.DisplayName != "Ivan" {
		t.Fatalf("contact = %+v, want separate push/agenda with display Ivan", contact)
	}
}

func TestContactRepoGetMergesMappedPhoneContactForLID(t *testing.T) {
	t.Parallel()

	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "mapped-contact.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})
	if err := db.ApplyInitialSchema(context.Background()); err != nil {
		t.Fatalf("ApplyInitialSchema returned error: %v", err)
	}

	contacts := NewContactRepo(db)
	if err := contacts.UpsertPushName(context.Background(), "contact@lid", "Los garcia"); err != nil {
		t.Fatalf("UpsertPushName(lid) returned error: %v", err)
	}
	if err := contacts.UpsertPushName(context.Background(), "contact@s.whatsapp.net", "SEBAS"); err != nil {
		t.Fatalf("UpsertPushName(phone) returned error: %v", err)
	}
	if err := contacts.UpsertAgendaName(context.Background(), "contact@s.whatsapp.net", "Papa Caluchi"); err != nil {
		t.Fatalf("UpsertAgendaName(phone) returned error: %v", err)
	}
	if err := contacts.BulkUpsertJIDMappings(context.Background(), []JIDMapping{{
		LIDJID:   "contact@lid",
		PhoneJID: "contact@s.whatsapp.net",
	}}); err != nil {
		t.Fatalf("BulkUpsertJIDMappings returned error: %v", err)
	}

	contact, err := contacts.Get(context.Background(), "contact@lid")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if contact.AgendaName != "Papa Caluchi" || contact.PushName != "SEBAS" || contact.DisplayName != "Papa Caluchi" {
		t.Fatalf("contact = %+v, want phone agenda/push merged into lid display", contact)
	}
}

func TestMessageRepoListChatsDeduplicatesLegacyJIDWhenLIDHasSameMessage(t *testing.T) {
	t.Parallel()

	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "dedupe.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})
	if err := db.ApplyInitialSchema(context.Background()); err != nil {
		t.Fatalf("ApplyInitialSchema returned error: %v", err)
	}

	repo := NewMessageRepo(db)
	contacts := NewContactRepo(db)
	if err := contacts.BulkUpsertJIDMappings(context.Background(), []JIDMapping{{
		LIDJID:   "contact@lid",
		PhoneJID: "contact@s.whatsapp.net",
	}}); err != nil {
		t.Fatalf("BulkUpsertJIDMappings returned error: %v", err)
	}
	ts := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	for _, chatJID := range []string{"contact@lid", "contact@s.whatsapp.net"} {
		if err := repo.SaveText(context.Background(), Message{
			ID:        "same-message",
			ChatJID:   chatJID,
			SenderJID: chatJID,
			Type:      "text",
			Body:      "hello",
			Status:    "received",
			Timestamp: ts,
		}, chatJID); err != nil {
			t.Fatalf("SaveText(%s) returned error: %v", chatJID, err)
		}
	}

	chats, err := repo.ListChats(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListChats returned error: %v", err)
	}
	if len(chats) != 1 {
		t.Fatalf("len(chats) = %d, want 1: %+v", len(chats), chats)
	}
	if chats[0].JID != "contact@lid" {
		t.Fatalf("chat jid = %q, want contact@lid", chats[0].JID)
	}
}

func TestMessageRepoListChatsUsesMappedPhoneNameForLID(t *testing.T) {
	t.Parallel()

	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "chat-mapped-name.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})
	if err := db.ApplyInitialSchema(context.Background()); err != nil {
		t.Fatalf("ApplyInitialSchema returned error: %v", err)
	}

	contacts := NewContactRepo(db)
	if err := contacts.UpsertPushName(context.Background(), "contact@lid", "Los garcia"); err != nil {
		t.Fatalf("UpsertPushName(lid) returned error: %v", err)
	}
	if err := contacts.UpsertPushName(context.Background(), "contact@s.whatsapp.net", "SEBAS"); err != nil {
		t.Fatalf("UpsertPushName(phone) returned error: %v", err)
	}
	if err := contacts.UpsertAgendaName(context.Background(), "contact@s.whatsapp.net", "Papa Caluchi"); err != nil {
		t.Fatalf("UpsertAgendaName(phone) returned error: %v", err)
	}
	if err := contacts.BulkUpsertJIDMappings(context.Background(), []JIDMapping{{
		LIDJID:   "contact@lid",
		PhoneJID: "contact@s.whatsapp.net",
	}}); err != nil {
		t.Fatalf("BulkUpsertJIDMappings returned error: %v", err)
	}
	repo := NewMessageRepo(db)
	if err := repo.SaveText(context.Background(), Message{
		ID:        "mapped-chat-message",
		ChatJID:   "contact@lid",
		SenderJID: "contact@lid",
		Type:      "text",
		Body:      "hello",
		Status:    "received",
		Timestamp: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	}, "Los garcia"); err != nil {
		t.Fatalf("SaveText returned error: %v", err)
	}

	chats, err := repo.ListChats(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListChats returned error: %v", err)
	}
	if len(chats) != 1 {
		t.Fatalf("len(chats) = %d, want 1: %+v", len(chats), chats)
	}
	if chats[0].DisplayName != "Papa Caluchi" {
		t.Fatalf("display name = %q, want Papa Caluchi", chats[0].DisplayName)
	}
}

func TestMessageRepoListChatsDoesNotUseStaleStoredDMDisplayName(t *testing.T) {
	t.Parallel()

	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "stale-dm-name.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})
	if err := db.ApplyInitialSchema(context.Background()); err != nil {
		t.Fatalf("ApplyInitialSchema returned error: %v", err)
	}

	repo := NewMessageRepo(db)
	if err := repo.SaveText(context.Background(), Message{
		ID:        "dm-message",
		ChatJID:   "contact@lid",
		SenderJID: "contact@lid",
		Type:      "text",
		Body:      "hola",
		Status:    "received",
		Timestamp: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	}, "Oliver"); err != nil {
		t.Fatalf("SaveText returned error: %v", err)
	}

	chats, err := repo.ListChats(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListChats returned error: %v", err)
	}
	if len(chats) != 1 {
		t.Fatalf("len(chats) = %d, want 1: %+v", len(chats), chats)
	}
	if chats[0].DisplayName == "Oliver" {
		t.Fatalf("display name = %q, want non-stored fallback", chats[0].DisplayName)
	}
}

func TestMessageRepoListChatsUsesGroupNameInsteadOfSenderPushName(t *testing.T) {
	t.Parallel()

	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "group-chat-name.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})
	if err := db.ApplyInitialSchema(context.Background()); err != nil {
		t.Fatalf("ApplyInitialSchema returned error: %v", err)
	}

	groups := NewGroupRepo(db)
	if err := groups.Save(context.Background(), Group{
		JID:  "family@g.us",
		Name: "Family",
	}, nil); err != nil {
		t.Fatalf("Save group returned error: %v", err)
	}
	repo := NewMessageRepo(db)
	if err := repo.SaveText(context.Background(), Message{
		ID:        "group-message",
		ChatJID:   "family@g.us",
		SenderJID: "marta@lid",
		Type:      "text",
		Body:      "hola",
		Status:    "received",
		Timestamp: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	}, "Marta"); err != nil {
		t.Fatalf("SaveText returned error: %v", err)
	}

	chats, err := repo.ListChats(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListChats returned error: %v", err)
	}
	if len(chats) != 1 {
		t.Fatalf("len(chats) = %d, want 1: %+v", len(chats), chats)
	}
	if chats[0].DisplayName != "Family" {
		t.Fatalf("display name = %q, want group name", chats[0].DisplayName)
	}
}

func TestMessageRepoListMessagesIncludesLegacyMessagesForMappedLID(t *testing.T) {
	t.Parallel()

	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "aliases.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})
	if err := db.ApplyInitialSchema(context.Background()); err != nil {
		t.Fatalf("ApplyInitialSchema returned error: %v", err)
	}

	contacts := NewContactRepo(db)
	if err := contacts.BulkUpsertJIDMappings(context.Background(), []JIDMapping{{
		LIDJID:   "contact@lid",
		PhoneJID: "contact@s.whatsapp.net",
	}}); err != nil {
		t.Fatalf("BulkUpsertJIDMappings returned error: %v", err)
	}
	repo := NewMessageRepo(db)
	for i, msg := range []Message{
		{
			ID:        "legacy-1",
			ChatJID:   "contact@s.whatsapp.net",
			SenderJID: "contact@s.whatsapp.net",
			Type:      "text",
			Body:      "old",
			Status:    "received",
			Timestamp: time.Date(2026, 5, 10, 11, 0, 0, 0, time.UTC),
		},
		{
			ID:        "lid-1",
			ChatJID:   "contact@lid",
			SenderJID: "contact@lid",
			Type:      "text",
			Body:      "new",
			Status:    "received",
			Timestamp: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
		},
	} {
		if err := repo.SaveText(context.Background(), msg, "Contact"); err != nil {
			t.Fatalf("SaveText[%d] returned error: %v", i, err)
		}
	}

	messages, err := repo.ListMessages(context.Background(), "contact@lid", 10)
	if err != nil {
		t.Fatalf("ListMessages returned error: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2: %+v", len(messages), messages)
	}
	if messages[0].ID != "lid-1" || messages[1].ID != "legacy-1" {
		t.Fatalf("messages order = %+v, want lid then legacy", messages)
	}
}

func TestMessageRepoCanonicalChatJIDMapsLegacyToLID(t *testing.T) {
	t.Parallel()

	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "canonical-chat.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})
	if err := db.ApplyInitialSchema(context.Background()); err != nil {
		t.Fatalf("ApplyInitialSchema returned error: %v", err)
	}

	contacts := NewContactRepo(db)
	if err := contacts.BulkUpsertJIDMappings(context.Background(), []JIDMapping{{
		LIDJID:   "contact@lid",
		PhoneJID: "contact@s.whatsapp.net",
	}}); err != nil {
		t.Fatalf("BulkUpsertJIDMappings returned error: %v", err)
	}
	repo := NewMessageRepo(db)
	got, err := repo.CanonicalChatJID(context.Background(), "contact@s.whatsapp.net")
	if err != nil {
		t.Fatalf("CanonicalChatJID returned error: %v", err)
	}
	if got != "contact@lid" {
		t.Fatalf("CanonicalChatJID = %q, want contact@lid", got)
	}
}

func TestContactRepoBulkUpsertJIDMappingsReplacesStalePhoneMapping(t *testing.T) {
	t.Parallel()

	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "jid-mappings.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})
	if err := db.ApplyInitialSchema(context.Background()); err != nil {
		t.Fatalf("ApplyInitialSchema returned error: %v", err)
	}

	repo := NewContactRepo(db)
	if err := repo.BulkUpsertJIDMappings(context.Background(), []JIDMapping{{LIDJID: "old@lid", PhoneJID: "phone@s.whatsapp.net"}}); err != nil {
		t.Fatalf("initial BulkUpsertJIDMappings returned error: %v", err)
	}
	if err := repo.BulkUpsertJIDMappings(context.Background(), []JIDMapping{{LIDJID: "new@lid", PhoneJID: "phone@s.whatsapp.net"}}); err != nil {
		t.Fatalf("replacement BulkUpsertJIDMappings returned error: %v", err)
	}

	var lid string
	if err := db.QueryRow(context.Background(), `SELECT lid_jid FROM jid_mappings WHERE phone_jid = ?`, "phone@s.whatsapp.net").Scan(&lid); err != nil {
		t.Fatalf("query mapping returned error: %v", err)
	}
	if lid != "new@lid" {
		t.Fatalf("lid = %q, want new@lid", lid)
	}
}

func TestApplyInitialSchemaMigratesExistingChatsTable(t *testing.T) {
	t.Parallel()

	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})
	_, err = db.Exec(context.Background(), `
CREATE TABLE chats (
    jid TEXT PRIMARY KEY,
    type TEXT NOT NULL CHECK (type IN ('dm', 'group')),
    last_message_id TEXT,
    unread_count INTEGER NOT NULL DEFAULT 0,
    pinned INTEGER NOT NULL DEFAULT 0,
    muted_until TEXT,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	if err != nil {
		t.Fatalf("create legacy chats table: %v", err)
	}
	if err := db.ApplyInitialSchema(context.Background()); err != nil {
		t.Fatalf("ApplyInitialSchema returned error: %v", err)
	}
	repo := NewMessageRepo(db)
	if _, err := repo.ListChats(context.Background(), 10); err != nil {
		t.Fatalf("ListChats returned error after migration: %v", err)
	}
}
