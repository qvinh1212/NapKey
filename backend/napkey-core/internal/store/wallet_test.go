package store

import (
	"math"
	"testing"
)

func TestValidateTopupAmount(t *testing.T) {
	if err := ValidateTopupAmount(19_999_000_000); err == nil {
		t.Fatal("top-up below 20,000 VND must be rejected")
	}
	if err := ValidateTopupAmount(20_000_000_000); err != nil {
		t.Fatalf("minimum top-up should be accepted: %v", err)
	}
}

func TestNormalizeTopupStatus(t *testing.T) {
	if got := topupStatus(50_000_000_000, 40_000_000_000); got != TopupUnderpaid {
		t.Fatalf("status = %q, want underpaid", got)
	}
	if got := topupStatus(50_000_000_000, 60_000_000_000); got != TopupPaid {
		t.Fatalf("status = %q, want paid", got)
	}
}

func TestWalletCreditValuePreservesLegacyPurchases(t *testing.T) {
	got, err := walletCreditMicros(45_000_000_000, 45)
	if err != nil {
		t.Fatalf("walletCreditMicros: %v", err)
	}
	if got != 60_000_000_000 {
		t.Fatalf("credited micros = %d, want 60000000000", got)
	}

	got, err = walletCreditMicros(60_000_000_000, 60)
	if err != nil || got != 60_000_000_000 {
		t.Fatalf("current-rate credit = %d, %v", got, err)
	}
	if _, err := walletCreditMicros(math.MaxInt64, 1); err == nil {
		t.Fatal("overflowing credit conversion should fail")
	}
}
