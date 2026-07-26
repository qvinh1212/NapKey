package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

// API key prefixes, identical to kiro-go's config package. They are duplicated
// rather than imported because the two services are separate modules and separate
// deployments; a shared constant would create a build dependency between the
// control plane and the data plane for the sake of two strings.
//
// Any change here must be made in kiro-go/config/apikeys.go at the same time.
const (
	PrefixLive = "nk_live_"
	PrefixTest = "nk_test_"
)

// keyRandomBytes is 32 bytes = 256 bits of entropy, rendered as 64 hex chars.
// Guessing a key is not a realistic attack at this size, which matters because
// a key is a bearer credential with no second factor.
const keyRandomBytes = 32

// ErrInvalidKeyFormat is returned when a string is not a NapKey API key.
var ErrInvalidKeyFormat = errors.New("not a valid NapKey API key")

// GeneratedKey is a freshly minted key. The cleartext value exists only in this
// struct and in the single HTTP response that returns it; only Hash reaches the
// database.
type GeneratedKey struct {
	// Value is the full cleartext key, shown to the user exactly once.
	Value string
	// Prefix is "nk_live_" or "nk_test_".
	Prefix string
	// Hash is SHA-256 of Value.
	Hash []byte
	// LastFour is the trailing 4 characters, for display alongside the prefix.
	LastFour string
}

// GenerateKey mints a new API key. testMode selects the nk_test_ prefix.
func GenerateKey(testMode bool) (GeneratedKey, error) {
	prefix := PrefixLive
	if testMode {
		prefix = PrefixTest
	}
	buf := make([]byte, keyRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return GeneratedKey{}, err
	}
	value := prefix + hex.EncodeToString(buf)
	return GeneratedKey{
		Value:    value,
		Prefix:   prefix,
		Hash:     HashKey(value),
		LastFour: value[len(value)-4:],
	}, nil
}

// HashKey returns SHA-256 of the key value.
//
// A plain hash is correct here, unlike for passwords. An API key is 256 bits of
// uniform random data, so there is no dictionary to attack and no need for a slow
// KDF; the reason to hash at all is that a database leak must not yield working
// keys. Using PBKDF2 instead would add latency to every single proxied request.
func HashKey(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

// ParseKey validates the shape of a key and reports whether it is test mode.
func ParseKey(value string) (prefix string, testMode bool, err error) {
	switch {
	case strings.HasPrefix(value, PrefixLive):
		prefix, testMode = PrefixLive, false
	case strings.HasPrefix(value, PrefixTest):
		prefix, testMode = PrefixTest, true
	default:
		return "", false, ErrInvalidKeyFormat
	}
	body := value[len(prefix):]
	if len(body) != keyRandomBytes*2 {
		return "", false, ErrInvalidKeyFormat
	}
	if _, err := hex.DecodeString(body); err != nil {
		return "", false, ErrInvalidKeyFormat
	}
	return prefix, testMode, nil
}

// IsTestKey reports whether the key is a test-mode key.
func IsTestKey(value string) bool { return strings.HasPrefix(value, PrefixTest) }

// MaskKey renders a key for display, keeping the full prefix so live and test
// keys stay distinguishable. Matches kiro-go's MaskApiKey output.
func MaskKey(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 10 {
		return value
	}
	for _, prefix := range []string{PrefixLive, PrefixTest} {
		if strings.HasPrefix(value, prefix) && len(value) > len(prefix)+6 {
			return value[:len(prefix)+2] + "****" + value[len(value)-4:]
		}
	}
	return value[:6] + "****" + value[len(value)-4:]
}

// DisplayKey rebuilds the masked form from the stored pieces, for rows where the
// cleartext is gone (which is every row after creation).
func DisplayKey(prefix, lastFour string) string {
	return prefix + "**" + "****" + lastFour
}
