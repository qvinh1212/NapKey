// Package mail sends transactional email.
//
// Two implementations: an SMTP sender for real deployments and a log sender for
// development. The log sender prints the verification link so a developer can
// complete signup without wiring up a mail server.
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"napkey-core/internal/logger"
)

// Message is one outbound email.
type Message struct {
	To      string
	Subject string
	// TextBody is the only body sent. Plain text avoids the HTML-email rendering
	// mess entirely and is what a verification link actually needs.
	TextBody string
}

// Sender delivers messages.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// LogSender writes messages to the log instead of sending them.
type LogSender struct{}

// Send logs the message.
func (LogSender) Send(_ context.Context, msg Message) error {
	logger.Infof("[mail:log] to=%s subject=%q\n%s", msg.To, msg.Subject, msg.TextBody)
	return nil
}

// SMTPSender delivers over SMTP with STARTTLS.
type SMTPSender struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// Send delivers one message.
func (s *SMTPSender) Send(ctx context.Context, msg Message) error {
	if err := validateHeaderValue(msg.To); err != nil {
		return fmt.Errorf("mail: recipient: %w", err)
	}
	if err := validateHeaderValue(msg.Subject); err != nil {
		return fmt.Errorf("mail: subject: %w", err)
	}

	addr := net.JoinHostPort(s.Host, fmt.Sprint(s.Port))
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("mail: dialing %s: %w", addr, err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	}

	client, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		return fmt.Errorf("mail: SMTP handshake with %s: %w", s.Host, err)
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		// Credentials must not cross the network in the clear.
		if err := client.StartTLS(&tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("mail: STARTTLS: %w", err)
		}
	} else if s.Password != "" {
		// Refusing here is deliberate: authenticating over an unencrypted link
		// would hand the mailbox password to anyone on the path.
		return errors.New("mail: server does not offer STARTTLS, refusing to send credentials in the clear")
	}

	if s.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.Username, s.Password, s.Host)); err != nil {
			return fmt.Errorf("mail: authenticating: %w", err)
		}
	}

	from := extractAddress(s.From)
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("mail: MAIL FROM: %w", err)
	}
	if err := client.Rcpt(msg.To); err != nil {
		return fmt.Errorf("mail: RCPT TO: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("mail: DATA: %w", err)
	}
	body := buildMessage(s.From, msg)
	if _, err := w.Write([]byte(body)); err != nil {
		return fmt.Errorf("mail: writing body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mail: closing body: %w", err)
	}
	return client.Quit()
}

func buildMessage(from string, msg Message) string {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", msg.To)
	fmt.Fprintf(&b, "Subject: %s\r\n", msg.Subject)
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	// Bare newlines are normalized to CRLF and lone dots escaped, per RFC 5321.
	body := strings.ReplaceAll(msg.TextBody, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\n", "\r\n")
	for _, line := range strings.Split(body, "\r\n") {
		if line == "." {
			b.WriteString("..\r\n")
			continue
		}
		b.WriteString(line)
		b.WriteString("\r\n")
	}
	return b.String()
}

// validateHeaderValue rejects CR and LF in header values.
//
// Without this an address containing a newline could inject extra headers, which
// is how a verification email gets silently BCC'd somewhere else.
func validateHeaderValue(v string) error {
	if strings.ContainsAny(v, "\r\n") {
		return errors.New("value contains a line break")
	}
	return nil
}

func extractAddress(from string) string {
	if i := strings.LastIndex(from, "<"); i >= 0 {
		if j := strings.Index(from[i:], ">"); j > 0 {
			return from[i+1 : i+j]
		}
	}
	return strings.TrimSpace(from)
}
