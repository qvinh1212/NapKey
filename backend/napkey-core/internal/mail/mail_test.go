package mail

import (
	"context"
	"strings"
	"testing"
)

func TestVerificationMessageIncludesToken(t *testing.T) {
	msg := VerificationMessage("https://napkey.vn", "vi", "user@napkey.vn", "tok123")
	if msg.To != "user@napkey.vn" {
		t.Errorf("To = %q", msg.To)
	}
	if !strings.Contains(msg.TextBody, "tok123") {
		t.Error("the body should carry the token")
	}
	if !strings.Contains(msg.TextBody, "https://napkey.vn/vi/verify-email") {
		t.Errorf("unexpected link in body:\n%s", msg.TextBody)
	}
}

func TestMessageLocaleSelection(t *testing.T) {
	vi := VerificationMessage("https://napkey.vn", "vi", "a@napkey.vn", "t")
	en := VerificationMessage("https://napkey.vn", "en", "a@napkey.vn", "t")
	if vi.Subject == en.Subject {
		t.Error("the subject should differ between locales")
	}
	if !strings.Contains(en.TextBody, "/en/") {
		t.Error("the English link should carry the en locale")
	}
	// Vietnamese is the default per DESIGN.md section 8, so an unknown locale
	// falls back to it rather than to English.
	fallback := VerificationMessage("https://napkey.vn", "fr", "a@napkey.vn", "t")
	if fallback.Subject != vi.Subject {
		t.Error("an unknown locale should fall back to Vietnamese")
	}
}

func TestTokenIsURLEscapedInLink(t *testing.T) {
	// Session and email tokens are base64url, which can contain characters that
	// would otherwise terminate the query string.
	msg := VerificationMessage("https://napkey.vn", "vi", "a@napkey.vn", "abc&def=ghi")
	if strings.Contains(msg.TextBody, "token=abc&def=ghi") {
		t.Error("the token was not URL-escaped, so the link would be truncated")
	}
	if !strings.Contains(msg.TextBody, "abc%26def%3Dghi") {
		t.Errorf("expected an escaped token in:\n%s", msg.TextBody)
	}
}

func TestBaseURLTrailingSlashDoesNotDoubleUp(t *testing.T) {
	msg := VerificationMessage("https://napkey.vn/", "vi", "a@napkey.vn", "t")
	if strings.Contains(msg.TextBody, "napkey.vn//") {
		t.Errorf("double slash in link:\n%s", msg.TextBody)
	}
}

func TestHeaderInjectionIsRejected(t *testing.T) {
	// A newline in a header value could append arbitrary headers, which is how a
	// verification email gets silently copied somewhere else.
	sender := &SMTPSender{Host: "localhost", Port: 25, From: "NapKey <no-reply@napkey.vn>"}
	cases := []Message{
		{To: "victim@napkey.vn\r\nBcc: attacker@evil.com", Subject: "hi", TextBody: "x"},
		{To: "victim@napkey.vn", Subject: "hi\r\nBcc: attacker@evil.com", TextBody: "x"},
		{To: "victim@napkey.vn\nBcc: attacker@evil.com", Subject: "hi", TextBody: "x"},
	}
	for _, msg := range cases {
		err := sender.Send(context.Background(), msg)
		if err == nil {
			t.Errorf("expected rejection for %q / %q", msg.To, msg.Subject)
			continue
		}
		if !strings.Contains(err.Error(), "line break") {
			t.Errorf("expected a line-break error, got: %v", err)
		}
	}
}

func TestBuildMessageEscapesLoneDot(t *testing.T) {
	// A line containing only "." terminates SMTP DATA; it has to be escaped or the
	// rest of the message would be dropped and interpreted as commands.
	body := buildMessage("NapKey <no-reply@napkey.vn>", Message{
		To:       "a@napkey.vn",
		Subject:  "test",
		TextBody: "before\n.\nafter",
	})
	if !strings.Contains(body, "\r\n..\r\n") {
		t.Errorf("a lone dot was not escaped:\n%q", body)
	}
	// Headers and body must be separated by CRLF CRLF.
	if !strings.Contains(body, "\r\n\r\n") {
		t.Error("missing the header/body separator")
	}
}

func TestBuildMessageNormalizesLineEndings(t *testing.T) {
	body := buildMessage("from@napkey.vn", Message{
		To: "a@napkey.vn", Subject: "s", TextBody: "line1\nline2",
	})
	if strings.Contains(strings.ReplaceAll(body, "\r\n", ""), "\n") {
		t.Error("the message still contains bare newlines")
	}
}

func TestExtractAddress(t *testing.T) {
	tests := map[string]string{
		"NapKey <no-reply@napkey.vn>": "no-reply@napkey.vn",
		"no-reply@napkey.vn":          "no-reply@napkey.vn",
		"  spaced@napkey.vn  ":        "spaced@napkey.vn",
	}
	for input, want := range tests {
		if got := extractAddress(input); got != want {
			t.Errorf("extractAddress(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestLogSenderDoesNotFail(t *testing.T) {
	// The log sender is what makes local development work without a mail server.
	if err := (LogSender{}).Send(context.Background(), Message{
		To: "a@napkey.vn", Subject: "s", TextBody: "b",
	}); err != nil {
		t.Errorf("LogSender.Send: %v", err)
	}
}
