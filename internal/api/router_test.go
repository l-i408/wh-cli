package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/l-i408/wh-cli/internal/auth"
	"github.com/l-i408/wh-cli/internal/store"
	"github.com/l-i408/wh-cli/internal/wa"
)

func TestHealthz(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status body = %q, want ok", body["status"])
	}
}

func TestAuthLoginRefreshAndProtectedLogout(t *testing.T) {
	t.Parallel()

	router := NewRouter(Dependencies{Auth: newTestAuth(t), Session: wa.NewMemorySession()})

	loginBody := bytes.NewBufferString(`{"passphrase_hash":"pass-hash","client_label":"cli"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/login", loginBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var pair auth.TokenPair
	if err := json.NewDecoder(rec.Body).Decode(&pair); err != nil {
		t.Fatalf("decode token pair: %v", err)
	}

	refreshBody := bytes.NewBufferString(`{"refresh_token":"` + pair.RefreshToken + `"}`)
	req = httptest.NewRequest(http.MethodPost, "/auth/refresh", refreshBody)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/auth/logout", bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestSessionQR(t *testing.T) {
	t.Parallel()

	router := NewRouter(Dependencies{Session: wa.NewMemorySession()})
	req := httptest.NewRequest(http.MethodGet, "/session/qr", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("qr status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() == 0 {
		t.Fatal("expected qr response body")
	}
}

func TestSessionQRPNG(t *testing.T) {
	t.Parallel()

	router := NewRouter(Dependencies{Session: wa.NewMemorySession()})
	req := httptest.NewRequest(http.MethodGet, "/session/qr.png", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("qr png status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("content-type = %q, want image/png", got)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("expected png body")
	}
}

func TestSessionPairCode(t *testing.T) {
	t.Parallel()

	router := NewRouter(Dependencies{Session: wa.NewMemorySession()})
	req := httptest.NewRequest(http.MethodPost, "/session/pair-code", bytes.NewBufferString(`{"phone":"15551234567"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("pair code status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode pair code response: %v", err)
	}
	if body["code"] == "" {
		t.Fatal("expected pair code")
	}
}

func TestPostMessage(t *testing.T) {
	t.Parallel()

	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "messages.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	if err := db.ApplyInitialSchema(context.Background()); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	authSvc := newTestAuth(t)
	pair, err := authSvc.Login(context.Background(), []byte("pass-hash"), "cli")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	router := NewRouter(Dependencies{
		Auth:     authSvc,
		Sender:   fakeSender{},
		Messages: store.NewMessageRepo(db),
	})

	req := httptest.NewRequest(http.MethodPost, "/messages", bytes.NewBufferString(`{"chat_jid":"chat@s.whatsapp.net","type":"text","body":"hello"}`))
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("post message status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Status *string `json:"status"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status == nil || *body.Status != "sent" {
		t.Fatalf("status = %v, want sent", body.Status)
	}
}

func TestGetMessagesIncludesNormalizedStatus(t *testing.T) {
	t.Parallel()

	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "list-message-status.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	if err := db.ApplyInitialSchema(context.Background()); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	repo := store.NewMessageRepo(db)
	if err := repo.SaveText(context.Background(), store.Message{
		ID:        "msg-status",
		ChatJID:   "chat@s.whatsapp.net",
		SenderJID: "me@s.whatsapp.net",
		Type:      "text",
		Body:      "hello",
		Status:    "server_ack",
		Timestamp: time.Date(2026, 5, 9, 11, 0, 0, 0, time.UTC),
	}, "chat"); err != nil {
		t.Fatalf("save message: %v", err)
	}

	authSvc := newTestAuth(t)
	pair, err := authSvc.Login(context.Background(), []byte("pass-hash"), "cli")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	router := NewRouter(Dependencies{Auth: authSvc, Messages: repo})

	req := httptest.NewRequest(http.MethodGet, "/chats/chat@s.whatsapp.net/messages?limit=10", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get messages status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var page struct {
		Items []struct {
			Status *string `json:"status"`
		} `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(page.Items))
	}
	if page.Items[0].Status == nil || *page.Items[0].Status != "sent" {
		t.Fatalf("status = %v, want sent", page.Items[0].Status)
	}
}

func TestPostMediaMessage(t *testing.T) {
	t.Parallel()

	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "media-message.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	if err := db.ApplyInitialSchema(context.Background()); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	srcPath := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(srcPath, []byte("\x89PNG\r\n\x1a\npayload"), 0o600); err != nil {
		t.Fatalf("write src media: %v", err)
	}

	authSvc := newTestAuth(t)
	pair, err := authSvc.Login(context.Background(), []byte("pass-hash"), "cli")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	router := NewRouter(Dependencies{
		Auth:     authSvc,
		Sender:   fakeSender{},
		Messages: store.NewMessageRepo(db),
		Media:    store.NewMediaRepo(db),
		MediaDir: filepath.Join(t.TempDir(), "media"),
	})

	reqBody := `{"chat_jid":"chat@s.whatsapp.net","file_path":"` + filepath.ToSlash(srcPath) + `","caption":"photo"}`
	req := httptest.NewRequest(http.MethodPost, "/messages", bytes.NewBufferString(reqBody))
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("post media status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var msg store.Message
	if err := json.NewDecoder(rec.Body).Decode(&msg); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if msg.Type != "image" {
		t.Fatalf("message type = %q, want image", msg.Type)
	}
	if msg.MediaPath == "" {
		t.Fatal("expected media path")
	}
}

func TestPostMessageReply(t *testing.T) {
	t.Parallel()

	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "reply-message.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	if err := db.ApplyInitialSchema(context.Background()); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	repo := store.NewMessageRepo(db)
	if err := repo.SaveText(context.Background(), store.Message{
		ID:        "target-1",
		ChatJID:   "chat@s.whatsapp.net",
		SenderJID: "sender@s.whatsapp.net",
		Type:      "text",
		Body:      "hello",
		Status:    "received",
		Timestamp: time.Date(2026, 5, 9, 11, 0, 0, 0, time.UTC),
	}, "chat"); err != nil {
		t.Fatalf("save target: %v", err)
	}

	authSvc := newTestAuth(t)
	pair, err := authSvc.Login(context.Background(), []byte("pass-hash"), "cli")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	router := NewRouter(Dependencies{Auth: authSvc, Sender: fakeSender{}, Messages: repo})

	req := httptest.NewRequest(http.MethodPost, "/messages/target-1/reply", bytes.NewBufferString(`{"body":"reply"}`))
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("reply status = %d, body=%s", rec.Code, rec.Body.String())
	}
	msg, err := repo.GetMessage(context.Background(), "reply-1")
	if err != nil {
		t.Fatalf("get reply: %v", err)
	}
	if msg.ReplyToID != "target-1" {
		t.Fatalf("reply_to_id = %q, want target-1", msg.ReplyToID)
	}
}

func TestPostMessageReaction(t *testing.T) {
	t.Parallel()

	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "reaction-message.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	if err := db.ApplyInitialSchema(context.Background()); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	repo := store.NewMessageRepo(db)
	if err := repo.SaveText(context.Background(), store.Message{
		ID:        "target-1",
		ChatJID:   "chat@s.whatsapp.net",
		SenderJID: "sender@s.whatsapp.net",
		Type:      "text",
		Body:      "hello",
		Status:    "received",
		Timestamp: time.Date(2026, 5, 9, 11, 0, 0, 0, time.UTC),
	}, "chat"); err != nil {
		t.Fatalf("save target: %v", err)
	}

	authSvc := newTestAuth(t)
	pair, err := authSvc.Login(context.Background(), []byte("pass-hash"), "cli")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	router := NewRouter(Dependencies{Auth: authSvc, Sender: fakeSender{}, Messages: repo})

	req := httptest.NewRequest(http.MethodPost, "/messages/target-1/react", bytes.NewBufferString(`{"emoji":"+"}`))
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("reaction status = %d, body=%s", rec.Code, rec.Body.String())
	}
	msg, err := repo.GetMessage(context.Background(), "target-1")
	if err != nil {
		t.Fatalf("get target: %v", err)
	}
	if msg.ReactionsJSON == "{}" || msg.ReactionsJSON == "" {
		t.Fatal("expected stored reaction")
	}
}

func newTestAuth(t *testing.T) *auth.Service {
	t.Helper()

	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "api-auth.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	if err := db.ApplyInitialSchema(context.Background()); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	return auth.NewService(
		[]byte("01234567890123456789012345678901"),
		[]byte("pass-hash"),
		auth.NewTokenStore(db),
		func() time.Time { return now },
	)
}

type fakeSender struct{}

func (fakeSender) SendText(_ context.Context, chatJID string, _ string) (wa.SentMessage, error) {
	return wa.SentMessage{
		ID:        "msg-1",
		ChatJID:   chatJID,
		SenderJID: "me@s.whatsapp.net",
		Timestamp: time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC),
	}, nil
}

func (fakeSender) SendMedia(_ context.Context, chatJID string, _ []byte, _ store.MediaBlob, _ wa.MediaKind, _ string, _ string) (wa.SentMessage, error) {
	return wa.SentMessage{
		ID:        "media-1",
		ChatJID:   chatJID,
		SenderJID: "me@s.whatsapp.net",
		Timestamp: time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC),
	}, nil
}

func (fakeSender) SendReaction(_ context.Context, target store.Message, _ string) (wa.SentMessage, error) {
	return wa.SentMessage{
		ID:        "reaction-1",
		ChatJID:   target.ChatJID,
		SenderJID: "me@s.whatsapp.net",
		Timestamp: time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC),
	}, nil
}

func (fakeSender) SendReply(_ context.Context, target store.Message, _ string) (wa.SentMessage, error) {
	return wa.SentMessage{
		ID:        "reply-1",
		ChatJID:   target.ChatJID,
		SenderJID: "me@s.whatsapp.net",
		Timestamp: time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC),
	}, nil
}

func (fakeSender) SendForward(_ context.Context, targetChatJID string, _ store.Message) (wa.SentMessage, error) {
	return wa.SentMessage{
		ID:        "forward-1",
		ChatJID:   targetChatJID,
		SenderJID: "me@s.whatsapp.net",
		Timestamp: time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC),
	}, nil
}
