package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Only the pool NapKey sells from may be advertised.
//
// 9Router fronts several provider pools. Listing another one invites a request that
// is authenticated and billed here and then refused upstream, which the customer
// experiences as NapKey being broken.
func TestPublicModelsKeepsOnlyTheConfiguredPool(t *testing.T) {
	got := publicModelsFromUpstream([]string{
		"Kiro/claude-sonnet-5",
		"Kiro/claude-opus-5",
		"Viberouter/claude-opus-5",
		"cx/gpt-5.6-sol",
	}, "Kiro/")

	want := []string{"claude-opus-5", "claude-sonnet-5"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// The prefix must be stripped, because the public id is what customers send and what
// billing prices against. Advertising the namespaced id would make a client send
// "Kiro/claude-sonnet-5", which nineRouterUpstreamModel passes through unchanged and
// model_prices has no row for.
func TestPublicModelsStripsThePoolPrefix(t *testing.T) {
	got := publicModelsFromUpstream([]string{"Kiro/claude-sonnet-5"}, "Kiro/")
	if len(got) != 1 || got[0] != "claude-sonnet-5" {
		t.Fatalf("got %v, want [claude-sonnet-5]", got)
	}
}

// With prefixing disabled the namespace is flat, but the served-set intersection still
// applies: an unpriced id ("gpt-5") is dropped even on a flat deployment, while a priced
// one passes through.
func TestPublicModelsStillIntersectsServedSetWhenPrefixingIsOff(t *testing.T) {
	got := publicModelsFromUpstream([]string{"claude-sonnet-5", "gpt-5"}, "")
	if len(got) != 1 || got[0] != "claude-sonnet-5" {
		t.Fatalf("got %v, want [claude-sonnet-5]", got)
	}
}

// A nested namespace cannot be mapped back to a public id, so it is dropped rather
// than advertised as something this gateway would fail to route.
func TestPublicModelsDropsNestedNamespaces(t *testing.T) {
	got := publicModelsFromUpstream([]string{"Kiro/vendor/claude-sonnet-5", "Kiro/claude-opus-5"}, "Kiro/")
	if len(got) != 1 || got[0] != "claude-opus-5" {
		t.Fatalf("got %v, want only the mappable id", got)
	}
}

// The live pool publishes ids NapKey does not price: thinking variants, a dash spelling
// of two ids, and models from pools that are not priced here. After migration 0021 none
// of those has an open price period, so advertising any of them would bill the request
// at the '*' top tier instead of a chosen rate. The served-set intersection must drop
// all of them while keeping the seven priced models.
func TestPublicModelsKeepsOnlyTheServedSet(t *testing.T) {
	got := publicModelsFromUpstream([]string{
		"Viberouter/claude-opus-4-7",
		"Viberouter/claude-opus-4.7",
		"Viberouter/claude-opus-4-8",
		"Viberouter/claude-opus-4.8",
		"Viberouter/claude-opus-4.7-thinking",
		"Viberouter/claude-opus-4.8-thinking",
		"Viberouter/claude-opus-5",
		"Viberouter/claude-opus-5-thinking",
		"Viberouter/claude-sonnet-5",
		"Viberouter/claude-sonnet-5-thinking",
		"Viberouter/gpt-5.6-luna",
		"Viberouter/gpt-5.6-luna-thinking",
		"Viberouter/gpt-5.6-sol",
		"Viberouter/gpt-5.6-sol-thinking",
		"Viberouter/gpt-5.6-terra",
		"Viberouter/gpt-5.6-terra-thinking",
		"Viberouter/deepseek-v4-pro",
	}, "Viberouter/")

	want := []string{
		"claude-opus-4.7",
		"claude-opus-4.8",
		"claude-opus-5",
		"claude-sonnet-5",
		"gpt-5.6-luna",
		"gpt-5.6-sol",
		"gpt-5.6-terra",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// The fallback catalog and the served set must name exactly the same models, and none of
// them may be unservable. A mismatch between the two is how an unpriced id reaches the
// menu when the upstream cannot be reached.
func TestFallbackCatalogMatchesTheServedSet(t *testing.T) {
	fallback := nineRouterFallbackModels()
	if len(fallback) != len(nineRouterServedModels) {
		t.Fatalf("fallback lists %d models, the served set has %d", len(fallback), len(nineRouterServedModels))
	}
	for _, model := range fallback {
		if !nineRouterServed(model) {
			t.Errorf("fallback catalog advertises %q, which is not in the served set", model)
		}
		if nineRouterUnservable(model) {
			t.Errorf("fallback catalog advertises unservable model %q", model)
		}
	}
}
func TestPublicModelsDropsUnservableModels(t *testing.T) {
	got := publicModelsFromUpstream([]string{
		"Viberouter/claude-sonnet-5",
		"Viberouter/claude-sonnet-4.8",
		"Viberouter/gpt-image-2",
		"Viberouter/claude-fable-5",
	}, "Viberouter/")

	want := []string{"claude-sonnet-5"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// claude-fable-5 is published by the pool and priced by migrations 0018 and 0019, so
// nothing except this check keeps it off the menu. Measured 2026-08-09: every probe
// returned 524 or a stream with no usage, on a key that served claude-sonnet-5 fine.
func TestFableIsWithdrawnFromSale(t *testing.T) {
	for _, id := range []string{"claude-fable-5", "CLAUDE-FABLE-5", " claude-fable-5 "} {
		if !nineRouterUnservable(id) {
			t.Errorf("nineRouterUnservable(%q) = false, want true", id)
		}
		if !nineRouterRefusesModel(id) {
			t.Errorf("nineRouterRefusesModel(%q) = false, want true", id)
		}
	}
	if nineRouterUnservable("claude-sonnet-5") {
		t.Error("claude-sonnet-5 must stay on sale")
	}
}

// A client that hardcoded the namespaced id must be refused too, otherwise the
// prefixed form slips past the check and pays for a 524.
func TestRefusalCoversThePrefixedForm(t *testing.T) {
	t.Setenv("NINEROUTER_MODEL_PREFIX", "NapKey/")
	for _, id := range []string{"NapKey/claude-fable-5", "napkey/claude-fable-5"} {
		if !nineRouterRefusesModel(id) {
			t.Errorf("nineRouterRefusesModel(%q) = false, want true", id)
		}
	}
	if nineRouterRefusesModel("NapKey/claude-sonnet-5") {
		t.Error("prefixed sonnet must stay on sale")
	}
}

// The three groups the pool publishes but NapKey does not sell must be refused before
// the wallet hold, not merely hidden from the catalog. A client that hardcoded one of
// them would otherwise settle at the '*' top tier: the dash spelling at 3.3x the dot
// rate, a thinking variant at a capability the request silently loses, and an unpriced
// model at the most expensive named tier.
func TestRefusalCoversUnsoldIds(t *testing.T) {
	for _, id := range []string{
		"claude-opus-4-7", "claude-opus-4-8",
		"claude-sonnet-5-thinking", "claude-opus-5-thinking", "gpt-5.6-sol-thinking",
		"deepseek-v4-pro",
	} {
		if !nineRouterUnservable(id) {
			t.Errorf("nineRouterUnservable(%q) = false, want true", id)
		}
		if !nineRouterRefusesModel(id) {
			t.Errorf("nineRouterRefusesModel(%q) = false, want true", id)
		}
	}
	// Truly unknown ids are the '*' fallback's job, not a refusal.
	if nineRouterUnservable("auto") {
		t.Error("auto must not be refused; the '*' fallback prices it")
	}
	if nineRouterRefusesModel("claude-opus-4.7") {
		t.Error("the dot form must stay on sale")
	}
}

// The refusal has to happen with the body intact. It runs before reserveBilling, which
// reads the same body to estimate the hold; a drained reader would break every request
// that is perfectly serviceable.
func TestRefusalRestoresTheRequestBody(t *testing.T) {
	t.Setenv("NINEROUTER_BASE_URL", "https://example.invalid/v1")
	t.Setenv("NINEROUTER_API_KEY", "test-key")
	t.Setenv("NINEROUTER_MODEL_PREFIX", "NapKey/")

	h := &Handler{}
	body := `{"model":"claude-sonnet-5","messages":[{"role":"user","content":"hi"}]}`
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()

	if h.refuseUnservableNineRouterModel(w, r, false) {
		t.Fatal("a servable model must not be refused")
	}
	got, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("reading the restored body: %v", err)
	}
	if string(got) != body {
		t.Fatalf("body not restored: got %q", string(got))
	}
}

// The refusal must be a 404 that names the model, not a 502 from the upstream, and it
// must say no credit was held so a support ticket does not have to ask.
func TestRefusalAnswers404WithoutCallingUpstream(t *testing.T) {
	t.Setenv("NINEROUTER_BASE_URL", "https://example.invalid/v1")
	t.Setenv("NINEROUTER_API_KEY", "test-key")
	t.Setenv("NINEROUTER_MODEL_PREFIX", "NapKey/")

	h := &Handler{}
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"claude-fable-5","messages":[]}`))
	w := httptest.NewRecorder()

	if !h.refuseUnservableNineRouterModel(w, r, false) {
		t.Fatal("claude-fable-5 must be refused")
	}
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "claude-fable-5") ||
		!strings.Contains(body, "No credit was held") {
		t.Fatalf("unhelpful refusal: %s", body)
	}
}

// A body this function cannot parse is not its problem: it must decline to answer and
// let the normal path produce the normal parse error.
func TestRefusalIgnoresMalformedBodies(t *testing.T) {
	t.Setenv("NINEROUTER_BASE_URL", "https://example.invalid/v1")
	t.Setenv("NINEROUTER_API_KEY", "test-key")

	h := &Handler{}
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{not json`))
	w := httptest.NewRecorder()

	if h.refuseUnservableNineRouterModel(w, r, false) {
		t.Fatal("a malformed body must not be answered here")
	}
}

// The unservable check is the reason these ids have no price row, so the two lists
// have to agree. A model that reaches billing without a price settles at '*', which is
// the state migration 0020 exists to end.
func TestFallbackCatalogExcludesUnservableModels(t *testing.T) {
	for _, model := range nineRouterFallbackModels() {
		if nineRouterUnservable(model) {
			t.Errorf("fallback catalog advertises unservable model %q", model)
		}
	}
}

// Duplicates and blanks must not reach the response.
func TestPublicModelsDeduplicates(t *testing.T) {
	got := publicModelsFromUpstream([]string{
		"Kiro/claude-sonnet-5",
		"Kiro/CLAUDE-SONNET-5",
		"  ",
		"Kiro/",
	}, "Kiro/")
	if len(got) != 1 {
		t.Fatalf("got %v, want one entry", got)
	}
}

// The advertised list must never contain thinking variants on this upstream.
//
// The suffix is a NapKey convention that the Anthropic path strips before
// forwarding, and extended thinking has no OpenAI equivalent so it is dropped here.
// Advertising it would promise a capability the request silently loses.
func TestNineRouterModelListHasNoThinkingVariants(t *testing.T) {
	models := nineRouterModelList(nineRouterFallbackModels())
	if len(models) == 0 {
		t.Fatal("the fallback catalog must not be empty")
	}
	for _, m := range models {
		id, _ := m["id"].(string)
		if strings.Contains(strings.ToLower(id), "thinking") {
			t.Errorf("%q must not be advertised: thinking is dropped on this upstream", id)
		}
	}
}

// When the upstream answers, its list is what gets advertised.
func TestModelsEndpointServesTheUpstreamCatalog(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/models") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"Kiro/claude-sonnet-5"},{"id":"Viberouter/claude-opus-5"}]}`))
	}))
	defer upstream.Close()

	t.Setenv(envNineRouterEnabled, "true")
	t.Setenv(envNineRouterBaseURL, upstream.URL+"/v1")
	t.Setenv(envNineRouterAPIKey, "catalog-key")
	t.Setenv(envNineRouterModelPrefix, "Kiro/")
	t.Setenv(envNineRouterModelMap, "")
	resetNineRouterClient(t)
	resetNineRouterCatalog(t)

	h := &Handler{}
	rec := httptest.NewRecorder()
	h.handleModels(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))

	ids := modelIDsFrom(t, rec.Body.Bytes())
	if !ids["claude-sonnet-5"] {
		t.Error("a model the upstream serves must be advertised")
	}
	if ids["Viberouter/claude-opus-5"] || ids["claude-opus-5"] {
		t.Error("a model from another pool must not be advertised")
	}
	// The hardcoded Anthropic list must not leak in on this path. claude-sonnet-4.5
	// is the sentinel because it appears only there: claude-sonnet-4.6 used to serve
	// that role, but the pool publishes it and 0020 prices it, so it can no longer
	// tell the two lists apart.
	if ids["claude-sonnet-4.5"] {
		t.Error("the account-pool fallback list must not be served under 9Router")
	}
}

// An unreachable upstream falls back to the priced catalog rather than to the
// account-pool list, which names models this upstream does not serve.
func TestModelsEndpointFallsBackToThePricedCatalog(t *testing.T) {
	t.Setenv(envNineRouterEnabled, "true")
	t.Setenv(envNineRouterBaseURL, "http://127.0.0.1:1/v1")
	t.Setenv(envNineRouterAPIKey, "catalog-key")
	t.Setenv(envNineRouterModelPrefix, "Kiro/")
	t.Setenv(envNineRouterTimeout, "5000")
	resetNineRouterClient(t)
	resetNineRouterCatalog(t)

	h := &Handler{}
	rec := httptest.NewRecorder()
	h.handleModels(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))

	ids := modelIDsFrom(t, rec.Body.Bytes())
	if !ids["claude-sonnet-5"] {
		t.Error("the static catalog must still advertise the models on sale")
	}
	if ids["claude-sonnet-4.5"] || ids["claude-opus-4.5"] {
		t.Error("the account-pool list must not be used as the 9Router fallback")
	}
}

// modelIDsFrom collects the advertised ids from a /v1/models payload.
func modelIDsFrom(t *testing.T, body []byte) map[string]bool {
	t.Helper()
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decoding the models payload: %v", err)
	}
	out := map[string]bool{}
	for _, m := range parsed.Data {
		out[m.ID] = true
	}
	return out
}

// resetNineRouterCatalog clears the memoised catalog so a test's upstream applies.
func resetNineRouterCatalog(t *testing.T) {
	t.Helper()
	reset := func() {
		nineRouterCatalogMu.Lock()
		nineRouterCatalogIDs = nil
		nineRouterCatalogAt = time.Time{}
		nineRouterCatalogMu.Unlock()
	}
	reset()
	t.Cleanup(reset)
}
