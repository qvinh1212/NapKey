package httpapi

import (
	"bytes"
	"testing"
)

func TestNormalizeTrialIP(t *testing.T) {
	tests := map[string]string{
		"203.0.113.9":                       "203.0.113.9",
		"2001:db8:abcd:1234:1111:2222:3333:4444": "2001:db8:abcd:1234::/64",
		"not-an-ip":                         "",
		"":                                  "",
	}
	for input, want := range tests {
		if got := normalizeTrialIP(input); got != want {
			t.Errorf("normalizeTrialIP(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestTrialIPHashIsKeyedAndDomainSeparated(t *testing.T) {
	secret := []byte("01234567890123456789012345678901")
	first := trialIPHash(secret, "203.0.113.9")
	second := trialIPHash(secret, "203.0.113.9")
	other := trialIPHash(secret, "203.0.113.10")
	if len(first) != 32 {
		t.Fatalf("hash length = %d, want 32", len(first))
	}
	if !bytes.Equal(first, second) {
		t.Fatal("same IP and secret must produce the same hash")
	}
	if bytes.Equal(first, other) {
		t.Fatal("different normalized IPs must not share a hash")
	}
	if got := trialIPHash(secret, "invalid"); got != nil {
		t.Fatal("invalid IP must not produce a reusable fingerprint")
	}
}
