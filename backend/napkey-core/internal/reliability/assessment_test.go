package reliability

import "testing"

func TestEvaluateHealthyService(t *testing.T) {
	got := Evaluate(true, &DataPlaneSnapshot{
		Accounts: 4, Available: 4, RecentRequests: 100, RecentFailures: 2,
		UsageHealthy: 1, UsagePending: 1,
	}, nil)
	if got.Status != StatusOperational || len(got.Issues) != 0 {
		t.Fatalf("assessment = %+v", got)
	}
}

func TestEvaluateDetectsCapacityAndUsageFailures(t *testing.T) {
	got := Evaluate(true, &DataPlaneSnapshot{
		Accounts: 4, Available: 0, RecentRequests: 100, RecentFailures: 20,
		UsageHealthy: 0, UsageDropped: 2,
	}, nil)
	if got.Status != StatusOutage {
		t.Fatalf("status = %q, want outage", got.Status)
	}
	for _, code := range []string{"upstream_capacity_empty", "usage_reporting_unhealthy", "usage_reports_dropped", "error_rate_high"} {
		if !hasIssue(got.Issues, code) {
			t.Fatalf("missing issue %q in %+v", code, got.Issues)
		}
	}
}

func TestEvaluateMarksLowCapacityAndBacklogDegraded(t *testing.T) {
	got := Evaluate(true, &DataPlaneSnapshot{
		Accounts: 8, Available: 2, RecentRequests: 50, RecentFailures: 1,
		UsageHealthy: 1, UsagePending: 25,
	}, nil)
	if got.Status != StatusDegraded {
		t.Fatalf("status = %q, want degraded", got.Status)
	}
	if !hasIssue(got.Issues, "upstream_capacity_low") || !hasIssue(got.Issues, "usage_backlog_high") {
		t.Fatalf("issues = %+v", got.Issues)
	}
}

func TestEvaluateHardDependencies(t *testing.T) {
	if got := Evaluate(false, nil, nil); got.Status != StatusOutage || !hasIssue(got.Issues, "postgres_unreachable") {
		t.Fatalf("postgres assessment = %+v", got)
	}
	if got := Evaluate(true, nil, assertError("dial failed")); got.Status != StatusOutage || !hasIssue(got.Issues, "data_plane_unreachable") {
		t.Fatalf("data plane assessment = %+v", got)
	}
}

type assertError string

func (e assertError) Error() string { return string(e) }

func hasIssue(issues []Issue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
