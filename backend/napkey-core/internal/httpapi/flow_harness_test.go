package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"napkey-core/internal/auth"
	"napkey-core/internal/config"
	"napkey-core/internal/kiro"
	"napkey-core/internal/mail"
	"napkey-core/internal/pgtest"
	"napkey-core/internal/store"
)

// harness wires a Server to a fake Postgres and a fake data plane, then drives it
// through its real route table. These tests are the ones that would catch an
// authorization mistake, because they exercise the middleware chain rather than a
// handler in isolation.
type harness struct {
	t        *testing.T
	pg       *pgtest.Server
	plane    *httptest.Server
	server   *Server
	handler  http.Handler
	planeLog []planeCall
}

type planeCall struct {
	Method string
	Path   string
	Body   map[string]any
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, pg: pgtest.New(t)}

	h.plane = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		h.planeLog = append(h.planeLog, planeCall{Method: r.Method, Path: r.URL.Path, Body: body})
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/admin/api/api-keys":
			json.NewEncoder(w).Encode(map[string]any{"success": true, "id": "remote-1"})
		case r.URL.Path == "/admin/api/api-keys":
			json.NewEncoder(w).Encode(map[string]any{"apiKeys": []any{}})
		default:
			json.NewEncoder(w).Encode(map[string]any{"success": true})
		}
	}))
	t.Cleanup(h.plane.Close)

	st, err := store.Open(t.Context(), h.pg.DSN(), 4, 2)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	cfg := &config.Config{
		SessionSecret:     []byte(strings.Repeat("k", 48)),
		SessionTTL:        24 * time.Hour,
		PublicBaseURL:     "https://console.napkey.vn",
		SecureCookies:     false,
		KiroAdminURL:      h.plane.URL,
		KiroAdminPassword: "plane-secret",
		EmailTokenTTL:     24 * time.Hour,
		MaxKeysPerUser:    3,
		AdminEmails:       []string{"boss@napkey.vn"},
		MailProvider:      "log",
	}
	h.server = New(cfg, st, kiro.New(h.plane.URL, "plane-secret"), mail.LogSender{})
	h.handler = h.server.Handler()
	return h
}

// do issues a request through the full middleware chain.
func (h *harness) do(method, path string, body any, opts ...func(*http.Request)) *httptest.ResponseRecorder {
	h.t.Helper()
	var reader *strings.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("marshal: %v", err)
		}
		reader = strings.NewReader(string(encoded))
	} else {
		reader = strings.NewReader("")
	}
	r := httptest.NewRequest(method, path, reader)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	for _, opt := range opts {
		opt(r)
	}
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, r)
	return w
}

// authed attaches a valid session cookie and matching CSRF pair.
func (h *harness) authed(token string) func(*http.Request) {
	return func(r *http.Request) {
		r.AddCookie(&http.Cookie{
			Name:  sessionCookieName,
			Value: auth.SignCookie(h.server.cfg.SessionSecret, token),
		})
		r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-token"})
		r.Header.Set(csrfHeaderName, "csrf-token")
	}
}

// sessionFor makes the fake Postgres resolve a session token to the given user.
func (h *harness) sessionFor(userID, email, status string, verified bool) {
	verifiedAt := pgtest.Null
	if verified {
		verifiedAt = pgtest.Text("2026-01-01 00:00:00+00")
	}
	h.pg.On("FROM sessions s", func(pgtest.Query) pgtest.Response {
		created := "2026-01-01 00:00:00+00"
		expires := time.Now().Add(24 * time.Hour).UTC().Format("2006-01-02 15:04:05-07:00")
		return pgtest.Response{
			Columns: []pgtest.Column{
				{Name: "token_hash"}, {Name: "user_id"}, {Name: "created_at", OID: 1184},
				{Name: "expires_at", OID: 1184}, {Name: "last_seen_at", OID: 1184},
				{Name: "user_agent"}, {Name: "ip"},
				{Name: "id"}, {Name: "email"}, {Name: "password_hash"},
				{Name: "email_verified_at", OID: 1184}, {Name: "status"},
				{Name: "u_created", OID: 1184}, {Name: "u_updated", OID: 1184},
			},
			Rows: [][]*string{{
				pgtest.Text("hash"), pgtest.Text(userID), pgtest.Text(created),
				pgtest.Text(expires), pgtest.Text(created),
				pgtest.Text("Go-test"), pgtest.Text(""),
				pgtest.Text(userID), pgtest.Text(email), pgtest.Text("pbkdf2-sha256$1$c2FsdA$a2V5"),
				verifiedAt, pgtest.Text(status), pgtest.Text(created), pgtest.Text(created),
			}},
			Tag: "SELECT 1",
		}
	})
}

// decode unmarshals a JSON response body.
func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	out := map[string]any{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding response %q: %v", w.Body.String(), err)
	}
	return out
}

// errorCode pulls error.code out of a response.
func errorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	body := decode(t, w)
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		return ""
	}
	code, _ := errObj["code"].(string)
	return code
}

// keyColumns matches the column order of the store's apiKeySelect.
func keyColumns() []pgtest.Column {
	return []pgtest.Column{
		{Name: "id"}, {Name: "user_id"}, {Name: "name"}, {Name: "key_prefix"},
		{Name: "last_four"}, {Name: "enabled", OID: 16}, {Name: "rpm_limit", OID: 23},
		{Name: "tpm_limit", OID: 23}, {Name: "token_limit", OID: 20},
		{Name: "credit_limit", OID: 701}, {Name: "revoked_at", OID: 1184},
		{Name: "last_used_at", OID: 1184}, {Name: "created_at", OID: 1184},
		{Name: "sync_state"}, {Name: "sync_error"}, {Name: "synced_at", OID: 1184},
		{Name: "sync_attempts", OID: 23}, {Name: "remote_id"},
		{Name: "tokens_used", OID: 20}, {Name: "credits_used", OID: 701},
		{Name: "requests_count", OID: 20},
	}
}

// keyRow builds one api_keys row.
func keyRow(id, userID, name string, enabled bool, revoked *string) []*string {
	enabledText := "f"
	if enabled {
		enabledText = "t"
	}
	return []*string{
		pgtest.Text(id), pgtest.Text(userID), pgtest.Text(name), pgtest.Text("nk_live_"),
		pgtest.Text("ab12"), pgtest.Text(enabledText), pgtest.Null,
		pgtest.Null, pgtest.Text("0"),
		pgtest.Text("0"), revoked,
		pgtest.Null, pgtest.Text("2026-01-01 00:00:00+00"),
		pgtest.Text("synced"), pgtest.Text(""), pgtest.Text("2026-01-01 00:00:00+00"),
		pgtest.Text("0"), pgtest.Text("remote-1"),
		pgtest.Text("0"), pgtest.Text("0"), pgtest.Text("0"),
	}
}

// signFor signs a token with the harness server's secret.
func signFor(h *harness, token string) string {
	return auth.SignCookie(h.server.cfg.SessionSecret, token)
}

// deadPlaneClient points at a closed port, standing in for a data plane that is
// down.
func deadPlaneClient() *kiro.Client {
	return kiro.New("http://127.0.0.1:1", "plane-secret")
}
