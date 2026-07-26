package auth

import (
	"strings"
	"testing"
)

func TestSessionTokenIsRandomAndHashed(t *testing.T) {
	token, hash, err := NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken: %v", err)
	}
	if len(token) < 40 {
		t.Errorf("token %q is shorter than expected for 32 bytes of entropy", token)
	}
	if string(hash) != string(HashToken(token)) {
		t.Error("the returned hash does not match the token")
	}
	// What lands in the database must not be the token itself.
	if strings.Contains(string(hash), token) {
		t.Error("the hash contains the raw token")
	}

	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		tk, _, err := NewSessionToken()
		if err != nil {
			t.Fatalf("NewSessionToken: %v", err)
		}
		if seen[tk] {
			t.Fatal("generated a duplicate session token")
		}
		seen[tk] = true
	}
}

func TestSignAndVerifyCookie(t *testing.T) {
	secret := []byte("this-is-a-32-byte-minimum-secret!")
	token, _, err := NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken: %v", err)
	}
	signed := SignCookie(secret, token)
	got, err := VerifyCookie(secret, signed)
	if err != nil {
		t.Fatalf("VerifyCookie: %v", err)
	}
	if got != token {
		t.Errorf("round trip gave %q, want %q", got, token)
	}
}

func TestVerifyCookieRejectsTampering(t *testing.T) {
	secret := []byte("this-is-a-32-byte-minimum-secret!")
	token := "abcdefghijklmnop"
	signed := SignCookie(secret, token)

	cases := map[string]string{
		"no signature":      token,
		"empty":             "",
		"only a dot":        ".",
		"bad signature":     token + ".AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"altered value":     "abcdefghijklmnoq" + signed[strings.Index(signed, "."):],
		"truncated":         signed[:len(signed)-4],
		"signature not b64": token + ".!!!not-base64!!!",
	}
	for name, cookie := range cases {
		if _, err := VerifyCookie(secret, cookie); err == nil {
			t.Errorf("%s: expected rejection, got success", name)
		}
	}

	// A cookie signed with a different secret must not verify, which is what stops
	// someone who guesses the token format from minting their own.
	other := []byte("a-completely-different-32-byte-key")
	if _, err := VerifyCookie(other, signed); err == nil {
		t.Error("a cookie signed with another secret was accepted")
	}
}

func TestCSRFTokenComparison(t *testing.T) {
	a, err := NewCSRFToken()
	if err != nil {
		t.Fatalf("NewCSRFToken: %v", err)
	}
	if !CompareCSRF(a, a) {
		t.Error("a token should equal itself")
	}
	b, err := NewCSRFToken()
	if err != nil {
		t.Fatalf("NewCSRFToken: %v", err)
	}
	if CompareCSRF(a, b) {
		t.Error("two different tokens compared equal")
	}
	// Empty values must never compare equal, or a request with no token would pass.
	if CompareCSRF("", "") {
		t.Error("two empty tokens must not compare equal")
	}
	if CompareCSRF(a, "") || CompareCSRF("", a) {
		t.Error("an empty token must never match a real one")
	}
}
