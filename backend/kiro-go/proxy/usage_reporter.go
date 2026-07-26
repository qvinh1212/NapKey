package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"kiro-go/logger"
)

// The usage reporter forwards per-request usage to the napkey-core control plane.
//
// Until now kiro-go recorded usage only in its own config.json counters, which made
// the control plane's usage view an educated guess. napkey-core owns billing, so it
// needs the per-request detail: token counts split by kind, the model, and an
// idempotency key.
//
// Design constraints that shaped this:
//
// It must never slow down or fail a customer's request. Reporting happens after the
// response is already written, on a bounded worker pool, and a failure to report is
// logged rather than surfaced.
//
// It must not lose usage silently. Every send is retried with backoff, and anything
// dropped is counted so the drop is visible rather than invisible.
//
// It must be safe to retry. Each report carries a request id that napkey-core
// deduplicates on, so a retry after an ambiguous failure cannot double-bill.

// usageReportEnvKeys are the environment variables that configure reporting.
const (
	envUsageReportURL   = "NAPKEY_CORE_URL"
	envUsageReportToken = "NAPKEY_INTERNAL_TOKEN"
)

// usageQueueSize bounds the backlog. When napkey-core is down the queue fills and
// further reports are dropped with a counter, which is preferable to growing an
// unbounded buffer until the proxy is killed for memory.
const usageQueueSize = 4096

// usageWorkers is how many reports are in flight at once. Small on purpose:
// reporting is not latency sensitive and a burst of connections to the control
// plane during a traffic spike is its own problem.
const usageWorkers = 4

// usageMaxAttempts bounds retries per report. With the backoff below this spans
// roughly ten seconds, long enough to ride out a restart of the control plane and
// short enough that a worker is not tied up indefinitely.
const usageMaxAttempts = 4

// usageReport is one request's usage, matching napkey-core's /internal/usage body.
//
// No cost field: the data plane reports what it measured and the control plane
// decides what it costs. Sending a price from here would make the amount charged a
// function of what this process claims.
type usageReport struct {
	RequestID string `json:"requestId"`
	KeyID     string `json:"keyId"`
	Model     string `json:"model"`

	InputTokens      int64 `json:"inputTokens"`
	OutputTokens     int64 `json:"outputTokens"`
	CacheReadTokens  int64 `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int64 `json:"cacheWriteTokens,omitempty"`

	UpstreamAccountID string `json:"upstreamAccountId,omitempty"`
	LatencyMS         *int   `json:"latencyMs,omitempty"`
	Status            string `json:"status,omitempty"`
	TokensEstimated   bool   `json:"tokensEstimated,omitempty"`
	OccurredAt        string `json:"occurredAt,omitempty"`
}

// usageReporter ships reports to napkey-core.
type usageReporter struct {
	baseURL string
	token   string
	client  *http.Client

	queue chan usageReport
	wg    sync.WaitGroup

	// Counters for the admin stats view. Dropped is the one that matters: a
	// non-zero value means usage was measured but never billed.
	queued    atomic.Int64
	sent      atomic.Int64
	dropped   atomic.Int64
	duplicate atomic.Int64

	stop     chan struct{}
	stopOnce sync.Once
}

// globalUsageReporter is the process-wide reporter, nil when reporting is not
// configured. A nil reporter makes every Report call a no-op, which is what keeps
// standalone kiro-go (no control plane) working unchanged.
var (
	globalUsageReporter *usageReporter
	usageReporterOnce   sync.Once
)

// UsageReporter returns the process reporter, starting it on first use.
//
// Reporting stays off unless both the URL and the shared token are present.
// Starting it with a URL but no token would send unauthenticated requests that the
// control plane rejects, and the resulting error log would be noise on every
// request.
func UsageReporter() *usageReporter {
	usageReporterOnce.Do(func() {
		baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv(envUsageReportURL)), "/")
		token := strings.TrimSpace(os.Getenv(envUsageReportToken))
		if baseURL == "" || token == "" {
			if baseURL != "" && token == "" {
				logger.Warnf("[Usage] %s is set but %s is empty, so usage reporting stays off",
					envUsageReportURL, envUsageReportToken)
			}
			return
		}
		globalUsageReporter = newUsageReporter(baseURL, token)
		logger.Infof("[Usage] reporting usage to %s", baseURL)
	})
	return globalUsageReporter
}

func newUsageReporter(baseURL, token string) *usageReporter {
	r := &usageReporter{
		baseURL: baseURL,
		token:   token,
		client: &http.Client{
			// Generous relative to the work, but bounded: a hung control plane must
			// not pin a worker forever.
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
				MaxIdleConns:          usageWorkers * 2,
				MaxIdleConnsPerHost:   usageWorkers * 2,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 10 * time.Second,
			},
		},
		queue: make(chan usageReport, usageQueueSize),
		stop:  make(chan struct{}),
	}
	for i := 0; i < usageWorkers; i++ {
		r.wg.Add(1)
		go r.worker()
	}
	return r
}

// Report enqueues a usage report without blocking.
//
// The non-blocking send is the important part: this is called from the request path
// after the response has been written, and blocking here would hold a connection
// open because the control plane is slow. A full queue drops the report and
// increments a counter, so the loss is measurable.
func (r *usageReporter) Report(report usageReport) bool {
	if r == nil {
		return false
	}
	select {
	case r.queue <- report:
		r.queued.Add(1)
		return true
	default:
		r.dropped.Add(1)
		logger.Warnf("[Usage] queue is full, dropped the report for request %s (key %s); %d dropped so far",
			report.RequestID, report.KeyID, r.dropped.Load())
		return false
	}
}

// Stats returns the counters for the admin view. These counters are also exposed
// under usageReporting in /admin/api/status and /admin/api/stats.
func (r *usageReporter) Stats() map[string]int64 {
	if r == nil {
		return map[string]int64{"enabled": 0}
	}
	return map[string]int64{
		"enabled":   1,
		"queued":    r.queued.Load(),
		"sent":      r.sent.Load(),
		"duplicate": r.duplicate.Load(),
		"dropped":   r.dropped.Load(),
		"pending":   int64(len(r.queue)),
	}
}

// usageReportingStatus is the operational truth exposed by the admin status API.
// A dropped report is measured traffic that never reached the billing ledger, so
// it makes the reporter unhealthy even if later reports are succeeding.
func usageReportingStatus(r *usageReporter) map[string]int64 {
	stats := r.Stats()
	healthy := int64(0)
	if stats["enabled"] == 1 && stats["dropped"] == 0 {
		healthy = 1
	}
	stats["healthy"] = healthy
	return stats
}

// Shutdown drains the queue, bounded by ctx.
//
// Called on a clean shutdown so usage already measured is not thrown away because
// the process is exiting.
func (r *usageReporter) Shutdown(ctx context.Context) {
	if r == nil {
		return
	}
	r.stopOnce.Do(func() { close(r.stop) })

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		logger.Warnf("[Usage] shutdown timed out with %d report(s) still queued", len(r.queue))
	}
}

func (r *usageReporter) worker() {
	defer r.wg.Done()
	for {
		select {
		case report := <-r.queue:
			r.deliver(report)
		case <-r.stop:
			// Drain what is already queued before exiting. These reports describe
			// traffic that was served, so discarding them loses revenue.
			for {
				select {
				case report := <-r.queue:
					r.deliver(report)
				default:
					return
				}
			}
		}
	}
}

// deliver posts one report, retrying transient failures.
func (r *usageReporter) deliver(report usageReport) {
	body, err := json.Marshal(report)
	if err != nil {
		// Unmarshalable payload will never succeed, so retrying is pointless.
		r.dropped.Add(1)
		logger.Errorf("[Usage] could not encode the report for request %s: %v", report.RequestID, err)
		return
	}

	backoff := 500 * time.Millisecond
	for attempt := 1; attempt <= usageMaxAttempts; attempt++ {
		status, retryable, err := r.post(body)
		switch {
		case err == nil && status == "duplicate":
			// napkey-core already had this request id. The idempotency key did its
			// job; nothing more to do.
			r.duplicate.Add(1)
			return
		case err == nil:
			r.sent.Add(1)
			return
		case !retryable:
			// A rejected report is a contract bug, not a transient fault. Retrying
			// would repeat the same rejection.
			r.dropped.Add(1)
			logger.Errorf("[Usage] the control plane rejected the report for request %s: %v",
				report.RequestID, err)
			return
		}

		if attempt == usageMaxAttempts {
			r.dropped.Add(1)
			logger.Errorf("[Usage] gave up reporting request %s after %d attempts: %v; this usage will not be billed",
				report.RequestID, attempt, err)
			return
		}
		// Wait, unless shutting down. The retry is safe because the request id makes
		// the write idempotent on the other side.
		select {
		case <-time.After(backoff):
		case <-r.stop:
			r.dropped.Add(1)
			logger.Warnf("[Usage] shutting down before request %s could be reported", report.RequestID)
			return
		}
		backoff *= 2
	}
}

// post sends the body once. It reports whether the failure is worth retrying.
func (r *usageReporter) post(body []byte) (status string, retryable bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/internal/usage", bytes.NewReader(body))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", r.token)

	resp, err := r.client.Do(req)
	if err != nil {
		// Network failures are exactly what retrying is for.
		return "", true, err
	}
	defer resp.Body.Close()

	var decoded struct {
		Status string `json:"status"`
		Error  struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&decoded)

	switch {
	case resp.StatusCode == http.StatusOK:
		return decoded.Status, false, nil
	case resp.StatusCode == http.StatusUnauthorized:
		// A wrong shared token will not fix itself, and retrying would hammer the
		// control plane with rejected requests.
		return "", false, fmt.Errorf("the control plane rejected the internal token (401)")
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return "", false, fmt.Errorf("http %d: %s", resp.StatusCode, decoded.Error.Message)
	default:
		// 5xx, including the 503 napkey-core returns when its database is
		// unavailable. Retry.
		return "", true, fmt.Errorf("http %d: %s", resp.StatusCode, decoded.Error.Message)
	}
}

// napkeyKeyIDFromName extracts the napkey-core key id from a key's name.
//
// napkey-core names the keys it provisions "napkey:<uuid> (email)", so the control
// plane's id travels with the key and usage can be attributed without kiro-go
// storing anything extra. A key created directly in kiro-go has no such prefix and
// returns "", which correctly disables reporting for it: napkey-core has no row to
// attribute the usage to.
func napkeyKeyIDFromName(name string) string {
	const prefix = "napkey:"
	if !strings.HasPrefix(name, prefix) {
		return ""
	}
	rest := name[len(prefix):]
	if idx := strings.IndexAny(rest, " \t("); idx >= 0 {
		rest = rest[:idx]
	}
	rest = strings.TrimSpace(rest)
	// A UUID is 36 characters. Checking the shape keeps a malformed name from
	// becoming a bogus key id in the report.
	if len(rest) != 36 {
		return ""
	}
	return rest
}

// newUsageRequestID builds the idempotency key for one request.
//
// It has to be unique per request and stable across retries of the same report.
// The account id and a monotonic counter make it unique within this process, and
// the process start time keeps it unique across restarts.
func newUsageRequestID(accountID string) string {
	n := usageRequestCounter.Add(1)
	var b strings.Builder
	b.WriteString("kiro-")
	b.WriteString(strconv.FormatInt(usageProcessEpoch, 36))
	b.WriteByte('-')
	if accountID != "" {
		// Keep it short; this is an identifier, not a log line.
		if len(accountID) > 12 {
			accountID = accountID[:12]
		}
		b.WriteString(accountID)
		b.WriteByte('-')
	}
	b.WriteString(strconv.FormatInt(n, 36))
	return b.String()
}

var (
	usageRequestCounter atomic.Int64
	usageProcessEpoch   = time.Now().UnixNano()
)
