package httpapi

import (
	"testing"

	"napkey-core/internal/pricing"
)

// The hold must cover the per-request fee, not just the token estimate.
//
// A hold is what stops a nearly empty wallet from funding requests it cannot pay for.
// On the token path the fee is most of what a small request costs, so a hold that
// ignored it would reserve 67 VND against a ~370 VND charge: a 1,000 VND wallet would
// authorise fourteen concurrent agent calls and settle owing more than it held.
//
// This passes because the hold is quoted through pricing.Compute, the same function
// settlement uses. The test exists so that stays true: quoting the hold from token
// rates alone is the regression it guards against.
func TestWalletHoldCoversTheRequestFee(t *testing.T) {
	rate := pricing.Rate{
		InputPer1k:  12_000_000,
		OutputPer1k: 12_000_000,
		RequestFee:  300 * pricing.MicrosPerVND,
	}

	// A typical Claude Code step: small prompt, max_tokens=4096.
	quote, err := pricing.Compute(pricing.Tokens{Input: 1_500, Output: 4_096}, rate)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	tokensOnly, err := pricing.Compute(pricing.Tokens{Input: 1_500, Output: 4_096}, pricing.Rate{
		InputPer1k:  rate.InputPer1k,
		OutputPer1k: rate.OutputPer1k,
	})
	if err != nil {
		t.Fatalf("Compute without fee: %v", err)
	}

	held := walletHoldAmount(quote.Micros)
	if held <= walletHoldAmount(tokensOnly.Micros) {
		t.Fatalf("hold %d does not exceed the token-only hold %d, so the fee is unsecured",
			held, walletHoldAmount(tokensOnly.Micros))
	}
	if held < quote.Micros {
		t.Errorf("hold %d is below the quoted charge %d", held, quote.Micros)
	}
	// The fee dominates a small request, which is the reason the hold had to change.
	if quote.RequestFeeMicros <= tokensOnly.Micros {
		t.Errorf("expected the fee %d to exceed the token cost %d on an agent-sized request",
			quote.RequestFeeMicros, tokensOnly.Micros)
	}
}

// The ceiling still has to sit above a realistic large request.
//
// The cap exists so one request cannot reserve an entire wallet, but a cap below what
// a legitimate request quotes would hold less than the request settles for, and the
// difference is unsecured. Adding the fee moved every quote up, so this checks the
// largest plausible one still fits underneath.
func TestWalletHoldCeilingStillCoversALargeRequest(t *testing.T) {
	rate := pricing.Rate{
		InputPer1k:  12_000_000,
		OutputPer1k: 12_000_000,
		RequestFee:  300 * pricing.MicrosPerVND,
	}

	// 200k of context with max_tokens=32000, above anything a coding agent sends.
	quote, err := pricing.Compute(pricing.Tokens{Input: 200_000, Output: 32_000}, rate)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	ceiling := walletHoldCeilingVND * pricing.MicrosPerVND
	if quote.Micros > ceiling {
		t.Errorf("a %d micro request exceeds the %d ceiling, so part of it is unsecured",
			quote.Micros, ceiling)
	}
}

// A request reporting no tokens still owes the fee, so it must still be held against.
//
// Otherwise a caller sending max_tokens=0 would reserve the 1 VND floor and settle at
// the full fee, which is the cheapest way to run a wallet negative.
func TestWalletHoldAppliesToAFeeOnlyRequest(t *testing.T) {
	rate := pricing.Rate{RequestFee: 300 * pricing.MicrosPerVND}

	quote, err := pricing.Compute(pricing.Tokens{}, rate)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if got := walletHoldAmount(quote.Micros); got != quote.Micros {
		t.Errorf("hold = %d, want the full fee %d", got, quote.Micros)
	}
}
