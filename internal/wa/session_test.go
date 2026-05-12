package wa

import (
	"context"
	"testing"

	"github.com/l-i408/wh-cli/internal/store"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestMemorySessionQRTransitionsToPending(t *testing.T) {
	t.Parallel()

	session := NewMemorySession()
	status, err := session.Status(context.Background())
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if status != StatusLoggedOut {
		t.Fatalf("status = %q, want %q", status, StatusLoggedOut)
	}

	qr, err := session.QR(context.Background())
	if err != nil {
		t.Fatalf("QR returned error: %v", err)
	}
	if got := <-qr; got == "" {
		t.Fatal("expected QR code")
	}

	status, err = session.Status(context.Background())
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if status != StatusQRPending {
		t.Fatalf("status = %q, want %q", status, StatusQRPending)
	}
}

func TestAudioMediaMessageIsNotVoiceNote(t *testing.T) {
	t.Parallel()

	msg := mediaMessage(whatsmeow.UploadResponse{}, store.MediaBlob{MIME: "audio/ogg"}, MediaKindAudio, "", "audio.ogg")
	if msg.GetAudioMessage() == nil {
		t.Fatal("expected audio message")
	}
	if msg.GetAudioMessage().GetPTT() {
		t.Fatal("expected --audio to be sent as regular audio, not PTT voice note")
	}
}

func TestReceiptStatus(t *testing.T) {
	t.Parallel()

	tests := map[types.ReceiptType]string{
		types.ReceiptTypeDelivered:   "delivered",
		types.ReceiptTypeRead:        "read",
		types.ReceiptTypePlayed:      "played",
		types.ReceiptTypeRetry:       "retry",
		types.ReceiptTypeServerError: "server_error",
	}
	for receiptType, want := range tests {
		if got := receiptStatus(receiptType); got != want {
			t.Fatalf("receiptStatus(%q) = %q, want %q", receiptType, got, want)
		}
	}
}

func TestHandleHistorySyncStoresMessagesAndContactNames(t *testing.T) {
	t.Parallel()

	sink := &historySyncSink{}
	session := &WhatsmeowSession{
		client:   whatsmeow.NewClient(nil, nil),
		messages: sink,
		contacts: sink,
	}
	session.handleHistorySync(context.Background(), &events.HistorySync{Data: &waHistorySync.HistorySync{
		Conversations: []*waHistorySync.Conversation{{
			ID:          proto.String("chat@lid"),
			DisplayName: proto.String("Ivan"),
			Messages: []*waHistorySync.HistorySyncMsg{{
				Message: &waWeb.WebMessageInfo{
					Key: &waCommon.MessageKey{
						RemoteJID: proto.String("chat@lid"),
						FromMe:    proto.Bool(false),
						ID:        proto.String("hist-1"),
					},
					Message:          &waE2E.Message{Conversation: proto.String("hola")},
					MessageTimestamp: proto.Uint64(1778414400),
					PushName:         proto.String("Oliver"),
				},
			}},
		}},
		Pushnames: []*waHistorySync.Pushname{{
			ID:       proto.String("chat@lid"),
			Pushname: proto.String("Oliver"),
		}},
		InlineContacts: []*waHistorySync.InlineContact{{
			LidJID:   proto.String("chat@lid"),
			FullName: proto.String("Ivan"),
		}},
	}})

	if len(sink.messages) != 1 {
		t.Fatalf("stored messages = %d, want 1", len(sink.messages))
	}
	if sink.messages[0].ID != "hist-1" || sink.messages[0].ChatJID != "chat@lid" || sink.messages[0].Body != "hola" {
		t.Fatalf("stored message = %+v, want history text message", sink.messages[0])
	}
	if sink.displayNames[0] != "Oliver" {
		t.Fatalf("display name = %q, want PushName from message", sink.displayNames[0])
	}
	if sink.pushNames["chat@lid"] != "Oliver" {
		t.Fatalf("push name = %q, want Oliver", sink.pushNames["chat@lid"])
	}
	if sink.agendaNames["chat@lid"] != "Ivan" {
		t.Fatalf("agenda name = %q, want Ivan", sink.agendaNames["chat@lid"])
	}
}

type historySyncSink struct {
	messages     []store.Message
	displayNames []string
	pushNames    map[string]string
	agendaNames  map[string]string
}

func (s *historySyncSink) SaveText(_ context.Context, msg store.Message, displayName string) error {
	s.messages = append(s.messages, msg)
	s.displayNames = append(s.displayNames, displayName)
	return nil
}

func (s *historySyncSink) UpdateStatus(_ context.Context, _ string, _ string) error {
	return nil
}

func (s *historySyncSink) UpsertPushName(_ context.Context, jid string, name string) error {
	if s.pushNames == nil {
		s.pushNames = make(map[string]string)
	}
	s.pushNames[jid] = name
	return nil
}

func (s *historySyncSink) UpsertAgendaName(_ context.Context, jid string, name string) error {
	if s.agendaNames == nil {
		s.agendaNames = make(map[string]string)
	}
	s.agendaNames[jid] = name
	return nil
}

func (s *historySyncSink) BulkUpsertPushNames(_ context.Context, names map[string]string) error {
	if s.pushNames == nil {
		s.pushNames = make(map[string]string)
	}
	for jid, name := range names {
		s.pushNames[jid] = name
	}
	return nil
}

func (s *historySyncSink) BulkUpsertAgendaNames(_ context.Context, names map[string]string) error {
	if s.agendaNames == nil {
		s.agendaNames = make(map[string]string)
	}
	for jid, name := range names {
		s.agendaNames[jid] = name
	}
	return nil
}

func (s *historySyncSink) BulkUpsertJIDMappings(_ context.Context, _ []store.JIDMapping) error {
	return nil
}
