package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"napkey-core/internal/pgtest"
	"napkey-core/internal/payos"
)

func TestPayOSWebhookRejectsInvalidSignatureBeforeDatabaseWrites(t *testing.T) {
	h := newHarness(t)
	h.server.cfg.PayOSChecksumKey = "checksum-secret"
	body := map[string]any{
		"code": "00", "success": true, "signature": "00",
		"data": map[string]any{"orderCode": json.Number("123456"), "amount": json.Number("45000"), "paymentLinkId": "link-1", "reference": "FT1"},
	}
	w := h.do(http.MethodPost, "/webhooks/payos", body)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body: %s", w.Code, w.Body.String())
	}
	if _, ok := h.pg.FindQuery("INSERT INTO payment_events"); ok {
		t.Fatal("an unverified PayOS webhook reached the payment ledger")
	}
}

func TestPayOSWebhookSignatureCoversAmount(t *testing.T) {
	data := map[string]any{"amount": json.Number("45000"), "orderCode": json.Number("123456"), "paymentLinkId": "link-1", "reference": "FT1"}
	signature, err := payos.SignWebhookData(data, "checksum-secret")
	if err != nil {
		t.Fatal(err)
	}
	data["amount"] = json.Number("90000")
	if err := payos.VerifyWebhookData(data, signature, "checksum-secret"); err == nil {
		t.Fatal("changing the credited amount did not invalidate the signature")
	}
}

func TestUnauthenticatedRequestsAreRejected(t *testing.T) {
	h := newHarness(t)
	// Every console route must demand a session. A miss here is a data leak, so
	// the whole table is checked rather than a sample.
	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/keys"},
		{http.MethodPost, "/v1/keys"},
		{http.MethodGet, "/v1/keys/" + pgtest.UUID(1)},
		{http.MethodPatch, "/v1/keys/" + pgtest.UUID(1)},
		{http.MethodDelete, "/v1/keys/" + pgtest.UUID(1)},
		{http.MethodGet, "/v1/me/usage"},
		{http.MethodPost, "/v1/me/password"},
		{http.MethodGet, "/v1/auth/session"},
		{http.MethodGet, "/v1/admin/users"},
		{http.MethodGet, "/v1/admin/audit"},
		{http.MethodGet, "/v1/admin/business/summary"},
	}
	for _, route := range routes {
		w := h.do(route.method, route.path, nil)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s returned %d, want 401", route.method, route.path, w.Code)
		}
	}
}

func TestInternalUsageEndpointRequiresToken(t *testing.T) {
	h := newHarness(t)
	// This endpoint moves usage counters, so an unauthenticated caller could
	// inflate a customer's consumption.
	w := h.do(http.MethodPost, "/internal/usage",
		map[string]any{"requestId": "req-1", "keyId": pgtest.UUID(1), "inputTokens": 100})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestKeyAccessIsScopedToTheOwner(t *testing.T) {
	h := newHarness(t)
	const ownerID = "00000000-0000-4000-8000-000000000001"
	const otherKey = "00000000-0000-4000-8000-000000000099"
	h.sessionFor(ownerID, "owner@napkey.vn", "active", true)

	// The store filters by user_id, so another user's key looks absent.
	h.pg.On("WHERE k.id = $1 AND k.user_id = $2", func(q pgtest.Query) pgtest.Response {
		return pgtest.Response{Columns: keyColumns(), Rows: nil, Tag: "SELECT 0"}
	})

	w := h.do(http.MethodGet, "/v1/keys/"+otherKey, nil, h.authed("token"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("reading another user's key returned %d, want 404", w.Code)
	}

	// Confirm the owner id came from the session, not from anything client-supplied.
	q, ok := h.pg.FindQuery("WHERE k.id = $1 AND k.user_id = $2")
	if !ok {
		t.Fatal("the scoped query never ran")
	}
	if len(q.Params) != 2 || q.Params[1] != ownerID {
		t.Errorf("bound params = %v, want the session's user id in position 2", q.Params)
	}
}

func TestUnverifiedEmailCannotManageKeys(t *testing.T) {
	h := newHarness(t)
	h.sessionFor(pgtest.UUID(1), "new@napkey.vn", "active", false)

	// Gating key creation on verification is what stops a throwaway address from
	// minting credentials.
	w := h.do(http.MethodPost, "/v1/keys", map[string]any{"name": "test"}, h.authed("token"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if code := errorCode(t, w); code != codeEmailUnverified {
		t.Errorf("error code = %q, want %q", code, codeEmailUnverified)
	}
	// Nothing should have been pushed to the data plane.
	if len(h.planeLog) != 0 {
		t.Errorf("the data plane was called despite an unverified email: %+v", h.planeLog)
	}
}

func TestSuspendedUserIsRejected(t *testing.T) {
	h := newHarness(t)
	h.sessionFor(pgtest.UUID(1), "suspended@napkey.vn", "suspended", true)

	// A suspension has to bite on the next request rather than at session expiry,
	// otherwise a suspended account keeps spending.
	w := h.do(http.MethodGet, "/v1/keys", nil, h.authed("token"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

func TestNonAdminGetsNotFoundOnAdminRoutes(t *testing.T) {
	h := newHarness(t)
	h.sessionFor(pgtest.UUID(1), "regular@napkey.vn", "active", true)

	w := h.do(http.MethodGet, "/v1/admin/users", nil, h.authed("token"))
	// 404 rather than 403: confirming the route exists tells an attacker where to aim.
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestAdminRouteAllowsAllowlistedEmail(t *testing.T) {
	h := newHarness(t)
	h.sessionFor(pgtest.UUID(1), "boss@napkey.vn", "active", true)
	h.pg.On("FROM users u", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Columns: []pgtest.Column{
			{Name: "id"}, {Name: "email"}, {Name: "password_hash"},
			{Name: "email_verified_at", OID: 1184}, {Name: "status"},
			{Name: "created_at", OID: 1184}, {Name: "updated_at", OID: 1184},
			{Name: "key_count", OID: 20},
		}, Rows: nil, Tag: "SELECT 0"}
	})
	h.pg.On("count(*) FROM users", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "count", OID: 20}},
			Rows:    [][]*string{{pgtest.Text("0")}},
			Tag:     "SELECT 1",
		}
	})

	w := h.do(http.MethodGet, "/v1/admin/users", nil, h.authed("token"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestCSRFTokenIsRequiredForStateChange(t *testing.T) {
	h := newHarness(t)
	h.sessionFor(pgtest.UUID(1), "user@napkey.vn", "active", true)

	// A session cookie alone must not be enough for a write: that is exactly the
	// cross-site request a CSRF token exists to stop.
	w := h.do(http.MethodPost, "/v1/keys", map[string]any{"name": "x"}, func(r *http.Request) {
		r.AddCookie(&http.Cookie{
			Name:  sessionCookieName,
			Value: signFor(h, "token"),
		})
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

func TestCreateKeyReturnsCleartextOnceAndPushesToDataPlane(t *testing.T) {
	h := newHarness(t)
	const userID = "00000000-0000-4000-8000-000000000001"
	h.sessionFor(userID, "user@napkey.vn", "active", true)

	h.pg.On("SELECT status FROM users", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "status"}},
			Rows:    [][]*string{{pgtest.Text("active")}},
			Tag:     "SELECT 1",
		}
	})
	h.pg.On("count(*) FROM api_keys", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "count", OID: 20}},
			Rows:    [][]*string{{pgtest.Text("0")}},
			Tag:     "SELECT 1",
		}
	})
	h.pg.On("INSERT INTO api_keys", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{
				{Name: "id"}, {Name: "user_id"}, {Name: "name"}, {Name: "key_prefix"},
				{Name: "last_four"}, {Name: "enabled", OID: 16}, {Name: "rpm_limit", OID: 23},
				{Name: "tpm_limit", OID: 23}, {Name: "token_limit", OID: 20},
				{Name: "credit_limit", OID: 701}, {Name: "revoked_at", OID: 1184},
				{Name: "last_used_at", OID: 1184}, {Name: "created_at", OID: 1184},
				{Name: "sync_state"}, {Name: "sync_error"}, {Name: "synced_at", OID: 1184},
				{Name: "sync_attempts", OID: 23}, {Name: "remote_id"},
			},
			Rows: [][]*string{{
				pgtest.Text(pgtest.UUID(5)), pgtest.Text(userID), pgtest.Text("my key"),
				pgtest.Text("nk_live_"), pgtest.Text("ab12"), pgtest.Text("t"),
				pgtest.Null, pgtest.Null, pgtest.Text("0"), pgtest.Text("0"),
				pgtest.Null, pgtest.Null, pgtest.Text("2026-01-01 00:00:00+00"),
				pgtest.Text("pending"), pgtest.Text(""), pgtest.Null,
				pgtest.Text("0"), pgtest.Text(""),
			}},
			Tag: "INSERT 0 1",
		}
	})
	h.pg.On("WHERE k.id = $1 AND k.user_id = $2", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: keyColumns(),
			Rows:    [][]*string{keyRow(pgtest.UUID(5), userID, "my key", true, pgtest.Null)},
			Tag:     "SELECT 1",
		}
	})
	h.pg.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "UPDATE 1"} })

	w := h.do(http.MethodPost, "/v1/keys", map[string]any{"name": "my key"}, h.authed("token"))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	body := decode(t, w)
	cleartext, _ := body["key"].(string)
	// The cleartext is returned exactly once, at creation. Everything after this
	// only ever sees the hash.
	if !strings.HasPrefix(cleartext, "nk_live_") || len(cleartext) != len("nk_live_")+64 {
		t.Fatalf("returned key has the wrong shape: %q", cleartext)
	}

	// The same cleartext must reach the data plane, or the key would authenticate
	// nowhere.
	var pushed string
	for _, call := range h.planeLog {
		if call.Method == http.MethodPost && call.Path == "/admin/api/api-keys" {
			pushed, _ = call.Body["key"].(string)
		}
	}
	if pushed != cleartext {
		t.Errorf("the data plane received %q but the user got %q", pushed, cleartext)
	}

	// What goes into Postgres must be the hash, never the key itself.
	insert, ok := h.pg.FindQuery("INSERT INTO api_keys")
	if !ok {
		t.Fatal("the insert never ran")
	}
	for _, p := range insert.Params {
		if strings.Contains(p, cleartext) {
			t.Error("the cleartext key was bound into the INSERT")
		}
	}
}

func TestCreateKeyFailsClosedWhenDataPlaneIsDown(t *testing.T) {
	h := newHarness(t)
	const userID = "00000000-0000-4000-8000-000000000001"
	h.sessionFor(userID, "user@napkey.vn", "active", true)
	// Point the client at a dead port so the push fails.
	h.server.kiro = deadPlaneClient()

	h.pg.On("SELECT status FROM users", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "status"}},
			Rows:    [][]*string{{pgtest.Text("active")}},
			Tag:     "SELECT 1",
		}
	})
	h.pg.On("count(*) FROM api_keys", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "count", OID: 20}},
			Rows:    [][]*string{{pgtest.Text("0")}},
			Tag:     "SELECT 1",
		}
	})
	h.pg.On("INSERT INTO api_keys", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{
				{Name: "id"}, {Name: "user_id"}, {Name: "name"}, {Name: "key_prefix"},
				{Name: "last_four"}, {Name: "enabled", OID: 16}, {Name: "rpm_limit", OID: 23},
				{Name: "tpm_limit", OID: 23}, {Name: "token_limit", OID: 20},
				{Name: "credit_limit", OID: 701}, {Name: "revoked_at", OID: 1184},
				{Name: "last_used_at", OID: 1184}, {Name: "created_at", OID: 1184},
				{Name: "sync_state"}, {Name: "sync_error"}, {Name: "synced_at", OID: 1184},
				{Name: "sync_attempts", OID: 23}, {Name: "remote_id"},
			},
			Rows: [][]*string{{
				pgtest.Text(pgtest.UUID(5)), pgtest.Text(userID), pgtest.Text(""),
				pgtest.Text("nk_live_"), pgtest.Text("ab12"), pgtest.Text("t"),
				pgtest.Null, pgtest.Null, pgtest.Text("0"), pgtest.Text("0"),
				pgtest.Null, pgtest.Null, pgtest.Text("2026-01-01 00:00:00+00"),
				pgtest.Text("pending"), pgtest.Text(""), pgtest.Null,
				pgtest.Text("0"), pgtest.Text(""),
			}},
			Tag: "INSERT 0 1",
		}
	})
	h.pg.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "DELETE 1"} })

	w := h.do(http.MethodPost, "/v1/keys", map[string]any{"name": "x"}, h.authed("token"))
	// Handing back a key that authenticates nowhere would be worse than an error,
	// and the cleartext is gone after this request so it could never be retried.
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body: %s", w.Code, w.Body.String())
	}
	if code := errorCode(t, w); code != codeUpstreamFailure {
		t.Errorf("error code = %q, want %q", code, codeUpstreamFailure)
	}
	// The orphaned row must be cleaned up rather than left looking valid.
	if _, ok := h.pg.FindQuery("DELETE FROM api_keys"); !ok {
		t.Error("the unsynced key row should have been deleted")
	}
	// No cleartext key in the error response.
	if strings.Contains(w.Body.String(), "nk_live_") {
		t.Error("the response leaked a key value")
	}
}

func TestKeyNameValidation(t *testing.T) {
	h := newHarness(t)
	h.sessionFor(pgtest.UUID(1), "user@napkey.vn", "active", true)

	cases := map[string]any{
		"too long":     strings.Repeat("a", maxKeyNameLength+1),
		"with newline": "line1\nline2",
		"with NUL":     "abc\x00def",
	}
	for name, value := range cases {
		w := h.do(http.MethodPost, "/v1/keys", map[string]any{"name": value}, h.authed("token"))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, w.Code)
		}
	}
}

func TestUsageReportRejectsNegativeValues(t *testing.T) {
	h := newHarness(t)
	// Negative usage would subtract consumption already recorded, and in Stage 4 it
	// would credit the wallet. Each token kind is checked separately so a missed
	// field in the handler cannot pass by hiding behind a checked sibling.
	for _, field := range []string{"inputTokens", "outputTokens", "cacheReadTokens", "cacheWriteTokens"} {
		body := map[string]any{"requestId": "req-negative-" + field, "keyId": pgtest.UUID(1), field: -500}
		w := h.do(http.MethodPost, "/internal/usage", body,
			func(r *http.Request) { r.Header.Set("X-Internal-Token", "plane-secret") })
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400; body: %s", field, w.Code, w.Body.String())
		}
	}
	w := h.do(http.MethodPost, "/internal/usage",
		map[string]any{"requestId": "req-negative-credits", "keyId": pgtest.UUID(1), "credits": -0.01},
		func(r *http.Request) { r.Header.Set("X-Internal-Token", "plane-secret") })
	if w.Code != http.StatusBadRequest {
		t.Errorf("credits: status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

// TestUsageReportRequiresRequestID pins the idempotency contract. Without a request
// id the report cannot be deduplicated, and kiro-go retries on failure, so
// accepting one would mean a network blip bills a request twice.
func TestUsageReportRequiresRequestID(t *testing.T) {
	h := newHarness(t)
	w := h.do(http.MethodPost, "/internal/usage",
		map[string]any{"keyId": pgtest.UUID(1), "inputTokens": 100, "outputTokens": 50},
		func(r *http.Request) { r.Header.Set("X-Internal-Token", "plane-secret") })
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestUsageReportRejectsTimestampTooFarInTheFuture(t *testing.T) {
	h := newHarness(t)
	w := h.do(http.MethodPost, "/internal/usage", map[string]any{
		"requestId":  "req-from-the-future",
		"keyId":      pgtest.UUID(1),
		"model":      "claude-sonnet-4-20250514",
		"occurredAt": time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339),
	}, func(r *http.Request) { r.Header.Set("X-Internal-Token", "plane-secret") })

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

// TestUsageReportReturns503WhenStorageFails is the difference between losing
// revenue and retrying. A storage failure must not be reported as success, or the
// data plane discards a report for billable traffic it already served.
func TestUsageReportReturns503WhenStorageFails(t *testing.T) {
	h := newHarness(t)
	h.pg.On("SELECT user_id FROM api_keys", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{ErrCode: "57P03", ErrMessage: "the database system is shutting down"}
	})
	w := h.do(http.MethodPost, "/internal/usage",
		map[string]any{
			"requestId":   "req-storage-down",
			"keyId":       pgtest.UUID(1),
			"model":       "claude-sonnet-4-20250514",
			"inputTokens": 1000, "outputTokens": 500,
		},
		func(r *http.Request) { r.Header.Set("X-Internal-Token", "plane-secret") })
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 so the data plane retries; body: %s", w.Code, w.Body.String())
	}
}

// TestUsageReportIgnoresUnknownKey covers the opposite case: a key that no longer
// exists here can never be retried into existence, so the data plane is told to
// stop rather than retry forever.
func TestUsageReportIgnoresUnknownKey(t *testing.T) {
	h := newHarness(t)
	h.pg.On("SELECT user_id FROM api_keys", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Columns: []pgtest.Column{{Name: "user_id"}}, Tag: "SELECT 0"}
	})
	w := h.do(http.MethodPost, "/internal/usage",
		map[string]any{
			"requestId":   "req-unknown-key",
			"keyId":       pgtest.UUID(9),
			"model":       "claude-sonnet-4-20250514",
			"inputTokens": 10,
		},
		func(r *http.Request) { r.Header.Set("X-Internal-Token", "plane-secret") })
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if got := decode(t, w)["status"]; got != "ignored_unknown_key" {
		t.Errorf("status = %v, want ignored_unknown_key", got)
	}
}

// TestUsageReportRejectsClientSuppliedCost is a contract test. The data plane
// reports what it measured; the control plane decides what it costs. If a cost
// field were ever accepted here, the amount charged would become a function of what
// kiro-go claims, and kiro-go is the component handling untrusted traffic.
func TestUsageReportRejectsClientSuppliedCost(t *testing.T) {
	h := newHarness(t)
	for _, field := range []string{"costMicros", "cost", "priceMicrosPer1k"} {
		w := h.do(http.MethodPost, "/internal/usage",
			map[string]any{"requestId": "req-cost-" + field, "keyId": pgtest.UUID(1), field: 1},
			func(r *http.Request) { r.Header.Set("X-Internal-Token", "plane-secret") })
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s was accepted (status %d); pricing must not be caller-controlled", field, w.Code)
		}
	}
}

func TestHealthAndReadyAreOpen(t *testing.T) {
	h := newHarness(t)
	if w := h.do(http.MethodGet, "/health", nil); w.Code != http.StatusOK {
		t.Errorf("/health returned %d, want 200", w.Code)
	}
	h.pg.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "SELECT 1"} })
	// Readiness reports the data plane but does not fail on it: existing keys keep
	// working when only key creation is affected.
	if w := h.do(http.MethodGet, "/ready", nil); w.Code != http.StatusOK {
		t.Errorf("/ready returned %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestLoginDoesNotRevealWhetherEmailExists(t *testing.T) {
	h := newHarness(t)
	h.pg.On("FROM users WHERE email", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Columns: []pgtest.Column{
			{Name: "id"}, {Name: "email"}, {Name: "password_hash"},
			{Name: "email_verified_at", OID: 1184}, {Name: "status"},
			{Name: "created_at", OID: 1184}, {Name: "updated_at", OID: 1184},
		}, Rows: nil, Tag: "SELECT 0"}
	})
	h.pg.OnAny(func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "count", OID: 20}},
			Rows:    [][]*string{{pgtest.Text("0")}},
			Tag:     "SELECT 1",
		}
	})

	w := h.do(http.MethodPost, "/v1/auth/login", map[string]any{
		"email": "nobody@napkey.vn", "password": "some-password-here",
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	body := w.Body.String()
	// The message must be identical for an unknown address and a wrong password.
	if !strings.Contains(body, "email or password is incorrect") {
		t.Errorf("unexpected message, an enumeration hint may have leaked: %s", body)
	}
	for _, leak := range []string{"not found", "no such user", "unknown email"} {
		if strings.Contains(strings.ToLower(body), leak) {
			t.Errorf("response leaks account existence: %s", body)
		}
	}
}

func TestRegisterValidatesInput(t *testing.T) {
	h := newHarness(t)
	h.pg.OnAny(func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "count", OID: 20}},
			Rows:    [][]*string{{pgtest.Text("0")}},
			Tag:     "SELECT 1",
		}
	})

	cases := []struct {
		name  string
		email string
		pass  string
	}{
		{"bad email", "not-an-email", "a-good-long-password"},
		{"short password", "user@napkey.vn", "short"},
		{"common password", "user@napkey.vn", "password123"},
		{"empty both", "", ""},
	}
	for _, tc := range cases {
		w := h.do(http.MethodPost, "/v1/auth/register", map[string]any{
			"email": tc.email, "password": tc.pass,
		})
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400; body: %s", tc.name, w.Code, w.Body.String())
		}
	}
}

func TestVerifyEmailRejectsUnknownToken(t *testing.T) {
	h := newHarness(t)
	h.pg.On("UPDATE email_tokens", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Columns: []pgtest.Column{{Name: "user_id"}}, Rows: nil, Tag: "UPDATE 0"}
	})

	w := h.do(http.MethodPost, "/v1/auth/verify-email", map[string]any{"token": "made-up-token"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	// Expired, already used, and never existed all look the same, so a token cannot
	// be probed for validity.
	if !strings.Contains(w.Body.String(), "invalid or has expired") {
		t.Errorf("unexpected message: %s", w.Body.String())
	}
}

func TestLogoutClearsCookiesWithoutASession(t *testing.T) {
	h := newHarness(t)
	// Logout must work even with a stale cookie, otherwise a user with an expired
	// session cannot clear it.
	w := h.do(http.MethodPost, "/v1/auth/logout", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Header().Get("Set-Cookie"), sessionCookieName+"=;") {
		t.Errorf("the session cookie was not cleared: %q", w.Header().Values("Set-Cookie"))
	}
}

func TestLoginIsRateLimited(t *testing.T) {
	h := newHarness(t)
	// Report the per-address budget as already spent.
	h.pg.On("count(*) FROM auth_attempts", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "count", OID: 20}},
			Rows:    [][]*string{{pgtest.Text("999")}},
			Tag:     "SELECT 1",
		}
	})
	h.pg.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "INSERT 0 1"} })

	w := h.do(http.MethodPost, "/v1/auth/login", map[string]any{
		"email": "target@napkey.vn", "password": "guess-attempt-here",
	})
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body: %s", w.Code, w.Body.String())
	}
	if code := errorCode(t, w); code != codeRateLimited {
		t.Errorf("error code = %q, want %q", code, codeRateLimited)
	}
	// Retry-After tells a well-behaved client when to come back.
	if w.Header().Get("Retry-After") == "" {
		t.Error("a 429 should carry Retry-After")
	}
}

func TestRateLimitFailsOpenWhenCountingBreaks(t *testing.T) {
	h := newHarness(t)
	// If the counter query errors, the limiter must not lock everybody out. It is
	// abuse control, not an authorization boundary, and the credential check still
	// runs either way.
	h.pg.On("count(*) FROM auth_attempts", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{ErrCode: "42P01", ErrMessage: `relation "auth_attempts" does not exist`}
	})
	h.pg.On("FROM users WHERE email", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Columns: []pgtest.Column{
			{Name: "id"}, {Name: "email"}, {Name: "password_hash"},
			{Name: "email_verified_at", OID: 1184}, {Name: "status"},
			{Name: "created_at", OID: 1184}, {Name: "updated_at", OID: 1184},
		}, Rows: nil, Tag: "SELECT 0"}
	})
	h.pg.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "INSERT 0 1"} })

	w := h.do(http.MethodPost, "/v1/auth/login", map[string]any{
		"email": "user@napkey.vn", "password": "some-password-here",
	})
	// 401 means the request was actually evaluated rather than refused outright.
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (fail open); body: %s", w.Code, w.Body.String())
	}
}

func TestPasswordChangeRequiresCurrentPassword(t *testing.T) {
	h := newHarness(t)
	h.sessionFor(pgtest.UUID(1), "user@napkey.vn", "active", true)
	h.pg.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "UPDATE 1"} })

	// The stored hash in sessionFor does not match this password, so a stolen
	// session cannot be used to lock the real owner out.
	w := h.do(http.MethodPost, "/v1/me/password", map[string]any{
		"currentPassword": "not-the-real-password",
		"newPassword":     "a-brand-new-password",
	}, h.authed("token"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "currentPassword") {
		t.Errorf("the error should name the offending field: %s", w.Body.String())
	}
	if _, ok := h.pg.FindQuery("UPDATE users SET password_hash"); ok {
		t.Error("the password must not be updated when the current one is wrong")
	}
}

func TestAdminCannotSuspendOwnAccount(t *testing.T) {
	h := newHarness(t)
	const adminID = "00000000-0000-4000-8000-000000000001"
	h.sessionFor(adminID, "boss@napkey.vn", "active", true)
	h.pg.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "UPDATE 1"} })

	// Self-suspension would leave the env allowlist as the only way back in.
	w := h.do(http.MethodPost, "/v1/admin/users/"+adminID+"/status",
		map[string]any{"status": "suspended"}, h.authed("token"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestQuotaChangeRejectsKeyBelongingToAnotherUser(t *testing.T) {
	h := newHarness(t)
	h.sessionFor(pgtest.UUID(1), "boss@napkey.vn", "active", true)
	const targetUser = "00000000-0000-4000-8000-000000000050"
	const otherUser = "00000000-0000-4000-8000-000000000077"

	// The key exists but belongs to somebody else.
	h.pg.On("WHERE k.id = $1", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: keyColumns(),
			Rows:    [][]*string{keyRow(pgtest.UUID(9), otherUser, "theirs", true, pgtest.Null)},
			Tag:     "SELECT 1",
		}
	})
	h.pg.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "UPDATE 1"} })

	w := h.do(http.MethodPost, "/v1/admin/users/"+targetUser+"/quota",
		map[string]any{"keyId": pgtest.UUID(9), "tokenLimit": 1000000}, h.authed("token"))
	// Granting quota on the wrong account is a money mistake, so the mismatch is
	// refused rather than applied to whatever the key points at.
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "does not belong") {
		t.Errorf("unexpected message: %s", w.Body.String())
	}
}

func TestNegativeQuotaIsRejected(t *testing.T) {
	h := newHarness(t)
	h.sessionFor(pgtest.UUID(1), "boss@napkey.vn", "active", true)
	h.pg.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "UPDATE 1"} })

	for field, value := range map[string]any{"tokenLimit": -1, "creditLimit": -0.5} {
		body := map[string]any{"keyId": pgtest.UUID(9), field: value}
		w := h.do(http.MethodPost, "/v1/admin/users/"+pgtest.UUID(50)+"/quota", body, h.authed("token"))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s = %v: status = %d, want 400", field, value, w.Code)
		}
	}
}
