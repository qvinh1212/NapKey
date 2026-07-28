package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNapkeyKeyIDFromName(t *testing.T) {
	const id = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"
	cases := map[string]string{
		// The shape napkey-core actually writes.
		"napkey:" + id + " (user@napkey.vn)": id,
		"napkey:" + id:                       id,
		"napkey:" + id + "(user@x.vn)":       id,
		// Keys created directly in kiro-go have no control-plane row to bill, so
		// they must not report.
		"my local key":      "",
		"":                  "",
		"napkey:":           "",
		"napkey:not-a-uuid": "",
		// A prefix that merely starts the same must not be mistaken for ours.
		"napkeyish:" + id: "",
	}
	for name, want := range cases {
		if got := napkeyKeyIDFromName(name); got != want {
			t.Errorf("napkeyKeyIDFromName(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestNewUsageRequestIDIsUnique matters because the id is the idempotency key. A
// collision would make napkey-core silently discard a real request as a duplicate,
// so the customer gets served and never billed.
func TestNewUsageRequestIDIsUnique(t *testing.T) {
	const n = 2000
	var mu sync.Mutex
	seen := make(map[string]bool, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := newUsageRequestID("account-1")
			mu.Lock()
			defer mu.Unlock()
			if seen[id] {
				t.Errorf("duplicate request id %q", id)
			}
			seen[id] = true
		}()
	}
	wg.Wait()
	if len(seen) != n {
		t.Errorf("got %d unique ids out of %d", len(seen), n)
	}
}

func TestUsageReporterSendsReport(t *testing.T) {
	var got usageReport
	var token string
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token = r.Header.Get("X-Internal-Token")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "recorded"})
		close(done)
	}))
	defer srv.Close()

	r := newUsageReporter(srv.URL, "secret-token")
	r.Report(usageReport{
		RequestID: "req-1", KeyID: "key-1", Model: "claude-sonnet-4-20250514",
		InputTokens: 100, OutputTokens: 50, CacheReadTokens: 900, Credits: 1.87,
	})
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the report never arrived")
	}
	r.Shutdown(context.Background())

	if token != "secret-token" {
		t.Errorf("X-Internal-Token = %q, want the configured token", token)
	}
	if got.RequestID != "req-1" || got.KeyID != "key-1" {
		t.Errorf("report identity = %+v", got)
	}
	// Cache reads must survive the round trip as their own field; folding them into
	// input would bill them at roughly ten times their rate.
	if got.CacheReadTokens != 900 {
		t.Errorf("CacheReadTokens = %d, want 900", got.CacheReadTokens)
	}
	if got.Credits != 1.87 {
		t.Errorf("Credits = %v, want 1.87", got.Credits)
	}
	if r.sent.Load() != 1 {
		t.Errorf("sent = %d, want 1", r.sent.Load())
	}
}

// TestUsageReporterRetriesServerErrors covers the case that decides whether usage is
// lost or billed: napkey-core returning 503 because its database is briefly
// unavailable.
func TestUsageReporterRetriesServerErrors(t *testing.T) {
	var attempts atomic.Int32
	done := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "database is down"}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "recorded"})
		once.Do(func() { close(done) })
	}))
	defer srv.Close()

	r := newUsageReporter(srv.URL, "secret-token")
	r.Report(usageReport{RequestID: "req-retry", KeyID: "key-1"})
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the report was never delivered")
	}
	r.Shutdown(context.Background())

	if attempts.Load() < 3 {
		t.Errorf("attempts = %d, want at least 3", attempts.Load())
	}
	if r.sent.Load() != 1 {
		t.Errorf("sent = %d, want 1", r.sent.Load())
	}
	if r.dropped.Load() != 0 {
		t.Errorf("dropped = %d, want 0", r.dropped.Load())
	}
}

// TestUsageReporterDoesNotRetryRejections is the other half: a 400 means the report
// is malformed and will be rejected identically forever, so retrying only wastes
// attempts and floods the control plane.
func TestUsageReporterDoesNotRetryRejections(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "keyId is required"}})
	}))
	defer srv.Close()

	r := newUsageReporter(srv.URL, "secret-token")
	r.Report(usageReport{RequestID: "req-bad"})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && r.dropped.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	r.Shutdown(context.Background())

	if attempts.Load() != 1 {
		t.Errorf("attempts = %d, want exactly 1 for a client error", attempts.Load())
	}
	if r.dropped.Load() != 1 {
		t.Errorf("dropped = %d, want 1", r.dropped.Load())
	}
}

// TestUsageReporterCountsDuplicates checks the reporter understands napkey-core's
// idempotency response, so a retry that lands twice is not counted as new usage.
func TestUsageReporterCountsDuplicates(t *testing.T) {
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "duplicate"})
		close(done)
	}))
	defer srv.Close()

	r := newUsageReporter(srv.URL, "secret-token")
	r.Report(usageReport{RequestID: "req-dup", KeyID: "key-1"})
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the report never arrived")
	}
	r.Shutdown(context.Background())

	if r.duplicate.Load() != 1 {
		t.Errorf("duplicate = %d, want 1", r.duplicate.Load())
	}
	if r.sent.Load() != 0 {
		t.Errorf("sent = %d, want 0; a duplicate is not new usage", r.sent.Load())
	}
}

// TestUsageReporterReportIsNonBlocking is the property that keeps reporting off the
// critical path: a full queue must drop and count, never block the caller, because
// the caller is finishing a customer's request.
func TestUsageReporterReportIsNonBlocking(t *testing.T) {
	// A server that never answers, so workers stay busy and the queue backs up.
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer srv.Close()
	defer close(block)

	r := newUsageReporter(srv.URL, "secret-token")
	start := time.Now()
	for i := 0; i < usageQueueSize+usageWorkers+50; i++ {
		r.Report(usageReport{RequestID: "req-flood", KeyID: "key-1"})
	}
	elapsed := time.Since(start)

	if elapsed > 3*time.Second {
		t.Errorf("enqueuing took %s; Report must not block the request path", elapsed)
	}
	if r.dropped.Load() == 0 {
		t.Error("a full queue should drop and count, so the loss is measurable")
	}
}

// TestUsageReporterNilIsSafe covers standalone kiro-go, where no control plane is
// configured. Reporting must be a no-op rather than a nil dereference.
func TestUsageReporterNilIsSafe(t *testing.T) {
	var r *usageReporter
	r.Report(usageReport{RequestID: "req-1"})
	r.Shutdown(context.Background())
	if stats := r.Stats(); stats["enabled"] != 0 {
		t.Errorf("a nil reporter should report itself disabled, got %v", stats)
	}
}

func TestUsageReportingStatusExposesDeliveryLoss(t *testing.T) {
	r := &usageReporter{queue: make(chan usageReport, 3)}
	r.sent.Store(12)
	r.dropped.Store(2)
	r.duplicate.Store(1)
	r.queue <- usageReport{RequestID: "pending-1"}

	status := usageReportingStatus(r)
	if status["sent"] != 12 || status["dropped"] != 2 || status["pending"] != 1 {
		t.Fatalf("unexpected usage reporting status: %v", status)
	}
	if status["healthy"] != 0 {
		t.Fatalf("reporting with dropped usage must be unhealthy: %v", status)
	}
}
