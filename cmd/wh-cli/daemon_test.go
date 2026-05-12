package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/l-i408/wh-cli/internal/store"
	"github.com/l-i408/wh-cli/internal/ws"
)

func TestValidateListenAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		addr    string
		wantErr bool
	}{
		{name: "loopback", addr: "127.0.0.1:7777"},
		{name: "localhost", addr: "localhost:7777"},
		{name: "external rejected", addr: "0.0.0.0:7777", wantErr: true},
		{name: "missing port", addr: "127.0.0.1", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateListenAddress(tt.addr)
			if tt.wantErr && !errors.Is(err, errInvalidInput) {
				t.Fatalf("expected invalid input, got %v", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
		})
	}
}

func TestExitCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		want int
	}{
		{err: nil, want: 0},
		{err: errAuth, want: 2},
		{err: errDaemonUnavailable, want: 3},
		{err: errInvalidInput, want: 4},
		{err: errLocked, want: 5},
		{err: errors.New("other"), want: 1},
	}

	for _, tt := range tests {
		if got := exitCode(tt.err); got != tt.want {
			t.Fatalf("exitCode(%v) = %d, want %d", tt.err, got, tt.want)
		}
	}
}

func TestRunUnlockRejectsInvalidTTL(t *testing.T) {
	t.Parallel()

	err := runUnlock(context.Background(), []string{"--ttl", "0s"})
	if !errors.Is(err, errInvalidInput) {
		t.Fatalf("expected invalid input, got %v", err)
	}
}

func TestParseSSEData(t *testing.T) {
	t.Parallel()

	got, ok := parseSSEData("event: session.qr\ndata: abc123\n\n")
	if !ok {
		t.Fatal("expected SSE data")
	}
	if got != "abc123" {
		t.Fatalf("data = %q, want abc123", got)
	}
}

func TestParseSSEDataMissing(t *testing.T) {
	t.Parallel()

	_, ok := parseSSEData("event: session.qr_timeout\n\n")
	if ok {
		t.Fatal("expected missing SSE data")
	}
}

func TestRunPairCodeRejectsMissingPhone(t *testing.T) {
	t.Parallel()

	err := runPairCode(context.Background(), nil)
	if !errors.Is(err, errInvalidInput) {
		t.Fatalf("expected invalid input, got %v", err)
	}
}

func TestWebsocketURL(t *testing.T) {
	t.Parallel()

	got, err := websocketURL("http://127.0.0.1:7777")
	if err != nil {
		t.Fatalf("websocketURL returned error: %v", err)
	}
	if got != "ws://127.0.0.1:7777/ws" {
		t.Fatalf("url = %q, want ws://127.0.0.1:7777/ws", got)
	}
}

func TestLoadOrCreateJWTSecret(t *testing.T) {
	t.Skip("uses the OS keyring; covered by integration runs on developer machines")
}

func TestEventingMessageSinkSavesText(t *testing.T) {
	t.Parallel()

	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "sink.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	if err := db.ApplyInitialSchema(context.Background()); err != nil {
		t.Fatalf("schema: %v", err)
	}
	sink := &eventingMessageSink{repo: store.NewMessageRepo(db)}
	err = sink.SaveText(context.Background(), store.Message{
		ID:        "msg-1",
		ChatJID:   "chat@s.whatsapp.net",
		SenderJID: "sender@s.whatsapp.net",
		Type:      "text",
		Body:      "hello",
		Status:    "received",
		Timestamp: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	}, "Sender")
	if err != nil {
		t.Fatalf("SaveText returned error: %v", err)
	}
}

func TestEventingMessageSinkSavesHistoricalTextWithoutPublishing(t *testing.T) {
	t.Parallel()

	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "historical-sink.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	if err := db.ApplyInitialSchema(context.Background()); err != nil {
		t.Fatalf("schema: %v", err)
	}
	hub := ws.NewHub()
	sink := &eventingMessageSink{repo: store.NewMessageRepo(db), hub: hub}
	err = sink.SaveHistoricalText(context.Background(), store.Message{
		ID:        "hist-1",
		ChatJID:   "chat@s.whatsapp.net",
		SenderJID: "sender@s.whatsapp.net",
		Type:      "text",
		Body:      "hello",
		Status:    "received",
		Timestamp: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	}, "Sender")
	if err != nil {
		t.Fatalf("SaveHistoricalText returned error: %v", err)
	}
	messages, err := store.NewMessageRepo(db).ListMessages(context.Background(), "chat@s.whatsapp.net", 10)
	if err != nil {
		t.Fatalf("ListMessages returned error: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
}
