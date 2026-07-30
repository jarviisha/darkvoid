package mailer

import (
	"strings"
	"testing"
)

func TestNewMessageID_UsesSenderDomain(t *testing.T) {
	id := newMessageID("noreply@darkvoid.app")

	if !strings.HasPrefix(id, "<") || !strings.HasSuffix(id, ">") {
		t.Errorf("Message-ID must be angle-bracketed per RFC 5322, got %q", id)
	}
	if !strings.HasSuffix(id, "@darkvoid.app>") {
		t.Errorf("Message-ID = %q, want the sender's domain as the right-hand side", id)
	}
	if id == newMessageID("noreply@darkvoid.app") {
		t.Error("two Message-IDs are identical — each send needs its own")
	}
}

func TestNewMessageID_FallsBackWhenSenderHasNoDomain(t *testing.T) {
	for _, from := range []string{"", "noreply", "noreply@"} {
		id := newMessageID(from)
		if !strings.HasSuffix(id, "@darkvoid.local>") {
			t.Errorf("newMessageID(%q) = %q, want the fallback domain", from, id)
		}
	}
}

func TestBuildMIME_IncludesMessageIDAndBothParts(t *testing.T) {
	msg := &Message{
		To:      []string{"user@example.com"},
		Subject: "Welcome to DarkVoid",
		HTML:    "<p>hello</p>",
		Text:    "hello",
	}

	body := buildMIME("DarkVoid <noreply@darkvoid.app>", "<abc@darkvoid.app>", msg)

	for _, want := range []string{
		"From: DarkVoid <noreply@darkvoid.app>\r\n",
		"To: user@example.com\r\n",
		"Subject: Welcome to DarkVoid\r\n",
		"Message-ID: <abc@darkvoid.app>\r\n",
		"MIME-Version: 1.0\r\n",
		"Content-Type: text/plain; charset=\"UTF-8\"",
		"Content-Type: text/html; charset=\"UTF-8\"",
		"<p>hello</p>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("MIME body is missing %q\n---\n%s", want, body)
		}
	}

	// Headers end before the first part; a Message-ID in the body would not count.
	headers, _, found := strings.Cut(body, "\r\n\r\n")
	if !found {
		t.Fatal("MIME body has no header/body separator")
	}
	if !strings.Contains(headers, "Message-ID:") {
		t.Errorf("Message-ID is not in the header block, got headers:\n%s", headers)
	}
}

func TestExtractEmail(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"DarkVoid <noreply@darkvoid.app>", "noreply@darkvoid.app"},
		{"noreply@darkvoid.app", "noreply@darkvoid.app"},
		{"not an address", "not an address"},
	}

	for _, tt := range tests {
		if got := extractEmail(tt.in); got != tt.want {
			t.Errorf("extractEmail(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
