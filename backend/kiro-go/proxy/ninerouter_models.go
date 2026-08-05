package proxy

// Model routing for the 9Router upstream.
//
// 9Router namespaces its models by provider pool: the id NapKey sells as
// "claude-sonnet-5" is "Viberouter/claude-sonnet-5" upstream. Forwarding the public id
// unchanged returns 404 with {"error":{"code":"upstream_status"}}, so the mapping is
// mandatory rather than cosmetic.
//
// The prefix is configuration, not a constant, because it selects which provider
// pool serves the traffic. Moving NapKey to a different pool later is then an
// environment change instead of a code change.

import (
	"os"
	"strings"
)

const (
	envNineRouterModelPrefix = "NINEROUTER_MODEL_PREFIX"
	envNineRouterModelMap    = "NINEROUTER_MODEL_MAP"
)

// nineRouterDefaultPrefix is the pool NapKey sells from.
//
// Verified against the live upstream: it serves four pools (nvidia, Free, Viberouter,
// cx) and Viberouter is the one carrying the full Claude line, including every model
// this price book has a rate for. "Free" also carries them but is a free tier, so
// reselling it would put paying customers behind someone else's quota.
//
// A wrong prefix is not a soft failure: every request 404s, because the upstream
// addresses models by pool. That is why this default is a verified value rather than
// a guess, and why NINEROUTER_MODEL_PREFIX exists to move pools without a redeploy.
const nineRouterDefaultPrefix = "Viberouter/"

// nineRouterUpstreamModel maps a public model id to its upstream id.
//
// Resolution order, most specific first:
//
//  1. An explicit entry in NINEROUTER_MODEL_MAP ("public=upstream,public=upstream").
//     This is the escape hatch for a model whose upstream name does not follow the
//     prefix convention.
//  2. The configured prefix, applied to the public id.
//
// An id that already carries a prefix is left alone, so a caller that knows the
// upstream name can use it directly and a double prefix can never be produced.
func nineRouterUpstreamModel(publicModel string) string {
	model := strings.TrimSpace(publicModel)
	if model == "" {
		return ""
	}

	if mapped, ok := lookupNineRouterModelMap(model); ok {
		return mapped
	}

	// Legacy OpenAI ids have to be resolved before namespacing. The upstream retired
	// the gpt-4 generation, and nothing else on this path rewrites them:
	// ParseModelAndThinking maps those names only on the Anthropic endpoint. Left alone
	// they would become "Viberouter/gpt-4o" and 404 after the request was already
	// authenticated and held against the customer's wallet.
	if resolved, ok := nineRouterAliasTarget(model); ok {
		model = resolved
	}

	// Already namespaced. "Viberouter/claude-sonnet-5" stays as it is rather than
	// becoming "Viberouter/Viberouter/claude-sonnet-5".
	if strings.Contains(model, "/") {
		return model
	}

	return nineRouterModelPrefix() + model
}

// nineRouterModelPrefix reads the pool prefix, defaulting to the Kiro pool.
//
// An explicitly empty value disables prefixing, which is what a 9Router deployment
// with a flat namespace would need.
func nineRouterModelPrefix() string {
	raw, set := os.LookupEnv(envNineRouterModelPrefix)
	if !set {
		return nineRouterDefaultPrefix
	}
	prefix := strings.TrimSpace(raw)
	if prefix == "" {
		return ""
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return prefix
}

// lookupNineRouterModelMap resolves an explicit override.
//
// Matching is case-insensitive on the public id because model ids arrive in mixed
// case from clients, while the upstream value is returned verbatim: the upstream
// decides its own capitalisation and normalising it here would break the lookup on
// the other side.
func lookupNineRouterModelMap(publicModel string) (string, bool) {
	raw := strings.TrimSpace(os.Getenv(envNineRouterModelMap))
	if raw == "" {
		return "", false
	}
	want := strings.ToLower(publicModel)
	for _, entry := range strings.Split(raw, ",") {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		from := strings.ToLower(strings.TrimSpace(parts[0]))
		to := strings.TrimSpace(parts[1])
		if from == "" || to == "" {
			continue
		}
		if from == want {
			return to, true
		}
	}
	return "", false
}

// nineRouterAliasModel is what the legacy OpenAI aliases resolve to.
//
// Sonnet rather than Opus: these aliases carry no information about what the caller
// wanted, so the cheaper capable model is the honest default. A caller who wants Opus
// can name it.
const nineRouterAliasModel = "claude-sonnet-5"

// nineRouterAliasTarget resolves a legacy OpenAI model id to a real model.
//
// Only ids the upstream does not serve are rewritten. "auto" is deliberately absent:
// the upstream publishes its own "auto" route, and rewriting it here would replace the
// router's model selection with one fixed model, silently removing a capability the
// caller asked for.
//
// The gpt-4 generation is genuinely gone upstream, so a client still asking for it
// would get a 404 after being authenticated and charged a wallet hold. Those are
// mapped rather than refused, because they are what an OpenAI SDK sends by default.
//
// Matching is exact rather than by substring, so a real model whose name merely
// contains one of these strings is left alone. NINEROUTER_MODEL_MAP is consulted
// first, so an operator can still override any of it.
func nineRouterAliasTarget(publicModel string) (string, bool) {
	switch strings.ToLower(publicModel) {
	case "gpt-4o", "gpt-4", "gpt-4-turbo", "gpt-3.5-turbo", "gpt-4o-mini":
		return nineRouterAliasModel, true
	}
	return "", false
}
