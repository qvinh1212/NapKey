package proxy

import (
	"kiro-go/config"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Tests for the limit-enforcement race documented in DESIGN.md section 3.5.
//
// The proxy checks a limit BEFORE forwarding a request and records usage AFTER
// the upstream responds. Nothing reserves quota in between, so requests that
// overlap in that window all observe the same pre-request counters and all pass
// the check. Overspend is bounded by concurrency, not by the limit.
//
// These tests pin the CURRENT behaviour on purpose: they are the reference
// point for the reserve/settle rework (DESIGN.md section 6). When reservation
// lands, the assertions here are what must change, and each comment states
// which direction it must change in.
//
// The overlap is staged explicitly with a two-phase barrier rather than left to
// the scheduler. A test that only *sometimes* reproduces an overspend would be
// worse than no test at all in code that moves money.

func newLimitTestConfig(t *testing.T) {
	t.Helper()
	cfgFile := filepath.Join(t.TempDir(), "config.json")
	if err := config.Init(cfgFile); err != nil {
		t.Fatalf("config.Init: %v", err)
	}
	on := true
	if err := config.UpdateSettingsPatch(nil, &on, ""); err != nil {
		t.Fatalf("set requireApiKey=true: %v", err)
	}
}

// raceLimitCheck runs n callers that all clear the auth check before any of them
// records usage, reproducing the read-then-write window deterministically.
// Returns how many callers were admitted.
func raceLimitCheck(t *testing.T, h *Handler, keyValue, keyID string, n int, tokens int, credits float64) int {
	t.Helper()

	var checked sync.WaitGroup // every caller has finished authenticate()
	var done sync.WaitGroup    // every caller has finished recording usage
	record := make(chan struct{})
	var mu sync.Mutex
	admitted := 0

	checked.Add(n)
	done.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer done.Done()
			r := newAuthTestRequest(t, "Authorization", "Bearer "+keyValue)
			_, err := h.authenticate(r)
			if err == nil {
				mu.Lock()
				admitted++
				mu.Unlock()
			}
			checked.Done()

			// Hold here until every caller has passed its check, mirroring
			// in-flight upstream requests that have not been billed yet.
			<-record
			if err == nil {
				h.recordSuccessForApiKey(keyID, tokens, 0, credits)
			}
		}()
	}

	checked.Wait()
	close(record)
	done.Wait()
	return admitted
}

// Baseline: a single sequential caller. Usage is recorded before the next check
// runs, so the boundary behaves predictably. Note it still overshoots slightly,
// because the check admits a request without knowing what it will cost.
func TestTokenLimitHoldsWhenRequestsAreSequential(t *testing.T) {
	newLimitTestConfig(t)
	const limit = int64(100)
	const perRequest = 40

	created, err := config.AddApiKey(config.ApiKeyEntry{
		Name: "sequential", Key: "nk_test_sequential", Enabled: true, TokenLimit: limit,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	h := &Handler{}
	admitted := 0
	for i := 0; i < 10; i++ {
		if _, err := h.authenticate(newAuthTestRequest(t, "Authorization", "Bearer nk_test_sequential")); err != nil {
			break
		}
		admitted++
		h.recordSuccessForApiKey(created.ID, perRequest, 0, 0)
	}

	// 0 -> 40 -> 80 all pass the check; the third request pushes usage to 120.
	// The limit acts as a floor the last request may cross, not a hard ceiling.
	const wantAdmitted = 3
	if admitted != wantAdmitted {
		t.Fatalf("admitted = %d, want %d", admitted, wantAdmitted)
	}
	got := config.GetApiKeyEntry(created.ID)
	if got == nil {
		t.Fatalf("entry missing")
	}
	if got.TokensUsed != int64(wantAdmitted*perRequest) {
		t.Fatalf("TokensUsed = %d, want %d", got.TokensUsed, wantAdmitted*perRequest)
	}
	if _, err := h.authenticate(newAuthTestRequest(t, "Authorization", "Bearer nk_test_sequential")); err == nil {
		t.Fatalf("expected rejection once TokensUsed >= TokenLimit")
	}
}

// The race: with the checks staged to overlap, every caller is admitted and the
// key spends concurrency x perRequest regardless of TokenLimit.
func TestTokenLimitRaceAllowsOverspendWhenConcurrent(t *testing.T) {
	newLimitTestConfig(t)
	const limit = int64(100)
	const perRequest = 40
	const concurrency = 16

	created, err := config.AddApiKey(config.ApiKeyEntry{
		Name: "race", Key: "nk_test_race", Enabled: true, TokenLimit: limit,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	h := &Handler{}
	admitted := raceLimitCheck(t, h, "nk_test_race", created.ID, concurrency, perRequest, 0)

	// Documented defect. Once reserve/settle lands this must become
	// admitted <= 3 and TokensUsed <= limit.
	if admitted != concurrency {
		t.Fatalf("admitted = %d, want %d: every overlapping caller should pass the "+
			"pre-request check today; if reservation was implemented, tighten this "+
			"test to assert the limit holds", admitted, concurrency)
	}
	got := config.GetApiKeyEntry(created.ID)
	if got == nil {
		t.Fatalf("entry missing")
	}
	wantUsed := int64(concurrency * perRequest)
	if got.TokensUsed != wantUsed {
		t.Fatalf("TokensUsed = %d, want %d", got.TokensUsed, wantUsed)
	}
	if got.TokensUsed <= limit {
		t.Fatalf("expected overspend, got TokensUsed %d <= limit %d", got.TokensUsed, limit)
	}
	t.Logf("limit=%d admitted=%d tokensUsed=%d (%.0fx over limit)",
		limit, admitted, got.TokensUsed, float64(got.TokensUsed)/float64(limit))

	// The gate does close once usage is recorded, so the breach is bounded to a
	// single burst rather than being permanent.
	if _, err := h.authenticate(newAuthTestRequest(t, "Authorization", "Bearer nk_test_race")); err == nil {
		t.Fatalf("expected the key to be refused after the burst")
	}
}

// Same defect on the credit axis. Credits map to a wallet balance, so this is
// the variant that becomes an unpaid bill rather than merely free tokens.
func TestCreditLimitRaceAllowsOverspendWhenConcurrent(t *testing.T) {
	newLimitTestConfig(t)
	const limit = 1.0
	const perRequest = 0.5
	const concurrency = 12

	created, err := config.AddApiKey(config.ApiKeyEntry{
		Name: "credit-race", Key: "nk_test_credit", Enabled: true, CreditLimit: limit,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	h := &Handler{}
	admitted := raceLimitCheck(t, h, "nk_test_credit", created.ID, concurrency, 0, perRequest)
	if admitted != concurrency {
		t.Fatalf("admitted = %d, want %d", admitted, concurrency)
	}

	got := config.GetApiKeyEntry(created.ID)
	if got == nil {
		t.Fatalf("entry missing")
	}
	wantUsed := float64(concurrency) * perRequest
	if got.CreditsUsed != wantUsed {
		t.Fatalf("CreditsUsed = %v, want %v", got.CreditsUsed, wantUsed)
	}
	// Tighten to CreditsUsed <= limit once wallet reservation lands.
	if got.CreditsUsed <= limit {
		t.Fatalf("expected credit overspend, got %v <= limit %v", got.CreditsUsed, limit)
	}
	t.Logf("creditLimit=%v creditsUsed=%v (%.1fx over limit)",
		limit, got.CreditsUsed, got.CreditsUsed/limit)

	// Confirm the refusal is the credit path specifically, not the token path.
	_, err = h.authenticate(newAuthTestRequest(t, "Authorization", "Bearer nk_test_credit"))
	ae, ok := err.(*authError)
	if !ok {
		t.Fatalf("expected *authError after the burst, got %T (%v)", err, err)
	}
	if ae.status != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", ae.status)
	}
	if !strings.Contains(ae.message, "credit limit") {
		t.Fatalf("expected credit limit message, got %q", ae.message)
	}
}

// The kill switch must not race. Enabled is read fresh on every check and has no
// read-then-write window, so a disabled key stays refused even under the same
// staged burst. napkey-core relies on this to cut off a key when the wallet runs
// dry (DESIGN.md section 4), so it is worth pinning separately from the counters.
func TestDisabledKeyIsRefusedUnderConcurrency(t *testing.T) {
	newLimitTestConfig(t)
	created, err := config.AddApiKey(config.ApiKeyEntry{
		Name: "disabled", Key: "nk_test_disabled", Enabled: false,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	h := &Handler{}
	admitted := raceLimitCheck(t, h, "nk_test_disabled", created.ID, 16, 10, 0)
	if admitted != 0 {
		t.Fatalf("disabled key admitted %d requests; the kill switch must never race", admitted)
	}
	got := config.GetApiKeyEntry(created.ID)
	if got == nil {
		t.Fatalf("entry missing")
	}
	if got.TokensUsed != 0 || got.RequestsCount != 0 {
		t.Fatalf("disabled key accrued usage: %+v", got)
	}
}
