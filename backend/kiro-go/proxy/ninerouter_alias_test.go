package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Retired OpenAI ids must resolve to a model the upstream still serves.
//
// The gpt-4 generation is gone from the upstream, but it is what an OpenAI SDK sends
// by default. Forwarded unchanged it becomes "Viberouter/gpt-4o" and 404s after the
// request has already been authenticated and held against the customer's wallet.
func TestRetiredOpenAIIdsResolveToAServedModel(t *testing.T) {
	t.Setenv(envNineRouterModelPrefix, "Viberouter/")
	t.Setenv(envNineRouterModelMap, "")

	for _, alias := range []string{"gpt-4o", "gpt-4", "GPT-4O", "gpt-3.5-turbo"} {
		got := nineRouterUpstreamModel(alias)
		if strings.Contains(strings.ToLower(got), "gpt-4") || strings.Contains(strings.ToLower(got), "gpt-3") {
			t.Errorf("%q resolved to %q, which the upstream no longer serves", alias, got)
		}
		if got != "Viberouter/"+nineRouterAliasModel {
			t.Errorf("%q resolved to %q, want %q", alias, got, "Viberouter/"+nineRouterAliasModel)
		}
	}
}

// "auto" must reach the upstream untouched.
//
// The upstream publishes its own "auto" route that picks a model per request. Mapping
// it to a fixed model here would silently replace that routing with one model, taking
// away the capability the caller explicitly asked for. This is the regression guard for
// an earlier version of this file that did exactly that.
func TestAutoIsLeftToTheUpstreamRouter(t *testing.T) {
	t.Setenv(envNineRouterModelPrefix, "Viberouter/")
	t.Setenv(envNineRouterModelMap, "")

	if got := nineRouterUpstreamModel("auto"); got != "Viberouter/auto" {
		t.Errorf("auto resolved to %q, want the upstream's own auto route", got)
	}
	if got := nineRouterUpstreamModel("AUTO"); got != "Viberouter/AUTO" {
		t.Errorf("AUTO resolved to %q; case is the upstream's business, not ours", got)
	}
}

// An explicit override still wins over the built-in alias, so an operator can point
// "auto" somewhere else without a code change.
func TestModelMapOverridesAnAlias(t *testing.T) {
	t.Setenv(envNineRouterModelPrefix, "Viberouter/")
	t.Setenv(envNineRouterModelMap, "gpt-4o=Viberouter/claude-opus-5")

	if got := nineRouterUpstreamModel("gpt-4o"); got != "Viberouter/claude-opus-5" {
		t.Errorf("got %q, want the operator's override", got)
	}
}

// A real model whose name contains an alias substring must not be captured.
func TestAliasMatchingIsExact(t *testing.T) {
	t.Setenv(envNineRouterModelPrefix, "Viberouter/")
	t.Setenv(envNineRouterModelMap, "")

	if got := nineRouterUpstreamModel("gpt-4o-audio"); got != "Viberouter/gpt-4o-audio" {
		t.Errorf("got %q: only an exact alias should be rewritten", got)
	}
}

// The advertised catalog must not contain ids the upstream cannot serve.
//
// The account-pool path appends "auto" and the gpt-* aliases because it resolves them
// itself. This path appends nothing: the upstream list already includes whatever it
// serves, and the retired gpt-4 names would point a client at a guaranteed failure.
func TestModelsEndpointWithholdsUnroutableAliases(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"Viberouter/claude-sonnet-5"},{"id":"Viberouter/claude-opus-5"}]}`))
	}))
	defer upstream.Close()

	t.Setenv(envNineRouterEnabled, "true")
	t.Setenv(envNineRouterBaseURL, upstream.URL+"/v1")
	t.Setenv(envNineRouterAPIKey, "alias-key")
	t.Setenv(envNineRouterModelPrefix, "Viberouter/")
	t.Setenv(envNineRouterModelMap, "")
	resetNineRouterClient(t)
	resetNineRouterCatalog(t)

	h := &Handler{}
	rec := httptest.NewRecorder()
	h.handleModels(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))

	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("decoding the models payload: %v", err)
	}
	for _, m := range parsed.Data {
		switch strings.ToLower(m.ID) {
		case "gpt-4o", "gpt-4":
			t.Errorf("%q is advertised but 404s on this upstream", m.ID)
		}
	}
	if len(parsed.Data) != 2 {
		t.Errorf("advertised %d models, want only the two the upstream serves", len(parsed.Data))
	}
}
