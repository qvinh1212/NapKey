package proxy

// The model catalog when 9Router is the upstream.
//
// /v1/models exists so a client can discover what it may ask for. On the account
// pool that list comes from the accounts themselves, so it is accurate by
// construction. With 9Router there are no accounts: refreshModelsCache() returns
// early on an empty pool, the cache stays empty, and the endpoint falls through to
// the hardcoded Anthropic list. That list is not what this upstream serves, so
// clients were being told to ask for models that answer 404 once namespaced.
//
// So the catalog is read from the upstream instead, filtered to the pool NapKey
// sells from, and mapped back to the public ids customers actually send.
//
// Thinking variants are deliberately absent. The suffix is a NapKey convention that
// the Anthropic path strips before forwarding, and extended thinking has no OpenAI
// equivalent so it is dropped on this upstream. Advertising "-thinking" here would
// promise a capability the request silently loses.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"kiro-go/logger"
)

// nineRouterCatalogTTL bounds how stale the advertised list may be.
//
// The upstream's model list changes when a pool is reconfigured, which is rare, so a
// short TTL would spend requests on an answer that almost never moves. Fifteen
// minutes is long enough to keep /v1/models cheap and short enough that adding a
// model does not need a redeploy to become visible.
const nineRouterCatalogTTL = 15 * time.Minute

// nineRouterCatalogTimeout bounds the upstream call.
//
// /v1/models is on the client's critical path the first time it is called, so this
// is much tighter than a completion timeout: a slow catalog falls back to the static
// list rather than making the client wait.
const nineRouterCatalogTimeout = 10 * time.Second

var (
	nineRouterCatalogMu  sync.RWMutex
	nineRouterCatalogIDs []string
	nineRouterCatalogAt  time.Time
)

// nineRouterModelsResponse is the OpenAI model list shape.
type nineRouterModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// nineRouterPublicModels returns the public model ids this upstream can serve.
//
// Returns nil when the upstream cannot be reached and nothing is cached, which the
// caller turns into the static list. Returning an empty list instead would advertise
// no models at all and break discovery for a client that is perfectly able to send a
// model id it already knows.
func nineRouterPublicModels(ctx context.Context) []string {
	nineRouterCatalogMu.RLock()
	cached := nineRouterCatalogIDs
	at := nineRouterCatalogAt
	nineRouterCatalogMu.RUnlock()
	if len(cached) > 0 && time.Since(at) < nineRouterCatalogTTL {
		return cached
	}

	ids, err := fetchNineRouterModels(ctx)
	if err != nil || len(ids) == 0 {
		// Serve a stale list rather than none: it was true recently, and the
		// alternative is telling the client this upstream has no models.
		return cached
	}

	nineRouterCatalogMu.Lock()
	nineRouterCatalogIDs = ids
	nineRouterCatalogAt = time.Now()
	nineRouterCatalogMu.Unlock()
	return ids
}

// fetchNineRouterModels reads the upstream list and maps it to public ids.
func fetchNineRouterModels(ctx context.Context) ([]string, error) {
	client, err := getNineRouterClient()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, nineRouterCatalogTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+client.apiKey)
	req.Header.Set("Accept", "application/json")
	if client.cfClientID != "" {
		req.Header.Set("CF-Access-Client-Id", client.cfClientID)
		req.Header.Set("CF-Access-Client-Secret", client.cfSecret)
	}

	resp, err := client.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, errNineRouterCatalog
	}

	var parsed nineRouterModelsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}

	upstreamIDs := make([]string, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		upstreamIDs = append(upstreamIDs, m.ID)
	}
	return publicModelsFromUpstream(upstreamIDs, nineRouterModelPrefix()), nil
}

// publicModelsFromUpstream maps upstream ids back to the ids customers send.
//
// Only the configured pool is kept. 9Router fronts several provider pools and the
// others are not on sale here, so listing them would invite a request that is
// authenticated, billed and then refused by the upstream.
//
// With prefixing disabled the namespace is flat and every id is already public, so
// the whole list passes through.
func publicModelsFromUpstream(upstreamIDs []string, prefix string) []string {
	seen := make(map[string]bool, len(upstreamIDs))
	out := make([]string, 0, len(upstreamIDs))
	for _, raw := range upstreamIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		public := id
		if prefix != "" {
			if !strings.HasPrefix(strings.ToLower(id), strings.ToLower(prefix)) {
				continue
			}
			public = id[len(prefix):]
		}
		public = strings.TrimSpace(public)
		// A nested namespace ("pool/vendor/model") is not an id this gateway can map
		// back, since nineRouterUpstreamModel would pass it through unprefixed.
		if public == "" || strings.Contains(public, "/") {
			continue
		}
		key := strings.ToLower(public)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, public)
	}
	// Stable order so the response does not churn between calls for no reason.
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

// nineRouterModelList renders the /v1/models payload for this upstream.
//
// supportsImage is reported true because every model on the Kiro pool accepts image
// input, and the upstream list carries no modality metadata to narrow it with.
// Claiming text-only would make vision clients skip a model that works.
func nineRouterModelList(ids []string) []map[string]interface{} {
	models := make([]map[string]interface{}, 0, len(ids))
	for _, id := range ids {
		models = append(models, buildModelInfo(id, "napkey", true))
	}
	return models
}

// nineRouterFallbackModels is the list used when the upstream cannot be reached.
//
// These are the models NapKey sells on this upstream, and they are the same ids the
// control plane has price rows for. Keeping the two lists aligned is what stops a
// client being pointed at a model that would fall through to the '*' fallback rate
// instead of its own price.
//
// It is a fallback, not the source of truth: the upstream list wins whenever it can
// be read, so adding a model there does not require editing this.
func nineRouterFallbackModels() []string {
	return []string{
		"claude-fable-5",
		"claude-opus-4.7",
		"claude-opus-4.8",
		"claude-opus-5",
		"claude-sonnet-5",
	}
}

// handleNineRouterModels answers /v1/models for the 9Router upstream.
func (h *Handler) handleNineRouterModels(w http.ResponseWriter, r *http.Request) {
	ids := nineRouterPublicModels(r.Context())
	if len(ids) == 0 {
		logger.Warnf("[9Router] could not read the upstream model list; serving the static catalog")
		ids = nineRouterFallbackModels()
	}

	// The list is exactly what the upstream reports, including its own "auto" route.
	//
	// The account-pool path appends "auto", "gpt-4o" and "gpt-4" here because it
	// resolves those itself. They are not appended on this path: whatever the upstream
	// serves is already in ids (it publishes "auto"), and the retired gpt-4 names would
	// be an id that authenticates, reserves wallet balance and then 404s.
	models := nineRouterModelList(ids)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"object": "list",
		"data":   models,
	})
}
