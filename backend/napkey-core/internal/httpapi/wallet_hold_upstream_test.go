package httpapi

import (
	"testing"

	"napkey-core/internal/pricing"
)

// The rate every model on the pool carries: migrations 0018-0020.
func poolRate() pricing.Rate {
	return pricing.Rate{
		InputPer1k:  12_000_000,
		OutputPer1k: 12_000_000,
		RequestFee:  300 * pricing.MicrosPerVND,
	}
}

// quoteHold reproduces what handleReserveWallet reserves for a request, and what
// settlement later charges for it, so the two can be compared the way production
// compares them.
func quoteHold(t *testing.T, declaredIn, declaredOut int64) int64 {
	t.Helper()
	rate := poolRate()
	quote, err := pricing.Compute(walletHoldTokens(pricing.Tokens{Input: declaredIn, Output: declaredOut}), rate)
	if err != nil {
		t.Fatalf("quoting the hold: %v", err)
	}
	return walletHoldAmount(quote.Micros)
}

func settlementFor(t *testing.T, actualIn, actualOut int64) int64 {
	t.Helper()
	charge, err := pricing.Compute(pricing.Tokens{Input: actualIn, Output: actualOut}, poolRate())
	if err != nil {
		t.Fatalf("pricing the settlement: %v", err)
	}
	return charge.Micros
}

// A hold must cover what the request will actually settle for.
//
// Both figures below were measured against the live upstream on 2026-08-06, not chosen
// to make the test pass. Before the upstream floor existed these two shapes settled
// above their hold -- 301.5 held against 337.5 settled, and 900.2 against 948.4 -- which
// on a nearly empty wallet means the settlement UPDATE matches no row, the charge is
// lost, and the hold stays open until it expires.
//
// The gap is structural rather than unlucky: the hold is quoted from what the caller
// declared, settlement from what the upstream reported, and the upstream adds a prompt
// nobody declared while ignoring max_tokens.
func TestHoldCoversSettlementOnMeasuredTraffic(t *testing.T) {
	cases := []struct {
		name                    string
		declaredIn, declaredOut int64
		actualIn, actualOut     int64
	}{
		{
			// max_tokens=100 on a one-line question, billed 2,000 input and 1,121 output.
			name:       "small cap, agent step",
			declaredIn: 21, declaredOut: 100,
			actualIn: 2_000, actualOut: 1_121,
		},
		{
			// No max_tokens sent; kiro-go defaults the estimate to 8,192 output.
			name:       "no cap declared",
			declaredIn: 350, declaredOut: 8_192,
			actualIn: 2_600, actualOut: 1_400,
		},
		{
			// Long context, where the injected prompt is proportionally small.
			name:       "large context",
			declaredIn: 50_000, declaredOut: 4_096,
			actualIn: 52_600, actualOut: 1_200,
		},
		{
			// The worst shape: a large prompt with a cap the upstream disregards.
			name:       "large prompt, small cap",
			declaredIn: 50_000, declaredOut: 16,
			actualIn: 52_600, actualOut: 1_431,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			held := quoteHold(t, c.declaredIn, c.declaredOut)
			settled := settlementFor(t, c.actualIn, c.actualOut)
			if held < settled {
				t.Errorf("hold %.1f VND is below the %.1f VND this request settles for; %.1f VND is unsecured",
					float64(held)/1e6, float64(settled)/1e6, float64(settled-held)/1e6)
			}
		})
	}
}

// The injected prompt is added to every declaration, short or long.
//
// It is charged on top of the caller's text rather than replacing it, so a long prompt
// needs the same allowance as a short one. Treating it as a floor is what left the
// worst-affected shape exposed.
func TestInjectedPromptIsAddedToEveryDeclaration(t *testing.T) {
	small := walletHoldTokens(pricing.Tokens{Input: 21, Output: 100})
	if small.Input != 21+upstreamInjectedInputTokens {
		t.Errorf("short prompt: got %d, want %d", small.Input, 21+upstreamInjectedInputTokens)
	}

	large := walletHoldTokens(pricing.Tokens{Input: 50_000, Output: 4_096})
	if large.Input != 50_000+upstreamInjectedInputTokens {
		t.Errorf("long prompt: got %d, want %d", large.Input, 50_000+upstreamInjectedInputTokens)
	}
}

// The output allowance is a floor, and a generous cap is left alone.
//
// It stands in for a cap the upstream does not apply, so it only has to lift a
// declaration that is too small to cover what the model will actually return. A caller
// who already allows more must be held against their own number, or every large request
// would reserve more of the wallet than it can spend.
func TestOutputAllowanceIsAFloorNotAnAddition(t *testing.T) {
	raised := walletHoldTokens(pricing.Tokens{Input: 0, Output: 16})
	if raised.Output != upstreamMinimumOutputTokens {
		t.Errorf("a cap below the floor must be raised: got %d, want %d",
			raised.Output, upstreamMinimumOutputTokens)
	}

	generous := walletHoldTokens(pricing.Tokens{Input: 0, Output: 8_192})
	if generous.Output != 8_192 {
		t.Errorf("a cap above the floor must be kept: got %d, want 8192", generous.Output)
	}
}

// Each component is handled on its own.
//
// A large prompt with a small cap is a real shape, and it had the largest shortfall of
// any measured. Deciding from whichever side happened to be smaller would leave it
// exposed.
func TestHoldTokensHandleEachComponentIndependently(t *testing.T) {
	got := walletHoldTokens(pricing.Tokens{Input: 50_000, Output: 16})
	if got.Input != 50_000+upstreamInjectedInputTokens {
		t.Errorf("prompt: got %d, want %d", got.Input, 50_000+upstreamInjectedInputTokens)
	}
	if got.Output != upstreamMinimumOutputTokens {
		t.Errorf("cap: got %d, want %d", got.Output, upstreamMinimumOutputTokens)
	}
}

// The allowance must not swallow the ceiling.
//
// walletHoldCeilingVND exists so one request cannot reserve a whole wallet. If the
// allowance priced out near it, every request would reserve the maximum and a funded
// wallet would serve one call at a time.
func TestHoldAllowanceStaysWellBelowTheCeiling(t *testing.T) {
	allowance, err := pricing.Compute(walletHoldTokens(pricing.Tokens{}), poolRate())
	if err != nil {
		t.Fatal(err)
	}
	ceiling := walletHoldCeilingVND * pricing.MicrosPerVND
	if allowance.Micros >= ceiling/2 {
		t.Errorf("the allowance prices at %d, more than half the %d ceiling, leaving too little room for real requests",
			allowance.Micros, ceiling)
	}
}
