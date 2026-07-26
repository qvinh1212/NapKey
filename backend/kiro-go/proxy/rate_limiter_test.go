package proxy

import (
	"sync"
	"testing"
	"time"
)

func TestRateLimiterEnforcesExactRPMBoundary(t *testing.T) {
	l := newRateLimiter()
	if !l.allow("key", 2, 0, 0).Allowed || !l.allow("key", 2, 0, 0).Allowed {
		t.Fatal("requests through the configured boundary should pass")
	}
	if l.allow("key", 2, 0, 0).Allowed {
		t.Fatal("request beyond the RPM boundary should be rejected")
	}
}

func TestRateLimiterEnforcesTPMAndExpiresWindow(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	l := newRateLimiter()
	l.now = func() time.Time { return now }
	if !l.allow("key", 0, 100, 60).Allowed {
		t.Fatal("tokens below the boundary should pass")
	}
	if l.allow("key", 0, 100, 41).Allowed {
		t.Fatal("tokens beyond the TPM boundary should be rejected")
	}
	now = now.Add(time.Minute)
	if !l.allow("key", 0, 100, 100).Allowed {
		t.Fatal("a new minute window should reset counters")
	}
}

func TestRateLimiterSerializesConcurrentFinalSlot(t *testing.T) {
	l := newRateLimiter()
	var wg sync.WaitGroup
	allowed := 0
	var mu sync.Mutex
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.allow("key", 1, 0, 0).Allowed {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed != 1 {
		t.Fatalf("allowed = %d, want exactly 1", allowed)
	}
}

func TestRateLimiterZeroLimitsAreUnlimited(t *testing.T) {
	l := newRateLimiter()
	for i := 0; i < 1000; i++ {
		if !l.allow("key", 0, 0, 1_000_000).Allowed {
			t.Fatal("zero limits should remain unlimited")
		}
	}
}
