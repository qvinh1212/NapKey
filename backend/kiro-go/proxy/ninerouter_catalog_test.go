package proxy

import (
	"encoding/json"
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

// With prefixing disabled the namespace is flat, so everything is already public.
func TestPublicModelsPassesThroughWhenPrefixingIsOff(t *testing.T) {
	got := publicModelsFromUpstream([]string{"claude-sonnet-5", "gpt-5"}, "")
	if len(got) != 2 {
		t.Fatalf("got %v, want both ids", got)
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
	// The hardcoded Anthropic list must not leak in on this path.
	if ids["claude-sonnet-4.6"] {
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
	if ids["claude-sonnet-4.6"] || ids["claude-opus-4.5"] {
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
