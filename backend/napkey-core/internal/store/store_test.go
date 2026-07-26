package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"napkey-core/internal/pgtest"
	"napkey-core/internal/pgwire"
	"napkey-core/internal/pricing"
)

// openTestStore connects a Store to a fake Postgres.
func openTestStore(t *testing.T, srv *pgtest.Server) *Store {
	t.Helper()
	st, err := Open(context.Background(), srv.DSN(), 2, 1)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// userRow builds a users row in the column order the queries select.
func userRow(id, email, hash, status string, verified *string) []*string {
	created := "2026-01-01 00:00:00+00"
	return []*string{
		pgtest.Text(id), pgtest.Text(email), pgtest.Text(hash),
		verified, pgtest.Text(status), pgtest.Text(created), pgtest.Text(created),
	}
}

var userColumns = []pgtest.Column{
	{Name: "id"}, {Name: "email"}, {Name: "password_hash"},
	{Name: "email_verified_at", OID: 1184}, {Name: "status"},
	{Name: "created_at", OID: 1184}, {Name: "updated_at", OID: 1184},
}

func TestCreateUserNormalizesEmail(t *testing.T) {
	srv := pgtest.New(t)
	srv.On("INSERT INTO users", func(q pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: userColumns,
			Rows:    [][]*string{userRow(pgtest.UUID(1), "user@napkey.vn", "hash", "active", nil)},
			Tag:     "INSERT 0 1",
		}
	})
	st := openTestStore(t, srv)

	user, err := st.CreateUser(context.Background(), "  User@NapKey.VN  ", "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if user.Email != "user@napkey.vn" {
		t.Errorf("Email = %q", user.Email)
	}

	q, ok := srv.FindQuery("INSERT INTO users")
	if !ok {
		t.Fatal("the insert never reached the server")
	}
	// Normalization must happen before the value is bound, so what is stored is
	// predictable regardless of how the user typed it.
	if len(q.Params) < 1 || q.Params[0] != "user@napkey.vn" {
		t.Errorf("bound email = %v, want the normalized form", q.Params)
	}
}

func TestCreateUserMapsUniqueViolation(t *testing.T) {
	srv := pgtest.New(t)
	srv.On("INSERT INTO users", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			ErrCode:       pgwire.CodeUniqueViolation,
			ErrMessage:    `duplicate key value violates unique constraint "users_email_key"`,
			ErrConstraint: "users_email_key",
		}
	})
	st := openTestStore(t, srv)

	// Relying on the constraint rather than a prior SELECT is what closes the race
	// between two simultaneous registrations.
	_, err := st.CreateUser(context.Background(), "taken@napkey.vn", "hash")
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("expected ErrEmailTaken, got: %v", err)
	}
}

func TestGetUserByEmailNotFound(t *testing.T) {
	srv := pgtest.New(t)
	srv.On("FROM users WHERE email", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Columns: userColumns, Rows: nil, Tag: "SELECT 0"}
	})
	st := openTestStore(t, srv)

	_, err := st.GetUserByEmail(context.Background(), "nobody@napkey.vn")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestUserVerificationAndStatusHelpers(t *testing.T) {
	verified := time.Now()
	u := User{Status: "active", EmailVerifiedAt: &verified}
	if !u.IsVerified() || !u.IsActive() {
		t.Error("an active verified user should report both")
	}
	u2 := User{Status: "suspended"}
	if u2.IsVerified() || u2.IsActive() {
		t.Error("a suspended unverified user should report neither")
	}
}

func TestListUsersStripsPasswordHash(t *testing.T) {
	srv := pgtest.New(t)
	cols := append([]pgtest.Column{}, userColumns...)
	cols = append(cols, pgtest.Column{Name: "key_count", OID: 20})
	srv.On("FROM users u", func(pgtest.Query) pgtest.Response {
		row := append(userRow(pgtest.UUID(1), "a@napkey.vn", "super-secret-hash", "active", nil), pgtest.Text("2"))
		return pgtest.Response{Columns: cols, Rows: [][]*string{row}, Tag: "SELECT 1"}
	})
	st := openTestStore(t, srv)

	users, err := st.ListUsers(context.Background(), 50, 0)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("got %d users, want 1", len(users))
	}
	// A list view has no business carrying password hashes toward the transport
	// layer, where a careless serialization would expose them.
	if users[0].PasswordHash != "" {
		t.Error("ListUsers should blank the password hash")
	}
	if users[0].KeyCount != 2 {
		t.Errorf("KeyCount = %d, want 2", users[0].KeyCount)
	}
}

func TestListUsersClampsLimit(t *testing.T) {
	srv := pgtest.New(t)
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "SELECT 0"} })
	st := openTestStore(t, srv)

	// An unbounded limit would let one request read the whole table.
	if _, err := st.ListUsers(context.Background(), 100000, -5); err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	q, ok := srv.FindQuery("FROM users u")
	if !ok {
		t.Fatal("query never arrived")
	}
	if q.Params[0] != "50" {
		t.Errorf("limit bound as %q, want the clamped 50", q.Params[0])
	}
	if q.Params[1] != "0" {
		t.Errorf("offset bound as %q, want 0", q.Params[1])
	}
}

func TestGetAPIKeyScopesToOwner(t *testing.T) {
	srv := pgtest.New(t)
	srv.On("WHERE k.id = $1 AND k.user_id = $2", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Columns: apiKeyColumns(), Rows: nil, Tag: "SELECT 0"}
	})
	st := openTestStore(t, srv)

	_, err := st.GetAPIKey(context.Background(), pgtest.UUID(1), pgtest.UUID(99))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}

	q, ok := srv.FindQuery("WHERE k.id = $1 AND k.user_id = $2")
	if !ok {
		t.Fatal("the ownership-scoped query never ran")
	}
	// Ownership is enforced in the WHERE clause. Loading by id and comparing in Go
	// is the shape that turns into an IDOR bug when someone forgets the comparison.
	if len(q.Params) != 2 {
		t.Fatalf("expected 2 bound parameters, got %v", q.Params)
	}
	if q.Params[0] != pgtest.UUID(99) || q.Params[1] != pgtest.UUID(1) {
		t.Errorf("bound parameters = %v, want key id then user id", q.Params)
	}
}

func TestRevokeAPIKeyRequiresOwnershipAndReportsMissing(t *testing.T) {
	srv := pgtest.New(t)
	srv.On("SET revoked_at = now()", func(pgtest.Query) pgtest.Response {
		// No rows matched: either the key does not exist or it belongs to somebody
		// else. Both must look the same to the caller.
		return pgtest.Response{Tag: "UPDATE 0"}
	})
	st := openTestStore(t, srv)

	err := st.RevokeAPIKey(context.Background(), pgtest.UUID(1), pgtest.UUID(2))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound when no row matched, got: %v", err)
	}

	q, _ := srv.FindQuery("SET revoked_at = now()")
	if !strings.Contains(q.SQL, "user_id = $2") {
		t.Error("revoke must be scoped by user_id")
	}
	// Revocation has to queue removal from the data plane, otherwise a leaked key
	// keeps working there.
	if !strings.Contains(q.SQL, "delete_pending") {
		t.Error("revoke should set sync_state to delete_pending")
	}
}

// TestRecordUsageRejectsNegativeTokens covers the Stage 3 replacement for the old
// counter clamp. Stage 2 clamped negatives with greatest(...,0) in SQL; the ledger
// refuses them outright in Go, before any statement runs, because in Stage 4 a
// negative cost credits the wallet and that makes it an attack on the balance
// rather than a data quality issue.
func TestRecordUsageRejectsNegativeTokens(t *testing.T) {
	srv := pgtest.New(t)
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "SELECT 0"} })
	st := openTestStore(t, srv)

	_, err := st.RecordUsage(context.Background(), RecordUsageParams{
		RequestID: "req-negative",
		KeyID:     pgtest.UUID(1),
		Model:     "claude-sonnet-4-20250514",
		Tokens:    pricing.Tokens{Input: -100},
	})
	if err == nil {
		t.Fatal("expected negative token counts to be rejected")
	}
	if _, ok := srv.FindQuery("INSERT INTO usage_records"); ok {
		t.Error("a negative report must not reach the ledger")
	}
}

func TestSetUserStatusRejectsUnknownStatus(t *testing.T) {
	srv := pgtest.New(t)
	st := openTestStore(t, srv)
	// Anything outside the enum would violate the CHECK constraint; catching it
	// here gives a clear error instead of a database failure.
	if err := st.SetUserStatus(context.Background(), pgtest.UUID(1), "deleted"); err == nil {
		t.Error("expected an unknown status to be rejected")
	}
	// The connection ping is expected; what must not appear is the UPDATE.
	if _, ok := srv.FindQuery("UPDATE users"); ok {
		t.Error("an invalid status should not reach the database")
	}
}

func TestConsumeEmailTokenIsSingleUse(t *testing.T) {
	srv := pgtest.New(t)
	calls := 0
	srv.On("UPDATE email_tokens", func(pgtest.Query) pgtest.Response {
		calls++
		if calls == 1 {
			return pgtest.Response{
				Columns: []pgtest.Column{{Name: "user_id"}},
				Rows:    [][]*string{{pgtest.Text(pgtest.UUID(7))}},
				Tag:     "UPDATE 1",
			}
		}
		// Second attempt matches nothing because consumed_at is now set.
		return pgtest.Response{Columns: []pgtest.Column{{Name: "user_id"}}, Rows: nil, Tag: "UPDATE 0"}
	})
	st := openTestStore(t, srv)

	userID, err := st.ConsumeEmailToken(context.Background(), []byte("hash"), "verify_email")
	if err != nil {
		t.Fatalf("first consume: %v", err)
	}
	if userID != pgtest.UUID(7) {
		t.Errorf("userID = %q", userID)
	}

	if _, err := st.ConsumeEmailToken(context.Background(), []byte("hash"), "verify_email"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a reused token should fail, got: %v", err)
	}

	q, _ := srv.FindQuery("UPDATE email_tokens")
	// The atomic UPDATE ... WHERE consumed_at IS NULL RETURNING is what makes this
	// single-use under concurrency; a SELECT-then-UPDATE would let two clicks both
	// succeed.
	for _, fragment := range []string{"consumed_at IS NULL", "expires_at > now()", "RETURNING user_id"} {
		if !strings.Contains(q.SQL, fragment) {
			t.Errorf("token consumption query is missing %q", fragment)
		}
	}
}

func TestLookupSessionFiltersExpired(t *testing.T) {
	srv := pgtest.New(t)
	srv.On("FROM sessions s", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Columns: sessionColumns(), Rows: nil, Tag: "SELECT 0"}
	})
	st := openTestStore(t, srv)

	_, err := st.LookupSession(context.Background(), []byte("hash"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
	q, _ := srv.FindQuery("FROM sessions s")
	// Expiry is enforced in SQL so a stale session cannot authenticate even if the
	// cleanup job is behind.
	if !strings.Contains(q.SQL, "expires_at > now()") {
		t.Error("session lookup must filter out expired rows in SQL")
	}
}

func TestWriteAuditEncodesMetadata(t *testing.T) {
	srv := pgtest.New(t)
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "INSERT 0 1"} })
	st := openTestStore(t, srv)

	err := st.WriteAudit(context.Background(), AuditEntry{
		ActorType: "user",
		ActorID:   pgtest.UUID(1),
		Action:    "api_key.create",
		Metadata:  map[string]any{"testMode": true},
		IP:        "203.0.113.9",
	})
	if err != nil {
		t.Fatalf("WriteAudit: %v", err)
	}
	q, ok := srv.FindQuery("INSERT INTO audit_logs")
	if !ok {
		t.Fatal("the audit insert never ran")
	}
	var sawMetadata bool
	for _, p := range q.Params {
		if strings.Contains(p, "testMode") {
			sawMetadata = true
		}
	}
	if !sawMetadata {
		t.Errorf("metadata was not bound: %v", q.Params)
	}
}

func TestWriteAuditRejectsMalformedIP(t *testing.T) {
	srv := pgtest.New(t)
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "INSERT 0 1"} })
	st := openTestStore(t, srv)

	// A malformed X-Forwarded-For must not fail the insert, since losing the audit
	// row would be worse than losing the address.
	err := st.WriteAudit(context.Background(), AuditEntry{
		ActorType: "system", Action: "test", IP: "not-an-ip",
	})
	if err != nil {
		t.Fatalf("WriteAudit with a bad IP should still succeed: %v", err)
	}
	q, _ := srv.FindQuery("INSERT INTO audit_logs")
	if q.Params[len(q.Params)-1] != "<NULL>" {
		t.Errorf("an unparseable IP should bind as NULL, got %q", q.Params[len(q.Params)-1])
	}
}

func TestNormalizeEmail(t *testing.T) {
	tests := map[string]string{
		"  User@NapKey.VN  ": "user@napkey.vn",
		"already@lower.vn":   "already@lower.vn",
		"":                   "",
	}
	for input, want := range tests {
		if got := NormalizeEmail(input); got != want {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestAPIKeyHelpers(t *testing.T) {
	revoked := time.Now()
	tests := []struct {
		name   string
		key    APIKey
		active bool
	}{
		{"enabled and not revoked", APIKey{Enabled: true}, true},
		{"disabled", APIKey{Enabled: false}, false},
		{"revoked", APIKey{Enabled: true, RevokedAt: &revoked}, false},
	}
	for _, tc := range tests {
		if got := tc.key.IsActive(); got != tc.active {
			t.Errorf("%s: IsActive() = %v, want %v", tc.name, got, tc.active)
		}
	}
	if !(&APIKey{KeyPrefix: "nk_test_"}).IsTestMode() {
		t.Error("an nk_test_ key should report test mode")
	}
	if (&APIKey{KeyPrefix: "nk_live_"}).IsTestMode() {
		t.Error("an nk_live_ key should not report test mode")
	}
}

// apiKeyColumns matches the column order of apiKeySelect.
func apiKeyColumns() []pgtest.Column {
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

func sessionColumns() []pgtest.Column {
	return []pgtest.Column{
		{Name: "token_hash", OID: 17}, {Name: "user_id"}, {Name: "created_at", OID: 1184},
		{Name: "expires_at", OID: 1184}, {Name: "last_seen_at", OID: 1184},
		{Name: "user_agent"}, {Name: "ip"},
		{Name: "id"}, {Name: "email"}, {Name: "password_hash"},
		{Name: "email_verified_at", OID: 1184}, {Name: "status"},
		{Name: "u_created_at", OID: 1184}, {Name: "u_updated_at", OID: 1184},
	}
}
