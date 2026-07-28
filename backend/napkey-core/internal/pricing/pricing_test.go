package pricing

import (
	"errors"
	"math"
	"testing"
)

// sonnetRate mirrors the seeded Sonnet row in migration 0003.
var sonnetRate = Rate{
	ID:                      1,
	Model:                   "claude-sonnet-4-20250514",
	InputPer1k:              101_400_000,
	OutputPer1k:             507_000_000,
	CacheReadPer1k:          10_140_000,
	CacheWritePer1k:         126_750_000,
	UpstreamInputPer1k:      78_000_000,
	UpstreamOutputPer1k:     390_000_000,
	UpstreamCacheReadPer1k:  7_800_000,
	UpstreamCacheWritePer1k: 97_500_000,
}

func TestRetailCreditRate(t *testing.T) {
	if RetailVNDPerCredit != 60 {
		t.Fatalf("RetailVNDPerCredit = %d, want 60", RetailVNDPerCredit)
	}
}

func TestCreditBillingUsesFixedPointArithmetic(t *testing.T) {
	credits, err := CreditMicrosFromFloat(1.87)
	if err != nil {
		t.Fatalf("converting credits: %v", err)
	}
	if credits != 1_870_000 {
		t.Fatalf("credits = %d microcredits, want 1870000", credits)
	}

	cost, err := ComputeCreditCost(credits, 45_000_000)
	if err != nil {
		t.Fatalf("pricing credits: %v", err)
	}
	if cost != 84_150_000 {
		t.Fatalf("cost = %d micro-VND, want 84150000", cost)
	}
}

func TestCreditBillingRejectsInvalidMeasurements(t *testing.T) {
	for _, value := range []float64{-1, math.NaN(), math.Inf(1)} {
		if _, err := CreditMicrosFromFloat(value); err == nil {
			t.Fatalf("CreditMicrosFromFloat(%v) should fail", value)
		}
	}
}

func TestUpstreamRateUsesIndependentCostBasis(t *testing.T) {
	cost, err := Compute(Tokens{Input: 1000, Output: 1000}, sonnetRate.UpstreamRate())
	if err != nil {
		t.Fatalf("Compute upstream: %v", err)
	}
	if cost.Micros != 468_000_000 {
		t.Fatalf("upstream cost = %d, want 468000000", cost.Micros)
	}
}

func TestComputeSplitsCostByTokenKind(t *testing.T) {
	// 1k of each kind makes each component equal to its own rate, so a swapped
	// rate assignment shows up as a wrong component rather than hiding in the total.
	cost, err := Compute(Tokens{Input: 1000, Output: 1000, CacheRead: 1000, CacheWrite: 1000}, sonnetRate)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if cost.InputMicros != 101_400_000 {
		t.Errorf("InputMicros = %d, want 101400000", cost.InputMicros)
	}
	if cost.OutputMicros != 507_000_000 {
		t.Errorf("OutputMicros = %d, want 507000000", cost.OutputMicros)
	}
	if cost.CacheReadMicros != 10_140_000 {
		t.Errorf("CacheReadMicros = %d, want 10140000", cost.CacheReadMicros)
	}
	if cost.CacheWriteMicros != 126_750_000 {
		t.Errorf("CacheWriteMicros = %d, want 126750000", cost.CacheWriteMicros)
	}
	want := int64(101_400_000 + 507_000_000 + 10_140_000 + 126_750_000)
	if cost.Micros != want {
		t.Errorf("Micros = %d, want %d", cost.Micros, want)
	}
	if cost.RateID != sonnetRate.ID {
		t.Errorf("RateID = %d, want %d", cost.RateID, sonnetRate.ID)
	}
	if cost.Unpriced {
		t.Error("a priced request should not be flagged unpriced")
	}
}

// TestCacheReadIsMuchCheaperThanInput guards the distinction the schema exists to
// preserve. If cache reads were ever priced as fresh input, cache-heavy traffic
// would be overcharged by roughly 10x and the customer would be right to complain.
func TestCacheReadIsMuchCheaperThanInput(t *testing.T) {
	fresh, err := Compute(Tokens{Input: 100_000}, sonnetRate)
	if err != nil {
		t.Fatalf("Compute fresh: %v", err)
	}
	cached, err := Compute(Tokens{CacheRead: 100_000}, sonnetRate)
	if err != nil {
		t.Fatalf("Compute cached: %v", err)
	}
	if cached.Micros >= fresh.Micros {
		t.Fatalf("cache read (%d) should cost less than fresh input (%d)", cached.Micros, fresh.Micros)
	}
	// The seeded rates put cache read at exactly a tenth of input.
	if fresh.Micros != cached.Micros*10 {
		t.Errorf("expected a 10x gap, got fresh=%d cached=%d", fresh.Micros, cached.Micros)
	}
}

// TestComputeRoundsDownPerComponent pins the rounding direction. DESIGN.md section
// 5 requires that an amount owed is never rounded up.
func TestComputeRoundsDownPerComponent(t *testing.T) {
	// 1 token at 101,400,000 per 1k is 101,400 micros exactly, so use a rate that
	// does not divide evenly: 1 token at 999 per 1k = 0.999 micros -> 0.
	rate := Rate{ID: 9, InputPer1k: 999, OutputPer1k: 1001}
	cost, err := Compute(Tokens{Input: 1, Output: 1}, rate)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if cost.InputMicros != 0 {
		t.Errorf("InputMicros = %d, want 0 (floor of 0.999)", cost.InputMicros)
	}
	if cost.OutputMicros != 1 {
		t.Errorf("OutputMicros = %d, want 1 (floor of 1.001)", cost.OutputMicros)
	}
}

func TestComputeZeroTokensCostsNothing(t *testing.T) {
	cost, err := Compute(Tokens{}, sonnetRate)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if cost.Micros != 0 {
		t.Errorf("Micros = %d, want 0", cost.Micros)
	}
	// A zero-token request still carries the rate it was priced against, so the
	// row satisfies the pricing-coherence constraint in migration 0003.
	if cost.RateID != sonnetRate.ID {
		t.Errorf("RateID = %d, want the rate to be recorded even at zero cost", cost.RateID)
	}
}

// TestComputeRejectsNegativeTokens covers the case where a compromised or buggy
// data plane reports negative usage. In Stage 4 a negative cost credits the wallet,
// so this is an attack on the balance rather than a data quality issue.
func TestComputeRejectsNegativeTokens(t *testing.T) {
	cases := map[string]Tokens{
		"input":       {Input: -1},
		"output":      {Output: -5},
		"cache read":  {CacheRead: -10},
		"cache write": {CacheWrite: -100},
	}
	for name, tokens := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Compute(tokens, sonnetRate); err == nil {
				t.Fatal("expected negative token counts to be rejected")
			}
		})
	}
}

func TestComputeDetectsOverflow(t *testing.T) {
	_, err := Compute(Tokens{Input: math.MaxInt64 / 1000}, sonnetRate)
	if !errors.Is(err, ErrOverflow) {
		t.Fatalf("expected ErrOverflow, got %v", err)
	}
}

// TestComputeLargeButRealisticRequestDoesNotOverflow checks the guard is not so
// eager that it rejects a legitimate request. A 1M-token context on the most
// expensive model is the practical ceiling.
func TestComputeLargeButRealisticRequestDoesNotOverflow(t *testing.T) {
	opus := Rate{ID: 3, InputPer1k: 507_000_000, OutputPer1k: 2_535_000_000,
		CacheReadPer1k: 50_700_000, CacheWritePer1k: 633_750_000}
	cost, err := Compute(Tokens{Input: 1_000_000, Output: 64_000, CacheWrite: 200_000}, opus)
	if err != nil {
		t.Fatalf("a 1M-token Opus request should price cleanly: %v", err)
	}
	if cost.Micros <= 0 {
		t.Fatalf("Micros = %d, want a positive cost", cost.Micros)
	}
	// Sanity: this should land in the hundreds of thousands of VND, not millions.
	vnd := VNDFromMicros(cost.Micros)
	if vnd < 100_000 || vnd > 10_000_000 {
		t.Errorf("cost of %d VND is outside the plausible range, check the rate scale", vnd)
	}
}

func TestUnpricedCarriesNoCost(t *testing.T) {
	cost := Unpriced()
	if !cost.Unpriced {
		t.Error("Unpriced should set the flag")
	}
	if cost.Micros != 0 || cost.RateID != 0 {
		t.Errorf("an unpriced cost must be zero with no rate, got micros=%d rate=%d", cost.Micros, cost.RateID)
	}
}

func TestTokensTotal(t *testing.T) {
	tokens := Tokens{Input: 1, Output: 2, CacheRead: 4, CacheWrite: 8}
	if got := tokens.Total(); got != 15 {
		t.Errorf("Total() = %d, want 15", got)
	}
	if !(Tokens{}).IsZero() {
		t.Error("an empty Tokens should be zero")
	}
	if tokens.IsZero() {
		t.Error("a populated Tokens should not be zero")
	}
}

func TestNormalizeModel(t *testing.T) {
	cases := map[string]string{
		"  Claude-Sonnet-4-20250514 ": "claude-sonnet-4-20250514",
		"CLAUDE-OPUS-4-20250514":      "claude-opus-4-20250514",
		"":                            "",
	}
	for in, want := range cases {
		if got := NormalizeModel(in); got != want {
			t.Errorf("NormalizeModel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVNDConversionRoundsDown(t *testing.T) {
	// 1.999999 VND displays as 1, never 2: a balance must not show money that is
	// not there.
	if got := VNDFromMicros(1_999_999); got != 1 {
		t.Errorf("VNDFromMicros(1999999) = %d, want 1", got)
	}
	if got := VNDFromMicros(999_999); got != 0 {
		t.Errorf("VNDFromMicros(999999) = %d, want 0", got)
	}
	micros, err := MicrosFromVND(500_000)
	if err != nil {
		t.Fatalf("MicrosFromVND: %v", err)
	}
	if micros != 500_000_000_000 {
		t.Errorf("MicrosFromVND(500000) = %d, want 500000000000", micros)
	}
	if _, err := MicrosFromVND(math.MaxInt64 / 2); !errors.Is(err, ErrOverflow) {
		t.Errorf("expected overflow for an absurd VND amount, got %v", err)
	}
}

func TestFormatVND(t *testing.T) {
	cases := map[int64]string{
		0:                 "0",
		999_999:           "0",
		1_000_000:         "1",
		1_234_000_000:     "1.234",
		500_000_000_000:   "500.000",
		1_234_567_000_000: "1.234.567",
	}
	for micros, want := range cases {
		if got := FormatVND(micros); got != want {
			t.Errorf("FormatVND(%d) = %q, want %q", micros, got, want)
		}
	}
}

// TestRoundTripKeepsSubDongPrecision is the reason money is stored in micros. A
// realistic per-token price is a fraction of a dong, so a store that rounded to
// whole VND would record a thousand small requests as costing nothing.
func TestRoundTripKeepsSubDongPrecision(t *testing.T) {
	var totalMicros int64
	for i := 0; i < 1000; i++ {
		cost, err := Compute(Tokens{Input: 100}, sonnetRate)
		if err != nil {
			t.Fatalf("Compute: %v", err)
		}
		if cost.Micros == 0 {
			t.Fatal("a 100-token request should cost more than zero micros")
		}
		totalMicros += cost.Micros
	}
	// 100 tokens at 101.4 micros/token = 10,140,000 micros = 10.14 VND per request.
	// A thousand of those is 10,140 VND. Rounding each to whole dong would have
	// lost nothing here, but rounding to zero would have lost all of it.
	if got := VNDFromMicros(totalMicros); got != 10_140 {
		t.Errorf("1000 small requests totalled %d VND, want 10140", got)
	}
}

// TestRateFieldsStayIntegral is a compile-time guard written as a test: if a rate
// or cost field is ever changed to float64 to "make the math easier", these
// assignments stop compiling. Float money is the bug this package exists to avoid.
func TestRateFieldsStayIntegral(t *testing.T) {
	var r Rate
	var _ int64 = r.InputPer1k
	var _ int64 = r.OutputPer1k
	var _ int64 = r.CacheReadPer1k
	var _ int64 = r.CacheWritePer1k

	var c Cost
	var _ int64 = c.Micros
	var _ int64 = c.InputMicros
	var _ int64 = c.OutputMicros
	var _ int64 = c.CacheReadMicros
	var _ int64 = c.CacheWriteMicros
}
