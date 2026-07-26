package pgwire

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

// openTestDB dials the fake server through database/sql, which is how the rest of
// the codebase reaches Postgres.
func openTestDB(t *testing.T, s *fakeServer) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgwire", s.dsn())
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	// One connection keeps the assertion about which query the server saw simple.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSCRAMAuthenticationSucceeds(t *testing.T) {
	s := newFakeServer(t, "s3cr3t-pw")
	db := openTestDB(t, s)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping over SCRAM: %v", err)
	}
}

func TestSCRAMRejectsWrongPassword(t *testing.T) {
	s := newFakeServer(t, "correct-password")
	// Point the driver at the same server with a different password.
	dsn := strings.Replace(s.dsn(), "correct-password", "wrong-password", 1)
	db, err := sql.Open("pgwire", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = db.PingContext(ctx)
	if err == nil {
		t.Fatal("expected authentication to fail with the wrong password")
	}
	// The failure must be the server rejecting the proof, not a protocol crash.
	if !strings.Contains(err.Error(), "28P01") && !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("expected an authentication error, got: %v", err)
	}
}

func TestCleartextAuthentication(t *testing.T) {
	s := newFakeServer(t, "plain-pw")
	s.authMethod = "cleartext"
	db := openTestDB(t, s)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping over cleartext auth: %v", err)
	}
}

func TestQueryWithParametersUsesExtendedProtocol(t *testing.T) {
	s := newFakeServer(t, "pw")
	s.setHandler(func(q fakeQuery) fakeResponse {
		if !strings.Contains(q.SQL, "SELECT id, email") {
			return fakeResponse{Tag: "SELECT 0"}
		}
		return fakeResponse{
			Fields: []fakeField{
				{Name: "id", OID: oidText},
				{Name: "email", OID: oidText},
			},
			Rows: [][][]byte{
				{[]byte("11111111-1111-1111-1111-111111111111"), []byte("a@napkey.vn")},
			},
			Tag: "SELECT 1",
		}
	})
	db := openTestDB(t, s)

	var id, email string
	err := db.QueryRow("SELECT id, email FROM users WHERE email = $1", "a@napkey.vn").Scan(&id, &email)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if email != "a@napkey.vn" {
		t.Fatalf("email = %q, want a@napkey.vn", email)
	}

	// The parameter must travel as a bound value, never spliced into the SQL.
	// That is the property that makes SQL injection structurally impossible here.
	var found bool
	for _, q := range s.recordedQueries() {
		if !strings.Contains(q.SQL, "SELECT id, email") {
			continue
		}
		found = true
		if !strings.Contains(q.SQL, "$1") {
			t.Errorf("SQL reached the server without a placeholder: %q", q.SQL)
		}
		if strings.Contains(q.SQL, "a@napkey.vn") {
			t.Errorf("parameter value was interpolated into the SQL: %q", q.SQL)
		}
		if len(q.Params) != 1 || string(q.Params[0]) != "a@napkey.vn" {
			t.Errorf("bound params = %q, want [a@napkey.vn]", q.Params)
		}
	}
	if !found {
		t.Fatal("server never received the query")
	}
}

func TestSQLInjectionAttemptStaysAParameter(t *testing.T) {
	s := newFakeServer(t, "pw")
	s.setHandler(func(fakeQuery) fakeResponse {
		return fakeResponse{
			Fields: []fakeField{{Name: "count", OID: oidInt8}},
			Rows:   [][][]byte{{[]byte("0")}},
			Tag:    "SELECT 1",
		}
	})
	db := openTestDB(t, s)

	// A classic payload. It must arrive as data, with the SQL text untouched.
	payload := "'; DROP TABLE users; --"
	var n int64
	if err := db.QueryRow("SELECT count(*) FROM users WHERE email = $1", payload).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	for _, q := range s.recordedQueries() {
		if strings.Contains(q.SQL, "DROP TABLE") {
			t.Fatalf("payload leaked into SQL text: %q", q.SQL)
		}
	}
	var sawPayload bool
	for _, q := range s.recordedQueries() {
		for _, p := range q.Params {
			if string(p) == payload {
				sawPayload = true
			}
		}
	}
	if !sawPayload {
		t.Error("payload should have arrived as a bound parameter")
	}
}

func TestExecReportsRowsAffected(t *testing.T) {
	s := newFakeServer(t, "pw")
	s.setHandler(func(fakeQuery) fakeResponse {
		return fakeResponse{Tag: "UPDATE 3"}
	})
	db := openTestDB(t, s)

	res, err := db.Exec("UPDATE api_keys SET enabled = false WHERE user_id = $1", "u1")
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		t.Fatalf("RowsAffected: %v", err)
	}
	if n != 3 {
		t.Fatalf("RowsAffected = %d, want 3", n)
	}
}

func TestUniqueViolationIsDetectable(t *testing.T) {
	s := newFakeServer(t, "pw")
	s.setHandler(func(fakeQuery) fakeResponse {
		return fakeResponse{
			ErrCode:       CodeUniqueViolation,
			ErrMessage:    `duplicate key value violates unique constraint "users_email_key"`,
			ErrConstraint: "users_email_key",
		}
	})
	db := openTestDB(t, s)

	_, err := db.Exec("INSERT INTO users (email) VALUES ($1)", "dup@napkey.vn")
	if err == nil {
		t.Fatal("expected a unique violation")
	}
	if !IsUniqueViolation(err) {
		t.Fatalf("IsUniqueViolation was false for: %v", err)
	}
	if !IsUniqueViolation(err, "users_email_key") {
		t.Error("IsUniqueViolation should match the named constraint")
	}
	if IsUniqueViolation(err, "some_other_key") {
		t.Error("IsUniqueViolation should not match an unrelated constraint")
	}
	if got := SQLState(err); got != CodeUniqueViolation {
		t.Errorf("SQLState = %q, want %q", got, CodeUniqueViolation)
	}
}

func TestBinaryDecodingRoundTrip(t *testing.T) {
	s := newFakeServer(t, "pw")

	bigint := binary.BigEndian.AppendUint64(nil, uint64(1_234_567_890_123))
	boolTrue := []byte{1}
	float := binary.BigEndian.AppendUint64(nil, math.Float64bits(3.5))
	// 2024-03-01T12:30:45Z expressed as microseconds since 2000-01-01.
	want := time.Date(2024, 3, 1, 12, 30, 45, 0, time.UTC)
	ts := binary.BigEndian.AppendUint64(nil, uint64(want.Sub(pgEpoch).Microseconds()))

	s.setHandler(func(fakeQuery) fakeResponse {
		return fakeResponse{
			Fields: []fakeField{
				{Name: "n", OID: oidInt8, Format: formatBinary},
				{Name: "flag", OID: oidBool, Format: formatBinary},
				{Name: "ratio", OID: oidFloat8, Format: formatBinary},
				{Name: "at", OID: oidTimestamptz, Format: formatBinary},
			},
			Rows: [][][]byte{{bigint, boolTrue, float, ts}},
			Tag:  "SELECT 1",
		}
	})
	db := openTestDB(t, s)

	var (
		n     int64
		flag  bool
		ratio float64
		at    time.Time
	)
	err := db.QueryRow("SELECT n, flag, ratio, at FROM t WHERE id = $1", 1).Scan(&n, &flag, &ratio, &at)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n != 1_234_567_890_123 {
		t.Errorf("n = %d", n)
	}
	if !flag {
		t.Error("flag should be true")
	}
	if ratio != 3.5 {
		t.Errorf("ratio = %v", ratio)
	}
	if !at.Equal(want) {
		t.Errorf("at = %v, want %v", at, want)
	}
}

func TestNullScansIntoPointer(t *testing.T) {
	s := newFakeServer(t, "pw")
	s.setHandler(func(fakeQuery) fakeResponse {
		return fakeResponse{
			Fields: []fakeField{{Name: "revoked_at", OID: oidTimestamptz, Format: formatBinary}},
			Rows:   [][][]byte{{nil}},
			Tag:    "SELECT 1",
		}
	})
	db := openTestDB(t, s)

	var revokedAt *time.Time
	if err := db.QueryRow("SELECT revoked_at FROM api_keys WHERE id = $1", "k1").Scan(&revokedAt); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if revokedAt != nil {
		t.Fatalf("expected NULL to scan as nil, got %v", revokedAt)
	}
}

func TestTransactionCommitAndRollback(t *testing.T) {
	s := newFakeServer(t, "pw")
	s.setHandler(func(fakeQuery) fakeResponse { return fakeResponse{Tag: "INSERT 0 1"} })
	db := openTestDB(t, s)

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec("INSERT INTO audit_logs (action) VALUES ($1)", "test"); err != nil {
		t.Fatalf("exec in tx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	tx2, err := db.Begin()
	if err != nil {
		t.Fatalf("begin 2: %v", err)
	}
	if err := tx2.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	var sawBegin, sawCommit, sawRollback bool
	for _, q := range s.recordedQueries() {
		switch {
		case strings.HasPrefix(q.SQL, "BEGIN"):
			sawBegin = true
		case q.SQL == "COMMIT":
			sawCommit = true
		case q.SQL == "ROLLBACK":
			sawRollback = true
		}
	}
	if !sawBegin || !sawCommit || !sawRollback {
		t.Errorf("transaction control missing: begin=%v commit=%v rollback=%v", sawBegin, sawCommit, sawRollback)
	}
}

func TestSerializableIsolationIsRequested(t *testing.T) {
	s := newFakeServer(t, "pw")
	s.setHandler(func(fakeQuery) fakeResponse { return fakeResponse{Tag: "SELECT 0"} })
	db := openTestDB(t, s)

	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatalf("begin serializable: %v", err)
	}
	defer tx.Rollback()

	var found bool
	for _, q := range s.recordedQueries() {
		if q.SQL == "BEGIN ISOLATION LEVEL SERIALIZABLE" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a SERIALIZABLE BEGIN, saw %v", s.recordedQueries())
	}
}

func TestReadOnlyTransaction(t *testing.T) {
	s := newFakeServer(t, "pw")
	s.setHandler(func(fakeQuery) fakeResponse { return fakeResponse{Tag: "SELECT 0"} })
	db := openTestDB(t, s)

	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("begin read-only: %v", err)
	}
	defer tx.Rollback()

	var found bool
	for _, q := range s.recordedQueries() {
		if strings.Contains(q.SQL, "READ ONLY") {
			found = true
		}
	}
	if !found {
		t.Error("expected READ ONLY in the BEGIN statement")
	}
}

func TestNulInParameterIsRejected(t *testing.T) {
	s := newFakeServer(t, "pw")
	db := openTestDB(t, s)

	// A NUL cannot be stored in a Postgres text column. Silently truncating would
	// let a crafted name shorten a value after validation ran on the full string.
	_, err := db.Exec("INSERT INTO api_keys (name) VALUES ($1)", "abc\x00def")
	if err == nil {
		t.Fatal("expected a NUL byte in a text parameter to be rejected")
	}
	if !strings.Contains(err.Error(), "NUL") {
		t.Fatalf("expected the error to mention NUL, got: %v", err)
	}
}

func TestContextCancellationStopsQuery(t *testing.T) {
	s := newFakeServer(t, "pw")
	db := openTestDB(t, s)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled

	_, err := db.QueryContext(ctx, "SELECT 1")
	if err == nil {
		t.Fatal("expected a canceled context to fail the query")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

func TestDSNParsing(t *testing.T) {
	tests := []struct {
		name    string
		dsn     string
		wantErr bool
		check   func(*testing.T, *dsnConfig)
	}{
		{
			name: "url form",
			dsn:  "postgres://napkey:pw@db.internal:5433/napkey?sslmode=require&application_name=core",
			check: func(t *testing.T, c *dsnConfig) {
				if c.host != "db.internal" || c.port != "5433" {
					t.Errorf("addr = %s", c.addr())
				}
				if c.user != "napkey" || c.password != "pw" || c.database != "napkey" {
					t.Errorf("credentials parsed wrong: %+v", c)
				}
				if c.sslMode != sslRequire {
					t.Errorf("sslmode = %s", c.sslMode)
				}
				if c.appName != "core" {
					t.Errorf("appName = %s", c.appName)
				}
			},
		},
		{
			name: "keyword form with quoted password",
			dsn:  "host=localhost user=napkey password='a b c' dbname=napkey sslmode=disable",
			check: func(t *testing.T, c *dsnConfig) {
				if c.password != "a b c" {
					t.Errorf("password = %q, want 'a b c'", c.password)
				}
				if c.sslMode != sslDisable {
					t.Errorf("sslmode = %s", c.sslMode)
				}
			},
		},
		{
			name: "password with url-encoded special characters",
			dsn:  "postgres://napkey:p%40ss%3Aword@localhost/napkey",
			check: func(t *testing.T, c *dsnConfig) {
				if c.password != "p@ss:word" {
					t.Errorf("password = %q, want p@ss:word", c.password)
				}
			},
		},
		{
			name:    "missing user",
			dsn:     "postgres://localhost/napkey",
			wantErr: true,
		},
		{
			name:    "bad sslmode",
			dsn:     "postgres://u:p@localhost/db?sslmode=maybe",
			wantErr: true,
		},
		{
			name:    "non-numeric port",
			dsn:     "postgres://u:p@localhost:abc/db",
			wantErr: true,
		},
		{
			name: "dbname defaults to user",
			dsn:  "postgres://napkey:pw@localhost/",
			check: func(t *testing.T, c *dsnConfig) {
				if c.database != "napkey" {
					t.Errorf("database = %q, want napkey", c.database)
				}
			},
		},
		{
			name:    "empty dsn",
			dsn:     "",
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := parseDSN(tc.dsn)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", tc.dsn)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDSN(%q): %v", tc.dsn, err)
			}
			if tc.check != nil {
				tc.check(t, cfg)
			}
		})
	}
}

func TestSASLPrepRejectsControlCharacters(t *testing.T) {
	if _, err := saslPrepare("pass\x01word"); err == nil {
		t.Error("expected a control character to be rejected")
	}
	if _, err := saslPrepare("pass\x00word"); err == nil {
		t.Error("expected a NUL to be rejected")
	}
	// A non-breaking space maps to a plain space so the password still matches.
	out, err := saslPrepare("a\u00A0b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "a b" {
		t.Errorf("saslPrepare mapped to %q, want %q", out, "a b")
	}
	// Plain ASCII must pass through byte for byte.
	out, err = saslPrepare("Str0ng-P@ssw0rd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "Str0ng-P@ssw0rd" {
		t.Errorf("ASCII password was altered: %q", out)
	}
}

func TestSCRAMRejectsLowIterationCount(t *testing.T) {
	c, err := newSCRAMClient("u", "pw", nil)
	if err != nil {
		t.Fatalf("newSCRAMClient: %v", err)
	}
	c.firstMessage()
	// An iteration count below the RFC floor makes offline cracking cheap; a
	// server asking for it is either broken or hostile.
	_, err = c.handleServerFirst("r=" + c.clientNonce + "extra,s=" + "c2FsdA==" + ",i=1000")
	if err == nil {
		t.Fatal("expected a low iteration count to be rejected")
	}
	if !strings.Contains(err.Error(), "4096") {
		t.Errorf("error should mention the minimum, got: %v", err)
	}
}

func TestSCRAMRejectsNonceThatDoesNotExtendClientNonce(t *testing.T) {
	c, err := newSCRAMClient("u", "pw", nil)
	if err != nil {
		t.Fatalf("newSCRAMClient: %v", err)
	}
	c.firstMessage()
	// A server nonce unrelated to ours means the exchange is not bound to this
	// session and could be replayed from another.
	_, err = c.handleServerFirst("r=totally-different,s=c2FsdA==,i=4096")
	if err == nil {
		t.Fatal("expected a mismatched server nonce to be rejected")
	}
}

func TestSCRAMRejectsBadServerSignature(t *testing.T) {
	c, err := newSCRAMClient("u", "pw", nil)
	if err != nil {
		t.Fatalf("newSCRAMClient: %v", err)
	}
	c.firstMessage()
	if _, err := c.handleServerFirst("r=" + c.clientNonce + "srv,s=c2FsdA==,i=4096"); err != nil {
		t.Fatalf("handleServerFirst: %v", err)
	}
	// Without this check a MITM only has to answer "ok" to be believed.
	if err := c.verifyServerFinal("v=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="); err == nil {
		t.Fatal("expected a forged server signature to be rejected")
	}
}

func TestReadBufTruncationDoesNotPanic(t *testing.T) {
	// A malformed backend message must surface as an error. Panicking inside a
	// database call would take down the request goroutine.
	r := &readBuf{buf: []byte{0x00, 0x01}}
	r.int32()
	if r.err == nil {
		t.Error("expected an error reading int32 from a 2-byte buffer")
	}
	r2 := &readBuf{buf: []byte("no-terminator")}
	r2.string()
	if r2.err == nil {
		t.Error("expected an error reading an unterminated string")
	}
	r3 := &readBuf{buf: []byte{1, 2, 3}}
	if got := r3.next(99); got != nil {
		t.Error("expected next() past the end to return nil")
	}
}

func TestLastInsertIdIsRefused(t *testing.T) {
	r := &result{}
	// Postgres has no general last-insert-id; returning 0 would look like a real
	// row id. Callers must use INSERT ... RETURNING.
	if _, err := r.LastInsertId(); err == nil {
		t.Error("expected LastInsertId to return an error")
	}
}

func TestCommandTagParsing(t *testing.T) {
	tests := map[string]int64{
		"INSERT 0 5": 5,
		"UPDATE 12":  12,
		"DELETE 3":   3,
		"SELECT 7":   7,
		"MERGE 2":    2,
	}
	for tag, want := range tests {
		r := &result{}
		r.applyTag(tag)
		got, err := r.RowsAffected()
		if err != nil {
			t.Fatalf("RowsAffected for %q: %v", tag, err)
		}
		if got != want {
			t.Errorf("tag %q gave %d, want %d", tag, got, want)
		}
	}
}
