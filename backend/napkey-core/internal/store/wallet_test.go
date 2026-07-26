package store

import "testing"

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
