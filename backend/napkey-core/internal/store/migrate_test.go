package store

import (
	"strings"
	"testing"
)

func TestMigrationsLoadInOrder(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("no migrations were embedded")
	}
	for i := 1; i < len(migrations); i++ {
		if migrations[i-1].version >= migrations[i].version {
			t.Errorf("migrations are out of order at index %d: %d then %d",
				i, migrations[i-1].version, migrations[i].version)
		}
	}
	for _, m := range migrations {
		if m.name == "" {
			t.Errorf("migration %d has no name", m.version)
		}
		if strings.TrimSpace(m.sql) == "" {
			t.Errorf("migration %d is empty", m.version)
		}
		// The checksum is what detects a migration edited after being applied.
		if len(m.checksum) != 64 {
			t.Errorf("migration %d has checksum %q, want 64 hex characters", m.version, m.checksum)
		}
	}
}

func TestFirstMigrationCreatesExpectedTables(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	combined := strings.ToLower(strings.Join(sqlOf(migrations), "\n"))

	for _, table := range []string{"users", "sessions", "email_tokens", "api_keys", "api_key_usage", "audit_logs", "auth_attempts"} {
		if !strings.Contains(combined, "create table "+table) {
			t.Errorf("no CREATE TABLE for %s", table)
		}
	}

	for _, table := range []string{"wallets", "ledger_entries", "wallet_holds", "topup_orders", "payment_events"} {
		if !strings.Contains(combined, "create table "+table) {
			t.Errorf("no CREATE TABLE for Stage 4 table %s", table)
		}
	}
}

func TestSchemaEnforcesKeyHashUniqueness(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	combined := strings.ToLower(strings.Join(sqlOf(migrations), "\n"))

	// Two keys hashing to the same value would make key lookup ambiguous.
	if !strings.Contains(combined, "key_hash") || !strings.Contains(combined, "unique") {
		t.Error("api_keys.key_hash should carry a UNIQUE constraint")
	}
	// The cleartext key must never have a column to live in.
	for _, forbidden := range []string{"key_plaintext", "key_value text", "raw_key"} {
		if strings.Contains(combined, forbidden) {
			t.Errorf("schema contains %q, which would store a key in cleartext", forbidden)
		}
	}
	// citext is what makes email uniqueness case-insensitive in the database rather
	// than in application code, where it would race.
	if !strings.Contains(combined, "citext") {
		t.Error("users.email should be citext so uniqueness is case-insensitive")
	}
}

func sqlOf(migrations []migration) []string {
	out := make([]string, len(migrations))
	for i, m := range migrations {
		out[i] = m.sql
	}
	return out
}
