package config

import (
	"strings"
	"testing"
	"time"
)

// setValidEnv sets the minimum variables needed for Load to succeed.
func setValidEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://napkey:pw@localhost:5432/napkey?sslmode=disable")
	t.Setenv("SESSION_SECRET", strings.Repeat("s", 48))
	t.Setenv("KIRO_ADMIN_URL", "http://kiro-go:8080")
	t.Setenv("KIRO_ADMIN_PASSWORD", "data-plane-password")
}

func TestLoadWithValidEnvironment(t *testing.T) {
	setValidEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 8081 {
		t.Errorf("Port = %d, want the 8081 default", cfg.Port)
	}
	if !cfg.SecureCookies {
		t.Error("SecureCookies should default to true")
	}
	if cfg.MailProvider != "log" {
		t.Errorf("MailProvider = %q, want log", cfg.MailProvider)
	}
}

func TestLoadRequiresSessionSecret(t *testing.T) {
	setValidEnv(t)
	// A short secret means less entropy than HMAC-SHA256 implies, so it is refused
	// rather than silently padded.
	t.Setenv("SESSION_SECRET", "too-short")
	_, err := Load()
	if err == nil {
		t.Fatal("expected a short SESSION_SECRET to be rejected")
	}
	if !strings.Contains(err.Error(), "SESSION_SECRET") {
		t.Errorf("error should name SESSION_SECRET, got: %v", err)
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	setValidEnv(t)
	t.Setenv("DATABASE_URL", "")
	_, err := Load()
	if err == nil {
		t.Fatal("expected a missing DATABASE_URL to be rejected")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("error should name DATABASE_URL, got: %v", err)
	}
}

func TestLoadRequiresDataPlaneSettings(t *testing.T) {
	// A key that exists in Postgres but not in kiro-go authenticates nothing, so
	// startup refuses to proceed without somewhere to push keys.
	setValidEnv(t)
	t.Setenv("KIRO_ADMIN_URL", "")
	if _, err := Load(); err == nil {
		t.Error("expected a missing KIRO_ADMIN_URL to be rejected")
	}

	setValidEnv(t)
	t.Setenv("KIRO_ADMIN_PASSWORD", "")
	if _, err := Load(); err == nil {
		t.Error("expected a missing KIRO_ADMIN_PASSWORD to be rejected")
	}
}

func TestLoadReportsEveryProblemAtOnce(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("SESSION_SECRET", "")
	t.Setenv("KIRO_ADMIN_URL", "")
	t.Setenv("KIRO_ADMIN_PASSWORD", "")
	_, err := Load()
	if err == nil {
		t.Fatal("expected an error")
	}
	// Listing every problem at once avoids a restart cycle per missing variable.
	for _, want := range []string{"DATABASE_URL", "SESSION_SECRET", "KIRO_ADMIN_URL", "KIRO_ADMIN_PASSWORD"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %s, got: %v", want, err)
		}
	}
}

func TestSMTPValidation(t *testing.T) {
	setValidEnv(t)
	t.Setenv("MAIL_PROVIDER", "smtp")
	if _, err := Load(); err == nil {
		t.Error("MAIL_PROVIDER=smtp without SMTP_HOST should be rejected")
	}

	t.Setenv("SMTP_HOST", "smtp.example.com")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load with valid SMTP settings: %v", err)
	}
	if cfg.SMTPPort != 587 {
		t.Errorf("SMTPPort = %d, want the 587 default", cfg.SMTPPort)
	}

	t.Setenv("MAIL_PROVIDER", "carrier-pigeon")
	if _, err := Load(); err == nil {
		t.Error("an unknown MAIL_PROVIDER should be rejected")
	}
}

func TestIsAdminIsCaseInsensitive(t *testing.T) {
	setValidEnv(t)
	t.Setenv("ADMIN_EMAILS", "Boss@NapKey.vn, ops@napkey.vn ")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, email := range []string{"boss@napkey.vn", "BOSS@NAPKEY.VN", "Boss@napkey.vn", "ops@napkey.vn"} {
		if !cfg.IsAdmin(email) {
			t.Errorf("IsAdmin(%q) = false, want true", email)
		}
	}
	for _, email := range []string{"", "someone@napkey.vn", "boss@napkey.vn.attacker.com"} {
		if cfg.IsAdmin(email) {
			t.Errorf("IsAdmin(%q) = true, want false", email)
		}
	}
}

func TestRedactedDatabaseURLHidesPassword(t *testing.T) {
	setValidEnv(t)
	t.Setenv("DATABASE_URL", "postgres://napkey:sup3rs3cret@db:5432/napkey")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	redacted := cfg.RedactedDatabaseURL()
	// This string gets logged at startup, so the password must not be in it.
	if strings.Contains(redacted, "sup3rs3cret") {
		t.Errorf("the redacted DSN still contains the password: %s", redacted)
	}
	if !strings.Contains(redacted, "napkey") {
		t.Errorf("the redacted DSN lost the useful parts: %s", redacted)
	}
}

func TestDurationParsing(t *testing.T) {
	setValidEnv(t)
	t.Setenv("SESSION_TTL", "48h")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SessionTTL != 48*time.Hour {
		t.Errorf("SessionTTL = %v, want 48h", cfg.SessionTTL)
	}

	// A bare integer is read as seconds, so an operator writing "3600" gets what
	// they meant instead of a silent fallback.
	t.Setenv("SESSION_TTL", "3600")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SessionTTL != time.Hour {
		t.Errorf("SessionTTL = %v, want 1h from the bare integer", cfg.SessionTTL)
	}

	t.Setenv("SESSION_TTL", "not-a-duration")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SessionTTL != 14*24*time.Hour {
		t.Errorf("SessionTTL = %v, want the default after an unparseable value", cfg.SessionTTL)
	}
}
