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
// So the catalog is read from the upstream, filtered to the pool NapKey sells from,
// and intersected with the exact set of models NapKey prices (nineRouterServedModels),
// then mapped back to the public ids customers actually send.
//
// The intersection is what keeps the advertised list honest. The pool publishes far more
// than NapKey offers -- thinking variants, a dash spelling of two ids, and models from
// pools that are not priced here -- and every id outside the served set has no open price
// period after migration 0021, so it would hold and settle at the '*' top tier instead of
// a rate anyone chose. Filtering on the pool prefix alone let those through; filtering on
// the served set does not.

import (
	"bytes"
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
// Two filters apply, in order. First, only the configured pool is kept: 9Router fronts
// several provider pools and the others are not on sale here, so listing them would
// invite a request that is authenticated, billed and then refused by the upstream.
// Second, the result is intersected with nineRouterServedModels, because the pool lists
// ids NapKey does not price (thinking variants, dash spellings, other pools' models) and
// any of those would settle at the '*' fallback rate instead of a chosen one.
//
// With prefixing disabled the namespace is flat and every id is already public, but the
// served-set intersection still applies, so an unpriced id is never advertised even on a
// flat deployment.
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
		if nineRouterUnservable(public) {
			continue
		}
		// Only models NapKey actually sells and prices may be advertised. The pool lists
		// thinking variants, a dash spelling of two ids, and other pools' models; none of
		// those have an open price period after migration 0021, so advertising any of them
		// would bill the request at the '*' top tier rather than a chosen rate.
		if !nineRouterServed(public) {
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

// nineRouterUnservable reports whether an id the pool publishes cannot actually be
// served over this endpoint.
//
// The catalog is taken from the upstream, so anything the pool lists is offered for
// sale. These ids do not survive contact with a request, verified against the live
// upstream:
//
//	claude-sonnet-4.8  published, but a completion returns no usable response
//	gpt-image-2        an image model; /v1/chat/completions cannot serve it
//
// claude-fable-5 sat in this switch from 2026-08-09 until 2026-08-16. Fifteen probes
// against the NapKey/ pool returned Cloudflare 524 or closed the stream without usage,
// never a completion, while claude-sonnet-5 answered in nine seconds on the same key:
// the pool key was not entitled to it, so the id was published but unbuyable. The
// provider granted the entitlement on 2026-08-16 and the id returned to sale at the
// top tier it already anchored. Unlike the ids above it carries price rows, from
// migrations 0018 and 0019: withdrawing an id from sale is a catalog decision, not a
// pricing one, and closing a price period would reprice traffic that already settled
// against it.
//
// Seven more ids were dropped from sale on 2026-08-15, when migration 0021 replaced
// the flat price book with a tiered one covering eight models:
//
//	claude-haiku-4.5, claude-haiku-4-5   retired with the repricing
//	claude-sonnet-4.6, claude-sonnet-4-6 retired with the repricing
//	claude-sonnet-4.7                    retired with the repricing
//	claude-opus-4.6, claude-opus-4-6     retired with the repricing
//
// Their open price periods were closed in the same change, so advertising them would
// settle traffic at the '*' fallback rate instead of a price anyone chose. Offering
// what you have not priced is exactly the accident the price book exists to prevent.
//
// The same reasoning caught three more groups the live pool publishes but NapKey does
// not sell, added to this check on 2026-08-15:
//
//	claude-opus-4-7, claude-opus-4-8  dash spellings of priced models; only the dot
//	                                  form carries a period after 0021, so the dash form
//	                                  settles the same traffic at '*', 3.3x the dot rate
//	*-thinking                        a NapKey convention the Anthropic path strips; on
//	                                  this upstream thinking has no equivalent, so the
//	                                  suffix promises a capability the request loses
//	deepseek-v4-pro                   a model NapKey has never priced or measured
//
// Truly unknown ids ("auto" included) are deliberately NOT refused: the '*' fallback
// row in the price book is the designed answer for them, never cheaper than the top
// named tier.
//
// Advertising any of them costs the customer a real request, so hiding them from
// /v1/models is only half the fix -- nineRouterRefusesModel is what stops a client
// that already knows the id from paying for a refusal.
//
// Matched case-insensitively, as the ids arrive in mixed case from clients.
func nineRouterUnservable(publicModel string) bool {
	id := strings.ToLower(strings.TrimSpace(publicModel))
	if strings.HasSuffix(id, "-thinking") {
		return true
	}
	switch id {
	case "claude-sonnet-4.8", "claude-sonnet-4-8", "gpt-image-2",
		"claude-haiku-4.5", "claude-haiku-4-5",
		"claude-sonnet-4.6", "claude-sonnet-4-6",
		"claude-sonnet-4.7",
		"claude-opus-4.6", "claude-opus-4-6",
		"claude-opus-4-7", "claude-opus-4-8",
		"deepseek-v4-pro":
		return true
	}
	return false
}

// nineRouterRefusesModel reports whether a request for this model must be refused
// before it reaches the wallet.
//
// nineRouterUnservable hides an id from the catalog, which only helps a client that
// discovers models. A client with the id hardcoded still sends the request, and the
// wallet hold in reserveBilling happens before any handler looks at the model, so the
// customer pays a hold for a request the upstream will never answer. The hold is
// released when the handler fails, but a 524 costs them the round trip and shows up
// as a failed request they were charged for.
//
// Refusing here keeps the money path honest: no hold, no upstream call, and a 404
// that names the model instead of a gateway timeout that does not.
func nineRouterRefusesModel(publicModel string) bool {
	return nineRouterUnservable(nineRouterPublicModel(publicModel))
}

// refuseUnservableNineRouterModel answers the request itself and reports true when
// the client asked for a model this upstream cannot serve.
//
// It runs before reserveBilling, so the body has not been consumed yet. Reading it
// here means putting it back: the billing estimator and the handler both read the
// same body afterwards, and a drained reader would turn a refusal into a parse error
// on the next request that is perfectly fine.
//
// A malformed body is not this function's problem -- it returns false and lets the
// normal path produce the normal parse error.
func (h *Handler) refuseUnservableNineRouterModel(w http.ResponseWriter, r *http.Request, anthropic bool) bool {
	if r == nil || r.Body == nil || !nineRouterConfigured() {
		return false
	}
	raw, err := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(raw))
	if err != nil {
		return false
	}
	var request struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return false
	}
	if !nineRouterRefusesModel(request.Model) {
		return false
	}
	message := "model " + strings.TrimSpace(request.Model) +
		" is not available on this upstream; it is published by the pool but the " +
		"account is not entitled to it. No credit was held for this request."
	if anthropic {
		h.sendClaudeError(w, http.StatusNotFound, "not_found_error", message)
	} else {
		h.sendOpenAIError(w, http.StatusNotFound, "invalid_request_error", message)
	}
	return true
}

// nineRouterPublicModel strips the pool prefix if a client sends the upstream id
// verbatim, so the refusal covers both "gpt-image-2" and "NapKey/gpt-image-2". gpt-image-2
// stands in for the unservable id because it stays unservable while other ids rotate
// in and out of sale.
func nineRouterPublicModel(model string) string {
	trimmed := strings.TrimSpace(model)
	prefix := nineRouterModelPrefix()
	if prefix != "" && len(trimmed) > len(prefix) &&
		strings.EqualFold(trimmed[:len(prefix)], prefix) {
		return trimmed[len(prefix):]
	}
	return trimmed
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

// nineRouterServedModels is the exact set of public ids NapKey sells on this upstream.
//
// This is the single source of truth for what may be advertised and billed. The
// upstream pool publishes far more than NapKey offers -- thinking variants, a dash
// spelling of two ids, and models from pools that are not priced here -- and every id
// outside this set has no open price period after migration 0021, so it would hold and
// settle at the '*' top tier (10,000 VND/1M) instead of a rate anyone chose. Selling
// an unpriced id is the accident the price book exists to prevent, so the catalog is
// intersected with this list rather than trusting whatever the pool lists.
//
// The eight entries match MODEL_TIERS in napkey-web and the eight named rows of
// migration 0021. claude-fable-5 rejoined on 2026-08-16, when the provider granted
// the pool key entitlement for it; until then it was priced only to anchor the
// '*' fallback.
var nineRouterServedModels = map[string]bool{
	"claude-sonnet-5": true,
	"gpt-5.6-luna":    true,
	"claude-opus-4.7": true,
	"claude-opus-4.8": true,
	"gpt-5.6-terra":   true,
	"claude-opus-5":   true,
	"gpt-5.6-sol":     true,
	"claude-fable-5":  true,
}

// nineRouterServed reports whether a public id is one of the models NapKey sells.
//
// Matched case-insensitively and trimmed, because ids arrive from clients and from the
// upstream in mixed case and with stray whitespace.
func nineRouterServed(publicModel string) bool {
	return nineRouterServedModels[strings.ToLower(strings.TrimSpace(publicModel))]
}

// nineRouterFallbackModels is the list used when the upstream cannot be reached.
//
// These are the models NapKey sells on this upstream, and they are the same ids the
// control plane has price rows for (migrations 0018, 0019, 0020 and 0021). Keeping
// the two lists aligned is what stops a client being pointed at a model that would
// fall through to the '*' fallback rate instead of its own price.
//
// That alignment only ever governed this fallback, never the live path: the upstream
// list wins whenever it can be read. The list was trimmed on 2026-08-15 when
// migration 0021 repriced the catalog; haiku, sonnet-4.6/4.7 and opus-4.6 were
// retired then and stay excluded for the same reason nineRouterUnservable drops
// them from the live list. claude-fable-5 left in the same change and returned on
// 2026-08-16, when the provider granted the pool key entitlement for it.
//
// It is a fallback, not the source of truth: the upstream list wins whenever it can
// be read, so adding a model there does not require editing this. Ids that cannot be
// served are excluded here for the same reason nineRouterUnservable drops them from
// the live list.
func nineRouterFallbackModels() []string {
	return []string{
		"claude-sonnet-5",
		"gpt-5.6-luna",
		"claude-opus-4.7",
		"claude-opus-4.8",
		"gpt-5.6-terra",
		"claude-opus-5",
		"gpt-5.6-sol",
		"claude-fable-5",
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
