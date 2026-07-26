package pgwire

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// scramClient implements the client half of SCRAM-SHA-256 (RFC 5802) plus the
// -PLUS channel binding variant (RFC 5929/9266), which is what Postgres 10+ uses
// by default for password authentication.
//
// The point of SCRAM over MD5: the password never crosses the wire, and the
// server proof lets the client verify it is talking to something that actually
// holds the stored verifier rather than a relay.
type scramClient struct {
	username string
	password string
	// clientNonce is fresh per authentication attempt.
	clientNonce string
	// firstMessageBare is the client-first-message without the GS2 header; it is
	// part of the auth message the proofs are computed over.
	firstMessageBare string
	// gs2Header records the channel-binding decision so the server can detect a
	// downgrade attempt.
	gs2Header string
	// channelBinding carries tls-server-end-point data for SCRAM-SHA-256-PLUS.
	channelBinding []byte
	// saltedPassword is retained to verify the server's final proof.
	saltedPassword []byte
	authMessage    string
}

// scramNonceBytes is the client nonce entropy. RFC 5802 requires only that it be
// unpredictable; 18 bytes yields 24 base64 characters.
const scramNonceBytes = 18

func newSCRAMClient(username, password string, channelBinding []byte) (*scramClient, error) {
	raw := make([]byte, scramNonceBytes)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("pgwire: generating SCRAM nonce: %w", err)
	}
	c := &scramClient{
		username:       username,
		password:       password,
		clientNonce:    base64.StdEncoding.EncodeToString(raw),
		channelBinding: channelBinding,
	}
	if len(channelBinding) > 0 {
		c.gs2Header = "p=tls-server-end-point,,"
	} else {
		// "n" means the client does not support channel binding. "y" would mean
		// it does but believes the server does not; sending "n" unconditionally
		// when we have no binding data is the honest signal.
		c.gs2Header = "n,,"
	}
	return c, nil
}

// firstMessage builds client-first-message.
func (c *scramClient) firstMessage() string {
	// saslPrepare would normally run over the username here, but Postgres ignores
	// the SCRAM username field entirely (it authenticates the startup-packet
	// user), so RFC 5802 says to send "n=" empty or anything; libpq sends empty.
	c.firstMessageBare = "n=,r=" + c.clientNonce
	return c.gs2Header + c.firstMessageBare
}

// handleServerFirst parses server-first-message and returns client-final-message.
func (c *scramClient) handleServerFirst(serverFirst string) (string, error) {
	attrs, err := parseSCRAMAttrs(serverFirst)
	if err != nil {
		return "", err
	}
	serverNonce := attrs["r"]
	saltB64 := attrs["s"]
	iterStr := attrs["i"]
	if serverNonce == "" || saltB64 == "" || iterStr == "" {
		return "", fmt.Errorf("pgwire: SCRAM server-first-message missing r/s/i")
	}
	// The server nonce must extend the client nonce. If it does not, the exchange
	// is not bound to our nonce and could be a replay of another session.
	if !strings.HasPrefix(serverNonce, c.clientNonce) || serverNonce == c.clientNonce {
		return "", fmt.Errorf("pgwire: SCRAM server nonce does not extend the client nonce")
	}
	salt, err := base64.StdEncoding.DecodeString(saltB64)
	if err != nil {
		return "", fmt.Errorf("pgwire: SCRAM salt is not valid base64: %w", err)
	}
	iterations, err := strconv.Atoi(iterStr)
	if err != nil {
		return "", fmt.Errorf("pgwire: SCRAM iteration count %q is not an integer", iterStr)
	}
	// RFC 5802 sets 4096 as the floor. A server asking for less is either broken
	// or trying to make the derived key cheap to attack offline.
	if iterations < 4096 {
		return "", fmt.Errorf("pgwire: SCRAM iteration count %d is below the RFC 5802 minimum of 4096", iterations)
	}
	if iterations > 1_000_000 {
		return "", fmt.Errorf("pgwire: SCRAM iteration count %d is implausibly high, refusing", iterations)
	}

	prepared, err := saslPrepare(c.password)
	if err != nil {
		return "", err
	}
	c.saltedPassword, err = pbkdf2.Key(sha256.New, string(prepared), salt, iterations, sha256.Size)
	if err != nil {
		return "", fmt.Errorf("pgwire: deriving SCRAM salted password: %w", err)
	}

	// channel-binding attribute is base64 of the GS2 header plus, for -PLUS, the
	// server certificate hash.
	cbindInput := append([]byte(c.gs2Header), c.channelBinding...)
	clientFinalWithoutProof := "c=" + base64.StdEncoding.EncodeToString(cbindInput) + ",r=" + serverNonce

	c.authMessage = c.firstMessageBare + "," + serverFirst + "," + clientFinalWithoutProof

	clientKey := hmacSHA256(c.saltedPassword, []byte("Client Key"))
	storedKey := sha256.Sum256(clientKey)
	clientSignature := hmacSHA256(storedKey[:], []byte(c.authMessage))

	proof := make([]byte, len(clientKey))
	for i := range clientKey {
		proof[i] = clientKey[i] ^ clientSignature[i]
	}
	return clientFinalWithoutProof + ",p=" + base64.StdEncoding.EncodeToString(proof), nil
}

// verifyServerFinal checks the server's proof. Skipping this check would leave
// the client unable to distinguish the real server from an attacker who only
// needs to answer "ok".
func (c *scramClient) verifyServerFinal(serverFinal string) error {
	attrs, err := parseSCRAMAttrs(serverFinal)
	if err != nil {
		return err
	}
	if e, ok := attrs["e"]; ok {
		return fmt.Errorf("pgwire: SCRAM authentication failed: %s", e)
	}
	verifierB64 := attrs["v"]
	if verifierB64 == "" {
		return fmt.Errorf("pgwire: SCRAM server-final-message has no verifier")
	}
	got, err := base64.StdEncoding.DecodeString(verifierB64)
	if err != nil {
		return fmt.Errorf("pgwire: SCRAM verifier is not valid base64: %w", err)
	}
	serverKey := hmacSHA256(c.saltedPassword, []byte("Server Key"))
	want := hmacSHA256(serverKey, []byte(c.authMessage))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return fmt.Errorf("pgwire: SCRAM server signature mismatch, refusing the connection")
	}
	return nil
}

func hmacSHA256(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return m.Sum(nil)
}

// parseSCRAMAttrs splits "k=v,k=v" into a map. Values may contain '=' (base64
// padding), so only the first '=' separates.
func parseSCRAMAttrs(s string) (map[string]string, error) {
	out := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		if part == "" {
			continue
		}
		k, v, found := strings.Cut(part, "=")
		if !found || k == "" {
			return nil, fmt.Errorf("pgwire: malformed SCRAM attribute %q", part)
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("pgwire: empty SCRAM message")
	}
	return out, nil
}

// saslPrepare applies the SASLprep (RFC 4013) rules that matter here.
//
// Full stringprep needs Unicode tables this project will not carry. What it does
// implement is the part with security consequences: reject characters that
// change how the string is interpreted, and map the space-equivalents so a
// password typed with a non-breaking space still authenticates. ASCII passwords,
// which is what a service account uses, pass through untouched.
func saslPrepare(password string) ([]byte, error) {
	var b strings.Builder
	b.Grow(len(password))
	for _, r := range password {
		switch {
		case r == 0:
			return nil, fmt.Errorf("pgwire: password contains a NUL byte")
		// Non-ASCII space characters map to ASCII space (RFC 4013 mapping table C.1.2).
		case r == 0x00A0 || r == 0x1680 || (r >= 0x2000 && r <= 0x200B) || r == 0x202F || r == 0x205F || r == 0x3000:
			b.WriteRune(' ')
		// Mapped-to-nothing (table B.1).
		case r == 0x00AD || r == 0x034F || r == 0x1806 || (r >= 0x180B && r <= 0x180D) ||
			(r >= 0x200C && r <= 0x200F) || (r >= 0x202A && r <= 0x202E) ||
			(r >= 0x2060 && r <= 0x2063) || (r >= 0x206A && r <= 0x206F) ||
			r == 0xFEFF || (r >= 0xFE00 && r <= 0xFE0F) || r == 0xFFFC:
			// dropped
		// Prohibited: control, private use, non-characters, surrogates.
		case r < 0x20 || r == 0x7F || (r >= 0x80 && r <= 0x9F) ||
			(r >= 0xD800 && r <= 0xDFFF) || (r >= 0xE000 && r <= 0xF8FF) ||
			(r >= 0xFDD0 && r <= 0xFDEF) || (r&0xFFFF) == 0xFFFE || (r&0xFFFF) == 0xFFFF:
			return nil, fmt.Errorf("pgwire: password contains a character prohibited by SASLprep (U+%04X)", r)
		default:
			b.WriteRune(r)
		}
	}
	return []byte(b.String()), nil
}
