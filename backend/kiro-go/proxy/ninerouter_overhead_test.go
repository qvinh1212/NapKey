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
