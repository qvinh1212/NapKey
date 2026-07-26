package casso

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"testing"
	"time"
)

func TestVerifySignatureAcceptsCanonicalEnvelope(t *testing.T) {
	body := []byte(`{"data":{"id":123,"description":"Nap NK7F3QK2"},"error":0}`)
	timestamp := time.Now().UnixMilli()
	canonical := `{"data":{"description":"Nap NK7F3QK2","id":123},"error":0}`
	mac := hmac.New(sha512.New, []byte("fixture-secret"))
	mac.Write([]byte(formatTimestamp(timestamp) + "." + canonical))
	header := "t=" + formatTimestamp(timestamp) + ",v1=" + hex.EncodeToString(mac.Sum(nil))

	if err := VerifySignature(body, header, "fixture-secret", time.Now()); err != nil {
		t.Fatalf("VerifySignature: %v", err)
	}
}

func TestVerifySignatureRejectsReplay(t *testing.T) {
	body := []byte(`{"data":{"id":123},"error":0}`)
	old := time.Now().Add(-10 * time.Minute)
	header := "t=" + formatTimestamp(old.UnixMilli()) + ",v1=" + string(make([]byte, 128))
	if err := VerifySignature(body, header, "fixture-secret", time.Now()); err == nil {
		t.Fatal("expected a stale webhook to be rejected")
	}
}

func TestExtractMemoCodeNormalizesBankDescription(t *testing.T) {
	got := ExtractMemoCode("CHUYỂN TIỀN TỪ A - ND: nk 7f3-qk2")
	if got != "NK7F3QK2" {
		t.Fatalf("ExtractMemoCode = %q, want NK7F3QK2", got)
	}
}
