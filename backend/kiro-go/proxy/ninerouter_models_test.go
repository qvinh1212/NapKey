package proxy

import (
	"encoding/json"
	"os"
	"testing"
)

// The public id must be namespaced before it reaches 9Router. Forwarding it
// unchanged returns 404 {"error":{"code":"upstream_status"}}, verified against the
// live instance, so this mapping is what makes the upstream reachable at all.
func TestUpstreamModelAppliesPoolPrefix(t *testing.T) {
	t.Setenv(envNineRouterModelPrefix, "Kiro/")
	t.Setenv(envNineRouterModelMap, "")

	for _, tc := range []struct{ in, want string }{
		{"claude-sonnet-5", "Kiro/claude-sonnet-5"},
		{"claude-opus-5", "Kiro/claude-opus-5"},
		{"claude-opus-4.7", "Kiro/claude-opus-4.7"},
		{"claude-opus-4.8", "Kiro/claude-opus-4.8"},
	} {
		if got := nineRouterUpstreamModel(tc.in); got != tc.want {
			t.Errorf("%q -> %q, want %q", tc.in, got, tc.want)
		}
	}
}

// An id that already names a pool must not be prefixed twice.
func TestUpstreamModelDoesNotDoublePrefix(t *testing.T) {
	t.Setenv(envNineRouterModelPrefix, "Kiro/")
	t.Setenv(envNineRouterModelMap, "")
	if got := nineRouterUpstreamModel("Kiro/claude-sonnet-5"); got != "Kiro/claude-sonnet-5" {
		t.Errorf("got %q, want the id unchanged", got)
	}
	if got := nineRouterUpstreamModel("Viberouter/claude-opus-5"); got != "Viberouter/claude-opus-5" {
		t.Errorf("got %q: a different pool must be respected", got)
	}
}

// The default pool must be one the upstream actually serves.
//
// A wrong prefix 404s every request, so this pins the default to the pool verified
// against the live upstream. The earlier default ("Kiro/") named a pool that does not
// exist there, which would have failed all traffic on the first deploy.
//
// Unset and empty are different settings here: unset means "use the default pool",
// empty means "do not namespace at all". t.Setenv cannot express unset, so the
// variable is removed directly and restored on cleanup.
func TestUpstreamModelDefaultsToAServedPool(t *testing.T) {
	t.Setenv(envNineRouterModelMap, "")
	original, existed := os.LookupEnv(envNineRouterModelPrefix)
	os.Unsetenv(envNineRouterModelPrefix)
	t.Cleanup(func() {
		if existed {
			os.Setenv(envNineRouterModelPrefix, original)
			return
		}
		os.Unsetenv(envNineRouterModelPrefix)
	})

	if got := nineRouterUpstreamModel("claude-sonnet-5"); got != "Viberouter/claude-sonnet-5" {
		t.Errorf("got %q, want the verified default pool", got)
	}
}

// A prefix without a trailing slash still produces a valid id.
func TestUpstreamModelNormalisesPrefix(t *testing.T) {
	t.Setenv(envNineRouterModelMap, "")
	t.Setenv(envNineRouterModelPrefix, "Kiro")
	if got := nineRouterUpstreamModel("claude-sonnet-5"); got != "Kiro/claude-sonnet-5" {
		t.Errorf("got %q, want the separator added", got)
	}
}

// An explicitly empty prefix disables namespacing, for a flat 9Router deployment.
func TestUpstreamModelEmptyPrefixDisablesNamespacing(t *testing.T) {
	t.Setenv(envNineRouterModelMap, "")
	t.Setenv(envNineRouterModelPrefix, "")
	if got := nineRouterUpstreamModel("claude-sonnet-5"); got != "claude-sonnet-5" {
		t.Errorf("got %q, want no prefix", got)
	}
}

// The explicit map wins, for a model whose upstream name breaks the convention.
func TestUpstreamModelMapOverridesPrefix(t *testing.T) {
	t.Setenv(envNineRouterModelPrefix, "Kiro/")
	t.Setenv(envNineRouterModelMap, "claude-sonnet-5=cx/gpt-5.6-sol,claude-opus-5=Viberouter/claude-opus-5")

	if got := nineRouterUpstreamModel("claude-sonnet-5"); got != "cx/gpt-5.6-sol" {
		t.Errorf("got %q, want the mapped upstream id", got)
	}
	if got := nineRouterUpstreamModel("claude-opus-5"); got != "Viberouter/claude-opus-5" {
		t.Errorf("got %q", got)
	}
	// Unmapped ids still take the prefix.
	if got := nineRouterUpstreamModel("claude-fable-5"); got != "Kiro/claude-fable-5" {
		t.Errorf("got %q, want the prefix fallback", got)
	}
}

// Client ids arrive in mixed case; the lookup must still match.
func TestUpstreamModelMapIsCaseInsensitiveOnPublicID(t *testing.T) {
	t.Setenv(envNineRouterModelPrefix, "Kiro/")
	t.Setenv(envNineRouterModelMap, "Claude-Sonnet-5=Kiro/claude-sonnet-5")
	if got := nineRouterUpstreamModel("claude-sonnet-5"); got != "Kiro/claude-sonnet-5" {
		t.Errorf("got %q", got)
	}
}

// Malformed map entries are skipped rather than breaking every lookup.
func TestUpstreamModelMapIgnoresMalformedEntries(t *testing.T) {
	t.Setenv(envNineRouterModelPrefix, "Kiro/")
	t.Setenv(envNineRouterModelMap, "garbage,=missing-left,missing-right=,claude-opus-5=Kiro/claude-opus-5")
	if got := nineRouterUpstreamModel("claude-opus-5"); got != "Kiro/claude-opus-5" {
		t.Errorf("got %q, want the valid entry to still resolve", got)
	}
	if got := nineRouterUpstreamModel("claude-sonnet-5"); got != "Kiro/claude-sonnet-5" {
		t.Errorf("got %q, want the prefix fallback", got)
	}
}

// The rewrite must replace the model without disturbing anything else the client
// sent. Dropping a customer's parameters would be worse than the routing bug.
func TestRewriteRequestModelPreservesOtherFields(t *testing.T) {
	in := []byte(`{"model":"claude-sonnet-5","stream":true,"seed":7,"messages":[{"role":"user","content":"hi"}],` +
		`"response_format":{"type":"json_object"},"tools":[{"type":"function"}]}`)

	out, err := rewriteRequestModel(in, "Kiro/claude-sonnet-5")
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatal(err)
	}
	if body["model"] != "Kiro/claude-sonnet-5" {
		t.Errorf("model = %v, want the upstream id", body["model"])
	}
	if body["seed"] != float64(7) {
		t.Errorf("seed lost: %v", body["seed"])
	}
	if body["stream"] != true {
		t.Errorf("stream lost: %v", body["stream"])
	}
	if _, ok := body["response_format"].(map[string]interface{}); !ok {
		t.Errorf("response_format lost: %v", body["response_format"])
	}
	if msgs, ok := body["messages"].([]interface{}); !ok || len(msgs) != 1 {
		t.Errorf("messages lost: %v", body["messages"])
	}
}

// An empty upstream id leaves the body alone rather than writing a blank model.
func TestRewriteRequestModelIgnoresEmptyTarget(t *testing.T) {
	in := []byte(`{"model":"claude-sonnet-5"}`)
	out, err := rewriteRequestModel(in, "")
	if err != nil || string(out) != string(in) {
		t.Errorf("body should be untouched, got %s (err=%v)", out, err)
	}
}
