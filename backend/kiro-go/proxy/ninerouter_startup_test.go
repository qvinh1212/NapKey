package proxy

import (
	"strings"
	"testing"

	"kiro-go/config"
	accountpool "kiro-go/pool"
)

// emptyAccountPool points the config at a fresh file so the pool holds no accounts.
//
// The pool is a process-wide singleton, so any earlier test that imported an account
// leaves one behind. Without this, a test asserting on the empty-pool case passes or
// fails depending on which tests ran before it.
func emptyAccountPool(t *testing.T) {
	t.Helper()
	if err := config.Init(t.TempDir() + "/config.json"); err != nil {
		t.Fatalf("config.Init: %v", err)
	}
	accountpool.GetPool().Reload()
}

// An unusable upstream must be reported, not hidden.
//
// This is what the startup check is for: the operator learns from the deploy log that
// nothing can be served, instead of from the first customer to be refused.
func TestDescribeUpstreamReportsAnUnusableUpstream(t *testing.T) {
	emptyAccountPool(t)
	resetNineRouterClient(t)
	t.Setenv(envNineRouterEnabled, "false")

	_, err := DescribeUpstream()
	if err == nil {
		t.Fatal("an empty pool with 9Router disabled reported a usable upstream")
	}
	if !strings.Contains(err.Error(), "NINEROUTER_ENABLED") {
		t.Errorf("the error does not say how to fix it: %v", err)
	}
}

// Reporting an unusable upstream must stay a value, never an exit.
//
// An empty account pool is the state a fresh deployment starts in, and it is repaired
// through the admin panel this same process serves, so exiting here removes the only
// way to fix it. A NapKey deploy failed exactly this way: kiro-go exited on an empty
// pool, and because napkey-core waits for kiro-go to become healthy, the whole stack
// refused to start with no way in.
//
// The guard is that DescribeUpstream returns its finding instead of acting on it. This
// calls it twice on a deployment that cannot serve and requires the process to survive,
// which it would not if the check regained the power to terminate.
func TestDescribeUpstreamDoesNotEndTheProcess(t *testing.T) {
	emptyAccountPool(t)
	resetNineRouterClient(t)
	t.Setenv(envNineRouterEnabled, "false")

	_, _ = DescribeUpstream()

	// Reached only if the first call returned rather than exiting.
	if _, err := DescribeUpstream(); err == nil {
		t.Fatal("expected the upstream to still be unusable on the second call")
	}
}

// A configured upstream must be described, not merely accepted.
//
// The description goes in the deploy log so an operator can confirm which upstream and
// which model pool will serve traffic before any of it is customer traffic.
func TestDescribeUpstreamNamesTheUpstreamAndPool(t *testing.T) {
	resetNineRouterClient(t)
	t.Setenv(envNineRouterEnabled, "true")
	t.Setenv(envNineRouterBaseURL, "https://gateway.example.com/v1")
	t.Setenv(envNineRouterAPIKey, "test-key")
	t.Setenv(envNineRouterModelPrefix, "Viberouter/")

	got, err := DescribeUpstream()
	if err != nil {
		t.Fatalf("a configured upstream was reported unusable: %v", err)
	}
	if !strings.Contains(got, "gateway.example.com") {
		t.Errorf("description %q does not name the upstream", got)
	}
	if !strings.Contains(got, "Viberouter/") {
		t.Errorf("description %q does not name the model pool", got)
	}
	if strings.Contains(got, "test-key") {
		t.Errorf("description %q leaks the api key into the deploy log", got)
	}
}
