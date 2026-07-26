// Package auth holds password hashing, API key generation, and token handling.
package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Password hashing uses PBKDF2-HMAC-SHA256 from the standard library.
//
// Argon2id would be the stronger choice and is what a greenfield service should
// use, but it lives in golang.org/x/crypto, which is not available here. PBKDF2
// is the best option in std: it is the KDF specified by RFC 8018, it is what
// NIST SP 800-132 recommends, and at a high iteration count it is a real cost to
// an offline attacker. The parameters are recorded in the stored hash so raising
// them later does not invalidate existing passwords.
const (
	// pbkdf2Iterations follows OWASP's 2023 guidance for PBKDF2-HMAC-SHA256.
	pbkdf2Iterations = 600_000
	// saltBytes is per-password random salt; 16 bytes is the SP 800-132 floor.
	saltBytes = 16
	// keyBytes matches the SHA-256 output size. Asking for more would force extra
	// PBKDF2 blocks for no added strength.
	keyBytes = 32
)

// Password policy. Length carries most of the strength, so the rule is a
// meaningful minimum rather than a character-class checklist that pushes people
// toward "Password1!".
const (
	MinPasswordLength = 10
	// MaxPasswordLength bounds the PBKDF2 input. Without a cap, a multi-megabyte
	// password is a cheap way to burn server CPU.
	MaxPasswordLength = 200
)

var (
	// ErrPasswordTooShort and friends are returned by ValidatePassword.
	ErrPasswordTooShort = fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	ErrPasswordTooLong  = fmt.Errorf("password must be at most %d characters", MaxPasswordLength)
	ErrPasswordCommon   = errors.New("password is too common, choose something less predictable")
	// ErrInvalidHash means the stored hash is not in a format this code wrote.
	ErrInvalidHash = errors.New("stored password hash is malformed")
	// ErrMismatch means the password does not match the hash.
	ErrMismatch = errors.New("password does not match")
)

// commonPasswords blocks the handful of passwords that dominate credential
// stuffing lists. This is not a substitute for a full breach corpus check, and it
// is not presented as one; it is the cheap 90% of the benefit.
var commonPasswords = map[string]bool{
	"password":    true,
	"password1":   true,
	"password123": true,
	"123456":      true,
	"12345678":    true,
	"123456789":   true,
	"1234567890":  true,
	"qwerty":      true,
	"qwerty123":   true,
	"letmein":     true,
	"welcome":     true,
	"welcome123":  true,
	"admin":       true,
	"admin123":    true,
	"iloveyou":    true,
	"monkey":      true,
	"dragon":      true,
	"football":    true,
	"baseball":    true,
	"abc123":      true,
	"changeme":    true,
	"napkey":      true,
	"napkey123":   true,
	"matkhau":     true,
	"matkhau123":  true,
}

// ValidatePassword enforces the policy. It runs before hashing so a rejected
// password never costs a PBKDF2 derivation.
func ValidatePassword(password string) error {
	// Counted in runes: a Vietnamese password of 10 characters is more than 10
	// bytes, and rejecting it for "length" would be wrong.
	n := utf8.RuneCountInString(password)
	if n < MinPasswordLength {
		return ErrPasswordTooShort
	}
	if n > MaxPasswordLength {
		return ErrPasswordTooLong
	}
	if commonPasswords[strings.ToLower(strings.TrimSpace(password))] {
		return ErrPasswordCommon
	}
	return nil
}

// HashPassword derives a storable hash. The format is
//
//	pbkdf2-sha256$<iterations>$<base64 salt>$<base64 key>
//
// which carries its own parameters so the cost can be raised without a migration.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generating salt: %w", err)
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, keyBytes)
	if err != nil {
		return "", fmt.Errorf("auth: deriving password hash: %w", err)
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s",
		pbkdf2Iterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword checks a password against a stored hash.
//
// The returned needsRehash is true when the hash used weaker parameters than the
// current setting, letting the caller transparently upgrade it on a successful
// login instead of forcing a reset.
func VerifyPassword(password, encoded string) (needsRehash bool, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false, ErrInvalidHash
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 1 {
		return false, ErrInvalidHash
	}
	// A hash claiming an absurd iteration count would turn one login attempt into
	// a denial of service against this process.
	if iterations > 10_000_000 {
		return false, ErrInvalidHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(salt) == 0 {
		return false, ErrInvalidHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false, ErrInvalidHash
	}

	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	if err != nil {
		return false, fmt.Errorf("auth: deriving password hash: %w", err)
	}
	// Constant time: a byte-by-byte comparison leaks how much of the hash matched
	// through timing, which is enough to reconstruct it one byte at a time.
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, ErrMismatch
	}
	return iterations < pbkdf2Iterations || len(salt) < saltBytes, nil
}

// DummyVerify burns roughly the same CPU as a real verification.
//
// Login must take the same time whether or not the email exists. Skipping the
// hash for an unknown address turns response latency into an account-enumeration
// oracle, which is how attackers build target lists before credential stuffing.
func DummyVerify(password string) {
	salt := make([]byte, saltBytes)
	_, _ = pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, keyBytes)
}
