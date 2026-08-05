package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"kiro-go/pool"
)

// Capacity reporting when 9Router is the upstream.
//
// The account pool is the capacity model for the Kiro path: a request needs a usable
// account, so "available accounts" is a meaningful health signal and zero of them is
// an outage. With 9Router that model does not apply. There are no local accounts at
// all, and capacity belongs to the router: this process holds one endpoint key and
// the pool is legitimately empty.
//
// This matters because napkey-core turns available <= 0 into an outage on the public
// status page (internal/reliability). Reporting the empty pool while serving traffic
// through 9Router would show customers an outage during normal operation, which is
// worse than showing nothing: an alert that fires when nothing is wrong is an alert
// people learn to ignore.
//
// So when 9Router is enabled these counts describe the upstream link rather than the
// pool. One reachable upstream is one unit of capacity.

// upstreamCapacity is what the status endpoints report for accounts and available.
type upstreamCapacity struct {
	Accounts  int
	Available int
}

// capacity reports the capacity of whichever upstream is actually serving.
//
// The health probe is deliberately not part of this. Reporting "available" would
// otherwise depend on a network call in a handler the admin UI polls, and a slow
// upstream would make the status page slow rather than merely pessimistic. A
// configuration error is already refused at request time with a 503, and it is
// visible in the deploy log, so treating configured-as-available here is honest.
func (h *Handler) capacity() upstreamCapacity {
	if !nineRouterConfigured() {
		return upstreamCapacity{Accounts: h.pool.Count(), Available: h.pool.AvailableCount()}
	}
	if _, err := getNineRouterClient(); err != nil {
		// Enabled but unusable. Zero available is correct here: every request will be
		// refused, which is exactly the outage the status page should show.
		return upstreamCapacity{Accounts: 1, Available: 0}
	}
	return upstreamCapacity{Accounts: 1, Available: 1}
}

// apiUpstreamProbe answers "is the upstream actually reachable right now".
//
// This is the live counterpart to capacity(), which deliberately answers from
// configuration alone so that polling the status page cannot be slowed down by a
// slow upstream. Reachability still has to be checkable somewhere, or diagnosing a
// Cloudflare token that expired means reading request logs; this is that somewhere,
// on an explicit operator-triggered call rather than on a polled path.
func (h *Handler) apiUpstreamProbe(w http.ResponseWriter, r *http.Request) {
	description, configErr := DescribeUpstream()
	out := map[string]interface{}{"upstream": description}
	if configErr != nil {
		// Misconfigured, so there is nothing to probe: the error is the answer.
		out["upstream"] = ""
		out["ok"] = false
		out["error"] = configErr.Error()
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(out)
		return
	}

	if !nineRouterConfigured() {
		// The account pool has no single endpoint to probe; per-account checks are
		// what /accounts/{id}/test is for. Reporting configured capacity here is
		// honest, and claiming a reachability check that did not happen would not be.
		out["ok"] = pool.GetPool().AvailableCount() > 0
		out["probed"] = false
		json.NewEncoder(w).Encode(out)
		return
	}

	// Bounded independently of the request timeout: an operator waiting on a
	// diagnostic wants a verdict quickly, and a slow upstream is itself the finding.
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	out["probed"] = true
	if err := nineRouterHealth(ctx); err != nil {
		out["ok"] = false
		out["error"] = err.Error()
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(out)
		return
	}
	out["ok"] = true
	json.NewEncoder(w).Encode(out)
}

// DescribeUpstream reports which upstream will serve traffic, and any reason it
// cannot. Called once at startup so a misconfiguration is visible in the deploy log
// rather than discovered by the first customer whose request is refused.
func DescribeUpstream() (string, error) {
	if !nineRouterConfigured() {
		count := pool.GetPool().Count()
		if count == 0 {
			return "", errors.New("the account pool is empty and 9Router is disabled, so no request can be served: " +
				"set NINEROUTER_ENABLED=true or add an upstream account")
		}
		return fmt.Sprintf("account pool (%d accounts)", count), nil
	}
	client, err := getNineRouterClient()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("9Router at %s (model prefix %q)", redactURL(client.baseURL), nineRouterModelPrefix()), nil
}
