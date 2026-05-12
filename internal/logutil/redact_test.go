package logutil

import "testing"

func TestRedactJID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		jid  string
		want string
	}{
		{name: "empty", jid: "", want: ""},
		{name: "phone jid", jid: "1234567@s.whatsapp.net", want: "12****67@s.whatsapp.net"},
		{name: "short local", jid: "123@s.whatsapp.net", want: "****@s.whatsapp.net"},
		{name: "unknown shape", jid: "group@g.us", want: "[redacted-jid]"},
	}

	for _, tt := range tests {
		if got := RedactJID(tt.jid); got != tt.want {
			t.Fatalf("%s: RedactJID() = %q, want %q", tt.name, got, tt.want)
		}
	}
}
