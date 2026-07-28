// Package config loads napkey-core settings from the environment.
//
// Everything comes from env vars, nothing from a file on disk. That is a
// deliberate break from kiro-go, whose config.json holds the admin password and
// customer keys in cleartext (DESIGN.md section 10.1). Secrets here live in the
// Coolify environment and never touch the repo or a volume.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the fully validated runtime configuration.
type Config struct {
	// Server
	Host string
	Port int

	// Postgres DSN, e.g. postgres://user:pass@host:5432/napkey?sslmode=disable
	DatabaseURL string
	// MaxOpenConns caps the pool. Postgres handles connections with a process
	// each, so an unbounded pool is a way to take the database down.
	MaxOpenConns int
	MaxIdleConns int

	// SessionSecret keys the session cookie HMAC. Must be >= 32 bytes.
	SessionSecret []byte
	// TrialFingerprintSecret keys privacy-preserving IP fingerprints. Keep it
	// stable across session-secret rotations or the same network could receive a
	// second trial after a rotation.
	TrialFingerprintSecret []byte
	// SessionTTL is how long a login lasts before re-authentication.
	SessionTTL time.Duration
	// SecureCookies sets the Secure attribute. Off only for local http.
	SecureCookies bool
	// TrustedProxyHops is the number of reverse proxies in front of the API.
	// It is used to select the client address from the right side of X-Forwarded-For.
	TrustedProxyHops int

	// PublicBaseURL is the console origin, used to build email links and to
	// decide CORS. No trailing slash.
	PublicBaseURL string

	// kiro-go data plane, where keys get pushed.
	KiroAdminURL      string
	KiroAdminPassword string
	// KiroSyncInterval is how often the reconciler sweeps for keys whose push
	// to kiro-go failed.
	KiroSyncInterval time.Duration

	// MailFrom is the From header on verification mail. When MailProvider is
	// "log" the message body is written to the log instead of being sent, which
	// is what local development uses.
	MailProvider string
	MailFrom     string
	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string
	GoogleClientID string
	GoogleClientSecret string

	// EmailTokenTTL bounds how long a verification link stays usable.
	EmailTokenTTL time.Duration

	// DefaultTokenLimit / DefaultCreditLimit are the manual quotas handed to a
	// newly created key. Stage 2 has no wallet yet (DESIGN.md section 9), so
	// quota is granted by hand and these are the starting values.
	DefaultTokenLimit  int64
	DefaultCreditLimit float64

	// MaxKeysPerUser stops one account from minting keys without bound.
	MaxKeysPerUser int

	// AdminEmails may reach /admin/* endpoints. Comma-separated in the env.
	AdminEmails []string

	// Payment providers. PayOS is used for new checkout orders; Casso remains
	// optional so existing deployments can reconcile historical bank transfers.
	PayOSClientID    string
	PayOSAPIKey      string
	PayOSChecksumKey string
	CassoWebhookSecret string
	CassoAPIKey        string
	BankAccountNumber  string
	BankAccountName    string
	BankName           string
	BankBin            string
	WalletHoldMicros   int64

	LogLevel string
}

// Load reads and validates the environment. It returns every problem it finds at
// once rather than one per restart, because a misconfigured deploy usually has
// more than one missing variable.
func Load() (*Config, error) {
	c := &Config{
		Host:               env("HOST", "0.0.0.0"),
		Port:               envInt("PORT", 8081),
		DatabaseURL:        strings.TrimSpace(os.Getenv("DATABASE_URL")),
		MaxOpenConns:       envInt("DB_MAX_OPEN_CONNS", 10),
		MaxIdleConns:       envInt("DB_MAX_IDLE_CONNS", 5),
		SessionTTL:         envDuration("SESSION_TTL", 14*24*time.Hour),
		SecureCookies:      envBool("SECURE_COOKIES", true),
		TrustedProxyHops:   envInt("TRUSTED_PROXY_HOPS", 1),
		PublicBaseURL:      strings.TrimRight(env("PUBLIC_BASE_URL", "http://localhost:3000"), "/"),
		KiroAdminURL:       strings.TrimRight(strings.TrimSpace(os.Getenv("KIRO_ADMIN_URL")), "/"),
		KiroAdminPassword:  os.Getenv("KIRO_ADMIN_PASSWORD"),
		KiroSyncInterval:   envDuration("KIRO_SYNC_INTERVAL", 60*time.Second),
		MailProvider:       strings.ToLower(env("MAIL_PROVIDER", "log")),
		MailFrom:           env("MAIL_FROM", "NapKey <no-reply@napkey.vn>"),
		SMTPHost:           os.Getenv("SMTP_HOST"),
		SMTPPort:           envInt("SMTP_PORT", 587),
		SMTPUser:           os.Getenv("SMTP_USER"),
		SMTPPassword:       os.Getenv("SMTP_PASSWORD"),
		GoogleClientID:     strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID")),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		EmailTokenTTL:      envDuration("EMAIL_TOKEN_TTL", 24*time.Hour),
		DefaultTokenLimit:  int64(envInt("DEFAULT_TOKEN_LIMIT", 0)),
		DefaultCreditLimit: envFloat("DEFAULT_CREDIT_LIMIT", 0),
		MaxKeysPerUser:     envInt("MAX_KEYS_PER_USER", 10),
		LogLevel:           env("LOG_LEVEL", "info"),
		PayOSClientID:      strings.TrimSpace(os.Getenv("PAYOS_CLIENT_ID")),
		PayOSAPIKey:        strings.TrimSpace(os.Getenv("PAYOS_API_KEY")),
		PayOSChecksumKey:   os.Getenv("PAYOS_CHECKSUM_KEY"),
		CassoWebhookSecret: os.Getenv("CASSO_WEBHOOK_SECRET"),
		CassoAPIKey:        os.Getenv("CASSO_API_KEY"),
		BankAccountNumber:  strings.TrimSpace(os.Getenv("BANK_ACCOUNT_NUMBER")),
		BankAccountName:    strings.TrimSpace(os.Getenv("BANK_ACCOUNT_NAME")),
		BankName:           strings.TrimSpace(os.Getenv("BANK_NAME")),
		BankBin:            strings.TrimSpace(os.Getenv("BANK_BIN")),
		WalletHoldMicros:   int64(envInt("WALLET_HOLD_VND", 1_000_000)) * 1_000_000,
	}

	for _, e := range strings.Split(os.Getenv("ADMIN_EMAILS"), ",") {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			c.AdminEmails = append(c.AdminEmails, e)
		}
	}

	secret := os.Getenv("SESSION_SECRET")
	c.SessionSecret = []byte(secret)
	trialSecret := os.Getenv("TRIAL_FINGERPRINT_SECRET")
	if trialSecret == "" {
		trialSecret = secret
	}
	c.TrialFingerprintSecret = []byte(trialSecret)

	var problems []string
	if c.DatabaseURL == "" {
		problems = append(problems, "DATABASE_URL is required")
	} else if _, err := url.Parse(c.DatabaseURL); err != nil {
		problems = append(problems, "DATABASE_URL is not a valid URL: "+err.Error())
	}
	// 32 bytes is the HMAC-SHA256 block floor; a shorter secret is padded
	// internally, which means less entropy than the algorithm implies.
	if len(c.SessionSecret) < 32 {
		problems = append(problems, "SESSION_SECRET must be at least 32 bytes (generate with: openssl rand -base64 48)")
	}
	if len(c.TrialFingerprintSecret) < 32 {
		problems = append(problems, "TRIAL_FINGERPRINT_SECRET must be at least 32 bytes when set")
	}
	if c.Port < 1 || c.Port > 65535 {
		problems = append(problems, "PORT must be between 1 and 65535")
	}
	if c.MaxOpenConns < 1 {
		problems = append(problems, "DB_MAX_OPEN_CONNS must be at least 1")
	}
	if c.MaxIdleConns > c.MaxOpenConns {
		problems = append(problems, "DB_MAX_IDLE_CONNS must not exceed DB_MAX_OPEN_CONNS")
	}
	// A key that exists in Postgres but not in kiro-go authenticates nothing, so
	// the sync target is not optional.
	if c.KiroAdminURL == "" {
		problems = append(problems, "KIRO_ADMIN_URL is required (napkey-core must be able to push keys to the data plane)")
	}
	if c.KiroAdminPassword == "" {
		problems = append(problems, "KIRO_ADMIN_PASSWORD is required")
	}
	switch c.MailProvider {
	case "log":
		// Verification links go to the log. Fine locally, not in production.
	case "smtp":
		if c.SMTPHost == "" {
			problems = append(problems, "SMTP_HOST is required when MAIL_PROVIDER=smtp")
		}
		if c.SMTPPort < 1 || c.SMTPPort > 65535 {
			problems = append(problems, "SMTP_PORT must be between 1 and 65535")
		}
	default:
		problems = append(problems, fmt.Sprintf("MAIL_PROVIDER %q is not supported (use \"log\" or \"smtp\")", c.MailProvider))
	}
	if c.MaxKeysPerUser < 1 {
		problems = append(problems, "MAX_KEYS_PER_USER must be at least 1")
	}
	if c.SessionTTL <= 0 {
		problems = append(problems, "SESSION_TTL must be positive")
	}
	if c.TrustedProxyHops < 0 || c.TrustedProxyHops > 5 {
		problems = append(problems, "TRUSTED_PROXY_HOPS must be between 0 and 5")
	}
	if c.EmailTokenTTL <= 0 {
		problems = append(problems, "EMAIL_TOKEN_TTL must be positive")
	}
	if c.WalletHoldMicros < 20_000_000_000 {
		problems = append(problems, "WALLET_HOLD_VND must be at least 20,000")
	}
	payOSConfigured := c.PayOSClientID != "" || c.PayOSAPIKey != "" || c.PayOSChecksumKey != ""
	if payOSConfigured {
		for _, item := range []struct{ name, value string }{{"PAYOS_CLIENT_ID", c.PayOSClientID}, {"PAYOS_API_KEY", c.PayOSAPIKey}, {"PAYOS_CHECKSUM_KEY", c.PayOSChecksumKey}} {
			if item.value == "" { problems = append(problems, item.name+" is required when PayOS top-ups are configured") }
		}
	}
	googleConfigured := c.GoogleClientID != "" || c.GoogleClientSecret != ""
	if googleConfigured && (c.GoogleClientID == "" || c.GoogleClientSecret == "") {
		problems = append(problems, "GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET must both be set")
	}
	walletConfigured:=c.CassoWebhookSecret!=""||c.CassoAPIKey!=""||c.BankAccountNumber!=""||c.BankAccountName!=""||c.BankName!=""||c.BankBin!=""
	if walletConfigured {
		for _,item:=range []struct{name,value string}{{"CASSO_WEBHOOK_SECRET",c.CassoWebhookSecret},{"CASSO_API_KEY",c.CassoAPIKey},{"BANK_ACCOUNT_NUMBER",c.BankAccountNumber},{"BANK_ACCOUNT_NAME",c.BankAccountName},{"BANK_NAME",c.BankName},{"BANK_BIN",c.BankBin}}{if item.value==""{problems=append(problems,item.name+" is required when wallet top-ups are configured")}}
	}

	if len(problems) > 0 {
		return nil, fmt.Errorf("invalid configuration:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return c, nil
}

// IsAdmin reports whether the email is on the admin allowlist. Comparison is
// case-insensitive because email local parts are treated case-insensitively in
// practice and the users table stores them lowercased.
func (c *Config) IsAdmin(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return false
	}
	for _, a := range c.AdminEmails {
		if a == email {
			return true
		}
	}
	return false
}

// Addr is the listen address.
func (c *Config) Addr() string { return fmt.Sprintf("%s:%d", c.Host, c.Port) }

// RedactedDatabaseURL strips the password so the DSN can be logged safely.
func (c *Config) RedactedDatabaseURL() string {
	u, err := url.Parse(c.DatabaseURL)
	if err != nil {
		return "(unparseable)"
	}
	if _, hasPassword := u.User.Password(); hasPassword {
		u.User = url.UserPassword(u.User.Username(), "****")
	}
	return u.String()
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	// Bare integers are read as seconds so operators can write "3600".
	if n, err := strconv.Atoi(v); err == nil {
		return time.Duration(n) * time.Second
	}
	return def
}
