package auth

import (
	"strings"
	"testing"
	"time"
)

func TestHashAndVerifyPassword(t *testing.T) {
	const password = "correct horse battery"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "pbkdf2-sha256$") {
		t.Errorf("unexpected hash format: %q", hash)
	}
	// The cleartext must not be recoverable from what gets stored.
	if strings.Contains(hash, password) {
		t.Error("the stored hash contains the cleartext password")
	}
	needsRehash, err := VerifyPassword(password, hash)
	if err != nil {
		t.Fatalf("VerifyPassword with the right password: %v", err)
	}
	if needsRehash {
		t.Error("a freshly created hash should not need rehashing")
	}
	if _, err := VerifyPassword("wrong password entirely", hash); err == nil {
		t.Error("expected the wrong password to be rejected")
	}
}

func TestHashesAreSaltedPerPassword(t *testing.T) {
	const password = "same password twice"
	a, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	b, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	// Identical passwords must produce different hashes, otherwise one cracked
	// hash reveals every account that shares that password.
	if a == b {
		t.Error("two hashes of the same password are identical, the salt is not random")
	}
}

func TestVerifyRejectsMalformedHash(t *testing.T) {
	cases := []string{
		"",
		"not-a-hash",
		"pbkdf2-sha256$notanumber$c2FsdA$a2V5",
		"pbkdf2-sha256$1000$!!!$a2V5",
		"bcrypt$10$abc$def",
		"pbkdf2-sha256$1000$c2FsdA",
		// An absurd iteration count would turn one login into a CPU exhaustion.
		"pbkdf2-sha256$99999999999$c2FsdA$a2V5",
	}
	for _, encoded := range cases {
		if _, err := VerifyPassword("whatever", encoded); err == nil {
			t.Errorf("expected %q to be rejected", encoded)
		}
	}
}

func TestNeedsRehashWhenIterationsAreLow(t *testing.T) {
	// A hash written by an older, cheaper configuration must be flagged for
	// upgrade rather than silently accepted forever.
	const password = "legacy password value"
	weak, err := legacyHash(password, 1000)
	if err != nil {
		t.Fatalf("building legacy hash: %v", err)
	}
	needsRehash, err := VerifyPassword(password, weak)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !needsRehash {
		t.Error("a hash with fewer iterations than the current setting should need rehashing")
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{"long enough", "a-decent-passphrase", false},
		{"exactly at minimum", strings.Repeat("a", MinPasswordLength), false},
		{"one under minimum", strings.Repeat("a", MinPasswordLength-1), true},
		{"empty", "", true},
		{"over maximum", strings.Repeat("a", MaxPasswordLength+1), true},
		{"common password", "password123", true},
		{"common password mixed case", "PassWord123", true},
		{"common vietnamese", "matkhau123", true},
		// Rune-counted, so a short Vietnamese passphrase is not misjudged by bytes.
		{"vietnamese passphrase", "mật khẩu của tôi", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePassword(tc.password)
			if tc.wantErr && err == nil {
				t.Errorf("expected %q to be rejected", tc.password)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected %q to be accepted, got: %v", tc.password, err)
			}
		})
	}
}

func TestUnicodePasswordLengthCountsRunes(t *testing.T) {
	// 10 Vietnamese characters is 10 characters even though it is more bytes.
	password := "khẩunhật123"
	if err := ValidatePassword(password); err != nil {
		t.Errorf("a multi-byte password of sufficient length was rejected: %v", err)
	}
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if _, err := VerifyPassword(password, hash); err != nil {
		t.Errorf("round trip of a multi-byte password failed: %v", err)
	}
}

func TestDummyVerifyCostsSimilarTimeToRealVerify(t *testing.T) {
	// The point of DummyVerify is that a login for an unknown address takes about
	// as long as one for a known address. If it returned immediately, response time
	// would tell an attacker which addresses are registered.
	hash, err := HashPassword("some password here")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	realStart := time.Now()
	_, _ = VerifyPassword("some password here", hash)
	realElapsed := time.Since(realStart)

	dummyStart := time.Now()
	DummyVerify("some password here")
	dummyElapsed := time.Since(dummyStart)

	// Deliberately loose: this asserts the same order of magnitude, not a precise
	// match, so it does not turn into a flaky timing test on a loaded machine.
	if dummyElapsed < realElapsed/4 {
		t.Errorf("DummyVerify took %v against a real verify of %v, too fast to mask enumeration",
			dummyElapsed, realElapsed)
	}
}
