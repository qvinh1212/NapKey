// Package casso verifies Casso Webhook V2 and extracts NapKey transfer memos.
package casso

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Transaction struct {
	ID          string
	Reference   string
	Description string
	AmountVND   int64
}

// ParseTransaction reads the fields used for reconciliation from a V2 envelope.
func ParseTransaction(raw []byte) (Transaction, error) {
	var envelope struct {
		Data struct {
			ID          json.Number `json:"id"`
			Reference   string      `json:"reference"`
			Description string      `json:"description"`
			Amount      json.Number `json:"amount"`
		} `json:"data"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&envelope); err != nil { return Transaction{}, err }
	amount, err := envelope.Data.Amount.Int64()
	if err != nil { return Transaction{}, errors.New("casso: amount must be a whole VND value") }
	if envelope.Data.ID.String() == "" { return Transaction{}, errors.New("casso: transaction id is missing") }
	return Transaction{ID: envelope.Data.ID.String(), Reference: envelope.Data.Reference, Description: envelope.Data.Description, AmountVND: amount}, nil
}

const maxWebhookSkew = 5 * time.Minute

func formatTimestamp(v int64) string { return strconv.FormatInt(v, 10) }

// VerifySignature implements Casso Webhook V2 canonical JSON signing.
func VerifySignature(raw []byte, header, secret string, now time.Time) error {
	if secret == "" {
		return errors.New("casso: webhook secret is not configured")
	}
	var timestampText, signatureText string
	for _, part := range strings.Split(header, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch key {
		case "t":
			timestampText = value
		case "v1":
			signatureText = value
		}
	}
	timestamp, err := strconv.ParseInt(timestampText, 10, 64)
	if err != nil {
		return errors.New("casso: invalid signature timestamp")
	}
	if delta := now.Sub(time.UnixMilli(timestamp)); delta > maxWebhookSkew || delta < -maxWebhookSkew {
		return errors.New("casso: webhook timestamp is outside the replay window")
	}
	provided, err := hex.DecodeString(signatureText)
	if err != nil || len(provided) != sha512.Size {
		return errors.New("casso: invalid signature value")
	}
	canonical, err := canonicalJSON(raw)
	if err != nil {
		return fmt.Errorf("casso: invalid JSON: %w", err)
	}
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write([]byte(timestampText))
	mac.Write([]byte("."))
	mac.Write(canonical)
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return errors.New("casso: signature mismatch")
	}
	return nil
}

func canonicalJSON(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := writeCanonical(&out, value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func writeCanonical(out *bytes.Buffer, value any) error {
	switch v := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		if v { out.WriteString("true") } else { out.WriteString("false") }
	case json.Number:
		out.WriteString(v.String())
	case string:
		encoded, _ := json.Marshal(v)
		out.Write(encoded)
	case []any:
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 { out.WriteByte(',') }
			if err := writeCanonical(out, item); err != nil { return err }
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v { keys = append(keys, key) }
		sort.Strings(keys)
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 { out.WriteByte(',') }
			encoded, _ := json.Marshal(key)
			out.Write(encoded)
			out.WriteByte(':')
			if err := writeCanonical(out, v[key]); err != nil { return err }
		}
		out.WriteByte('}')
	default:
		return fmt.Errorf("unsupported JSON value %T", value)
	}
	return nil
}

// ExtractMemoCode tolerates accents, whitespace and punctuation inserted by banks.
func ExtractMemoCode(description string) string {
	var normalized strings.Builder
	for _, r := range strings.ToUpper(description) {
		r = foldVietnamese(r)
		if r >= 'A' && r <= 'Z' || unicode.IsDigit(r) {
			normalized.WriteRune(r)
		}
	}
	s := normalized.String()
	for i := 0; i+8 <= len(s); i++ {
		candidate := s[i : i+8]
		if strings.HasPrefix(candidate, "NK") && crockford(candidate[2:]) {
			return candidate
		}
	}
	return ""
}

func crockford(value string) bool {
	if len(value) != 6 { return false }
	for _, r := range value {
		if !strings.ContainsRune("0123456789ABCDEFGHJKMNPQRSTVWXYZ", r) { return false }
	}
	return true
}

func foldVietnamese(r rune) rune {
	switch r {
	case 'À','Á','Ạ','Ả','Ã','Â','Ầ','Ấ','Ậ','Ẩ','Ẫ','Ă','Ằ','Ắ','Ặ','Ẳ','Ẵ': return 'A'
	case 'È','É','Ẹ','Ẻ','Ẽ','Ê','Ề','Ế','Ệ','Ể','Ễ': return 'E'
	case 'Ì','Í','Ị','Ỉ','Ĩ': return 'I'
	case 'Ò','Ó','Ọ','Ỏ','Õ','Ô','Ồ','Ố','Ộ','Ổ','Ỗ','Ơ','Ờ','Ớ','Ợ','Ở','Ỡ': return 'O'
	case 'Ù','Ú','Ụ','Ủ','Ũ','Ư','Ừ','Ứ','Ự','Ử','Ữ': return 'U'
	case 'Ỳ','Ý','Ỵ','Ỷ','Ỹ': return 'Y'
	case 'Đ': return 'D'
	}
	return r
}
