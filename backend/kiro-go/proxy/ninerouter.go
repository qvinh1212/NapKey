package proxy

// 9Router is an alternative upstream: an OpenAI-compatible gateway that fronts a
// provider pool of its own. It replaces the account pool in this process rather
// than sitting beside it, so when it is enabled there are no OAuth tokens to
// refresh, no per-account failover, and no metering events.
//
// That last point is the one that matters to billing. The Kiro path reports a
// credit meter emitted by the upstream, and settlement multiplies it by the retail
// credit rate. 9Router speaks the OpenAI protocol, which has no such field: it
// reports prompt_tokens and completion_tokens and nothing else. Usage from this
// path is therefore reported with credits = 0, and napkey-core prices it from the
// token counts against model_prices. Reporting a fabricated credit number here
// would put an invented quantity into the ledger.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"kiro-go/logger"
)

const (
	envNineRouterEnabled  = "NINEROUTER_ENABLED"
	envNineRouterBaseURL  = "NINEROUTER_RUNTIME_BASE_URL"
	envNineRouterAPIKey   = "NINEROUTER_API_KEY"
	envNineRouterCFID     = "NINEROUTER_CF_ACCESS_CLIENT_ID"
	envNineRouterCFSecret = "NINEROUTER_CF_ACCESS_CLIENT_SECRET"
	envNineRouterTimeout  = "NINEROUTER_TIMEOUT_MS"
)

// nineRouterDefaultTimeout bounds one upstream call. Long enough for a slow
// completion, short enough that a hung upstream does not pin a connection.
const nineRouterDefaultTimeout = 180 * time.Second

// nineRouterClient talks to the 9Router runtime endpoint.
type nineRouterClient struct {
	baseURL    string
	apiKey     string
	cfClientID string
	cfSecret   string
	http       *http.Client
}

var (
	nineRouterOnce   sync.Once
	nineRouterShared *nineRouterClient
	nineRouterErr    error
)

// nineRouterConfigured reports whether this process should route through 9Router.
//
// 9Router is the upstream NapKey serves from, so this defaults to on. The account
// pool remains in the code for the case where an operator explicitly opts back into
// it, which is why the switch exists at all rather than the pool being deleted.
//
// Only an explicit false value turns it off. A typo therefore keeps the configured
// upstream instead of silently falling back to a pool that may hold no accounts,
// which would fail every request with "no available account" and look like an
// upstream outage rather than a configuration mistake.
func nineRouterConfigured() bool {
	raw, set := os.LookupEnv(envNineRouterEnabled)
	if !set {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "0", "false", "no", "off":
		return false
	}
	return true
}

// getNineRouterClient builds the shared client once.
//
// A configuration error is returned rather than logged and skipped. If the operator
// asked for 9Router and it cannot be reached, failing the request is the honest
// outcome: the alternative is serving traffic through an upstream the operator did
// not select, and billing it as if they had.
func getNineRouterClient() (*nineRouterClient, error) {
	nineRouterOnce.Do(func() {
		base := strings.TrimSpace(os.Getenv(envNineRouterBaseURL))
		key := strings.TrimSpace(os.Getenv(envNineRouterAPIKey))
		if base == "" || key == "" {
			nineRouterErr = fmt.Errorf("%s is set but %s and %s are both required",
				envNineRouterEnabled, envNineRouterBaseURL, envNineRouterAPIKey)
			return
		}
		cfID := strings.TrimSpace(os.Getenv(envNineRouterCFID))
		cfSecret := strings.TrimSpace(os.Getenv(envNineRouterCFSecret))
		// Cloudflare Access needs both halves of the service token or neither. One
		// alone yields a 403 that looks like an upstream outage.
		if (cfID == "") != (cfSecret == "") {
			nineRouterErr = fmt.Errorf("%s and %s must be set together", envNineRouterCFID, envNineRouterCFSecret)
			return
		}
		nineRouterShared = &nineRouterClient{
			baseURL:    strings.TrimRight(base, "/"),
			apiKey:     key,
			cfClientID: cfID,
			cfSecret:   cfSecret,
			http:       &http.Client{Timeout: nineRouterTimeout()},
		}
		logger.Infof("[9Router] enabled, upstream %s", redactURL(nineRouterShared.baseURL))
	})
	return nineRouterShared, nineRouterErr
}

func nineRouterTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv(envNineRouterTimeout))
	if raw == "" {
		return nineRouterDefaultTimeout
	}
	var ms int64
	if _, err := fmt.Sscanf(raw, "%d", &ms); err != nil || ms <= 0 {
		return nineRouterDefaultTimeout
	}
	d := time.Duration(ms) * time.Millisecond
	if d < 5*time.Second {
		return 5 * time.Second
	}
	if d > 10*time.Minute {
		return 10 * time.Minute
	}
	return d
}

// redactURL keeps credentials out of logs while leaving the host readable.
func redactURL(raw string) string {
	if at := strings.LastIndex(raw, "@"); at >= 0 {
		if scheme := strings.Index(raw, "://"); scheme >= 0 && scheme < at {
			return raw[:scheme+3] + "****@" + raw[at+1:]
		}
	}
	return raw
}

// nineRouterUsage is the token accounting an OpenAI-compatible response carries.
type nineRouterUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	// Cache accounting when the upstream reports it. Absent on most providers, in
	// which case the whole prompt is billed as fresh input, which errs toward
	// charging the customer more rather than less and so must stay visible.
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
}

// nineRouterResponse is the metadata this process needs from one call. The body is
// passed through to the client verbatim, so nothing here reshapes it.
type nineRouterResponse struct {
	Status   int
	Body     []byte
	Stream   io.ReadCloser
	Provider string
	Model    string
	Usage    *nineRouterUsage
}

// nineRouterMaxBody caps a non-streaming response. An upstream returning an
// unexpected HTML error page must not be able to exhaust memory.
const nineRouterMaxBody = 32 << 20

// ChatCompletions forwards an OpenAI chat request unchanged.
//
// The payload is passed through rather than translated: 9Router speaks the same
// protocol the caller does, so any rewriting here would be a second place for the
// two dialects to drift apart.
func (c *nineRouterClient) ChatCompletions(ctx context.Context, payload []byte, stream bool) (*nineRouterResponse, error) {
	endpoint := c.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("9router: building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	if c.cfClientID != "" {
		req.Header.Set("CF-Access-Client-Id", c.cfClientID)
		req.Header.Set("CF-Access-Client-Secret", c.cfSecret)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("9router: calling upstream: %w", err)
	}

	out := &nineRouterResponse{
		Status:   resp.StatusCode,
		Provider: firstHeader(resp.Header, "X-9Router-Provider", "X-Ninerouter-Provider", "X-Upstream-Provider", "X-Provider"),
		Model:    firstHeader(resp.Header, "X-9Router-Model", "X-Ninerouter-Model", "X-Upstream-Model", "X-Actual-Model"),
	}

	if stream && resp.StatusCode < 300 {
		// The caller owns the body from here and must close it.
		out.Stream = resp.Body
		return out, nil
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, nineRouterMaxBody))
	if err != nil {
		return nil, fmt.Errorf("9router: reading response: %w", err)
	}
	out.Body = body
	out.Usage = parseNineRouterUsage(body)
	return out, nil
}

func firstHeader(h http.Header, names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(h.Get(n)); v != "" {
			return v
		}
	}
	return ""
}

// parseNineRouterUsage pulls the usage block out of a completion response.
//
// Returns nil when the field is absent rather than a zero-valued struct, because
// "the upstream did not report usage" and "the upstream reported zero usage" have
// to reach billing as different facts: the first is a reporting gap worth flagging,
// the second is a request that genuinely consumed nothing.
func parseNineRouterUsage(body []byte) *nineRouterUsage {
	var envelope struct {
		Usage *nineRouterUsage `json:"usage"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil
	}
	return envelope.Usage
}

var errNineRouterNoUsage = errors.New("9router: upstream reported no usage")

// errNineRouterCatalog means the upstream model list could not be read. The caller
// falls back to the static list rather than advertising nothing.
var errNineRouterCatalog = errors.New("9router: upstream rejected the model list request")
