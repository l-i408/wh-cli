package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/coder/websocket"
)

// Hub broadcasts daemon events to WebSocket subscribers.
type Hub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}
}

// NewHub constructs an event hub.
func NewHub() *Hub {
	return &Hub{clients: make(map[*websocket.Conn]struct{})}
}

// ServeHTTP accepts a WebSocket client and keeps it subscribed until disconnect.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	h.mu.Lock()
	h.clients[conn] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.clients, conn)
		h.mu.Unlock()
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}()
	<-r.Context().Done()
}

// Publish sends an event to all connected subscribers.
func (h *Hub) Publish(ctx context.Context, event Event) {
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	h.mu.Lock()
	clients := make([]*websocket.Conn, 0, len(h.clients))
	for conn := range h.clients {
		clients = append(clients, conn)
	}
	h.mu.Unlock()
	for _, conn := range clients {
		_ = conn.Write(ctx, websocket.MessageText, payload)
	}
}
