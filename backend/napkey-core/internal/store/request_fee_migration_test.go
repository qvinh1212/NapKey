package store

import (
	"strings"
	"testing"
)

// The token-priced path must carry a per-request fee.
//
// Without it the models NapKey sells are priced below cost on coding-agent traffic:
// the upstream bills roughly one credit per call whatever its size, while a token-only
// price collects 22 VND for a 1,800-token request against a 110 VND cost. This asserts
// the fee reached the price book, since the columns existing is not the same as a rate
// actually using them.
func TestTokenPathCarriesARequestFee(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	combined := strings.ToLower(strings.Join(sqlOf(migrations), "\n"))

	// 300 VND retail over a 110 VND measured basis, in micro-VND.
	if !strings.Contains(combined, "300000000, 110000000") {
		t.Error("no price row charges a per-request fee, so small requests still settle below cost")
	}
	// Both halves are required. A retail fee with a zero basis would report the whole
	// fee as margin and overstate profitability on every request.
	if !strings.Contains(combined, "upstream_request_fee_micros") {
		t.Error("the fee needs a cost basis, or margin reporting treats revenue as free money")
	}
}

// The fee has to be a new price period, never an edit to an existing one.
//
// Editing a row would reprice traffic already settled against it, which is what
// DESIGN.md section 5 forbids: every invoice already sent would stop reconciling.
func TestRequestFeeOpensANewPricePeriod(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}

	var fee string
	for _, m := range migrations {
		if strings.Contains(m.name, "request_fee") {
			fee = strings.ToLower(m.sql)
		}
	}
	if fee == "" {
		t.Fatal("no request fee migration was found")
	}

	// The open period is closed and a successor inserted, rather than the rate being
	// mutated in place.
	if !strings.Contains(fee, "set effective_to = now()") {
		t.Error("the previous period must be closed, or two prices cover the same instant")
	}
	if !strings.Contains(fee, "insert into model_prices") {
		t.Error("the new price must be inserted as its own period")
	}
	// An UPDATE that rewrites a rate column would reprice settled usage.
	for _, forbidden := range []string{
		"set request_fee_micros",
		"set input_micros_per_1k",
		"update usage_records",
	} {
		if strings.Contains(fee, forbidden) {
			t.Errorf("migration contains %q, which would reprice usage already charged", forbidden)
		}
	}
}

// The unknown-model fallback must carry the fee as well.
//
// The '*' row exists so an unrecognized id is never served for free. If it were the
// only row without a fee, sending an unknown model id would be a way to avoid the
// fee while still consuming an upstream call.
func TestRequestFeeAppliesToTheFallbackModel(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}

	var fee string
	for _, m := range migrations {
		if strings.Contains(m.name, "request_fee") {
			fee = strings.ToLower(m.sql)
		}
	}
	if fee == "" {
		t.Fatal("no request fee migration was found")
	}
	if !strings.Contains(fee, "'*'") {
		t.Error("the '*' fallback must be repriced with the fee, or an unknown id avoids it")
	}
}
