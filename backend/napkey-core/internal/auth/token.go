package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
)

// Session and email tokens are opaque random strings. The database stores only
// SHA-256 of the value, so a leaked table does not yield usable sessions.
const (
	// sessionTokenBytes is 32 bytes of entropy for the cookie value.
	sessionTokenBytes = 32
	// emailTokenBytes is the same size for verification links. These travel
	// through email and may sit in an inbox indefinitely, so the entropy has to
	// stand on its own; the short TTL is a second layer, not the first.
	emailTokenBytes = 32
)

// ErrInvalidToken means the token is malformed or its signature does not verify.
var ErrInvalidToken = errors.New("invalid token")

// NewSessionToken returns a random session token and its hash.
func NewSessionToken() (token string, hash []byte, err error) {
	buf := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, err
	}
	token = base64.RawURLEncoding.EncodeToString(buf)
	return token, HashToken(token), nil
}

// NewEmailToken returns a random token for a verification or reset link.
func NewEmailToken() (token string, hash []byte, err error) {
	buf := make([]byte, emailTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, err
	}
	token = base64.RawURLEncoding.EncodeToString(buf)
	return token, HashToken(token), nil
}

// HashToken returns SHA-256 of a token. Like API keys, tokens are high-entropy
// random values, so a fast hash is the right tool.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// SignCookie appends an HMAC tag to a cookie value.
//
// The session token is already unguessable, so the tag is not what makes the
// session secure. What it buys is that a forged or truncated cookie is rejected
// without a database lookup, which keeps a flood of junk cookies from turning
// into a flood of queries.
func SignCookie(secret []byte, value string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(value))
	return value + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// VerifyCookie checks the HMAC tag and returns the value.
func VerifyCookie(secret []byte, signed string) (string, error) {
	value, tag, found := strings.Cut(signed, ".")
	if !found || value == "" || tag == "" {
		return "", ErrInvalidToken
	}
	got, err := base64.RawURLEncoding.DecodeString(tag)
	if err != nil {
		return "", ErrInvalidToken
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(value))
	if subtle.ConstantTimeCompare(got, mac.Sum(nil)) != 1 {
		return "", ErrInvalidToken
	}
	return value, nil
}

// NewCSRFToken returns a random token for the double-submit cookie pattern.
func NewCSRFToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// CompareCSRF compares two CSRF tokens in constant time.
func CompareCSRF(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
