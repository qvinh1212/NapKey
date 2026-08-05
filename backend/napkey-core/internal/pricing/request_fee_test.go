package pricing

import "testing"

// A request fee is charged once, on top of the token rates.
func TestComputeAddsRequestFee(t *testing.T) {
	rate := Rate{InputPer1k: 12_000_000, OutputPer1k: 12_000_000, RequestFee: 480_000_000}

	cost, err := Compute(Tokens{Input: 12_000, Output: 3_000}, rate)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	// 12,000 tokens * 12,000,000 / 1,000 = 144,000,000; output 3,000 -> 36,000,000.
	wantTokens := int64(144_000_000 + 36_000_000)
	if cost.InputMicros+cost.OutputMicros != wantTokens {
		t.Errorf("token component = %d, want %d", cost.InputMicros+cost.OutputMicros, wantTokens)
	}
	if cost.RequestFeeMicros != 480_000_000 {
		t.Errorf("fee = %d, want 480000000", cost.RequestFeeMicros)
	}
	if cost.Micros != wantTokens+480_000_000 {
		t.Errorf("total = %d, want %d", cost.Micros, wantTokens+480_000_000)
	}
	// The fee dominates on agent-sized traffic, which is the reason it exists.
	if cost.RequestFeeMicros <= wantTokens {
		t.Errorf("expected the flat fee to exceed the token cost on a small request")
	}
}

// A request that reported no tokens still owes the fee: the upstream call was made.
func TestRequestFeeAppliesWithZeroTokens(t *testing.T) {
	rate := Rate{InputPer1k: 12_000_000, OutputPer1k: 12_000_000, RequestFee: 960_000_000}

	cost, err := Compute(Tokens{}, rate)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if cost.Micros != 960_000_000 {
		t.Fatalf("cost = %d, want the bare request fee 960000000", cost.Micros)
	}
}

// UpstreamRate must swap the fee as well. If it kept the retail fee, margin
// reporting would treat revenue as cost and understate the margin on every request.
func TestUpstreamRateSwapsRequestFee(t *testing.T) {
	rate := Rate{
		InputPer1k: 12_000_000, UpstreamInputPer1k: 2_097_000,
		RequestFee: 480_000_000, UpstreamRequestFee: 84_000_000,
	}

	upstream := rate.UpstreamRate()
	if upstream.RequestFee != 84_000_000 {
		t.Fatalf("upstream fee = %d, want 84000000", upstream.RequestFee)
	}

	retailCost, err := Compute(Tokens{Input: 10_000}, rate)
	if err != nil {
		t.Fatalf("retail: %v", err)
	}
	upstreamCost, err := Compute(Tokens{Input: 10_000}, upstream)
	if err != nil {
		t.Fatalf("upstream: %v", err)
	}
	if upstreamCost.Micros >= retailCost.Micros {
		t.Fatalf("upstream cost %d must be below retail %d", upstreamCost.Micros, retailCost.Micros)
	}
}

// A negative fee would credit the wallet, so it is refused rather than clamped.
func TestNegativeRequestFeeIsRefused(t *testing.T) {
	if _, err := Compute(Tokens{Input: 1_000}, Rate{InputPer1k: 12_000_000, RequestFee: -1}); err == nil {
		t.Fatal("expected a negative request fee to be rejected")
	}
}
