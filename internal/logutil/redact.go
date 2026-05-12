// Package logutil contains helpers for safe structured logging.
package logutil

import "strings"

const redactedJIDSuffix = "@s.whatsapp.net"

// RedactJID masks WhatsApp identifiers before logging.
func RedactJID(jid string) string {
	if jid == "" {
		return ""
	}
	if !strings.HasSuffix(jid, redactedJIDSuffix) {
		return "[redacted-jid]"
	}

	local := strings.TrimSuffix(jid, redactedJIDSuffix)
	if len(local) <= 4 {
		return "****" + redactedJIDSuffix
	}
	return local[:2] + "****" + local[len(local)-2:] + redactedJIDSuffix
}
