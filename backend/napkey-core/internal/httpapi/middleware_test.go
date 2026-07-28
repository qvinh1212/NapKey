package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"napkey-core/internal/auth"
	"napkey-core/internal/config"
)

// testConfig builds a config sufficient for exercising middleware.
func testConfig() *config.Config {
	return &config.Config{
		SessionSecret:     []byte(strings.Repeat("k", 48)),
		PublicBaseURL:     "https://console.napkey.vn",
		SecureCookies:     true,
		KiroAdminPassword: "internal-shared-secret",
		AdminEmails:       []string{"boss@napkey.vn"},
	}
}

// serverForMiddleware builds a Server with no database, which is enough for the
// checks that run before any query.
func serverForMiddleware() *Server {
	return &Server{cfg: testConfig(), trustProxy: 1}
}

func TestRequireSessionRejectsMissingCookie(t *testing.T) {
	s := serverForMiddleware()
	handler := s.requireSession(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the handler should not have run")
	})
	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest(http.MethodGet, "/v1/keys", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestRequireSessionRejectsForgedCookie(t *testing.T) {
	s := serverForMiddleware()
	handler := s.requireSession(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the handler should not have run")
	})

	// A cookie signed with the wrong secret must be rejected without a database
	// lookup, so junk cookies cannot be turned into query load.
	forged := auth.SignCookie([]byte(strings.Repeat("x", 48)), "sometoken")
	r := httptest.NewRequest(http.MethodGet, "/v1/keys", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: forged})
	w := httptest.NewRecorder()
	handler(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
	// The bad cookie should also be cleared.
	if !strings.Contains(w.Header().Get("Set-Cookie"), sessionCookieName+"=;") {
		t.Errorf("expected the session cookie to be cleared, got %q", w.Header().Get("Set-Cookie"))
	}
}

func TestCSRFRequiredOnUnsafeMethods(t *testing.T) {
	s := serverForMiddleware()

	// A state-changing request with no CSRF token must fail.
	r := httptest.NewRequest(http.MethodPost, "/v1/keys", nil)
	w := httptest.NewRecorder()
	if s.checkCSRF(w, r) {
		t.Error("a POST with no CSRF token should be rejected")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}

	// A mismatched token must fail: this is what a cross-site form submission looks
	// like, since another origin cannot read the cookie to echo it back.
	r = httptest.NewRequest(http.MethodPost, "/v1/keys", nil)
	r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "cookie-token"})
	r.Header.Set(csrfHeaderName, "different-token")
	w = httptest.NewRecorder()
	if s.checkCSRF(w, r) {
		t.Error("a mismatched CSRF token should be rejected")
	}

	// Matching cookie and header passes.
	r = httptest.NewRequest(http.MethodPost, "/v1/keys", nil)
	r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "matching-token"})
	r.Header.Set(csrfHeaderName, "matching-token")
	w = httptest.NewRecorder()
	if !s.checkCSRF(w, r) {
		t.Errorf("matching tokens should pass, got status %d", w.Code)
	}
}

func TestCSRFNotRequiredOnSafeMethods(t *testing.T) {
	s := serverForMiddleware()
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		r := httptest.NewRequest(method, "/v1/keys", nil)
		w := httptest.NewRecorder()
		if !s.checkCSRF(w, r) {
			t.Errorf("%s should not require a CSRF token", method)
		}
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	s := serverForMiddleware()
	w := httptest.NewRecorder()
	if err := s.setSessionCookies(w, "session-token-value", timeIn(24)); err != nil {
		t.Fatalf("setSessionCookies: %v", err)
	}
	cookies := w.Result().Cookies()

	var session, csrf *http.Cookie
	for _, c := range cookies {
		switch c.Name {
		case sessionCookieName:
			session = c
		case csrfCookieName:
			csrf = c
		}
	}
	if session == nil || csrf == nil {
		t.Fatalf("expected both cookies, got %d", len(cookies))
	}
	// HttpOnly is what keeps the session out of reach of any script on the page.
	if !session.HttpOnly {
		t.Error("the session cookie must be HttpOnly")
	}
	if !session.Secure {
		t.Error("the session cookie must be Secure when SECURE_COOKIES is on")
	}
	if session.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", session.SameSite)
	}
	// The CSRF cookie is readable by design: the frontend copies it into a header.
	if csrf.HttpOnly {
		t.Error("the CSRF cookie must be readable by script to support double-submit")
	}
	// The raw token must not be the cookie value; it is signed.
	if session.Value == "session-token-value" {
		t.Error("the session cookie should carry a signed value, not the bare token")
	}
	if got, err := auth.VerifyCookie(s.cfg.SessionSecret, session.Value); err != nil || got != "session-token-value" {
		t.Errorf("the signed cookie did not verify back to the token: %q, %v", got, err)
	}
}

func TestInternalAuthRequiresSharedSecret(t *testing.T) {
	s := serverForMiddleware()
	called := false
	handler := s.requireInternalAuth(func(w http.ResponseWriter, r *http.Request) { called = true })

	// No token.
	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest(http.MethodPost, "/internal/usage", nil))
	if w.Code != http.StatusUnauthorized || called {
		t.Error("a request with no internal token should be rejected")
	}

	// Wrong token.
	r := httptest.NewRequest(http.MethodPost, "/internal/usage", nil)
	r.Header.Set("X-Internal-Token", "guessed")
	w = httptest.NewRecorder()
	handler(w, r)
	if w.Code != http.StatusUnauthorized || called {
		t.Error("a wrong internal token should be rejected")
	}

	// Correct token.
	r = httptest.NewRequest(http.MethodPost, "/internal/usage", nil)
	r.Header.Set("X-Internal-Token", "internal-shared-secret")
	w = httptest.NewRecorder()
	handler(w, r)
	if !called {
		t.Errorf("the correct internal token should pass, got status %d", w.Code)
	}
}

func TestSecurityHeadersAreSet(t *testing.T) {
	s := serverForMiddleware()
	handler := s.securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	}
	for header, expected := range want {
		if got := w.Header().Get(header); got != expected {
			t.Errorf("%s = %q, want %q", header, got, expected)
		}
	}
	if w.Header().Get("Strict-Transport-Security") == "" {
		t.Error("HSTS should be set when cookies are Secure")
	}
}

func TestCORSAllowsOnlyTheConsoleOrigin(t *testing.T) {
	s := serverForMiddleware()
	handler := s.cors(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// The console origin is allowed with credentials.
	r := httptest.NewRequest(http.MethodGet, "/v1/keys", nil)
	r.Header.Set("Origin", "https://console.napkey.vn")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://console.napkey.vn" {
		t.Errorf("Allow-Origin = %q, want the console origin", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials = %q, want true", got)
	}

	// Any other origin gets no CORS headers. With credentialed requests, echoing an
	// arbitrary origin would let a third-party page read a logged-in user's data.
	for _, origin := range []string{
		"https://evil.com",
		"https://console.napkey.vn.evil.com",
		"http://console.napkey.vn",
	} {
		r = httptest.NewRequest(http.MethodGet, "/v1/keys", nil)
		r.Header.Set("Origin", origin)
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("origin %q was allowed (%q), want no CORS headers", origin, got)
		}
	}
}

func TestPanicIsRecovered(t *testing.T) {
	s := serverForMiddleware()
	handler := s.recoverPanic(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something went badly wrong")
	}))
	w := httptest.NewRecorder()
	// One bad request must not take the process down.
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/keys", nil))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	// The panic message must not reach the client.
	if strings.Contains(w.Body.String(), "something went badly wrong") {
		t.Error("the panic detail leaked into the response")
	}
}

func TestValidateEmail(t *testing.T) {
	valid := []string{
		"user@napkey.vn",
		"first.last@sub.example.co.uk",
		"user+tag@napkey.vn",
	}
	for _, email := range valid {
		if err := validateEmail(email); err != nil {
			t.Errorf("validateEmail(%q) = %v, want nil", email, err)
		}
	}
	invalid := []string{
		"",
		"not-an-email",
		"@napkey.vn",
		"user@",
		"user@@napkey.vn",
		"user name@napkey.vn",
		// A display name must be refused; only a bare address is stored.
		"NapKey User <user@napkey.vn>",
		strings.Repeat("a", 250) + "@napkey.vn",
	}
	for _, email := range invalid {
		if err := validateEmail(email); err == nil {
			t.Errorf("validateEmail(%q) = nil, want an error", email)
		}
	}
}
