package store

import (
	"context"
	"strings"
	"testing"

	"napkey-core/internal/pgtest"
)

// TestMigrateAppliesPendingMigrations drives the migration runner end to end
// against the fake server, verifying the sequence it issues: advisory lock,
// bookkeeping table, per-migration transaction, then unlock.
func TestMigrateAppliesPendingMigrations(t *testing.T) {
	srv := pgtest.New(t)
	// No rows in schema_migrations means everything is pending.
	srv.On("SELECT version, checksum FROM schema_migrations", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "version", OID: 23}, {Name: "checksum"}},
			Rows:    nil,
			Tag:     "SELECT 0",
		}
	})
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "CREATE TABLE"} })

	st, err := Open(context.Background(), srv.DSN(), 2, 1)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	if err := Migrate(context.Background(), st.DB()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	queries := srv.Queries()
	var sawLock, sawUnlock, sawTable, sawInsert, sawUsers bool
	for _, q := range queries {
		switch {
		case strings.Contains(q.SQL, "pg_advisory_lock"):
			sawLock = true
		case strings.Contains(q.SQL, "pg_advisory_unlock"):
			sawUnlock = true
		case strings.Contains(q.SQL, "CREATE TABLE IF NOT EXISTS schema_migrations"):
			sawTable = true
		case strings.Contains(q.SQL, "INSERT INTO schema_migrations"):
			sawInsert = true
		case strings.Contains(q.SQL, "CREATE TABLE users"):
			sawUsers = true
		}
	}
	// The advisory lock is what stops two replicas starting at once from both
	// trying to create the same tables.
	if !sawLock {
		t.Error("the migration runner should take an advisory lock")
	}
	if !sawUnlock {
		t.Error("the advisory lock should be released")
	}
	if !sawTable {
		t.Error("schema_migrations should be created if absent")
	}
	if !sawUsers {
		t.Error("the first migration should have run")
	}
	if !sawInsert {
		t.Error("applied migrations should be recorded")
	}
}

// TestMigrateSkipsAlreadyAppliedMigrations verifies a restart is a no-op.
func TestMigrateSkipsAlreadyAppliedMigrations(t *testing.T) {
	loaded, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}

	srv := pgtest.New(t)
	srv.On("SELECT version, checksum FROM schema_migrations", func(pgtest.Query) pgtest.Response {
		rows := make([][]*string, 0, len(loaded))
		for _, m := range loaded {
			rows = append(rows, []*string{
				pgtest.Text(itoa(m.version)),
				pgtest.Text(m.checksum),
			})
		}
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "version", OID: 23}, {Name: "checksum"}},
			Rows:    rows,
			Tag:     "SELECT " + itoa(len(rows)),
		}
	})
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "SELECT 1"} })

	st, err := Open(context.Background(), srv.DSN(), 2, 1)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	if err := Migrate(context.Background(), st.DB()); err != nil {
		t.Fatalf("Migrate on an up-to-date schema: %v", err)
	}
	// Nothing should be re-applied.
	if _, ok := srv.FindQuery("CREATE TABLE users"); ok {
		t.Error("an already-applied migration was run again")
	}
	if _, ok := srv.FindQuery("INSERT INTO schema_migrations"); ok {
		t.Error("nothing should be recorded when no migration is pending")
	}
}

// TestMigrateRefusesModifiedMigration is the guard against editing a migration
// after it shipped, which would leave environments on schemas nobody can
// reproduce.
func TestMigrateRefusesModifiedMigration(t *testing.T) {
	loaded, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}

	srv := pgtest.New(t)
	srv.On("SELECT version, checksum FROM schema_migrations", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "version", OID: 23}, {Name: "checksum"}},
			Rows: [][]*string{{
				pgtest.Text(itoa(loaded[0].version)),
				// A checksum that does not match the file on disk.
				pgtest.Text(strings.Repeat("0", 64)),
			}},
			Tag: "SELECT 1",
		}
	})
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "SELECT 1"} })

	st, err := Open(context.Background(), srv.DSN(), 2, 1)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	err = Migrate(context.Background(), st.DB())
	if err == nil {
		t.Fatal("expected a modified migration to abort startup")
	}
	if !strings.Contains(err.Error(), "modified after it was applied") {
		t.Errorf("unexpected error: %v", err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
