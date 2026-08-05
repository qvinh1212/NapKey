package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

// 9Router is the upstream NapKey serves from, so an unconfigured process routes
// there rather than to an account pool that may hold nothing.
func TestNineRouterEnabledByDefault(t *testing.T) {
	original, existed := os.LookupEnv(envNineRouterEnabled)
	os.Unsetenv(envNineRouterEnabled)
	t.Cleanup(func() {
		if existed {
			os.Setenv(envNineRouterEnabled, original)
			return
		}
		os.Unsetenv(envNineRouterEnabled)
	})

	if !nineRouterConfigured() {
		t.Error("with the flag unset the 9Router upstream must be used")
	}
}

// Only an explicit false turns it off. A typo keeps the configured upstream instead
// of falling back to a pool that would fail every request.
func TestNineRouterOnlyExplicitFalseDisables(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"false", false}, {"FALSE", false}, {"0", false}, {"no", false}, {"off", false}, {" off ", false},
		{"true", true}, {"1", true}, {"yes", true}, {"on", true},
		// Neither on nor off: keep serving from the configured upstream rather than
		// silently switching to the pool because of a typo.
		{"maybe", true}, {"tru", true}, {"", true},
	} {
		t.Setenv(envNineRouterEnabled, tc.value)
		if got := nineRouterConfigured(); got != tc.want {
			t.Errorf("%s=%q -> %v, want %v", envNineRouterEnabled, tc.value, got, tc.want)
		}
	}
}

// Capacity must describe the upstream that is actually serving.
//
// napkey-core turns available <= 0 into an outage on the public status page, so
// reporting the empty local pool while 9Router serves traffic would show customers
// an outage during normal operation.
func TestCapacityReportsNineRouterNotTheEmptyPool(t *testing.T) {
	t.Setenv(envNineRouterEnabled, "true")
	t.Setenv(envNineRouterBaseURL, "http://127.0.0.1:20242/v1")
	t.Setenv(envNineRouterAPIKey, "test-key")

	h := &Handler{}
	got := h.capacity()
	if got.Available < 1 {
		t.Errorf("available = %d: a reachable upstream is capacity, even with no local accounts", got.Available)
	}
	if got.Accounts < 1 {
		t.Errorf("accounts = %d, want at least the one upstream link", got.Accounts)
	}
}

// A misconfigured upstream must be reported, not passed off as usable.
//
// main logs this at startup, so returning nil here would put "Upstream: " in the
// deploy log for a process that cannot serve, and the operator would go looking for
// the fault somewhere else entirely.
func TestDescribeUpstreamRefusesIncompleteConfig(t *testing.T) {
	t.Setenv(envNineRouterEnabled, "true")
	t.Setenv(envNineRouterBaseURL, "")
	t.Setenv(envNineRouterAPIKey, "")

	resetNineRouterClient(t)

	if _, err := DescribeUpstream(); err == nil {
		t.Fatal("an enabled upstream with no endpoint or key must not report itself usable")
	}
}

// The probe reports unreachability as a 503 with a reason, not as a healthy status.
func TestUpstreamProbeReportsUnreachableUpstream(t *testing.T) {
	// A port nothing listens on: the dial fails fast and deterministically.
	t.Setenv(envNineRouterEnabled, "true")
	t.Setenv(envNineRouterBaseURL, "http://127.0.0.1:1/v1")
	t.Setenv(envNineRouterAPIKey, "probe-key")
	t.Setenv(envNineRouterTimeout, "5000")

	resetNineRouterClient(t)

	h := &Handler{}
	rec := httptest.NewRecorder()
	h.apiUpstreamProbe(rec, httptest.NewRequest(http.MethodPost, "/admin/api/upstream/probe", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 when the upstream does not answer", rec.Code)
	}
	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding the probe response: %v", err)
	}
	if ok, _ := body["ok"].(bool); ok {
		t.Error("ok must be false when the upstream is unreachable")
	}
	if msg, _ := body["error"].(string); strings.TrimSpace(msg) == "" {
		t.Error("the probe must say why it failed, or it cannot be acted on")
	}
	// The endpoint key must never reach an operator-visible payload.
	if strings.Contains(strings.ToLower(rec.Body.String()), "probe-key") {
		t.Error("the upstream key leaked into the probe response")
	}
}

// resetNineRouterClient clears the memoised client so a test's environment applies.
//
// getNineRouterClient builds once per process by design, so without this a test that
// runs after any other 9Router test would assert against the first one's config.
func resetNineRouterClient(t *testing.T) {
	t.Helper()
	reset := func() {
		nineRouterOnce = sync.Once{}
		nineRouterShared = nil
		nineRouterErr = nil
	}
	reset()
	t.Cleanup(reset)
}
