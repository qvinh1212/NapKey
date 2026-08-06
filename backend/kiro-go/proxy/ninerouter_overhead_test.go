package proxy

import "testing"

// A normal request must not be reported as overhead.
//
// The estimator is approximate, so a small gap between its count and the upstream's is
// expected on every request. Reporting those would bury a real finding under noise.
func TestSmallEstimateGapIsNotReported(t *testing.T) {
	for _, tc := range []struct{ estimated, reported int }{
		{100, 100},
		{100, 112},
		{4000, 4200},
		{100, 599},
	} {
		if got := promptOverhead(tc.estimated, tc.reported); got != 0 {
			t.Errorf("estimated=%d reported=%d: reported overhead %d, want none",
				tc.estimated, tc.reported, got)
		}
	}
}

// A systemic injected prompt must be reported.
//
// These are the numbers measured against the live upstream: a one-word prompt billed as
// ~4,541 input tokens.
func TestInjectedPromptIsReported(t *testing.T) {
	got := promptOverhead(3, 4541)
	if got != 4538 {
		t.Fatalf("overhead = %d, want 4538", got)
	}
}

// Overhead is only meaningful when both numbers are known.
//
// A request the estimator could not size, or one the upstream reported nothing for,
// would otherwise show the entire token count as overhead.
func TestOverheadNeedsBothNumbers(t *testing.T) {
	for _, tc := range []struct {
		name                string
		estimated, reported int
	}{
		{"no estimate", 0, 4541},
		{"no report", 100, 0},
		{"neither", 0, 0},
		{"negative estimate", -5, 4541},
	} {
		if got := promptOverhead(tc.estimated, tc.reported); got != 0 {
			t.Errorf("%s: overhead = %d, want 0", tc.name, got)
		}
	}
}

// An upstream billing less than the caller sent is not negative overhead.
//
// Happens when the upstream tokenises more efficiently than the estimator guesses. It
// is not a finding, and must not be reported as a negative number.
func TestUpstreamBillingLessIsNotOverhead(t *testing.T) {
	if got := promptOverhead(5000, 4000); got != 0 {
		t.Errorf("overhead = %d, want 0 when the upstream bills less", got)
	}
}

// The upstream does not enforce max_tokens: a request capped at 600 was measured
// returning 751 to 1,431 tokens depending on the model. The caller is billed for the
// overage, so it has to be visible.
func TestOutputBudgetExceededFlagsAnIgnoredCap(t *testing.T) {
	if !outputBudgetExceeded(600, 1431) {
		t.Error("a response more than twice its budget must be reported")
	}
	if !outputBudgetExceeded(600, 751) {
		t.Error("a response 25% over its budget must be reported")
	}
}

// A few tokens over is the two tokenisers disagreeing, not a cap that failed. Reporting
// it would bury the real cases in noise.
func TestOutputBudgetExceededIgnoresRounding(t *testing.T) {
	if outputBudgetExceeded(600, 610) {
		t.Error("a response within rounding of its budget is not a finding")
	}
	if outputBudgetExceeded(600, 600) {
		t.Error("a response exactly at its budget is not a finding")
	}
}

// A caller who set no budget cannot have one exceeded. max_tokens is optional on the
// OpenAI path, so this is the common case rather than an edge case.
func TestOutputBudgetExceededNeedsABudget(t *testing.T) {
	if outputBudgetExceeded(0, 5000) {
		t.Error("no budget means no overshoot to report")
	}
	if outputBudgetExceeded(600, 0) {
		t.Error("an unmeasured response is not an overshoot")
	}
}
