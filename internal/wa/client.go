package wa

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/google/uuid"
	"github.com/l-i408/wh-cli/internal/media"
	"github.com/l-i408/wh-cli/internal/store"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWa6"
	waStore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// WhatsmeowSession manages the real WhatsApp multi-device connection.
type WhatsmeowSession struct {
	mu                sync.Mutex
	client            *whatsmeow.Client
	dbPath            string
	messages          MessageSink
	groups            GroupSink
	contacts          ContactSink
	devices           DeviceSink
	media             *store.MediaRepo
	mediaDir          string
	qrRunning         bool
	qrCancel          context.CancelFunc
	latestQR          string
	qrUpdated         time.Time
	fullSyncRequested bool
}

// MessageSink receives normalized incoming WhatsApp messages.
type MessageSink interface {
	SaveText(ctx context.Context, msg store.Message, displayName string) error
	UpdateStatus(ctx context.Context, messageID string, status string) error
}

type senderBackfillSink interface {
	BackfillMissingSenderJID(ctx context.Context, ownJID string) error
}

type historicalMessageSink interface {
	SaveHistoricalText(ctx context.Context, msg store.Message, displayName string) error
}

// GroupSink receives refreshed WhatsApp group metadata.
type GroupSink interface {
	Save(ctx context.Context, group store.Group, participants []store.GroupParticipant) error
}

// ContactSink stores resolved contact push names.
type ContactSink interface {
	UpsertPushName(ctx context.Context, jid string, pushName string) error
	UpsertAgendaName(ctx context.Context, jid string, agendaName string) error
	BulkUpsertPushNames(ctx context.Context, names map[string]string) error
	BulkUpsertAgendaNames(ctx context.Context, names map[string]string) error
	BulkUpsertJIDMappings(ctx context.Context, mappings []store.JIDMapping) error
}

// DeviceSink receives notifications when a new linked device is detected.
type DeviceSink interface {
	OnDeviceLinked(ctx context.Context, jid string, platform string, ts time.Time)
}

// LinkedDevice is a WhatsApp multi-device companion session.
type LinkedDevice struct {
	JID      string `json:"jid"`
	Platform string `json:"platform"`
}

// SentMessage describes a message accepted by WhatsApp.
type SentMessage struct {
	ID        string
	ChatJID   string
	SenderJID string
	Timestamp time.Time
}

// MediaKind selects the WhatsApp media envelope used for an outbound file.
type MediaKind string

const (
	MediaKindImage    MediaKind = "image"
	MediaKindDocument MediaKind = "document"
	MediaKindAudio    MediaKind = "audio"
)

// SendText sends a plain text message through WhatsApp.
func (s *WhatsmeowSession) SendText(ctx context.Context, chatJID string, body string) (SentMessage, error) {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()

	if !client.IsLoggedIn() {
		return SentMessage{}, errors.New("whatsapp is not connected")
	}
	to, err := types.ParseJID(chatJID)
	if err != nil {
		return SentMessage{}, fmt.Errorf("parse chat jid: %w", err)
	}
	resp, err := client.SendMessage(ctx, to, &waE2E.Message{Conversation: proto.String(body)})
	if err != nil {
		return SentMessage{}, fmt.Errorf("send text: %w", err)
	}
	return SentMessage{
		ID:        string(resp.ID),
		ChatJID:   chatJID,
		SenderJID: client.Store.GetJID().String(),
		Timestamp: resp.Timestamp,
	}, nil
}

// SendReaction sends or clears a reaction for an existing WhatsApp message.
func (s *WhatsmeowSession) SendReaction(ctx context.Context, target store.Message, emoji string) (SentMessage, error) {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()

	if !client.IsLoggedIn() {
		return SentMessage{}, errors.New("whatsapp is not connected")
	}
	chat, err := types.ParseJID(target.ChatJID)
	if err != nil {
		return SentMessage{}, fmt.Errorf("parse chat jid: %w", err)
	}
	sender, err := parseOptionalJID(target.SenderJID)
	if err != nil {
		return SentMessage{}, fmt.Errorf("parse sender jid: %w", err)
	}
	resp, err := client.SendMessage(ctx, chat, client.BuildReaction(chat, sender, types.MessageID(target.ID), emoji))
	if err != nil {
		return SentMessage{}, fmt.Errorf("send reaction: %w", err)
	}
	return SentMessage{
		ID:        string(resp.ID),
		ChatJID:   target.ChatJID,
		SenderJID: client.Store.GetJID().String(),
		Timestamp: resp.Timestamp,
	}, nil
}

// SendReply sends a text message quoting a previously stored text message.
func (s *WhatsmeowSession) SendReply(ctx context.Context, target store.Message, body string) (SentMessage, error) {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()

	if !client.IsLoggedIn() {
		return SentMessage{}, errors.New("whatsapp is not connected")
	}
	chat, err := types.ParseJID(target.ChatJID)
	if err != nil {
		return SentMessage{}, fmt.Errorf("parse chat jid: %w", err)
	}
	sender, err := parseOptionalJID(target.SenderJID)
	if err != nil {
		return SentMessage{}, fmt.Errorf("parse sender jid: %w", err)
	}
	resp, err := client.SendMessage(ctx, chat, &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text:        proto.String(body),
			ContextInfo: replyContextInfo(target, sender),
		},
	})
	if err != nil {
		return SentMessage{}, fmt.Errorf("send reply: %w", err)
	}
	return SentMessage{
		ID:        string(resp.ID),
		ChatJID:   target.ChatJID,
		SenderJID: client.Store.GetJID().String(),
		Timestamp: resp.Timestamp,
	}, nil
}

// SendForward sends a stored text message to another chat with WhatsApp's forwarded marker.
func (s *WhatsmeowSession) SendForward(ctx context.Context, targetChatJID string, original store.Message) (SentMessage, error) {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()

	if !client.IsLoggedIn() {
		return SentMessage{}, errors.New("whatsapp is not connected")
	}
	to, err := types.ParseJID(targetChatJID)
	if err != nil {
		return SentMessage{}, fmt.Errorf("parse target chat jid: %w", err)
	}
	if original.Type != "text" || original.Body == "" {
		return SentMessage{}, errors.New("only stored text messages can be forwarded")
	}
	resp, err := client.SendMessage(ctx, to, &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(original.Body),
			ContextInfo: &waE2E.ContextInfo{
				ForwardingScore: proto.Uint32(1),
				IsForwarded:     proto.Bool(true),
			},
		},
	})
	if err != nil {
		return SentMessage{}, fmt.Errorf("send forward: %w", err)
	}
	return SentMessage{
		ID:        string(resp.ID),
		ChatJID:   targetChatJID,
		SenderJID: client.Store.GetJID().String(),
		Timestamp: resp.Timestamp,
	}, nil
}

// RequestChatHistory asks the primary device for messages before oldestMessage in chatJID.
func (s *WhatsmeowSession) RequestChatHistory(ctx context.Context, chatJID string, oldestMessage store.Message, count int) error {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()

	if !client.IsLoggedIn() {
		return errors.New("whatsapp is not connected")
	}
	if count <= 0 || count > 50 {
		count = 50
	}
	if chatJID == "" {
		chatJID = oldestMessage.ChatJID
	}
	chat, err := types.ParseJID(chatJID)
	if err != nil {
		return fmt.Errorf("parse history chat jid: %w", err)
	}
	info := &types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat:     chat,
			IsFromMe: oldestMessage.SenderJID == client.Store.GetJID().String(),
			IsGroup:  chat.Server == types.GroupServer,
		},
		ID:        types.MessageID(oldestMessage.ID),
		Timestamp: oldestMessage.Timestamp,
	}
	if _, err := client.SendPeerMessage(ctx, client.BuildHistorySyncRequest(info, count)); err != nil {
		return fmt.Errorf("request chat history: %w", err)
	}
	slog.Default().Info("requested chat history", "chat", chat.String(), "count", count)
	return nil
}

// RequestFullHistory asks the primary device for a bounded full-history sync.
func (s *WhatsmeowSession) RequestFullHistory(ctx context.Context, days uint32) error {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()

	if !client.IsLoggedIn() {
		return errors.New("whatsapp is not connected")
	}
	if days == 0 {
		days = 3650
	}
	msg := &waE2E.Message{
		ProtocolMessage: &waE2E.ProtocolMessage{
			Type: waE2E.ProtocolMessage_PEER_DATA_OPERATION_REQUEST_MESSAGE.Enum(),
			PeerDataOperationRequestMessage: &waE2E.PeerDataOperationRequestMessage{
				PeerDataOperationRequestType: waE2E.PeerDataOperationRequestType_FULL_HISTORY_SYNC_ON_DEMAND.Enum(),
				FullHistorySyncOnDemandRequest: &waE2E.PeerDataOperationRequestMessage_FullHistorySyncOnDemandRequest{
					RequestMetadata: &waE2E.FullHistorySyncOnDemandRequestMetadata{
						RequestID:       proto.String(uuid.NewString()),
						BusinessProduct: proto.String("wh-cli"),
					},
					HistorySyncConfig: &waCompanionReg.DeviceProps_HistorySyncConfig{
						FullSyncDaysLimit:     proto.Uint32(days),
						FullSyncSizeMbLimit:   proto.Uint32(1024),
						StorageQuotaMb:        proto.Uint32(4096),
						RecentSyncDaysLimit:   proto.Uint32(30),
						SupportInlineContacts: proto.Bool(true),
						OnDemandReady:         proto.Bool(true),
						CompleteOnDemandReady: proto.Bool(true),
					},
					FullHistorySyncOnDemandConfig: &waE2E.FullHistorySyncOnDemandConfig{
						HistoryDurationDays: proto.Uint32(days),
					},
				},
			},
		},
	}
	if _, err := client.SendPeerMessage(ctx, msg); err != nil {
		return fmt.Errorf("request full history: %w", err)
	}
	slog.Default().Info("requested full history", "days", days)
	return nil
}

func (s *WhatsmeowSession) requestInitialFullHistory(ctx context.Context) {
	s.mu.Lock()
	if s.fullSyncRequested {
		s.mu.Unlock()
		return
	}
	s.fullSyncRequested = true
	s.mu.Unlock()

	if err := s.RequestFullHistory(ctx, 3650); err != nil {
		slog.Default().Warn("initial full history request failed", "error", err)
	}
}

func parseOptionalJID(raw string) (types.JID, error) {
	if raw == "" {
		return types.EmptyJID, nil
	}
	return types.ParseJID(raw)
}

func quotedMessage(msg store.Message) *waE2E.Message {
	if msg.Type == "text" {
		return &waE2E.Message{Conversation: proto.String(msg.Body)}
	}
	return &waE2E.Message{Conversation: proto.String("")}
}

func replyContextInfo(msg store.Message, sender types.JID) *waE2E.ContextInfo {
	info := &waE2E.ContextInfo{
		StanzaID:      proto.String(msg.ID),
		QuotedMessage: quotedMessage(msg),
	}
	if !sender.IsEmpty() {
		info.Participant = proto.String(sender.ToNonAD().String())
	}
	return info
}

// SendMedia uploads and sends a local media payload through WhatsApp.
func (s *WhatsmeowSession) SendMedia(ctx context.Context, chatJID string, data []byte, blob store.MediaBlob, kind MediaKind, caption string, filename string) (SentMessage, error) {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()

	if !client.IsLoggedIn() {
		return SentMessage{}, errors.New("whatsapp is not connected")
	}
	to, err := types.ParseJID(chatJID)
	if err != nil {
		return SentMessage{}, fmt.Errorf("parse chat jid: %w", err)
	}

	uploadType := whatsmeow.MediaDocument
	if kind == MediaKindImage {
		uploadType = whatsmeow.MediaImage
	}
	if kind == MediaKindAudio {
		uploadType = whatsmeow.MediaAudio
	}
	uploaded, err := client.Upload(ctx, data, uploadType)
	if err != nil {
		return SentMessage{}, fmt.Errorf("upload media: %w", err)
	}

	msg := mediaMessage(uploaded, blob, kind, caption, filename)
	resp, err := client.SendMessage(ctx, to, msg)
	if err != nil {
		return SentMessage{}, fmt.Errorf("send media: %w", err)
	}
	return SentMessage{
		ID:        string(resp.ID),
		ChatJID:   chatJID,
		SenderJID: client.Store.GetJID().String(),
		Timestamp: resp.Timestamp,
	}, nil
}

func mediaMessage(uploaded whatsmeow.UploadResponse, blob store.MediaBlob, kind MediaKind, caption string, filename string) *waE2E.Message {
	mimeType := blob.MIME
	switch kind {
	case MediaKindImage:
		return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
			Caption:       proto.String(caption),
			Mimetype:      proto.String(mimeType),
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uploaded.FileLength),
		}}
	case MediaKindAudio:
		return &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
			Mimetype:      proto.String(mimeType),
			PTT:           proto.Bool(false),
			Seconds:       proto.Uint32(1),
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uploaded.FileLength),
		}}
	default:
		if filename == "" {
			filename = filepath.Base(blob.LocalPath)
		}
		title := strings.TrimSuffix(filename, filepath.Ext(filename))
		return &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
			Caption:       proto.String(caption),
			Mimetype:      proto.String(mimeType),
			Title:         proto.String(title),
			FileName:      proto.String(filename),
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uploaded.FileLength),
		}}
	}
}

// RefreshGroups fetches the current joined group list and stores it through the group sink.
func (s *WhatsmeowSession) RefreshGroups(ctx context.Context) ([]store.Group, error) {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()

	if !client.IsLoggedIn() {
		return nil, errors.New("whatsapp is not connected")
	}
	infos, err := client.GetJoinedGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("get joined groups: %w", err)
	}
	groups := make([]store.Group, 0, len(infos))
	var allParticipants []store.GroupParticipant
	for _, info := range infos {
		group, participants := convertGroupInfo(info)
		groups = append(groups, group)
		allParticipants = append(allParticipants, participants...)
		if group.JID != "" && s.groups != nil {
			if err := s.groups.Save(ctx, group, participants); err != nil {
				return nil, err
			}
		}
	}
	s.syncParticipantPushNames(ctx, allParticipants)
	return groups, nil
}

// RefreshGroup fetches one group's metadata and participants.
func (s *WhatsmeowSession) RefreshGroup(ctx context.Context, groupJID string) (store.Group, []store.GroupParticipant, error) {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()

	if !client.IsLoggedIn() {
		return store.Group{}, nil, errors.New("whatsapp is not connected")
	}
	jid, err := types.ParseJID(groupJID)
	if err != nil {
		return store.Group{}, nil, fmt.Errorf("parse group jid: %w", err)
	}
	info, err := client.GetGroupInfo(ctx, jid)
	if err != nil {
		return store.Group{}, nil, fmt.Errorf("get group info: %w", err)
	}
	group, participants := convertGroupInfo(info)
	if s.groups != nil {
		if err := s.groups.Save(ctx, group, participants); err != nil {
			return store.Group{}, nil, err
		}
	}
	s.syncParticipantPushNames(ctx, participants)
	return group, participants, nil
}

// syncParticipantPushNames resolves push names for group participants by reading
// whatsmeow's internal contact store, which tracks names from all received messages.
// For @lid JIDs we use the associated phone JID as the lookup key.
func (s *WhatsmeowSession) syncParticipantPushNames(ctx context.Context, participants []store.GroupParticipant) {
	if s.contacts == nil || len(participants) == 0 {
		return
	}
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()

	names := make(map[string]string, len(participants))
	for _, p := range participants {
		// Prefer phone JID (@s.whatsapp.net) — whatsmeow's contact store is keyed by phone JID.
		lookupJID := p.PhoneJID
		if lookupJID == "" || strings.HasSuffix(lookupJID, "@lid") {
			lookupJID = p.ContactJID
		}
		if lookupJID == "" {
			continue
		}
		parsed, err := types.ParseJID(lookupJID)
		if err != nil {
			continue
		}
		info, err := client.Store.Contacts.GetContact(ctx, parsed)
		if err != nil || !info.Found {
			continue
		}
		name := info.PushName
		if name == "" {
			name = info.FullName
		}
		if name == "" {
			name = info.FirstName
		}
		if name == "" {
			continue
		}
		// Store under both the LID and the phone JID so both resolve.
		if p.ContactJID != "" {
			names[p.ContactJID] = name
		}
		if p.PhoneJID != "" && p.PhoneJID != p.ContactJID {
			names[p.PhoneJID] = name
		}
	}
	if err := s.contacts.BulkUpsertPushNames(ctx, names); err != nil {
		slog.Default().Warn("sync participant push names failed", "error", err)
	}
}

// SyncAllContacts bulk-reads all contacts from whatsmeow's internal store and
// persists their push names. Call this once after connecting.
func (s *WhatsmeowSession) SyncAllContacts(ctx context.Context) error {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()

	if !client.IsLoggedIn() {
		return nil
	}
	allContacts, err := client.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return fmt.Errorf("get all contacts from whatsmeow: %w", err)
	}
	pushNames := make(map[string]string, len(allContacts))
	agendaNames := make(map[string]string, len(allContacts))
	mappings := make([]store.JIDMapping, 0, len(allContacts))
	for jid, info := range allContacts {
		jidString := jid.String()
		var mappedLID types.JID
		if info.PushName != "" {
			pushNames[jidString] = info.PushName
		}
		agendaName := firstNonEmpty(info.FullName, info.FirstName)
		if agendaName != "" {
			agendaNames[jidString] = agendaName
		}
		if jid.Server == types.DefaultUserServer && client.Store.LIDs != nil {
			lid, err := client.Store.LIDs.GetLIDForPN(ctx, jid)
			if err == nil && !lid.IsEmpty() {
				mappedLID = lid
				mappings = append(mappings, store.JIDMapping{LIDJID: lid.String(), PhoneJID: jid.String()})
			}
		}
		if !mappedLID.IsEmpty() {
			if info.PushName != "" {
				pushNames[mappedLID.String()] = info.PushName
			}
			if agendaName != "" {
				agendaNames[mappedLID.String()] = agendaName
			}
		}
	}
	if s.contacts == nil {
		return nil
	}
	if err := s.contacts.BulkUpsertPushNames(ctx, pushNames); err != nil {
		return err
	}
	if err := s.contacts.BulkUpsertAgendaNames(ctx, agendaNames); err != nil {
		return err
	}
	dbMappings, err := s.readAllJIDMappings(ctx)
	if err != nil {
		slog.Default().Warn("read jid mappings failed", "error", err)
	} else {
		mappings = append(mappings, dbMappings...)
	}
	return s.contacts.BulkUpsertJIDMappings(ctx, mappings)
}

func (s *WhatsmeowSession) readAllJIDMappings(ctx context.Context) ([]store.JIDMapping, error) {
	if s.dbPath == "" {
		return nil, nil
	}
	db, err := sql.Open("sqlite3", "file:"+s.dbPath+"?_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open whatsmeow db for jid mappings: %w", err)
	}
	defer func() {
		_ = db.Close()
	}()
	rows, err := db.QueryContext(ctx, `SELECT lid, pn FROM whatsmeow_lid_map`)
	if err != nil {
		return nil, fmt.Errorf("query jid mappings: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()
	mappings := make([]store.JIDMapping, 0)
	for rows.Next() {
		var lid, pn string
		if err := rows.Scan(&lid, &pn); err != nil {
			return nil, fmt.Errorf("scan jid mapping: %w", err)
		}
		mappings = append(mappings, store.JIDMapping{
			LIDJID:   lid + "@lid",
			PhoneJID: pn + "@s.whatsapp.net",
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate jid mappings: %w", err)
	}
	return mappings, nil
}

func convertGroupInfo(info *types.GroupInfo) (store.Group, []store.GroupParticipant) {
	if info == nil {
		return store.Group{}, nil
	}
	group := store.Group{
		JID:       info.JID.String(),
		Name:      info.Name,
		Topic:     info.Topic,
		OwnerJID:  info.OwnerJID.String(),
		CreatedAt: info.GroupCreated,
	}
	participants := make([]store.GroupParticipant, 0, len(info.Participants))
	for _, part := range info.Participants {
		role := "member"
		if part.IsSuperAdmin {
			role = "superadmin"
		} else if part.IsAdmin {
			role = "admin"
		}
		participant := store.GroupParticipant{
			GroupJID:    group.JID,
			ContactJID:  part.JID.String(),
			DisplayName: part.DisplayName,
			Role:        role,
			PhoneJID:    part.PhoneNumber.String(),
			LIDJID:      part.LID.String(),
		}
		participants = append(participants, participant)
	}
	return group, participants
}

// PairCode starts phone-code linking and returns the code to enter on the phone.
func (s *WhatsmeowSession) PairCode(ctx context.Context, phone string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.client.IsLoggedIn() {
		return "", nil
	}
	if s.client.IsConnected() {
		s.client.Disconnect()
	}

	qrChan, err := s.client.GetQRChannel(ctx)
	if err != nil {
		return "", fmt.Errorf("get qr channel for pair code: %w", err)
	}
	if err := s.client.Connect(); err != nil {
		return "", fmt.Errorf("connect for pair code: %w", err)
	}
	if err := waitForFirstQR(ctx, qrChan); err != nil {
		return "", err
	}

	code, err := s.client.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, "Chrome (Windows)")
	if err != nil {
		return "", fmt.Errorf("pair phone: %w", err)
	}
	return code, nil
}

func waitForFirstQR(ctx context.Context, qrChan <-chan whatsmeow.QRChannelItem) error {
	timeout := time.NewTimer(15 * time.Second)
	defer timeout.Stop()

	for {
		select {
		case item, ok := <-qrChan:
			if !ok {
				return errors.New("qr channel closed before pair code")
			}
			if item.Event == "code" {
				return nil
			}
			if item.Error != nil {
				return fmt.Errorf("qr channel error before pair code: %w", item.Error)
			}
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C:
			return errors.New("timed out waiting for qr readiness")
		}
	}
}

// NewWhatsmeowSession opens the whatsmeow device store and constructs a client.
func NewWhatsmeowSession(ctx context.Context, dbPath string, sink MessageSink, groupSink GroupSink, contactSink ContactSink, deviceSink DeviceSink, mediaRepo *store.MediaRepo, mediaDir string) (*WhatsmeowSession, error) {
	configureClientIdentity()

	container, err := sqlstore.New(ctx, "sqlite3", "file:"+dbPath+"?_foreign_keys=on", nil)
	if err != nil {
		return nil, fmt.Errorf("open whatsmeow store: %w", err)
	}
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		return nil, fmt.Errorf("get whatsmeow device: %w", err)
	}
	client := whatsmeow.NewClient(device, nil)
	session := &WhatsmeowSession{client: client, dbPath: dbPath, messages: sink, groups: groupSink, contacts: contactSink, devices: deviceSink, media: mediaRepo, mediaDir: mediaDir}
	client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.PairSuccess:
			slog.Default().Info("whatsapp pairing succeeded", "platform", v.Platform)
			if session.devices != nil {
				session.devices.OnDeviceLinked(context.Background(), v.ID.String(), v.Platform, time.Now())
			}
		case *events.PairError:
			slog.Default().Warn("whatsapp pairing failed", "platform", v.Platform, "error", v.Error)
		case *events.Connected:
			slog.Default().Info("whatsapp connected")
			if sink, ok := session.messages.(senderBackfillSink); ok {
				if err := sink.BackfillMissingSenderJID(context.Background(), session.SelfJID()); err != nil {
					slog.Default().Warn("backfill sender jid failed", "error", err)
				}
			}
			go func() {
				if err := session.SyncAllContacts(context.Background()); err != nil {
					slog.Default().Warn("connected contact sync failed", "error", err)
				}
			}()
			go func() {
				if _, err := session.RefreshGroups(context.Background()); err != nil {
					slog.Default().Warn("connected group sync failed", "error", err)
				}
			}()
			go session.requestInitialFullHistory(context.Background())
		case *events.Disconnected:
			slog.Default().Warn("whatsapp disconnected")
		case *events.Message:
			session.handleMessage(context.Background(), v)
		case *events.HistorySync:
			session.handleHistorySync(context.Background(), v)
		case *events.Receipt:
			session.handleReceipt(context.Background(), v)
		case *events.Contact:
			session.handleContact(context.Background(), v)
		case *events.MediaRetry:
			if v.Error != nil {
				slog.Default().Warn("whatsapp media retry failed", "message_id", v.MessageID, "error", v.Error)
			}
		}
	})
	return session, nil
}

// SelfJID returns the linked account JID when the whatsmeow store knows it.
func (s *WhatsmeowSession) SelfJID() string {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil || client.Store == nil {
		return ""
	}
	jid := client.Store.GetJID()
	if jid.IsEmpty() {
		return ""
	}
	return jid.String()
}

func configureClientIdentity() {
	waStore.SetOSInfo("Windows", [3]uint32{10, 0, 0})
	waStore.DeviceProps.Os = proto.String("Windows")
	waStore.DeviceProps.PlatformType = waCompanionReg.DeviceProps_CHROME.Enum()
	waStore.BaseClientPayload.UserAgent.Platform = waWa6.ClientPayload_UserAgent_WEB.Enum()
	waStore.BaseClientPayload.UserAgent.Manufacturer = proto.String("Google")
	waStore.BaseClientPayload.UserAgent.Device = proto.String("Chrome")
	waStore.BaseClientPayload.WebInfo.WebSubPlatform = waWa6.ClientPayload_WebInfo_WEB_BROWSER.Enum()
	waStore.BaseClientPayload.WebInfo.Browser = proto.String("Chrome")
	waStore.BaseClientPayload.WebInfo.BrowserVersion = proto.String("124.0.0.0")
}

func (s *WhatsmeowSession) handleMessage(ctx context.Context, evt *events.Message) {
	s.handleMessageWithDisplayName(ctx, evt, "")
}

func (s *WhatsmeowSession) handleMessageWithDisplayName(ctx context.Context, evt *events.Message, fallbackDisplayName string) {
	if s.messages == nil || evt == nil || evt.Message == nil {
		return
	}
	msg, displayName, ok := s.messageFromEvent(ctx, evt, fallbackDisplayName)
	if !ok {
		return
	}
	if err := s.messages.SaveText(ctx, msg, displayName); err != nil {
		slog.Default().Warn("store incoming message failed", "error", err)
	}
}

func (s *WhatsmeowSession) messageFromEvent(ctx context.Context, evt *events.Message, fallbackDisplayName string) (store.Message, string, bool) {
	msgType, body, downloadable, mimeType, filename := messageContent(evt.Message)
	if downloadable != nil && s.media != nil && s.mediaDir != "" {
		s.mu.Lock()
		client := s.client
		s.mu.Unlock()
		data, err := client.Download(ctx, downloadable)
		if err != nil {
			slog.Default().Warn("download incoming media failed", "message_id", evt.Info.ID, "error", err)
		} else {
			blob, err := media.SaveDownloaded(ctx, s.media, s.mediaDir, string(evt.Info.ID), data, mimeType, filename)
			if err != nil {
				slog.Default().Warn("store incoming media failed", "message_id", evt.Info.ID, "error", err)
			} else {
				return s.messageEnvelopeFromEvent(evt, msgType, body, blob.LocalPath), fallbackDisplayNameForEvent(evt, fallbackDisplayName), true
			}
		}
	}
	if body == "" {
		return store.Message{}, "", false
	}
	return s.messageEnvelopeFromEvent(evt, msgType, body, ""), fallbackDisplayNameForEvent(evt, fallbackDisplayName), true
}

func messageContent(msg *waE2E.Message) (string, string, whatsmeow.DownloadableMessage, string, string) {
	body := msg.GetConversation()
	if body == "" && msg.GetExtendedTextMessage() != nil {
		body = msg.GetExtendedTextMessage().GetText()
	}
	if image := msg.GetImageMessage(); image != nil {
		return string(MediaKindImage), image.GetCaption(), image, image.GetMimetype(), ""
	}
	if audio := msg.GetAudioMessage(); audio != nil {
		return string(MediaKindAudio), "", audio, audio.GetMimetype(), ""
	}
	if doc := msg.GetDocumentMessage(); doc != nil {
		return string(MediaKindDocument), doc.GetCaption(), doc, doc.GetMimetype(), doc.GetFileName()
	}
	return "text", body, nil, "", ""
}

func fallbackDisplayNameForEvent(evt *events.Message, fallbackDisplayName string) string {
	displayName := evt.Info.PushName
	if displayName == "" {
		displayName = fallbackDisplayName
	}
	if displayName == "" {
		displayName = evt.Info.Chat.String()
	}
	return displayName
}

func (s *WhatsmeowSession) messageEnvelopeFromEvent(evt *events.Message, msgType string, body string, mediaPath string) store.Message {
	msg := store.Message{
		ID:        string(evt.Info.ID),
		ChatJID:   evt.Info.Chat.String(),
		SenderJID: evt.Info.Sender.String(),
		Type:      msgType,
		Body:      body,
		MediaPath: mediaPath,
		Status:    "received",
		Timestamp: evt.Info.Timestamp,
	}
	if evt.Info.IsFromMe {
		msg.Status = "sent"
	}
	return msg
}

func (s *WhatsmeowSession) handleHistorySync(ctx context.Context, evt *events.HistorySync) {
	if evt == nil || evt.Data == nil {
		return
	}
	slog.Default().Info(
		"received history sync",
		"type", evt.Data.GetSyncType().String(),
		"conversations", len(evt.Data.GetConversations()),
		"push_names", len(evt.Data.GetPushnames()),
		"inline_contacts", len(evt.Data.GetInlineContacts()),
		"progress", evt.Data.GetProgress(),
	)
	s.syncHistoryPushNames(ctx, evt)

	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	for _, conv := range evt.Data.GetConversations() {
		chat, err := types.ParseJID(conv.GetID())
		if err != nil {
			slog.Default().Warn("parse history chat jid failed", "error", err)
			continue
		}
		displayName := firstNonEmpty(conv.GetDisplayName(), conv.GetName())
		for _, historyMsg := range conv.GetMessages() {
			msgEvt, err := client.ParseWebMessage(chat, historyMsg.GetMessage())
			if err != nil {
				slog.Default().Warn("parse history message failed", "error", err)
				continue
			}
			s.handleHistoryMessageWithDisplayName(ctx, msgEvt, displayName)
		}
	}
}

func (s *WhatsmeowSession) handleHistoryMessageWithDisplayName(ctx context.Context, evt *events.Message, fallbackDisplayName string) {
	msg, displayName, ok := s.messageFromEvent(ctx, evt, fallbackDisplayName)
	if !ok {
		return
	}
	if sink, ok := s.messages.(historicalMessageSink); ok {
		if err := sink.SaveHistoricalText(ctx, msg, displayName); err != nil {
			slog.Default().Warn("store historical message failed", "error", err)
		}
		return
	}
	if err := s.messages.SaveText(ctx, msg, displayName); err != nil {
		slog.Default().Warn("store historical message failed", "error", err)
	}
}

func (s *WhatsmeowSession) syncHistoryPushNames(ctx context.Context, evt *events.HistorySync) {
	if s.contacts == nil {
		return
	}
	pushNames := make(map[string]string)
	for _, name := range evt.Data.GetPushnames() {
		if name.GetID() != "" && name.GetPushname() != "" {
			pushNames[name.GetID()] = name.GetPushname()
		}
	}
	if err := s.contacts.BulkUpsertPushNames(ctx, pushNames); err != nil {
		slog.Default().Warn("store history push names failed", "error", err)
	}
	agendaNames := make(map[string]string)
	mappings := make([]store.JIDMapping, 0, len(evt.Data.GetPhoneNumberToLidMappings()))
	for _, mapping := range evt.Data.GetPhoneNumberToLidMappings() {
		if mapping.GetLidJID() != "" && mapping.GetPnJID() != "" {
			mappings = append(mappings, store.JIDMapping{LIDJID: mapping.GetLidJID(), PhoneJID: mapping.GetPnJID()})
		}
	}
	for _, contact := range evt.Data.GetInlineContacts() {
		name := firstNonEmpty(contact.GetFullName(), contact.GetFirstName(), contact.GetUsername())
		if name == "" {
			continue
		}
		if contact.GetLidJID() != "" {
			agendaNames[contact.GetLidJID()] = name
		}
		if contact.GetPnJID() != "" {
			agendaNames[contact.GetPnJID()] = name
		}
	}
	if err := s.contacts.BulkUpsertAgendaNames(ctx, agendaNames); err != nil {
		slog.Default().Warn("store history agenda names failed", "error", err)
	}
	if err := s.contacts.BulkUpsertJIDMappings(ctx, mappings); err != nil {
		slog.Default().Warn("store history jid mappings failed", "error", err)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (s *WhatsmeowSession) handleReceipt(ctx context.Context, evt *events.Receipt) {
	if s.messages == nil || evt == nil {
		return
	}
	status := receiptStatus(evt.Type)
	if status == "" {
		return
	}
	for _, id := range evt.MessageIDs {
		if err := s.messages.UpdateStatus(ctx, string(id), status); err != nil {
			slog.Default().Warn("store receipt status failed", "message_id", id, "status", status, "error", err)
		}
	}
}

func receiptStatus(receiptType types.ReceiptType) string {
	switch receiptType {
	case types.ReceiptTypeDelivered:
		return "delivered"
	case types.ReceiptTypeRead:
		return "read"
	case types.ReceiptTypePlayed:
		return "played"
	case types.ReceiptTypeRetry:
		return "retry"
	case types.ReceiptTypeServerError:
		return "server_error"
	default:
		return ""
	}
}

// GetLinkedDevices returns all companion devices linked to the current account.
// Uses GetUserDevices to fetch all multi-device companions for our own JID.
func (s *WhatsmeowSession) GetLinkedDevices(ctx context.Context) ([]LinkedDevice, error) {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if !client.IsLoggedIn() {
		return nil, errors.New("whatsapp is not connected")
	}
	if client.Store.ID == nil {
		return nil, errors.New("device not initialised")
	}
	jids, err := client.GetUserDevices(ctx, []types.JID{client.Store.ID.ToNonAD()})
	if err != nil {
		return nil, fmt.Errorf("get linked devices: %w", err)
	}
	own := client.Store.ID.String()
	out := make([]LinkedDevice, 0, len(jids))
	for _, jid := range jids {
		if jid.String() == own {
			continue
		}
		out = append(out, LinkedDevice{JID: jid.String(), Platform: jid.User})
	}
	return out, nil
}

// RevokeDevice is not directly supported by whatsmeow's public API.
// Use the WhatsApp mobile app to revoke linked devices.
func (s *WhatsmeowSession) RevokeDevice(_ context.Context, _ string) error {
	return errors.New("device revocation is not supported via API — use the WhatsApp mobile app > Linked Devices")
}

func (s *WhatsmeowSession) handleContact(ctx context.Context, evt *events.Contact) {
	if s.contacts == nil || evt.Action == nil {
		return
	}
	name := evt.Action.GetFullName()
	if name == "" {
		name = evt.Action.GetFirstName()
	}
	if name == "" {
		return
	}
	if err := s.contacts.UpsertAgendaName(ctx, evt.JID.String(), name); err != nil {
		slog.Default().Warn("store contact event name failed", "error", err)
	}
}

// ConnectExisting connects when a device is already paired.
func (s *WhatsmeowSession) ConnectExisting() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.client.Store.ID == nil {
		return nil
	}
	if s.client.IsConnected() {
		return nil
	}
	if err := s.client.Connect(); err != nil {
		return fmt.Errorf("connect whatsmeow: %w", err)
	}
	return nil
}

// Status returns the current WhatsApp session state.
func (s *WhatsmeowSession) Status(_ context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.client.IsLoggedIn() {
		return StatusConnected, nil
	}
	if s.client.Store.ID == nil {
		return StatusLoggedOut, nil
	}
	return StatusQRPending, nil
}

// QR starts a WhatsApp QR pairing flow and streams QR codes.
func (s *WhatsmeowSession) QR(ctx context.Context) (<-chan string, error) {
	s.mu.Lock()
	if s.client.IsLoggedIn() {
		s.mu.Unlock()
		ch := make(chan string)
		close(ch)
		return ch, nil
	}
	if err := s.ensureQRLocked(ctx); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if s.latestQR != "" {
		code := s.latestQR
		s.mu.Unlock()
		ch := make(chan string, 1)
		ch <- code
		close(ch)
		return ch, nil
	}
	s.mu.Unlock()

	out := make(chan string, 1)
	go func() {
		defer close(out)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		timeout := time.NewTimer(15 * time.Second)
		defer timeout.Stop()
		for {
			select {
			case <-ticker.C:
				if code, ok := s.LatestQR(); ok {
					out <- code
					return
				}
			case <-ctx.Done():
				return
			case <-timeout.C:
				return
			}
		}
	}()
	return out, nil
}

// LatestQR returns the freshest QR code received from WhatsApp.
func (s *WhatsmeowSession) LatestQR() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latestQR == "" {
		return "", false
	}
	return s.latestQR, true
}

func (s *WhatsmeowSession) ensureQRLocked(_ context.Context) error {
	if s.qrRunning {
		return nil
	}
	if s.client.Store.ID != nil {
		return nil
	}
	if s.client.IsConnected() {
		s.client.Disconnect()
	}
	qrCtx, cancel := context.WithCancel(context.Background())
	qrChan, err := s.client.GetQRChannel(qrCtx)
	if err != nil {
		cancel()
		return fmt.Errorf("get qr channel: %w", err)
	}
	if err := s.client.Connect(); err != nil {
		cancel()
		return fmt.Errorf("connect for qr: %w", err)
	}
	s.qrRunning = true
	s.qrCancel = cancel
	go s.consumeQRChannel(qrChan)
	return nil
}

func (s *WhatsmeowSession) consumeQRChannel(qrChan <-chan whatsmeow.QRChannelItem) {
	for item := range qrChan {
		if item.Event == "code" {
			s.mu.Lock()
			s.latestQR = item.Code
			s.qrUpdated = time.Now()
			s.mu.Unlock()
			continue
		}
		if item.Error != nil && !errors.Is(item.Error, context.Canceled) {
			slog.Default().Warn("whatsapp qr channel error", "event", item.Event, "error", item.Error)
		} else {
			slog.Default().Info("whatsapp qr channel event", "event", item.Event)
		}
	}
	s.mu.Lock()
	s.qrRunning = false
	s.qrCancel = nil
	s.mu.Unlock()
}

// Logout logs out and disconnects the WhatsApp session.
func (s *WhatsmeowSession) Logout(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.client.IsConnected() {
		if err := s.client.Logout(ctx); err != nil {
			return fmt.Errorf("logout whatsmeow: %w", err)
		}
		return nil
	}
	if s.qrCancel != nil {
		s.qrCancel()
		s.qrCancel = nil
	}
	s.client.Disconnect()
	return nil
}
