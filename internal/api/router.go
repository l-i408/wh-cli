// Package api exposes the local daemon HTTP API.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/l-i408/wh-cli/internal/auth"
	"github.com/l-i408/wh-cli/internal/keyring"
	"github.com/l-i408/wh-cli/internal/media"
	"github.com/l-i408/wh-cli/internal/store"
	"github.com/l-i408/wh-cli/internal/wa"
	"github.com/l-i408/wh-cli/internal/ws"
	"rsc.io/qr"
)

// AuthService describes authentication operations used by handlers.
type AuthService interface {
	Login(ctx context.Context, passphraseHash []byte, clientLabel string) (auth.TokenPair, error)
	Refresh(ctx context.Context, refreshToken string) (auth.TokenPair, error)
	VerifyAccess(ctx context.Context, accessToken string) (*auth.Claims, error)
	Logout(ctx context.Context, clientLabel string) error
	RevokeAll(ctx context.Context) error
	RotateSecret(ctx context.Context, newSecret []byte) error
}

// DeviceService lists and revokes linked WhatsApp devices.
type DeviceService interface {
	GetLinkedDevices(ctx context.Context) ([]wa.LinkedDevice, error)
	RevokeDevice(ctx context.Context, jidStr string) error
}

// AdminService describes privileged daemon operations.
type AdminService interface {
	ExportDB(ctx context.Context) ([]byte, error)
	WipeAll(ctx context.Context) error
}

// AuditLogger records security-relevant events.
type AuditLogger interface {
	Log(ctx context.Context, actor, action, target, result, severity string) error
}

// SessionService describes WhatsApp session state used by handlers.
type SessionService interface {
	Status(ctx context.Context) (string, error)
	QR(ctx context.Context) (<-chan string, error)
	PairCode(ctx context.Context, phone string) (string, error)
	Logout(ctx context.Context) error
}

// MessageSender sends outbound WhatsApp messages.
type MessageSender interface {
	SendText(ctx context.Context, chatJID string, body string) (wa.SentMessage, error)
	SendMedia(ctx context.Context, chatJID string, data []byte, blob store.MediaBlob, kind wa.MediaKind, caption string, filename string) (wa.SentMessage, error)
	SendReaction(ctx context.Context, target store.Message, emoji string) (wa.SentMessage, error)
	SendReply(ctx context.Context, target store.Message, body string) (wa.SentMessage, error)
	SendForward(ctx context.Context, targetChatJID string, original store.Message) (wa.SentMessage, error)
}

// GroupProvider refreshes WhatsApp group metadata.
type GroupProvider interface {
	RefreshGroups(ctx context.Context) ([]store.Group, error)
	RefreshGroup(ctx context.Context, groupJID string) (store.Group, []store.GroupParticipant, error)
}

// HistoryProvider requests older messages from the primary WhatsApp device.
type HistoryProvider interface {
	RequestChatHistory(ctx context.Context, chatJID string, oldestMessage store.Message, count int) error
	RequestFullHistory(ctx context.Context, days uint32) error
}

// Dependencies groups the services required by HTTP handlers.
type Dependencies struct {
	Auth     AuthService
	Session  SessionService
	Sender   MessageSender
	GroupsWA GroupProvider
	History  HistoryProvider
	Devices  DeviceService
	Admin    AdminService
	Audit    AuditLogger
	Messages *store.MessageRepo
	Contacts *store.ContactRepo
	Groups   *store.GroupRepo
	Media    *store.MediaRepo
	MediaDir string
	Hub      *ws.Hub
	Unlock   *keyring.UnlockCache
}

// NewRouter builds the daemon HTTP router.
func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", handleHealthz)

	r.Post("/auth/login", handleAuthLogin(deps))
	r.Post("/auth/refresh", handleAuthRefresh(deps))

	r.Get("/session/status", handleSessionStatus(deps))
	r.Get("/session/qr", handleSessionQR(deps))
	r.Get("/session/qr.png", handleSessionQRPNG(deps))
	r.Post("/session/pair-code", handleSessionPairCode(deps))
	if deps.Hub != nil {
		r.Get("/ws", deps.Hub.ServeHTTP)
	}

	r.Group(func(protected chi.Router) {
		protected.Use(jwtMiddleware(deps.Auth))
		protected.Post("/auth/logout", handleAuthLogout(deps))
		protected.Post("/session/logout", handleWhatsAppLogout(deps))
		protected.Post("/messages", handlePostMessage(deps))
		protected.Post("/messages/{id}/react", handlePostMessageReaction(deps))
		protected.Post("/messages/{id}/reply", handlePostMessageReply(deps))
		protected.Post("/messages/{id}/forward", handlePostMessageForward(deps))
		protected.Get("/chats", handleGetChats(deps))
		protected.Get("/chats/{jid}/messages", handleGetMessages(deps))
		protected.Get("/contacts", handleGetContacts(deps))
		protected.Patch("/contacts/{jid}", handlePatchContact(deps))
		protected.Get("/groups", handleGetGroups(deps))
		protected.Get("/groups/{jid}", handleGetGroup(deps))
		protected.Get("/groups/{jid}/participants", handleGetGroupParticipants(deps))
		protected.Get("/session/devices", handleGetDevices(deps))
		protected.Delete("/session/devices/{jid}", handleRevokeDevice(deps))
		protected.Post("/admin/rotate-jwt", handleAdminRotateJWT(deps))
		protected.Post("/admin/wipe", handleAdminWipe(deps))
		protected.Get("/admin/export", handleAdminExport(deps))
		protected.Post("/admin/import", handleAdminImport(deps))
	})

	return r
}

func handleGetContacts(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Contacts == nil {
			writeError(w, http.StatusServiceUnavailable, "contacts_unavailable")
			return
		}
		contacts, err := deps.Contacts.List(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_contacts_failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": contacts})
	}
}

func handlePatchContact(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Contacts == nil {
			writeError(w, http.StatusServiceUnavailable, "contacts_unavailable")
			return
		}
		var req struct {
			Alias string `json:"alias"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		contact, err := deps.Contacts.SetAlias(r.Context(), chi.URLParam(r, "jid"), req.Alias)
		if errors.Is(err, store.ErrInvalidContact) {
			writeError(w, http.StatusBadRequest, "invalid_contact")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "set_alias_failed")
			return
		}
		writeJSON(w, http.StatusOK, contact)
	}
}

func handleGetGroups(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Groups == nil {
			writeError(w, http.StatusServiceUnavailable, "groups_unavailable")
			return
		}
		if deps.GroupsWA != nil {
			if _, err := deps.GroupsWA.RefreshGroups(r.Context()); err != nil {
				writeError(w, http.StatusInternalServerError, "refresh_groups_failed")
				return
			}
		}
		groups, err := deps.Groups.List(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_groups_failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": groups})
	}
}

func handleGetGroup(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Groups == nil {
			writeError(w, http.StatusServiceUnavailable, "groups_unavailable")
			return
		}
		groupJID := chi.URLParam(r, "jid")
		if deps.GroupsWA != nil {
			if _, _, err := deps.GroupsWA.RefreshGroup(r.Context(), groupJID); err != nil {
				writeError(w, http.StatusInternalServerError, "refresh_group_failed")
				return
			}
		}
		group, err := deps.Groups.Get(r.Context(), groupJID)
		if err != nil {
			writeError(w, http.StatusNotFound, "group_not_found")
			return
		}
		writeJSON(w, http.StatusOK, group)
	}
}

func handleGetGroupParticipants(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Groups == nil {
			writeError(w, http.StatusServiceUnavailable, "groups_unavailable")
			return
		}
		groupJID := chi.URLParam(r, "jid")
		if deps.GroupsWA != nil {
			if _, _, err := deps.GroupsWA.RefreshGroup(r.Context(), groupJID); err != nil {
				writeError(w, http.StatusInternalServerError, "refresh_group_failed")
				return
			}
		}
		participants, err := deps.Groups.ListParticipants(r.Context(), groupJID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_group_participants_failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": participants})
	}
}

func handleGetChats(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Messages == nil {
			writeError(w, http.StatusServiceUnavailable, "messages_unavailable")
			return
		}
		if deps.GroupsWA != nil {
			_, _ = deps.GroupsWA.RefreshGroups(r.Context())
		}
		chats, err := deps.Messages.ListChats(r.Context(), parseLimit(r.URL.Query().Get("limit")))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_chats_failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": chats})
	}
}

func handleGetMessages(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Messages == nil {
			writeError(w, http.StatusServiceUnavailable, "messages_unavailable")
			return
		}
		limit := parseMessageLimit(r.URL.Query().Get("limit"))
		chatJID := chi.URLParam(r, "jid")
		if deps.History != nil {
			requestMoreHistory(r.Context(), deps, chatJID, limit)
		}
		messages, err := deps.Messages.ListMessages(r.Context(), chatJID, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_messages_failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": apiMessagesFromStore(messages)})
	}
}

func requestMoreHistory(ctx context.Context, deps Dependencies, chatJID string, limit int) {
	oldest, err := deps.Messages.GetOldestMessage(ctx, chatJID)
	if err != nil {
		if errors.Is(err, store.ErrMessageNotFound) {
			_ = deps.History.RequestFullHistory(ctx, 3650)
			waitForHistory(ctx, 5*time.Second)
		}
		return
	}
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	const maxHistoryPages = 5
	for range maxHistoryPages {
		before := oldest.ID
		if err := deps.History.RequestChatHistory(ctx, chatJID, oldest, limit); err != nil {
			return
		}
		waitForHistory(ctx, 1500*time.Millisecond)
		nextOldest, err := deps.Messages.GetOldestMessage(ctx, chatJID)
		if err != nil || nextOldest.ID == before {
			return
		}
		oldest = nextOldest
	}
}

func waitForHistory(ctx context.Context, timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func parseLimit(raw string) int {
	var limit int
	_, _ = fmt.Sscanf(raw, "%d", &limit)
	return limit
}

func parseMessageLimit(raw string) int {
	limit := parseLimit(raw)
	if limit <= 0 || limit == 50 {
		return 5000
	}
	if limit > 5000 {
		return 5000
	}
	return limit
}

type apiMessage struct {
	ID            string  `json:"id"`
	ChatJID       string  `json:"chat_jid"`
	SenderJID     string  `json:"sender_jid"`
	Type          string  `json:"type"`
	Body          *string `json:"body"`
	MediaPath     *string `json:"media_path"`
	ReplyToID     *string `json:"reply_to_id"`
	ReactionsJSON *string `json:"reactions_json"`
	Status        *string `json:"status"`
	Timestamp     string  `json:"timestamp"`
}

func apiMessagesFromStore(messages []store.Message) []apiMessage {
	items := make([]apiMessage, 0, len(messages))
	for _, msg := range messages {
		items = append(items, apiMessageFromStore(msg))
	}
	return items
}

func apiMessageFromStore(msg store.Message) apiMessage {
	return apiMessage{
		ID:            msg.ID,
		ChatJID:       msg.ChatJID,
		SenderJID:     msg.SenderJID,
		Type:          msg.Type,
		Body:          stringPtrOrNil(msg.Body),
		MediaPath:     stringPtrOrNil(msg.MediaPath),
		ReplyToID:     stringPtrOrNil(msg.ReplyToID),
		ReactionsJSON: stringPtrOrNil(msg.ReactionsJSON),
		Status:        stringPtrOrNil(apiMessageStatus(msg.Status)),
		Timestamp:     msg.Timestamp.Format(time.RFC3339),
	}
}

func apiMessageStatus(status string) string {
	switch status {
	case "server_ack", "sent":
		return "sent"
	case "delivered":
		return "delivered"
	case "read", "played":
		return "read"
	case "retry", "server_error", "failed":
		return "failed"
	case "pending", "received":
		return status
	default:
		return ""
	}
}

func stringPtrOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func handlePostMessage(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Sender == nil || deps.Messages == nil {
			writeError(w, http.StatusServiceUnavailable, "messages_unavailable")
			return
		}
		var req struct {
			ChatJID   string `json:"chat_jid"`
			Type      string `json:"type"`
			Body      string `json:"body"`
			FilePath  string `json:"file_path"`
			AudioPath string `json:"audio_path"`
			Caption   string `json:"caption"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if req.ChatJID == "" {
			writeError(w, http.StatusBadRequest, "invalid_message")
			return
		}
		if req.FilePath != "" || req.AudioPath != "" {
			handlePostMediaMessage(deps, w, r, req.ChatJID, req.FilePath, req.AudioPath, req.Caption)
			return
		}
		if req.Body == "" || (req.Type != "" && req.Type != "text") {
			writeError(w, http.StatusBadRequest, "invalid_message")
			return
		}
		sent, err := deps.Sender.SendText(r.Context(), req.ChatJID, req.Body)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "send_failed")
			return
		}
		msg := store.Message{
			ID:        sent.ID,
			ChatJID:   sent.ChatJID,
			SenderJID: sent.SenderJID,
			Type:      "text",
			Body:      req.Body,
			Status:    "server_ack",
			Timestamp: sent.Timestamp,
		}
		if err := deps.Messages.SaveSentText(r.Context(), msg); err != nil {
			writeError(w, http.StatusInternalServerError, "store_failed")
			return
		}
		publishMessageEvents(r.Context(), deps.Hub, msg)
		writeJSON(w, http.StatusAccepted, apiMessageFromStore(msg))
	}
}

func handlePostMessageReaction(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Sender == nil || deps.Messages == nil {
			writeError(w, http.StatusServiceUnavailable, "messages_unavailable")
			return
		}
		var req struct {
			Emoji string `json:"emoji"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if req.Emoji == "" {
			writeError(w, http.StatusBadRequest, "invalid_reaction")
			return
		}
		target, ok := getTargetMessage(w, r, deps)
		if !ok {
			return
		}
		sent, err := deps.Sender.SendReaction(r.Context(), target, req.Emoji)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "reaction_failed")
			return
		}
		if err := deps.Messages.SaveReaction(r.Context(), target.ID, sent.SenderJID, req.Emoji); err != nil {
			writeError(w, http.StatusInternalServerError, "store_failed")
			return
		}
		publishReactionEvent(r.Context(), deps.Hub, target, sent, req.Emoji)
		w.WriteHeader(http.StatusNoContent)
	}
}

func handlePostMessageReply(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Sender == nil || deps.Messages == nil {
			writeError(w, http.StatusServiceUnavailable, "messages_unavailable")
			return
		}
		var req struct {
			Body string `json:"body"`
			Type string `json:"type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if req.Body == "" || (req.Type != "" && req.Type != "text") {
			writeError(w, http.StatusBadRequest, "invalid_reply")
			return
		}
		target, ok := getTargetMessage(w, r, deps)
		if !ok {
			return
		}
		sent, err := deps.Sender.SendReply(r.Context(), target, req.Body)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "reply_failed")
			return
		}
		msg := store.Message{
			ID:        sent.ID,
			ChatJID:   sent.ChatJID,
			SenderJID: sent.SenderJID,
			Type:      "text",
			Body:      req.Body,
			ReplyToID: target.ID,
			Status:    "server_ack",
			Timestamp: sent.Timestamp,
		}
		if err := deps.Messages.SaveReply(r.Context(), msg, target.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "store_failed")
			return
		}
		publishMessageEvents(r.Context(), deps.Hub, msg)
		writeJSON(w, http.StatusAccepted, apiMessageFromStore(msg))
	}
}

func handlePostMessageForward(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Sender == nil || deps.Messages == nil {
			writeError(w, http.StatusServiceUnavailable, "messages_unavailable")
			return
		}
		var req struct {
			ToJIDs []string `json:"to_jids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if len(req.ToJIDs) == 0 {
			writeError(w, http.StatusBadRequest, "invalid_forward")
			return
		}
		original, ok := getTargetMessage(w, r, deps)
		if !ok {
			return
		}
		sentMessages := make([]apiMessage, 0, len(req.ToJIDs))
		for _, toJID := range req.ToJIDs {
			if toJID == "" {
				writeError(w, http.StatusBadRequest, "invalid_forward")
				return
			}
			sent, err := deps.Sender.SendForward(r.Context(), toJID, original)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "forward_failed")
				return
			}
			msg := store.Message{
				ID:        sent.ID,
				ChatJID:   sent.ChatJID,
				SenderJID: sent.SenderJID,
				Type:      original.Type,
				Body:      original.Body,
				MediaPath: original.MediaPath,
				Status:    "server_ack",
				Timestamp: sent.Timestamp,
			}
			if err := deps.Messages.SaveText(r.Context(), msg, ""); err != nil {
				writeError(w, http.StatusInternalServerError, "store_failed")
				return
			}
			publishMessageEvents(r.Context(), deps.Hub, msg)
			sentMessages = append(sentMessages, apiMessageFromStore(msg))
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"items": sentMessages})
	}
}

func getTargetMessage(w http.ResponseWriter, r *http.Request, deps Dependencies) (store.Message, bool) {
	target, err := deps.Messages.GetMessage(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, store.ErrMessageNotFound) {
		writeError(w, http.StatusNotFound, "message_not_found")
		return store.Message{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get_message_failed")
		return store.Message{}, false
	}
	return target, true
}

func handlePostMediaMessage(deps Dependencies, w http.ResponseWriter, r *http.Request, chatJID string, filePath string, audioPath string, caption string) {
	if deps.Media == nil || deps.MediaDir == "" {
		writeError(w, http.StatusServiceUnavailable, "media_unavailable")
		return
	}
	srcPath := filePath
	kind := wa.MediaKindDocument
	if audioPath != "" {
		if filePath != "" {
			writeError(w, http.StatusBadRequest, "invalid_message")
			return
		}
		srcPath = audioPath
		kind = wa.MediaKindAudio
	}
	blob, data, err := media.PrepareLocalFile(r.Context(), deps.Media, deps.MediaDir, srcPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, "media_prepare_failed")
		return
	}
	if kind != wa.MediaKindAudio && strings.HasPrefix(blob.MIME, "image/") {
		kind = wa.MediaKindImage
	}
	sent, err := deps.Sender.SendMedia(r.Context(), chatJID, data, blob, kind, caption, filepath.Base(srcPath))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "send_failed")
		return
	}
	blob.MessageID = sent.ID
	if err := deps.Media.Save(r.Context(), blob); err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed")
		return
	}
	msg := store.Message{
		ID:        sent.ID,
		ChatJID:   sent.ChatJID,
		SenderJID: sent.SenderJID,
		Type:      string(kind),
		Body:      caption,
		MediaPath: blob.LocalPath,
		Status:    "server_ack",
		Timestamp: sent.Timestamp,
	}
	if err := deps.Messages.SaveText(r.Context(), msg, ""); err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed")
		return
	}
	publishMessageEvents(r.Context(), deps.Hub, msg)
	writeJSON(w, http.StatusAccepted, apiMessageFromStore(msg))
}

func publishReactionEvent(ctx context.Context, hub *ws.Hub, target store.Message, sent wa.SentMessage, emoji string) {
	if hub == nil {
		return
	}
	hub.Publish(ctx, ws.Event{
		ID:      sent.ID,
		Type:    ws.EventMessageReaction,
		Time:    sent.Timestamp,
		ChatJID: target.ChatJID,
		Payload: map[string]any{
			"message_id": target.ID,
			"sender_jid": sent.SenderJID,
			"emoji":      emoji,
		},
	})
}

func publishMessageEvents(ctx context.Context, hub *ws.Hub, msg store.Message) {
	if hub == nil {
		return
	}
	hub.Publish(ctx, ws.Event{
		ID:      msg.ID,
		Type:    ws.EventMessageNew,
		Time:    msg.Timestamp,
		ChatJID: msg.ChatJID,
		Payload: map[string]any{"message": msg},
	})
	hub.Publish(ctx, ws.Event{
		ID:      msg.ChatJID + ":" + msg.ID,
		Type:    ws.EventChatUpdated,
		Time:    msg.Timestamp,
		ChatJID: msg.ChatJID,
		Payload: map[string]any{"last_message_id": msg.ID},
	})
}

func handleSessionQRPNG(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Session == nil {
			writeError(w, http.StatusServiceUnavailable, "session_unavailable")
			return
		}
		qrCodes, err := deps.Session.QR(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "qr_failed")
			return
		}
		var code string
		select {
		case code = <-qrCodes:
		case <-r.Context().Done():
			return
		case <-time.After(15 * time.Second):
			writeError(w, http.StatusGatewayTimeout, "qr_timeout")
			return
		}
		if code == "" {
			writeError(w, http.StatusConflict, "already_logged_in")
			return
		}
		codePNG, err := qr.Encode(code, qr.L)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "qr_encode_failed")
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(codePNG.PNG())
	}
}

func handleSessionPairCode(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Session == nil {
			writeError(w, http.StatusServiceUnavailable, "session_unavailable")
			return
		}
		var req struct {
			Phone string `json:"phone"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if req.Phone == "" {
			writeError(w, http.StatusBadRequest, "missing_phone")
			return
		}
		code, err := deps.Session.PairCode(r.Context(), req.Phone)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "pair_code_failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"code": code})
	}
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleAuthLogin(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Auth == nil {
			writeError(w, http.StatusServiceUnavailable, "auth_unavailable")
			return
		}
		var req struct {
			PassphraseHash string `json:"passphrase_hash"`
			ClientLabel    string `json:"client_label"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if req.ClientLabel == "" {
			req.ClientLabel = "cli"
		}
		pair, err := deps.Auth.Login(r.Context(), []byte(req.PassphraseHash), req.ClientLabel)
		if errors.Is(err, auth.ErrInvalidPassphrase) {
			writeError(w, http.StatusUnauthorized, "invalid_passphrase")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "login_failed")
			return
		}
		writeJSON(w, http.StatusOK, pair)
	}
}

func handleAuthRefresh(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Auth == nil {
			writeError(w, http.StatusServiceUnavailable, "auth_unavailable")
			return
		}
		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		pair, err := deps.Auth.Refresh(r.Context(), req.RefreshToken)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid_refresh")
			return
		}
		writeJSON(w, http.StatusOK, pair)
	}
}

func handleAuthLogout(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := claimsFromContext(r.Context())
		clientLabel := "cli"
		if claims != nil && claims.ClientLabel != "" {
			clientLabel = claims.ClientLabel
		}
		if err := deps.Auth.Logout(r.Context(), clientLabel); err != nil {
			writeError(w, http.StatusInternalServerError, "logout_failed")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleSessionStatus(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := "logged_out"
		if deps.Session != nil {
			got, err := deps.Session.Status(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "session_status_failed")
				return
			}
			status = got
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": status})
	}
}

func handleSessionQR(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Session == nil {
			writeError(w, http.StatusServiceUnavailable, "session_unavailable")
			return
		}
		qr, err := deps.Session.QR(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "qr_failed")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		select {
		case code := <-qr:
			_, _ = w.Write([]byte("event: session.qr\n"))
			_, _ = w.Write([]byte("data: " + code + "\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
			_, _ = w.Write([]byte("event: session.qr_timeout\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

func handleWhatsAppLogout(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Session == nil {
			writeError(w, http.StatusServiceUnavailable, "session_unavailable")
			return
		}
		if err := deps.Session.Logout(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "session_logout_failed")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type claimsContextKey struct{}

func jwtMiddleware(authSvc AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if authSvc == nil {
				writeError(w, http.StatusServiceUnavailable, "auth_unavailable")
				return
			}
			header := r.Header.Get("Authorization")
			token, ok := strings.CutPrefix(header, "Bearer ")
			if !ok || token == "" {
				writeError(w, http.StatusUnauthorized, "missing_bearer")
				return
			}
			claims, err := authSvc.VerifyAccess(r.Context(), token)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid_bearer")
				return
			}
			ctx := context.WithValue(r.Context(), claimsContextKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func claimsFromContext(ctx context.Context) (*auth.Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey{}).(*auth.Claims)
	return claims, ok
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func handleGetDevices(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Devices == nil {
			writeError(w, http.StatusServiceUnavailable, "devices_unavailable")
			return
		}
		devices, err := deps.Devices.GetLinkedDevices(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "get_devices_failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": devices})
	}
}

func handleRevokeDevice(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Devices == nil {
			writeError(w, http.StatusServiceUnavailable, "devices_unavailable")
			return
		}
		jid := chi.URLParam(r, "jid")
		if err := deps.Devices.RevokeDevice(r.Context(), jid); err != nil {
			writeError(w, http.StatusInternalServerError, "revoke_device_failed")
			return
		}
		actor := actorFromContext(r.Context())
		if deps.Audit != nil {
			_ = deps.Audit.Log(r.Context(), actor, "device.revoke", jid, "ok", "high")
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleAdminRotateJWT(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Auth == nil {
			writeError(w, http.StatusServiceUnavailable, "auth_unavailable")
			return
		}
		newSecret, err := auth.GenerateSecret()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "generate_secret_failed")
			return
		}
		if err := deps.Auth.RotateSecret(r.Context(), newSecret); err != nil {
			writeError(w, http.StatusInternalServerError, "rotate_jwt_failed")
			return
		}
		actor := actorFromContext(r.Context())
		if deps.Audit != nil {
			_ = deps.Audit.Log(r.Context(), actor, "admin.rotate_jwt", "jwt_secret", "ok", "high")
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "rotated"})
	}
}

func handleAdminWipe(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Admin == nil {
			writeError(w, http.StatusServiceUnavailable, "admin_unavailable")
			return
		}
		actor := actorFromContext(r.Context())
		if deps.Audit != nil {
			_ = deps.Audit.Log(r.Context(), actor, "admin.wipe", "all", "initiated", "high")
		}
		if err := deps.Admin.WipeAll(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "wipe_failed")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleAdminExport(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Admin == nil {
			writeError(w, http.StatusServiceUnavailable, "admin_unavailable")
			return
		}
		data, err := deps.Admin.ExportDB(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "export_failed")
			return
		}
		actor := actorFromContext(r.Context())
		if deps.Audit != nil {
			_ = deps.Audit.Log(r.Context(), actor, "admin.export", "db", "ok", "high")
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="wh-cli-export.whcli.enc"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}

func handleAdminImport(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotImplemented, "import_requires_daemon_restart")
	}
}

func actorFromContext(ctx context.Context) string {
	claims, ok := claimsFromContext(ctx)
	if !ok || claims == nil {
		return "unknown"
	}
	return claims.ClientLabel
}
