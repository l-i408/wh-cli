// Package wa wraps WhatsApp provider state behind testable interfaces.
package wa

import (
	"context"
	"sync"
)

const (
	// StatusConnected means the WhatsApp socket is connected.
	StatusConnected = "connected"
	// StatusQRPending means QR pairing is waiting for a scan.
	StatusQRPending = "qr_pending"
	// StatusLoggedOut means there is no active WhatsApp session.
	StatusLoggedOut = "logged_out"
)

// MemorySession is an A1 placeholder session provider used until whatsmeow is wired.
type MemorySession struct {
	mu     sync.Mutex
	status string
	qr     string
}

// NewMemorySession constructs a session provider with a deterministic QR placeholder.
func NewMemorySession() *MemorySession {
	return &MemorySession{status: StatusLoggedOut, qr: "WH-CLI-QR-PENDING"}
}

// Status returns the current session state.
func (s *MemorySession) Status(_ context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status, nil
}

// QR starts QR mode and returns a single QR event channel.
func (s *MemorySession) QR(ctx context.Context) (<-chan string, error) {
	s.mu.Lock()
	s.status = StatusQRPending
	qr := s.qr
	s.mu.Unlock()

	ch := make(chan string, 1)
	select {
	case ch <- qr:
	case <-ctx.Done():
	}
	close(ch)
	return ch, nil
}

// LatestQR returns the memory session QR placeholder.
func (s *MemorySession) LatestQR() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.qr, s.qr != ""
}

// PairCode returns a deterministic placeholder pairing code for tests.
func (s *MemorySession) PairCode(_ context.Context, _ string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = StatusQRPending
	return "ABCD-1234", nil
}

// Logout clears the session state.
func (s *MemorySession) Logout(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = StatusLoggedOut
	return nil
}
