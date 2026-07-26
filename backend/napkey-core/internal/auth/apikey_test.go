package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestGenerateKeyShape(t *testing.T) {
	for _, testMode := range []bool{false, true} {
		k, err := GenerateKey(testMode)
		if err != nil {
			t.Fatalf("GenerateKey(%v): %v", testMode, err)
		}
		wantPrefix := PrefixLive
		if testMode {
			wantPrefix = PrefixTest
		}
		if !strings.HasPrefix(k.Value, wantPrefix) {
			t.Errorf("key %q does not start with %q", k.Value, wantPrefix)
		}
		if k.Prefix != wantPrefix {
			t.Errorf("Prefix = %q, want %q", k.Prefix, wantPrefix)
		}
		// 64 hex characters is the 256 bits of entropy the design calls for.
		body := strings.TrimPrefix(k.Value, wantPrefix)
		if len(body) != 64 {
			t.Errorf("key body is %d characters, want 64", len(body))
		}
		if _, err := hex.DecodeString(body); err != nil {
			t.Errorf("key body is not hex: %v", err)
		}
		if k.LastFour != k.Value[len(k.Value)-4:] {
			t.Errorf("LastFour = %q does not match the key tail", k.LastFour)
		}
		// The stored hash must be a hash, not the key.
		want := sha256.Sum256([]byte(k.Value))
		if string(k.Hash) != string(want[:]) {
			t.Error("Hash is not SHA-256 of the key value")
		}
		if strings.Contains(string(k.Hash), wantPrefix) {
			t.Error("the hash appears to contain the cleartext key")
		}
	}
}

func TestGeneratedKeysAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		k, err := GenerateKey(false)
		if err != nil {
			t.Fatalf("GenerateKey: %v", err)
		}
		if seen[k.Value] {
			t.Fatalf("generated a duplicate key on iteration %d", i)
		}
		seen[k.Value] = true
	}
}

func TestParseKey(t *testing.T) {
	live, err := GenerateKey(false)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	test, err := GenerateKey(true)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	prefix, isTest, err := ParseKey(live.Value)
	if err != nil || prefix != PrefixLive || isTest {
		t.Errorf("ParseKey(live) = (%q, %v, %v)", prefix, isTest, err)
	}
	prefix, isTest, err = ParseKey(test.Value)
	if err != nil || prefix != PrefixTest || !isTest {
		t.Errorf("ParseKey(test) = (%q, %v, %v)", prefix, isTest, err)
	}

	bad := []string{
		"",
		"sk-1234567890",
		"nk_live_",
		"nk_live_tooshort",
		"nk_live_" + strings.Repeat("z", 64), // right length, not hex
		"nk_live_" + strings.Repeat("a", 63), // one short
		"NK_LIVE_" + strings.Repeat("a", 64), // wrong case
	}
	for _, v := range bad {
		if _, _, err := ParseKey(v); err == nil {
			t.Errorf("expected %q to be rejected", v)
		}
	}
}

func TestMaskKeyKeepsPrefixVisible(t *testing.T) {
	k, err := GenerateKey(false)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	masked := MaskKey(k.Value)
	// The whole prefix has to survive masking, otherwise live and test keys are
	// indistinguishable in a list.
	if !strings.HasPrefix(masked, PrefixLive) {
		t.Errorf("masked key %q lost its prefix", masked)
	}
	if !strings.HasSuffix(masked, k.LastFour) {
		t.Errorf("masked key %q lost its last four characters", masked)
	}
	if !strings.Contains(masked, "****") {
		t.Errorf("masked key %q has no mask", masked)
	}
	// The full secret must not survive masking.
	if strings.Contains(masked, strings.TrimPrefix(k.Value, PrefixLive)) {
		t.Error("the masked key still contains the full secret")
	}

	testKey, err := GenerateKey(true)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if !strings.HasPrefix(MaskKey(testKey.Value), PrefixTest) {
		t.Error("a test key lost its prefix when masked")
	}
	if MaskKey("") != "" {
		t.Error("masking an empty string should stay empty")
	}
}

func TestHashKeyIsStable(t *testing.T) {
	const value = "nk_live_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if string(HashKey(value)) != string(HashKey(value)) {
		t.Error("HashKey is not deterministic")
	}
	if string(HashKey(value)) == string(HashKey(value+"x")) {
		t.Error("different keys hashed to the same value")
	}
}
