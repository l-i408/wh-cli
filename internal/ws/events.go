// Package ws defines daemon WebSocket event types.
package ws

import "time"

const (
	// EventMessageNew is emitted when a message is received or accepted for send.
	EventMessageNew = "message.new"
	// EventChatUpdated is emitted when chat list metadata changes.
	EventChatUpdated = "chat.updated"
	// EventMessageReaction is emitted when a message reaction is sent or received.
	EventMessageReaction = "message.reaction"
	// EventSessionConnected is emitted after WhatsApp connects.
	EventSessionConnected = "session.connected"
	// EventSessionDeviceLinked is emitted when a linked device is detected.
	EventSessionDeviceLinked = "session.device_linked"
)

// Event is the typed JSON envelope sent over WebSocket.
type Event struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	Time    time.Time      `json:"ts"`
	ChatJID string         `json:"chat_jid,omitempty"`
	Payload map[string]any `json:"payload"`
}
