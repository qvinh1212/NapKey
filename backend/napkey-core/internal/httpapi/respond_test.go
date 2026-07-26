package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONRejectsUnknownFields(t *testing.T) {
	// A typo'd field name has to be an error. Silently ignoring "enable" when the
	// field is "enabled" would mean a user asking to disable a key gets nothing.
	body := `{"email":"a@napkey.vn","passsword":"typo"}`
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	var dst struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if decodeJSON(w, r, &dst) {
		t.Fatal("expected an unknown field to be rejected")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestDecodeJSONRejectsWrongContentType(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	var dst map[string]any
	if decodeJSON(w, r, &dst) {
		t.Fatal("expected a non-JSON content type to be rejected")
	}
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want 415", w.Code)
	}
}

func TestDecodeJSONRejectsOversizedBody(t *testing.T) {
	// Without a cap, a large body is a cheap way to make the process allocate.
	huge := `{"email":"` + strings.Repeat("a", maxBodyBytes+1024) + `"}`
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(huge))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	var dst map[string]any
	if decodeJSON(w, r, &dst) {
		t.Fatal("expected an oversized body to be rejected")
	}
}

func TestDecodeJSONRejectsTrailingContent(t *testing.T) {
	// Two objects in one body means what was parsed is not what was sent.
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/login",
		strings.NewReader(`{"email":"a@napkey.vn"}{"email":"b@napkey.vn"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	var dst struct {
		Email string `json:"email"`
	}
	if decodeJSON(w, r, &dst) {
		t.Fatal("expected trailing JSON content to be rejected")
	}
}

func TestClientIPIgnoresForwardedHeaderWhenUntrusted(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.9:44444"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")

	// Trusting this header without a proxy in front lets anyone forge the identifier
	// that rate limiting keys on, which makes the limit meaningless.
	if got := clientIP(r, false); got != "203.0.113.9" {
		t.Errorf("clientIP(untrusted) = %q, want the socket address", got)
	}
	if got := clientIP(r, true); got != "1.2.3.4" {
		t.Errorf("clientIP(trusted) = %q, want the forwarded address", got)
	}
}

func TestClientIPRejectsMalformedForwardedValue(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.9:44444"
	r.Header.Set("X-Forwarded-For", "not-an-ip, 5.6.7.8")
	// A junk value must not become the rate-limit key.
	if got := clientIP(r, true); got != "203.0.113.9" {
		t.Errorf("clientIP = %q, want a fallback to the socket address", got)
	}
}

func TestClientIPUsesLeftmostForwardedEntry(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "198.51.100.7, 10.0.0.2, 10.0.0.3")
	if got := clientIP(r, true); got != "198.51.100.7" {
		t.Errorf("clientIP = %q, want the original client", got)
	}
}

func TestParsePaginationClampsValues(t *testing.T) {
	tests := []struct {
		query      string
		wantLimit  int
		wantOffset int
	}{
		{"", 50, 0},
		{"?limit=10&offset=20", 10, 20},
		{"?limit=99999", 200, 0},
		{"?limit=-5", 50, 0},
		{"?offset=-5", 50, 0},
		{"?limit=abc", 50, 0},
	}
	for _, tc := range tests {
		r := httptest.NewRequest(http.MethodGet, "/v1/admin/users"+tc.query, nil)
		limit, offset := parsePagination(r, 50, 200)
		if limit != tc.wantLimit || offset != tc.wantOffset {
			t.Errorf("query %q gave (%d, %d), want (%d, %d)",
				tc.query, limit, offset, tc.wantLimit, tc.wantOffset)
		}
	}
}

func TestWriteJSONSetsNoStore(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusOK, map[string]string{"a": "b"})
	// Everything here is per-session data, so nothing should be cached by a proxy.
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
}
