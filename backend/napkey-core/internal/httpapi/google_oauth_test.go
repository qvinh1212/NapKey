package httpapi

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"napkey-core/internal/auth"
	"napkey-core/internal/pgtest"
)

// googleReady turns Google sign-in on and lets the attempt counters answer.
func googleReady(h *harness) {
	h.server.cfg.GoogleClientID = "client-id.apps.googleusercontent.com"
	h.server.cfg.GoogleClientSecret = "client-secret"
	h.pg.On("count(*) FROM auth_attempts", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "count", OID: 20}},
			Rows:    [][]*string{{pgtest.Text("0")}},
			Tag:     "SELECT 1",
		}
	})
	h.pg.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "INSERT 0 1"} })
}

// oauthCookies pulls the state and verifier cookies out of a start response.
func oauthCookies(t *testing.T, header http.Header) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, raw := range header.Values("Set-Cookie") {
		name, rest, ok := strings.Cut(raw, "=")
		if !ok {
			continue
		}
		value, _, _ := strings.Cut(rest, ";")
		out[name] = value
	}
	return out
}

func TestGoogleStartRedirectsWithPKCEAndSignedState(t *testing.T) {
	h := newHarness(t)
	googleReady(h)

	w := h.do(http.MethodGet, "/v1/auth/google/start?locale=en", nil)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body: %s", w.Code, w.Body.String())
	}
	target, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parsing redirect: %v", err)
	}
	if target.Host != "accounts.google.com" {
		t.Fatalf("redirect host = %q, want accounts.google.com", target.Host)
	}
	q := target.Query()
	// The secret must never appear in a URL the browser can read.
	if strings.Contains(w.Header().Get("Location"), "client-secret") {
		t.Fatal("the client secret leaked into the authorization redirect")
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Fatalf("PKCE is missing from the redirect: %v", q)
	}
	if q.Get("redirect_uri") != "https://console.napkey.vn/api/v1/auth/google/callback" {
		t.Fatalf("redirect_uri = %q", q.Get("redirect_uri"))
	}

	cookies := oauthCookies(t, w.Header())
	signedState, ok := cookies[googleStateCookie]
	if !ok {
		t.Fatal("the state cookie was not issued")
	}
	// State lives in a signed cookie, so a caller cannot mint one to replay later.
	stateValue, err := auth.VerifyCookie(h.server.cfg.SessionSecret, signedState)
	if err != nil {
		t.Fatalf("the state cookie is not signed by this server: %v", err)
	}
	expected, locale, _ := strings.Cut(stateValue, ":")
	if expected != q.Get("state") {
		t.Fatal("the state in the cookie does not match the state sent to Google")
	}
	if locale != "en" {
		t.Fatalf("locale carried through state = %q, want en", locale)
	}

	// The verifier must hash to the challenge, otherwise Google rejects the code.
	signedVerifier, ok := cookies[googleVerifierCookie]
	if !ok {
		t.Fatal("the verifier cookie was not issued")
	}
	verifier, err := auth.VerifyCookie(h.server.cfg.SessionSecret, signedVerifier)
	if err != nil {
		t.Fatalf("the verifier cookie is not signed: %v", err)
	}
	sum := sha256.Sum256([]byte(verifier))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != q.Get("code_challenge") {
		t.Fatal("the challenge does not derive from the stored verifier")
	}
}

func TestGoogleStartFallsBackToSignInWhenUnconfigured(t *testing.T) {
	h := newHarness(t)
	// A browser navigates here, so an unconfigured provider has to land back on the
	// sign-in page rather than render a JSON error in the address bar.
	w := h.do(http.MethodGet, "/v1/auth/google/start?locale=en", nil)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Location"); got != "/en/signin?oauth_error=unconfigured" {
		t.Fatalf("Location = %q", got)
	}
}

func TestGoogleStartIsRateLimitedPerAddress(t *testing.T) {
	h := newHarness(t)
	h.server.cfg.GoogleClientID = "client-id"
	h.server.cfg.GoogleClientSecret = "client-secret"
	h.pg.On("count(*) FROM auth_attempts", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "count", OID: 20}},
			Rows:    [][]*string{{pgtest.Text("999")}},
			Tag:     "SELECT 1",
		}
	})
	h.pg.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "INSERT 0 1"} })

	w := h.do(http.MethodGet, "/v1/auth/google/start", nil)
	if w.Header().Get("Location") != "/vi/signin?oauth_error=rate_limited" {
		t.Fatalf("Location = %q, want the throttled sign-in page", w.Header().Get("Location"))
	}
	if strings.Contains(w.Header().Get("Location"), "accounts.google.com") {
		t.Fatal("a throttled start still redirected to Google")
	}
}

func TestGoogleCallbackRejectsForgedState(t *testing.T) {
	h := newHarness(t)
	googleReady(h)

	tests := map[string]func(*http.Request){
		"no cookies": func(*http.Request) {},
		"unsigned state cookie": func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: googleStateCookie, Value: "attacker-state:vi"})
			r.AddCookie(&http.Cookie{Name: googleVerifierCookie, Value: "attacker-verifier"})
		},
		"signed cookie with mismatched query state": func(r *http.Request) {
			r.AddCookie(&http.Cookie{
				Name:  googleStateCookie,
				Value: auth.SignCookie(h.server.cfg.SessionSecret, "real-state:vi"),
			})
			r.AddCookie(&http.Cookie{
				Name:  googleVerifierCookie,
				Value: auth.SignCookie(h.server.cfg.SessionSecret, "real-verifier"),
			})
		},
	}
	for name, decorate := range tests {
		t.Run(name, func(t *testing.T) {
			w := h.do(http.MethodGet, "/v1/auth/google/callback?code=abc&state=attacker-state", nil, decorate)
			if w.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want 303; body: %s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Header().Get("Location"), "oauth_error=") {
				t.Fatalf("Location = %q, want an oauth_error redirect", w.Header().Get("Location"))
			}
			// No session may be issued from a callback that failed state checks.
			if strings.Contains(strings.Join(w.Header().Values("Set-Cookie"), " "), sessionCookieName+"=napkey") {
				t.Fatal("a forged callback issued a session cookie")
			}
			if _, ok := h.pg.FindQuery("INSERT INTO sessions"); ok {
				t.Fatal("a forged callback created a session row")
			}
		})
	}
}

func TestGoogleCallbackNeverEchoesUnknownErrorCodes(t *testing.T) {
	h := newHarness(t)
	googleReady(h)
	// Google's own error parameter is attacker-influenced, so it must not reach the
	// redirect target. Only this server's fixed vocabulary may appear.
	w := h.do(http.MethodGet, "/v1/auth/google/callback?error=%3Cscript%3E&state=x", nil)
	location := w.Header().Get("Location")
	if strings.Contains(location, "script") || strings.Contains(location, "<") {
		t.Fatalf("Location = %q, want no caller-supplied content", location)
	}
	if !strings.HasPrefix(location, "/vi/signin?oauth_error=") {
		t.Fatalf("Location = %q", location)
	}
}

func TestGoogleFlowCookiesAreScopedToTheCallback(t *testing.T) {
	h := newHarness(t)
	googleReady(h)

	w := h.do(http.MethodGet, "/v1/auth/google/start", nil)
	for _, raw := range w.Header().Values("Set-Cookie") {
		if !strings.HasPrefix(raw, googleStateCookie) && !strings.HasPrefix(raw, googleVerifierCookie) {
			continue
		}
		// A narrow path keeps the flow cookies off every other request, and HttpOnly
		// keeps them out of reach of page scripts.
		if !strings.Contains(raw, "Path=/api/v1/auth/google/callback") {
			t.Errorf("cookie %q is not scoped to the callback path", raw)
		}
		if !strings.Contains(raw, "HttpOnly") {
			t.Errorf("cookie %q is readable by scripts", raw)
		}
		if !strings.Contains(raw, "SameSite=Lax") {
			t.Errorf("cookie %q lacks SameSite=Lax, so it would not survive the return trip safely", raw)
		}
	}
}
