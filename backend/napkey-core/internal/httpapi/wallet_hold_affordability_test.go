package httpapi

import (
	"testing"

	"napkey-core/internal/pricing"
)

// A trial wallet must still be able to make a request.
//
// Raising the hold takes real balance out of circulation, and the smallest funded wallet
// is the trial granted on signup. If one request reserved most of that, a new customer's
// first call would fail with an insufficient-balance error, which is the worst possible
// first impression and would look like a broken product rather than a policy.
func TestTrialWalletCanAffordATypicalRequest(t *testing.T) {
	trial := trialVND * pricing.MicrosPerVND

	quote, err := pricing.Compute(
		walletHoldTokens(pricing.Tokens{Input: 1_500, Output: 4_096}),
		poolRate(),
	)
	if err != nil {
		t.Fatal(err)
	}
	held := walletHoldAmount(quote.Micros)

	if held > trial {
		t.Fatalf("hold %.0f VND exceeds the %.0f VND trial grant, so a new account cannot call at all",
			float64(held)/1e6, float64(trial)/1e6)
	}
	// Not merely affordable once: a trial has to be usable for more than a single call,
	// or it demonstrates nothing about the product.
	if calls := trial / held; calls < 5 {
		t.Errorf("a trial wallet affords only %d concurrent holds at %.0f VND each; too few to try the product",
			calls, float64(held)/1e6)
	}
}

// The minimum top-up must buy meaningfully more than one request.
//
// 10,000 VND is the smallest amount a customer can pay. If the hold consumed a large
// share of it, the product would feel more expensive than the price list says.
func TestMinimumTopupAffordsManyRequests(t *testing.T) {
	const minimumTopupVND = 10_000
	wallet := int64(minimumTopupVND) * pricing.MicrosPerVND

	quote, err := pricing.Compute(
		walletHoldTokens(pricing.Tokens{Input: 1_500, Output: 4_096}),
		poolRate(),
	)
	if err != nil {
		t.Fatal(err)
	}
	held := walletHoldAmount(quote.Micros)

	if calls := wallet / held; calls < 10 {
		t.Errorf("the minimum top-up affords only %d concurrent holds at %.0f VND each",
			calls, float64(held)/1e6)
	}
}

// The allowance must not push an ordinary request past the admission ceiling.
//
// handleReserveWallet now checks the ceiling after the floor is applied, which is correct
// -- admitting a request on a size it will not be billed at is how the shortfall happened
// -- but it means the allowance could start refusing requests that used to be served.
func TestRaisedHoldStillPassesAdmissionForALargeRequest(t *testing.T) {
	// 200k of context with max_tokens=32000, above anything a coding agent sends.
	quote, err := pricing.Compute(
		walletHoldTokens(pricing.Tokens{Input: 200_000, Output: 32_000}),
		poolRate(),
	)
	if err != nil {
		t.Fatal(err)
	}
	ceiling := walletHoldCeilingVND * pricing.MicrosPerVND
	if quote.Micros > ceiling {
		t.Errorf("a large request now quotes %d against the %d ceiling, so it would be refused",
			quote.Micros, ceiling)
	}
}
