package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSeedConfig writes a config file with the given password and returns its path.
func writeSeedConfig(t *testing.T, password string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	seed := map[string]interface{}{
		"password": password,
		"port":     8080,
		"host":     "127.0.0.1",
		"accounts": []interface{}{},
	}
	raw, err := json.MarshalIndent(seed, "", "  ")
	if err != nil {
		t.Fatalf("marshal seed: %v", err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	return path
}

// The hardcoded "changeme" default is public knowledge: anyone who reads the
// repository can open the admin panel of a deployment that never changed it.
// Loading such a config must rotate the password rather than preserve it.
func TestLoadRotatesLegacyDefaultPassword(t *testing.T) {
	path := writeSeedConfig(t, legacyDefaultPassword)
	if err := Init(path); err != nil {
		t.Fatalf("init: %v", err)
	}

	pw := GetPassword()
	if pw == legacyDefaultPassword {
		t.Fatalf("legacy default password was preserved")
	}
	if len(pw) < 16 {
		t.Fatalf("rotated password is too short to be a real secret: %q", pw)
	}

	// The operator must be able to learn the new value exactly once.
	if got := TakeGeneratedPassword(); got != pw {
		t.Fatalf("TakeGeneratedPassword = %q, want %q", got, pw)
	}
	if got := TakeGeneratedPassword(); got != "" {
		t.Fatalf("expected the generated password to be reported only once, got %q", got)
	}

	// Rotation must be persisted, otherwise the next boot rotates again and
	// invalidates the password the operator just wrote down.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if strings.Contains(string(raw), legacyDefaultPassword) {
		t.Fatalf("legacy password still present on disk")
	}
	if !strings.Contains(string(raw), pw) {
		t.Fatalf("rotated password was not persisted")
	}
	if err := Init(path); err != nil {
		t.Fatalf("re-init: %v", err)
	}
	if got := GetPassword(); got != pw {
		t.Fatalf("password changed across reload: got %q want %q", got, pw)
	}
	if got := TakeGeneratedPassword(); got != "" {
		t.Fatalf("stable password must not be reported as generated, got %q", got)
	}
}

// An empty password is worse than a weak one: handleAdminAPI compares the
// supplied header against it, so an empty value would accept a request with no
// password header at all.
func TestLoadRotatesEmptyPassword(t *testing.T) {
	path := writeSeedConfig(t, "")
	if err := Init(path); err != nil {
		t.Fatalf("init: %v", err)
	}
	if GetPassword() == "" {
		t.Fatalf("empty password must not survive load")
	}
	if TakeGeneratedPassword() == "" {
		t.Fatalf("expected the generated password to be reported")
	}
}

// A password the operator chose is left alone, and must not be advertised as
// generated (which would print a secret they already manage into the logs).
func TestLoadPreservesOperatorPassword(t *testing.T) {
	const chosen = "an-operator-chosen-password"
	path := writeSeedConfig(t, chosen)
	if err := Init(path); err != nil {
		t.Fatalf("init: %v", err)
	}
	if got := GetPassword(); got != chosen {
		t.Fatalf("GetPassword = %q, want %q", got, chosen)
	}
	if got := TakeGeneratedPassword(); got != "" {
		t.Fatalf("expected no generated password, got %q", got)
	}
}

// A fresh install has no config file at all; the created default must already
// carry a random password.
func TestLoadGeneratesPasswordOnFirstRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Init(path); err != nil {
		t.Fatalf("init: %v", err)
	}
	pw := GetPassword()
	if pw == "" || pw == legacyDefaultPassword {
		t.Fatalf("first run produced an unusable password: %q", pw)
	}
	if len(pw) < 16 {
		t.Fatalf("first-run password too short: %q", pw)
	}
}

// SetPassword is the ADMIN_PASSWORD override path. It supersedes any generated
// secret, so nothing should be printed afterwards.
func TestSetPasswordClearsGeneratedPassword(t *testing.T) {
	path := writeSeedConfig(t, legacyDefaultPassword)
	if err := Init(path); err != nil {
		t.Fatalf("init: %v", err)
	}
	SetPassword("from-environment")
	if got := GetPassword(); got != "from-environment" {
		t.Fatalf("GetPassword = %q, want %q", got, "from-environment")
	}
	if got := TakeGeneratedPassword(); got != "" {
		t.Fatalf("generated password should be cleared by an explicit override, got %q", got)
	}
}
