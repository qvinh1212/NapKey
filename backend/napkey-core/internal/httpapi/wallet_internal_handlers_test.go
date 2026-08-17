package httpapi

import (
	"testing"

	"napkey-core/internal/pricing"
)

func TestWalletHoldAmountCapsLargeQuotesAtTheCeiling(t *testing.T) {
	want := walletHoldCeilingVND * pricing.MicrosPerVND
	if got := walletHoldAmount(100_000 * pricing.MicrosPerVND); got != want {
		t.Fatalf("walletHoldAmount() = %d, want %d", got, want)
	}
}

func TestWalletHoldAmountKeepsSmallerQuotes(t *testing.T) {
	quote := int64(240) * pricing.MicrosPerVND
	if got := walletHoldAmount(quote); got != quote {
		t.Fatalf("walletHoldAmount() = %d, want %d", got, quote)
	}
}
