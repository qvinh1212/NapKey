package store

import (
	"context"
	"strings"
	"testing"

	"napkey-core/internal/pgtest"
	"napkey-core/internal/pricing"
)

// A report with no credit meter must be priced from tokens, not silently zero.
//
// This is the regression that motivated the change: settlement used to multiply the
// credit meter by the retail rate and nothing else, so an upstream that does not
// emit a meter produced cost_micros = 0 on a row stored as priced. Served traffic
// became free with no error and nothing in the reconciliation view.
func TestRecordUsagePricesFromTokensWhenNoCreditMeter(t *testing.T) {
	srv, st := usageHarness(t)

	_, err := st.RecordUsage(context.Background(), RecordUsageParams{
		RequestID:     "req-token-priced",
		KeyID:         pgtest.UUID(1),
		Model:         "claude-sonnet-4-20250514",
		Tokens:        pricing.Tokens{Input: 12_000, Output: 3_000},
		CreditsMicros: 0,
	})
	if err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}

	// cost_micros is bound 9th. The harness price is 12,000,000 micros per 1k for
	// input and 507,000,000 for output, plus a 480,000,000 request fee, so the only
	// thing being asserted is that it is not zero and not unpriced.
	insertParams := usageInsertParams(t, srv)
	if len(insertParams) < 12 {
		t.Fatalf("expected the insert to bind at least 12 params, got %d", len(insertParams))
	}
	cost := insertParams[8]
	if cost == "0" || cost == "" {
		t.Fatalf("cost_micros = %q: a token-reported request must not settle at zero", cost)
	}
	if unpriced := insertParams[11]; unpriced != "f" && !strings.EqualFold(unpriced, "false") {
		t.Errorf("unpriced = %q, want false when a rate was found", unpriced)
	}
	if pricedWith := insertParams[10]; pricedWith == "" {
		t.Error("priced_with must name the rate that produced the charge")
	}
}

// With no price on file at all the row is still written, but flagged.
//
// Dropping it would erase the record of traffic that was already served; storing it
// as priced-at-zero is the silent failure this guards against. Flagged is the only
// honest option, and /v1/admin/usage-audit is what reports on it.
func TestRecordUsageFlagsUnpricedWhenNoRateExists(t *testing.T) {
	srv := pgtest.New(t)
	srv.On("SELECT user_id FROM api_keys", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "user_id"}},
			Rows:    [][]*string{{pgtest.Text(pgtest.UUID(7))}},
			Tag:     "SELECT 1",
		}
	})
	// No rows: neither the model nor the '*' fallback has a price.
	srv.On("FROM model_prices", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Columns: priceColumnsFor(), Tag: "SELECT 0"}
	})
	srv.On("INSERT INTO usage_records", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "id", OID: 20}},
			Rows:    [][]*string{{pgtest.Text("9002")}},
			Tag:     "INSERT 0 1",
		}
	})
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "UPDATE 1"} })
	st := openTestStore(t, srv)

	if _, err := st.RecordUsage(context.Background(), RecordUsageParams{
		RequestID: "req-no-price",
		KeyID:     pgtest.UUID(1),
		Model:     "some-unknown-model",
		Tokens:    pricing.Tokens{Input: 5_000, Output: 1_000},
	}); err != nil {
		t.Fatalf("RecordUsage must still record served traffic: %v", err)
	}

	insertParams := usageInsertParams(t, srv)
	if len(insertParams) < 12 {
		t.Fatalf("expected at least 12 bound params, got %d", len(insertParams))
	}
	if unpriced := insertParams[11]; unpriced != "t" && !strings.EqualFold(unpriced, "true") {
		t.Errorf("unpriced = %q, want true so the row surfaces in the audit view", unpriced)
	}
	if cost := insertParams[8]; cost != "0" {
		t.Errorf("cost_micros = %q, want 0 on an unpriced row", cost)
	}
}

// A successful request reporting neither credits nor tokens is refused.
//
// It would price to zero on every path, and it is indistinguishable from a data
// plane that lost its usage numbers. Errors and cancellations legitimately consume
// nothing, so they stay allowed.
func TestRecordUsageRejectsSuccessWithNothingMeasured(t *testing.T) {
	_, st := usageHarness(t)

	_, err := st.RecordUsage(context.Background(), RecordUsageParams{
		RequestID: "req-empty",
		KeyID:     pgtest.UUID(1),
		Model:     "claude-sonnet-4-20250514",
	})
	if err == nil {
		t.Fatal("a successful request with no credits and no tokens must be refused")
	}

	if _, err := st.RecordUsage(context.Background(), RecordUsageParams{
		RequestID: "req-failed",
		KeyID:     pgtest.UUID(1),
		Model:     "claude-sonnet-4-20250514",
		Status:    UsageStatusError,
	}); err != nil {
		t.Fatalf("a failed request consumes nothing and must still be recordable: %v", err)
	}
}

// usageInsertParams returns the parameters bound to the usage_records insert.
//
// The harness registers that handler first and pgtest.On is first-match, so a test
// cannot re-register it to capture params; the recorded query log is the seam.
func usageInsertParams(t *testing.T, srv *pgtest.Server) []string {
	t.Helper()
	for _, q := range srv.Queries() {
		if strings.Contains(q.SQL, "INSERT INTO usage_records") {
			return q.Params
		}
	}
	t.Fatal("no insert into usage_records was recorded")
	return nil
}

// The per-request fee has to reach the stored row, not just the total.
//
// request_fee_micros is frozen alongside cost_micros so a later fee change cannot
// alter what a settled request was charged. If the column stayed zero while the fee
// was folded into cost_micros, the charge would be unexplainable after the fact:
// nothing would record which part of the total was flat and which was per-token.
func TestRecordUsageStoresTheRequestFeeItCharged(t *testing.T) {
	srv, st := usageHarness(t)

	if _, err := st.RecordUsage(context.Background(), RecordUsageParams{
		RequestID: "req-with-fee",
		KeyID:     pgtest.UUID(1),
		Model:     "claude-sonnet-4-20250514",
		Tokens:    pricing.Tokens{Input: 1_000, Output: 100},
	}); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}

	// request_fee_micros is bound $16, right after the two credit price columns.
	insertParams := usageInsertParams(t, srv)
	if len(insertParams) < 16 {
		t.Fatalf("expected at least 16 bound params, got %d", len(insertParams))
	}
	// The harness rate carries a 480,000,000 micro-VND fee.
	if fee := insertParams[15]; fee != "480000000" {
		t.Errorf("request_fee_micros = %q, want the 480000000 charged by the rate", fee)
	}
}

// A credit-metered request records no per-request fee.
//
// The credit meter already prices the request end to end, and the fee belongs to the
// token path. Charging both would bill the same fixed cost twice, and recording a fee
// the customer was not charged would make the row disagree with cost_micros.
func TestRecordUsageChargesNoRequestFeeOnTheCreditPath(t *testing.T) {
	srv, st := usageHarness(t)

	if _, err := st.RecordUsage(context.Background(), RecordUsageParams{
		RequestID:     "req-credit-metered",
		KeyID:         pgtest.UUID(1),
		Model:         "claude-sonnet-4-20250514",
		CreditsMicros: 5 * pricing.MicrocreditsPerCredit,
	}); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}

	insertParams := usageInsertParams(t, srv)
	if len(insertParams) < 16 {
		t.Fatalf("expected at least 16 bound params, got %d", len(insertParams))
	}
	if fee := insertParams[15]; fee != "0" {
		t.Errorf("request_fee_micros = %q, want 0 when the credit meter priced the request", fee)
	}
}
