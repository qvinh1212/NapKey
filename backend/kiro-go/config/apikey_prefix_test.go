package config

import (
	"strings"
	"testing"
)

// Key prefixes are a product decision (DESIGN.md section 3.8): one canonical
// shape, live vs test distinguishable on sight. The old generator emitted "sk-",
// which collides with OpenAI keys and made a leaked NapKey key indistinguishable
// from an upstream one in a log.
func TestGenerateApiKeyValueUsesLivePrefix(t *testing.T) {
	key := GenerateApiKeyValue()
	if !strings.HasPrefix(key, ApiKeyPrefixLive) {
		t.Fatalf("expected %q prefix, got %q", ApiKeyPrefixLive, key)
	}
	if strings.HasPrefix(key, "sk-") {
		t.Fatalf("legacy sk- prefix must not reappear: %q", key)
	}
	// 8-char prefix + 64 hex chars of 32 random bytes.
	if want := len(ApiKeyPrefixLive) + 2*apiKeyRandomBytes; len(key) != want {
		t.Fatalf("len(key) = %d, want %d", len(key), want)
	}
	if IsTestApiKey(key) {
		t.Fatalf("live key must not be classified as a test key: %q", key)
	}
}

func TestGenerateTestApiKeyValueUsesTestPrefix(t *testing.T) {
	key := GenerateTestApiKeyValue()
	if !strings.HasPrefix(key, ApiKeyPrefixTest) {
		t.Fatalf("expected %q prefix, got %q", ApiKeyPrefixTest, key)
	}
	if !IsTestApiKey(key) {
		t.Fatalf("expected IsTestApiKey to report true for %q", key)
	}
	if key == GenerateTestApiKeyValue() {
		t.Fatalf("expected unique keys across calls")
	}
}

// Masking must keep the full prefix readable, otherwise the admin list cannot
// tell a live key from a test key at a glance.
func TestMaskApiKeyKeepsPrefixVisible(t *testing.T) {
	live := MaskApiKey(GenerateApiKeyValue())
	if !strings.HasPrefix(live, ApiKeyPrefixLive) {
		t.Fatalf("masked live key lost its prefix: %q", live)
	}
	test := MaskApiKey(GenerateTestApiKeyValue())
	if !strings.HasPrefix(test, ApiKeyPrefixTest) {
		t.Fatalf("masked test key lost its prefix: %q", test)
	}
	if live == test {
		t.Fatalf("masked live and test keys must stay distinguishable")
	}
	if !strings.Contains(live, "****") {
		t.Fatalf("expected the middle to be masked: %q", live)
	}
	// Legacy-shaped values keep the old behaviour so existing config files and
	// the admin UI do not change appearance for keys created before the switch.
	if got := MaskApiKey("sk-1234567890"); got != "sk-123****7890" {
		t.Fatalf("legacy masking changed: got %q", got)
	}
}
