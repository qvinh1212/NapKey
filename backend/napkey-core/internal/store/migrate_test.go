package store

import (
	"os"
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

func TestCreditRepricingPreservesExistingWalletCredits(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	combined := strings.ToLower(strings.Join(sqlOf(migrations), "\n"))
	for _, required := range []string{"wallet-credit-reprice-45-to-60", "kind, amount_micros", "'adjustment'"} {
		if !strings.Contains(combined, required) {
			t.Errorf("credit repricing migration is missing %q", required)
		}
	}
}

func TestCreditUsageCacheBackfillsFromLedger(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	combined := strings.ToLower(strings.Join(sqlOf(migrations), "\n"))
	for _, required := range []string{"add column credits_micros", "sum(credits_micros)", "update api_key_usage"} {
		if !strings.Contains(combined, required) {
			t.Errorf("credit usage cache migration is missing %q", required)
		}
	}
}

func TestTrialCreditSchemaEnforcesOneTimeGrants(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	combined := strings.ToLower(strings.Join(sqlOf(migrations), "\n"))
	for _, required := range []string{
		"create table trial_grants",
		"user_id uuid not null unique",
		"ip_hash bytea not null unique",
		"promotional_micros",
		"promotional_expires_at",
		"'trial'",
	} {
		if !strings.Contains(combined, required) {
			t.Errorf("trial credit migration is missing %q", required)
		}
	}
}

func TestGoogleIdentitySchemaUsesStableSubject(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	combined := strings.ToLower(strings.Join(sqlOf(migrations), "\n"))
	for _, required := range []string{
		"create table oauth_identities",
		"provider_subject text not null",
		"unique(provider, provider_subject)",
		"unique(user_id, provider)",
	} {
		if !strings.Contains(combined, required) {
			t.Errorf("Google identity migration is missing %q", required)
		}
	}
}

func TestWalletReconciliationIncludesTrialCredits(t *testing.T) {
	source, err := os.ReadFile("operations.go")
	if err != nil {
		t.Fatalf("read operations.go: %v", err)
	}
	if got := strings.Count(string(source), "'topup','trial','promotion','usage','refund','adjustment'"); got != 2 {
		t.Fatalf("wallet reconciliation includes promotional credits %d times, want 2", got)
	}
}

func TestFirstTopupBonusSchemaIsIdempotent(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil { t.Fatalf("loadMigrations: %v", err) }
	combined := strings.ToLower(strings.Join(sqlOf(migrations), "\n"))
	for _, required := range []string{"create table first_topup_bonus_grants", "user_id uuid not null", "topup_order_id uuid not null", "remaining_micros bigint not null", "first_topup_bonus_grants_expiry_idx", "unique", "'promotion'"} {
		if !strings.Contains(combined, required) { t.Errorf("first top-up promotion migration is missing %q", required) }
	}
}

func TestTopupMinimumSchemaMatchesApplicationMinimum(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	combined := strings.ToLower(strings.Join(sqlOf(migrations), "\n"))
	for _, required := range []string{
		"drop constraint topup_expected_check",
		"check (expected_amount_micros >= 10000000000)",
	} {
		if !strings.Contains(combined, required) {
			t.Errorf("10,000 VND top-up migration is missing %q", required)
		}
	}
}

func TestPricingMigrationPreservesCreditQuantitiesAndDisablesNewBonus(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil { t.Fatalf("loadMigrations: %v", err) }
	combined := strings.ToLower(strings.Join(sqlOf(migrations), "\n"))
	for _, required := range []string{
		"\"oldvndpercredit\":75",
		"\"newvndpercredit\":400",
		"\"reason\":\"preserve_existing_credits\"",
	} {
		if !strings.Contains(combined, required) { t.Errorf("pricing migration is missing %q", required) }
	}
}

func TestRetailRepricePreservesExistingCreditQuantities(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	combined := strings.ToLower(strings.Join(sqlOf(migrations), "\n"))
	for _, required := range []string{
		"alter column retail_vnd_per_credit set default 75",
		"where status <> 'paid'",
		"and status <> 'underpaid'",
		"received_amount_micros = 0",
		"wallet-credit-reprice-60-to-75",
		"promotional_micros",
		"update wallet_holds",
		"update trial_grants",
		"lock table wallets, wallet_holds, trial_grants, topup_orders",
		"raise exception 'retail credit reprice would overflow bigint'",
		"select sum(hold.amount_micros)",
	} {
		if !strings.Contains(combined, required) {
			t.Errorf("75 VND retail reprice migration is missing %q", required)
		}
	}
}

// The return to 75 VND per credit must not rescale any stored money.
//
// This is the opposite requirement to every earlier reprice. 0010, 0014, and 0017 each
// multiplied balance_micros so a customer's displayed credit count survived the rate
// change. Doing that in reverse here would divide real money by 400/75 and take 5.33x
// off every wallet, because the column holds the VND that was actually paid. The
// displayed credit count is meant to rise instead: 75 is the rate the price page
// quotes, and no buyer ever saw 400 before paying.
func TestReturnTo75DoesNotRescaleStoredMoney(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}

	var reprice string
	for _, m := range migrations {
		if m.version == 22 {
			reprice = strings.ToLower(m.sql)
		}
	}
	if reprice == "" {
		t.Fatal("migration 0022 was not found")
	}

	for _, required := range []string{
		"alter column retail_vnd_per_credit set default 75",
		"where status <> 'paid'",
		"and status <> 'underpaid'",
		"received_amount_micros = 0",
	} {
		if !strings.Contains(reprice, required) {
			t.Errorf("migration 0022 is missing %q", required)
		}
	}
	// Any of these would move money rather than change the unit it is displayed in.
	for _, forbidden := range []string{
		"update wallets",
		"balance_micros =",
		"promotional_micros =",
		"update wallet_holds",
		"update trial_grants",
		"update first_topup_bonus_grants",
		"insert into ledger_entries",
	} {
		if strings.Contains(reprice, forbidden) {
			t.Errorf("migration 0022 contains %q, which would shrink real wallet balances by 5.33x", forbidden)
		}
	}
}

func sqlOf(migrations []migration) []string {
	out := make([]string, len(migrations))
	for i, m := range migrations {
		out[i] = m.sql
	}
	return out
}
