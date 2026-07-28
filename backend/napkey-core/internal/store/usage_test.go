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

// priceColumnsFor matches the column order of priceColumns.
func priceColumnsFor() []pgtest.Column {
	return []pgtest.Column{
		{Name: "id", OID: 20}, {Name: "model"},
		{Name: "input_micros_per_1k", OID: 20}, {Name: "output_micros_per_1k", OID: 20},
		{Name: "cache_read_micros_per_1k", OID: 20}, {Name: "cache_write_micros_per_1k", OID: 20},
		{Name: "upstream_input_micros_per_1k", OID: 20}, {Name: "upstream_output_micros_per_1k", OID: 20},
		{Name: "upstream_cache_read_micros_per_1k", OID: 20}, {Name: "upstream_cache_write_micros_per_1k", OID: 20},
		{Name: "effective_from", OID: 1184}, {Name: "effective_to", OID: 1184},
		{Name: "source_note"},
	}
}

// sonnetPriceRow is the seeded Sonnet rate as the wire would return it.
func sonnetPriceRow() []*string {
	return []*string{
		pgtest.Text("1"), pgtest.Text("claude-sonnet-4-20250514"),
		pgtest.Text("101400000"), pgtest.Text("507000000"),
		pgtest.Text("10140000"), pgtest.Text("126750000"),
		pgtest.Text("78000000"), pgtest.Text("390000000"),
		pgtest.Text("7800000"), pgtest.Text("97500000"),
		pgtest.Text("2020-01-01 00:00:00+00"), pgtest.Null,
		pgtest.Text("seed"),
	}
}

// usageHarness wires the fake server for a successful RecordUsage call.
func usageHarness(t *testing.T) (*pgtest.Server, *Store) {
	t.Helper()
	srv := pgtest.New(t)
	srv.On("SELECT user_id FROM api_keys", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "user_id"}},
			Rows:    [][]*string{{pgtest.Text(pgtest.UUID(7))}},
			Tag:     "SELECT 1",
		}
	})
	srv.On("FROM model_prices", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Columns: priceColumnsFor(), Rows: [][]*string{sonnetPriceRow()}, Tag: "SELECT 1"}
	})
	srv.On("INSERT INTO usage_records", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "id", OID: 20}},
			Rows:    [][]*string{{pgtest.Text("4242")}},
			Tag:     "INSERT 0 1",
		}
	})
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "UPDATE 1"} })
	return srv, openTestStore(t, srv)
}

func TestRecordUsagePricesFromTheLedgerRate(t *testing.T) {
	srv, st := usageHarness(t)

	result, err := st.RecordUsage(context.Background(), RecordUsageParams{
		RequestID:     "req-abc",
		KeyID:         pgtest.UUID(1),
		Model:         "claude-sonnet-4-20250514",
		Tokens:        pricing.Tokens{Input: 10_000, Output: 2_000, CacheRead: 50_000},
		CreditsMicros: 1_870_000,
	})
	if err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	if result.Duplicate {
		t.Error("a first report should not be flagged duplicate")
	}
	if result.RecordID != 4242 {
		t.Errorf("RecordID = %d, want 4242", result.RecordID)
	}
	// The owner comes from the key row, never from the caller.
	if result.UserID != pgtest.UUID(7) {
		t.Errorf("UserID = %q, want the key owner", result.UserID)
	}
	want := int64(84_150_000)
	if result.CostMicros != want {
		t.Errorf("CostMicros = %d, want %d", result.CostMicros, want)
	}
	if result.Unpriced {
		t.Error("a priced model should not be flagged unpriced")
	}

	q, ok := srv.FindQuery("INSERT INTO usage_records")
	if !ok {
		t.Fatal("the ledger insert never ran")
	}
	// ON CONFLICT DO NOTHING is the whole deduplication mechanism. Without it a
	// retried report from the data plane bills the same request twice.
	if !strings.Contains(q.SQL, "ON CONFLICT (request_id) DO NOTHING") {
		t.Error("the insert must be idempotent on request_id")
	}
	// The cost has to be written to the row, not recomputed on read, or a later
	// price change would silently reprice history.
	if !strings.Contains(q.SQL, "cost_micros") {
		t.Error("cost must be frozen onto the row at insert time")
	}
}

// TestRecordUsageIsIdempotent covers the retry path. The data plane retries a
// report it could not confirm, and the second attempt must not move any number.
func TestRecordUsageIsIdempotent(t *testing.T) {
	srv := pgtest.New(t)
	srv.On("SELECT user_id FROM api_keys", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "user_id"}},
			Rows:    [][]*string{{pgtest.Text(pgtest.UUID(7))}},
			Tag:     "SELECT 1",
		}
	})
	srv.On("FROM model_prices", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Columns: priceColumnsFor(), Rows: [][]*string{sonnetPriceRow()}, Tag: "SELECT 1"}
	})
	// No RETURNING row: the request_id was already present, so DO NOTHING fired.
	srv.On("INSERT INTO usage_records", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Columns: []pgtest.Column{{Name: "id", OID: 20}}, Tag: "INSERT 0 0"}
	})
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "UPDATE 1"} })
	st := openTestStore(t, srv)

	result, err := st.RecordUsage(context.Background(), RecordUsageParams{
		RequestID: "req-repeat",
		KeyID:     pgtest.UUID(1),
		Model:     "claude-sonnet-4-20250514",
		Tokens:    pricing.Tokens{Input: 10_000, Output: 2_000},
	})
	if err != nil {
		t.Fatalf("a duplicate report is success, not an error: %v", err)
	}
	if !result.Duplicate {
		t.Fatal("the second report should be flagged duplicate")
	}
	if result.CostMicros != 0 {
		t.Errorf("CostMicros = %d, want 0 on a duplicate", result.CostMicros)
	}
	// The counter update is conditional on the insert having happened. If it ran
	// here, a retry would inflate the customer's usage.
	if _, ok := srv.FindQuery("INSERT INTO api_key_usage"); ok {
		t.Error("a duplicate report must not touch the counters")
	}
}

// Credit billing is model-independent: an upstream model can be charged as long
// as Kiro reports measured credits for it.
func TestRecordUsageRecordsUnpricedModel(t *testing.T) {
	srv := pgtest.New(t)
	srv.On("SELECT user_id FROM api_keys", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "user_id"}},
			Rows:    [][]*string{{pgtest.Text(pgtest.UUID(7))}},
			Tag:     "SELECT 1",
		}
	})
	// No price row, and no fallback either.
	srv.On("FROM model_prices", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Columns: priceColumnsFor(), Tag: "SELECT 0"}
	})
	srv.On("INSERT INTO usage_records", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "id", OID: 20}},
			Rows:    [][]*string{{pgtest.Text("77")}},
			Tag:     "INSERT 0 1",
		}
	})
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "UPDATE 1"} })
	st := openTestStore(t, srv)

	result, err := st.RecordUsage(context.Background(), RecordUsageParams{
		RequestID:     "req-nomodel",
		KeyID:         pgtest.UUID(1),
		Model:         "some-model-nobody-priced",
		Tokens:        pricing.Tokens{Input: 1_000, Output: 500},
		CreditsMicros: 500_000,
	})
	if err != nil {
		t.Fatalf("usage for an unpriced model must still be recorded: %v", err)
	}
	if result.Unpriced {
		t.Error("credit-priced usage must not depend on a model token rate")
	}
	if result.CostMicros != 22_500_000 {
		t.Errorf("CostMicros = %d, want 22500000", result.CostMicros)
	}
	if result.RateID != 0 {
		t.Errorf("RateID = %d, want 0 when no rate applies", result.RateID)
	}
}

func TestRecordUsageUnknownKeyIsNotFound(t *testing.T) {
	srv := pgtest.New(t)
	srv.On("SELECT user_id FROM api_keys", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Columns: []pgtest.Column{{Name: "user_id"}}, Tag: "SELECT 0"}
	})
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "SELECT 0"} })
	st := openTestStore(t, srv)

	_, err := st.RecordUsage(context.Background(), RecordUsageParams{
		RequestID: "req-ghost", KeyID: pgtest.UUID(9), Model: "claude-sonnet-4-20250514",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, ok := srv.FindQuery("INSERT INTO usage_records"); ok {
		t.Error("usage for a nonexistent key must not be inserted")
	}
}

func TestRecordUsageRequiresRequestID(t *testing.T) {
	srv, st := usageHarness(t)
	_, err := st.RecordUsage(context.Background(), RecordUsageParams{
		KeyID: pgtest.UUID(1), Model: "claude-sonnet-4-20250514",
	})
	if err == nil {
		t.Fatal("a report with no request id should be rejected")
	}
	if _, ok := srv.FindQuery("INSERT INTO usage_records"); ok {
		t.Error("nothing should be written without an idempotency key")
	}
}

func TestRecordUsageRejectsUnknownStatus(t *testing.T) {
	_, st := usageHarness(t)
	_, err := st.RecordUsage(context.Background(), RecordUsageParams{
		RequestID: "req-status", KeyID: pgtest.UUID(1), Status: "exploded",
	})
	if err == nil {
		t.Fatal("an out-of-enum status should be rejected before it hits the CHECK constraint")
	}
}

// TestRecordUsageClampsFutureTimestamps stops a caller from reaching a scheduled
// price early by claiming the request happened next month.
func TestRecordUsageClampsFutureTimestamps(t *testing.T) {
	srv, st := usageHarness(t)
	_, err := st.RecordUsage(context.Background(), RecordUsageParams{
		RequestID:  "req-future",
		KeyID:      pgtest.UUID(1),
		Model:      "claude-sonnet-4-20250514",
		Tokens:     pricing.Tokens{Input: 100},
		OccurredAt: time.Now().AddDate(0, 1, 0),
	})
	if err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	q, ok := srv.FindQuery("INSERT INTO usage_records")
	if !ok {
		t.Fatal("the price lookup never ran")
	}
	// OccurredAt is the final insert parameter and must be near now, not a month out.
	if len(q.Params) < 18 {
		t.Fatalf("expected 18 bound params, got %v", q.Params)
	}
	boundParam := q.Params[len(q.Params)-1]
	bound, err := time.Parse("2006-01-02 15:04:05.999999999-07:00", boundParam)
	if err != nil {
		// Format varies with the driver's encoding; fall back to a prefix check on
		// the current year-month.
		if !strings.HasPrefix(boundParam, time.Now().UTC().Format("2006-01")) {
			t.Errorf("bound timestamp %q was not clamped to now", boundParam)
		}
		return
	}
	if bound.After(time.Now().Add(2 * time.Minute)) {
		t.Errorf("bound timestamp %s was not clamped to now", bound)
	}
}

// TestFindRateFallsBackToTheWildcard checks that an unrecognized model still prices.
// Serving an unknown model for free is a silent loss, so the lookup falls back to
// the '*' sentinel rather than returning nothing.
func TestFindRateFallsBackToTheWildcard(t *testing.T) {
	srv := pgtest.New(t)
	srv.On("FROM model_prices", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Columns: priceColumnsFor(), Rows: [][]*string{sonnetPriceRow()}, Tag: "SELECT 1"}
	})
	st := openTestStore(t, srv)

	if _, err := st.FindRate(context.Background(), "Claude-Sonnet-4-20250514", time.Now()); err != nil {
		t.Fatalf("FindRate: %v", err)
	}
	q, ok := srv.FindQuery("FROM model_prices")
	if !ok {
		t.Fatal("the lookup never ran")
	}
	// The model must be normalized before binding, or a capitalized id misses its
	// price and the request is served free.
	if q.Params[0] != "claude-sonnet-4-20250514" {
		t.Errorf("bound model = %q, want the lowercased form", q.Params[0])
	}
	if q.Params[1] != pricing.FallbackModel {
		t.Errorf("bound fallback = %q, want %q", q.Params[1], pricing.FallbackModel)
	}
	// An exact match has to win over the fallback, or every model bills at the
	// wildcard rate.
	if !strings.Contains(q.SQL, "ORDER BY (model = $1) DESC") {
		t.Error("an exact model match must outrank the fallback")
	}
	// Half-open period: the instant a price is superseded belongs to exactly one row.
	if !strings.Contains(q.SQL, "effective_to IS NULL OR effective_to > $3") {
		t.Error("the period must be half-open so two prices never both apply")
	}
}

func TestSetRateClosesThePreviousPeriod(t *testing.T) {
	srv := pgtest.New(t)
	srv.On("WHERE model = $1 AND effective_to IS NULL", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "id", OID: 20}, {Name: "effective_from", OID: 1184}},
			Rows:    [][]*string{{pgtest.Text("1"), pgtest.Text("2020-01-01 00:00:00+00")}},
			Tag:     "SELECT 1",
		}
	})
	srv.On("INSERT INTO model_prices", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Columns: priceColumnsFor(), Rows: [][]*string{sonnetPriceRow()}, Tag: "INSERT 0 1"}
	})
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "UPDATE 1"} })
	st := openTestStore(t, srv)

	_, err := st.SetRate(context.Background(), SetRateParams{
		Model: "claude-sonnet-4-20250514", InputPer1k: 1, OutputPer1k: 2,
		CacheReadPer1k: 3, CacheWritePer1k: 4, SourceNote: "test",
	})
	if err != nil {
		t.Fatalf("SetRate: %v", err)
	}
	lock, ok := srv.FindQuery("WHERE model = $1 AND effective_to IS NULL")
	if !ok {
		t.Fatal("the open period was never locked")
	}
	// FOR UPDATE serializes concurrent price changes for one model. Without it both
	// callers close the same row and both insert, leaving two open periods.
	if !strings.Contains(lock.SQL, "FOR UPDATE") {
		t.Error("the open price row must be locked before it is replaced")
	}
	if _, ok := srv.FindQuery("SET effective_to"); !ok {
		t.Error("the previous period should be closed")
	}
}

// TestSetRateRejectsBackdatingOntoAnOpenPeriod protects the price book from
// overlapping periods, which would make cost non-deterministic.
func TestSetRateRejectsBackdatingOntoAnOpenPeriod(t *testing.T) {
	srv := pgtest.New(t)
	srv.On("WHERE model = $1 AND effective_to IS NULL", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "id", OID: 20}, {Name: "effective_from", OID: 1184}},
			Rows:    [][]*string{{pgtest.Text("1"), pgtest.Text("2030-01-01 00:00:00+00")}},
			Tag:     "SELECT 1",
		}
	})
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "SELECT 0"} })
	st := openTestStore(t, srv)

	_, err := st.SetRate(context.Background(), SetRateParams{
		Model: "claude-sonnet-4-20250514", SourceNote: "test",
		EffectiveFrom: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, ErrPriceOverlap) {
		t.Fatalf("expected ErrPriceOverlap, got %v", err)
	}
	if _, ok := srv.FindQuery("INSERT INTO model_prices"); ok {
		t.Error("an overlapping price must not be inserted")
	}
}

func TestSetRateMapsExclusionViolation(t *testing.T) {
	srv := pgtest.New(t)
	srv.On("WHERE model = $1 AND effective_to IS NULL", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "id", OID: 20}, {Name: "effective_from", OID: 1184}},
			Tag:     "SELECT 0",
		}
	})
	srv.On("INSERT INTO model_prices", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			ErrCode:       pgwire.CodeExclusionViolation,
			ErrMessage:    `conflicting key value violates exclusion constraint "model_prices_no_overlap"`,
			ErrConstraint: "model_prices_no_overlap",
		}
	})
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "SELECT 0"} })
	st := openTestStore(t, srv)

	// The database constraint is the backstop behind the Go check. It must surface
	// as a conflict a human can read, not a 500.
	_, err := st.SetRate(context.Background(), SetRateParams{
		Model: "claude-sonnet-4-20250514", SourceNote: "test",
	})
	if !errors.Is(err, ErrPriceOverlap) {
		t.Fatalf("expected ErrPriceOverlap, got %v", err)
	}
}

func TestSetRateRejectsNegativeAndBlank(t *testing.T) {
	srv := pgtest.New(t)
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "SELECT 0"} })
	st := openTestStore(t, srv)

	if _, err := st.SetRate(context.Background(), SetRateParams{Model: "  "}); err == nil {
		t.Error("a blank model should be rejected")
	}
	if _, err := st.SetRate(context.Background(), SetRateParams{
		Model: "claude-sonnet-4-20250514", InputPer1k: -1,
	}); err == nil {
		t.Error("a negative rate should be rejected")
	}
	if _, ok := srv.FindQuery("INSERT INTO model_prices"); ok {
		t.Error("invalid input should not reach the database")
	}
}

// TestUsageRangeNormalizeDefaultsToThirtyDays pins the default window so a console
// call with no parameters cannot accidentally scan the whole table.
func TestUsageRangeNormalizeDefaultsToThirtyDays(t *testing.T) {
	r := UsageRange{}.Normalize()
	if r.From.IsZero() || r.To.IsZero() {
		t.Fatal("Normalize should fill both ends")
	}
	days := r.To.Sub(r.From).Hours() / 24
	if days < 29 || days > 31 {
		t.Errorf("default window is %.1f days, want about 30", days)
	}
}

func TestFindCounterDriftComparesLedgerToCounters(t *testing.T) {
	srv := pgtest.New(t)
	srv.On("WITH ledger AS", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{
				{Name: "id"}, {Name: "user_id"},
				{Name: "requests_count", OID: 20}, {Name: "tokens_used", OID: 20}, {Name: "cost_micros", OID: 20},
				{Name: "l_requests", OID: 20}, {Name: "l_tokens", OID: 20}, {Name: "l_cost", OID: 20},
			},
			Rows: [][]*string{{
				pgtest.Text(pgtest.UUID(1)), pgtest.Text(pgtest.UUID(7)),
				pgtest.Text("10"), pgtest.Text("5000"), pgtest.Text("900"),
				pgtest.Text("9"), pgtest.Text("4500"), pgtest.Text("800"),
			}},
			Tag: "SELECT 1",
		}
	})
	st := openTestStore(t, srv)

	drift, err := st.FindCounterDrift(context.Background(), 10)
	if err != nil {
		t.Fatalf("FindCounterDrift: %v", err)
	}
	if len(drift) != 1 {
		t.Fatalf("got %d drift rows, want 1", len(drift))
	}
	// The delta is what an operator acts on: the counter claims 100 micros the
	// ledger cannot account for.
	if got := drift[0].CostDelta(); got != 100 {
		t.Errorf("CostDelta() = %d, want 100", got)
	}
}

func TestUsageTotalsCanBeScopedToOneKey(t *testing.T) {
	srv := pgtest.New(t)
	srv.On("FROM usage_records", func(q pgtest.Query) pgtest.Response {
		if !strings.Contains(q.SQL, "SELECT count(*)::bigint") {
			return pgtest.Response{Tag: "SELECT 0"}
		}
		if !strings.Contains(q.SQL, "api_key_id::text = $4") {
			t.Errorf("usage aggregate does not filter by key: %s", q.SQL)
		}
		return pgtest.Response{
			Columns: []pgtest.Column{
				{Name: "requests", OID: 20}, {Name: "input", OID: 20},
				{Name: "output", OID: 20}, {Name: "cache_read", OID: 20},
				{Name: "cache_write", OID: 20}, {Name: "cost", OID: 20},
				{Name: "credits", OID: 20},
				{Name: "unpriced", OID: 20}, {Name: "estimated", OID: 20},
				{Name: "errors", OID: 20},
			},
			Rows: [][]*string{{
				pgtest.Text("1"), pgtest.Text("10"), pgtest.Text("5"),
				pgtest.Text("0"), pgtest.Text("0"), pgtest.Text("100"),
				pgtest.Text("1000000"),
				pgtest.Text("0"), pgtest.Text("0"), pgtest.Text("0"),
			}},
			Tag: "SELECT 1",
		}
	})
	st := openTestStore(t, srv)

	_, err := st.GetUserUsageTotals(context.Background(), pgtest.UUID(1), UsageRange{}, UsageFilter{
		KeyID: pgtest.UUID(9),
	})
	if err != nil {
		t.Fatalf("GetUserUsageTotals: %v", err)
	}
}
