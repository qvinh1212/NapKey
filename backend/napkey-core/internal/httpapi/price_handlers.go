package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"napkey-core/internal/logger"
	"napkey-core/internal/pricing"
	"napkey-core/internal/store"
)

// rateView is the API shape for one price period.
type rateView struct {
	ID              int64      `json:"id"`
	Model           string     `json:"model"`
	InputPer1k      int64      `json:"inputMicrosPer1k"`
	OutputPer1k     int64      `json:"outputMicrosPer1k"`
	CacheReadPer1k  int64      `json:"cacheReadMicrosPer1k"`
	CacheWritePer1k int64      `json:"cacheWriteMicrosPer1k"`
	UpstreamInputPer1k      int64 `json:"upstreamInputMicrosPer1k"`
	UpstreamOutputPer1k     int64 `json:"upstreamOutputMicrosPer1k"`
	UpstreamCacheReadPer1k  int64 `json:"upstreamCacheReadMicrosPer1k"`
	UpstreamCacheWritePer1k int64 `json:"upstreamCacheWriteMicrosPer1k"`
	EffectiveFrom   time.Time  `json:"effectiveFrom"`
	EffectiveTo     *time.Time `json:"effectiveTo"`
	SourceNote      string     `json:"sourceNote,omitempty"`
	// Open reports whether this is the price currently in effect.
	Open bool `json:"open"`
}

func toRateView(r pricing.Rate) rateView {
	return rateView{
		ID:              r.ID,
		Model:           r.Model,
		InputPer1k:      r.InputPer1k,
		OutputPer1k:     r.OutputPer1k,
		CacheReadPer1k:  r.CacheReadPer1k,
		CacheWritePer1k: r.CacheWritePer1k,
		UpstreamInputPer1k: r.UpstreamInputPer1k,
		UpstreamOutputPer1k: r.UpstreamOutputPer1k,
		UpstreamCacheReadPer1k: r.UpstreamCacheReadPer1k,
		UpstreamCacheWritePer1k: r.UpstreamCacheWritePer1k,
		EffectiveFrom:   r.EffectiveFrom,
		EffectiveTo:     r.EffectiveTo,
		SourceNote:      r.SourceNote,
		Open:            r.EffectiveTo == nil,
	}
}

func (s *Server) handleAdminListPrices(w http.ResponseWriter, r *http.Request) {
	model := r.URL.Query().Get("model")
	includeExpired := r.URL.Query().Get("includeExpired") == "true"

	rates, err := s.store.ListRates(r.Context(), model, includeExpired)
	if err != nil {
		writeStoreError(w, err, "listing prices")
		return
	}
	out := make([]rateView, 0, len(rates))
	for _, rate := range rates {
		out = append(out, toRateView(rate))
	}
	writeJSON(w, http.StatusOK, map[string]any{"prices": out})
}

// setPriceRequest opens a new price period for a model.
//
// Rates are micro-VND per 1,000 tokens, matching the storage unit exactly. The
// alternative, accepting a USD figure and converting here, would put the exchange
// rate in the request path and make the stored price depend on when the call landed.
type setPriceRequest struct {
	Model           string `json:"model"`
	InputPer1k      int64  `json:"inputMicrosPer1k"`
	OutputPer1k     int64  `json:"outputMicrosPer1k"`
	CacheReadPer1k  int64  `json:"cacheReadMicrosPer1k"`
	CacheWritePer1k int64  `json:"cacheWriteMicrosPer1k"`
	UpstreamInputPer1k      int64 `json:"upstreamInputMicrosPer1k"`
	UpstreamOutputPer1k     int64 `json:"upstreamOutputMicrosPer1k"`
	UpstreamCacheReadPer1k  int64 `json:"upstreamCacheReadMicrosPer1k"`
	UpstreamCacheWritePer1k int64 `json:"upstreamCacheWriteMicrosPer1k"`
	// SourceNote records where these numbers came from: list price, exchange rate,
	// margin. Required, because a price nobody can explain is a price nobody can
	// defend in six months.
	SourceNote string `json:"sourceNote"`
	// EffectiveFrom is optional and defaults to now. A future timestamp schedules
	// the change.
	EffectiveFrom string `json:"effectiveFrom,omitempty"`
}

// handleAdminSetPrice opens a new price period and closes the current one.
//
// It never edits an existing row. Usage already recorded keeps the cost it was
// charged, because cost_micros is frozen at insert time (DESIGN.md section 5).
func (s *Server) handleAdminSetPrice(w http.ResponseWriter, r *http.Request) {
	su := sessionFromContext(r.Context())
	var req setPriceRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	fields := map[string]string{}
	model := pricing.NormalizeModel(req.Model)
	if model == "" {
		fields["model"] = "model is required"
	} else if len(model) > 200 {
		fields["model"] = "model is too long"
	}
	if strings.TrimSpace(req.SourceNote) == "" {
		fields["sourceNote"] = "explain where these rates came from (list price, exchange rate, margin)"
	}
	for name, value := range map[string]int64{
		"inputMicrosPer1k":      req.InputPer1k,
		"outputMicrosPer1k":     req.OutputPer1k,
		"cacheReadMicrosPer1k":  req.CacheReadPer1k,
		"cacheWriteMicrosPer1k": req.CacheWritePer1k,
		"upstreamInputMicrosPer1k": req.UpstreamInputPer1k,
		"upstreamOutputMicrosPer1k": req.UpstreamOutputPer1k,
		"upstreamCacheReadMicrosPer1k": req.UpstreamCacheReadPer1k,
		"upstreamCacheWriteMicrosPer1k": req.UpstreamCacheWritePer1k,
	} {
		if value < 0 {
			fields[name] = "must not be negative"
		}
	}

	var effectiveFrom time.Time
	if req.EffectiveFrom != "" {
		parsed, err := time.Parse(time.RFC3339, req.EffectiveFrom)
		if err != nil {
			fields["effectiveFrom"] = "must be an RFC3339 timestamp"
		} else {
			effectiveFrom = parsed
		}
	}
	if len(fields) > 0 {
		writeFieldErrors(w, fields)
		return
	}

	rate, err := s.store.SetRate(r.Context(), store.SetRateParams{
		Model:           model,
		InputPer1k:      req.InputPer1k,
		OutputPer1k:     req.OutputPer1k,
		CacheReadPer1k:  req.CacheReadPer1k,
		CacheWritePer1k: req.CacheWritePer1k,
		UpstreamInputPer1k: req.UpstreamInputPer1k,
		UpstreamOutputPer1k: req.UpstreamOutputPer1k,
		UpstreamCacheReadPer1k: req.UpstreamCacheReadPer1k,
		UpstreamCacheWritePer1k: req.UpstreamCacheWritePer1k,
		SourceNote:      strings.TrimSpace(req.SourceNote),
		EffectiveFrom:   effectiveFrom,
	})
	if err != nil {
		if errors.Is(err, store.ErrPriceOverlap) {
			writeError(w, http.StatusConflict, codeConflict, err.Error())
			return
		}
		writeStoreError(w, err, "setting a price")
		return
	}

	// Price changes are audited. This is the record that explains why a customer's
	// per-token cost moved between two months.
	if err := s.store.WriteAudit(r.Context(), store.AuditEntry{
		ActorType: "admin", ActorID: su.User.ID, Action: "model_price.set",
		TargetType: "model_price", TargetID: strconv.FormatInt(rate.ID, 10),
		Metadata: map[string]any{
			"model":                 rate.Model,
			"inputMicrosPer1k":      rate.InputPer1k,
			"outputMicrosPer1k":     rate.OutputPer1k,
			"cacheReadMicrosPer1k":  rate.CacheReadPer1k,
			"cacheWriteMicrosPer1k": rate.CacheWritePer1k,
			"upstreamInputMicrosPer1k": rate.UpstreamInputPer1k,
			"upstreamOutputMicrosPer1k": rate.UpstreamOutputPer1k,
			"upstreamCacheReadMicrosPer1k": rate.UpstreamCacheReadPer1k,
			"upstreamCacheWriteMicrosPer1k": rate.UpstreamCacheWritePer1k,
			"effectiveFrom":         rate.EffectiveFrom.UTC().Format(time.RFC3339),
			"sourceNote":            rate.SourceNote,
		},
		IP: clientIP(r, s.trustProxy),
	}); err != nil {
		logger.Warnf("writing audit log failed: %v", err)
	}

	writeJSON(w, http.StatusCreated, map[string]any{"price": toRateView(*rate)})
}

// handleAdminQuotePrice prices a hypothetical request without recording anything.
//
// This is the tool for answering "what would 100k tokens on Opus cost", and for
// verifying a price change before traffic hits it. Read-only by construction: it
// resolves a rate and computes, but writes nothing.
func (s *Server) handleAdminQuotePrice(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	model := query.Get("model")

	at := time.Now()
	if v := strings.TrimSpace(query.Get("at")); v != "" {
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeFieldErrors(w, map[string]string{"at": "must be an RFC3339 timestamp"})
			return
		}
		at = parsed
	}

	tokens := pricing.Tokens{
		Input:      queryInt64(r, "inputTokens"),
		Output:     queryInt64(r, "outputTokens"),
		CacheRead:  queryInt64(r, "cacheReadTokens"),
		CacheWrite: queryInt64(r, "cacheWriteTokens"),
	}

	rate, err := s.store.FindRate(r.Context(), model, at)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusOK, map[string]any{
				"model":    pricing.NormalizeModel(model),
				"unpriced": true,
				"message":  "no price covers this model at that time, so usage would be recorded at zero cost",
			})
			return
		}
		writeStoreError(w, err, "looking up a price")
		return
	}

	cost, err := pricing.Compute(tokens, *rate)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"model":  pricing.NormalizeModel(model),
		"at":     at.UTC().Format(time.RFC3339),
		"price":  toRateView(*rate),
		"tokens": tokensView(tokens),
		"cost": map[string]any{
			"total":      costView(cost.Micros),
			"input":      costView(cost.InputMicros),
			"output":     costView(cost.OutputMicros),
			"cacheRead":  costView(cost.CacheReadMicros),
			"cacheWrite": costView(cost.CacheWriteMicros),
		},
	})
}

// handleAdminUsageAudit is the pre-billing reconciliation view.
//
// DESIGN.md section 9 requires usage to be measured correctly before money is
// attached to it, and section 4 of the roadmap puts the reconciliation job before
// customers, not after. This endpoint answers the three questions that have to be
// clean before Stage 4: do the cached counters match the ledger, was anything served
// without a price, and how much of the billed total rests on estimated token counts.
func (s *Server) handleAdminUsageAudit(w http.ResponseWriter, r *http.Request) {
	window, problem := usageRangeFrom(r)
	if problem != "" {
		writeFieldErrors(w, map[string]string{"range": problem})
		return
	}

	drift, err := s.store.FindCounterDrift(r.Context(), 100)
	if err != nil {
		writeStoreError(w, err, "checking counter drift")
		return
	}
	unpriced, err := s.store.ListUnpricedModels(r.Context(), window.From)
	if err != nil {
		writeStoreError(w, err, "listing unpriced models")
		return
	}
	revenue, err := s.store.GetRevenueByModel(r.Context(), window)
	if err != nil {
		writeStoreError(w, err, "aggregating revenue")
		return
	}

	driftOut := make([]map[string]any, 0, len(drift))
	for _, d := range drift {
		driftOut = append(driftOut, map[string]any{
			"apiKeyId":        d.APIKeyID,
			"userId":          d.UserID,
			"counterRequests": d.CounterRequests,
			"ledgerRequests":  d.LedgerRequests,
			"counterTokens":   d.CounterTokens,
			"ledgerTokens":    d.LedgerTokens,
			"counterCost":     costView(d.CounterCost),
			"ledgerCost":      costView(d.LedgerCost),
			"costDeltaMicros": d.CostDelta(),
		})
	}
	unpricedOut := make([]map[string]any, 0, len(unpriced))
	for _, m := range unpriced {
		unpricedOut = append(unpricedOut, map[string]any{
			"model":     m.Model,
			"requests":  m.Requests,
			"tokens":    m.Tokens,
			"firstSeen": m.FirstSeen.UTC().Format(time.RFC3339),
			"lastSeen":  m.LastSeen.UTC().Format(time.RFC3339),
		})
	}

	var totalMicros, estimatedRequests, totalRequests int64
	revenueOut := make([]map[string]any, 0, len(revenue))
	for _, m := range revenue {
		totalMicros += m.CostMicros
		estimatedRequests += m.EstimatedRequests
		totalRequests += m.Requests
		revenueOut = append(revenueOut, map[string]any{
			"model":             m.Model,
			"requests":          m.Requests,
			"tokens":            tokensView(m.Tokens),
			"cost":              costView(m.CostMicros),
			"estimatedRequests": m.EstimatedRequests,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"range": map[string]any{
			"from": window.From.UTC().Format(time.RFC3339),
			"to":   window.To.UTC().Format(time.RFC3339),
		},
		// Empty means the derived counters agree with the ledger everywhere.
		"counterDrift":   driftOut,
		"unpricedModels": unpricedOut,
		"revenueByModel": revenueOut,
		"totals": map[string]any{
			"requests":          totalRequests,
			"cost":              costView(totalMicros),
			"estimatedRequests": estimatedRequests,
		},
		// The single number to look at before turning on billing: any drift at all,
		// or any traffic served without a price, means the ledger is not yet
		// trustworthy enough to bill from.
		"readyToBill": len(driftOut) == 0 && len(unpricedOut) == 0,
	})
}

// handleAdminRebuildCounters repairs one key's cached counters from the ledger.
func (s *Server) handleAdminRebuildCounters(w http.ResponseWriter, r *http.Request) {
	su := sessionFromContext(r.Context())
	keyID := r.PathValue("id")

	if err := s.store.RebuildKeyCounters(r.Context(), keyID); err != nil {
		writeStoreError(w, err, "rebuilding usage counters")
		return
	}
	if err := s.store.WriteAudit(r.Context(), store.AuditEntry{
		ActorType: "admin", ActorID: su.User.ID, Action: "api_key.rebuild_counters",
		TargetType: "api_key", TargetID: keyID, IP: clientIP(r, s.trustProxy),
	}); err != nil {
		logger.Warnf("writing audit log failed: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "rebuilt"})
}

// queryInt64 reads a non-negative integer query parameter, defaulting to zero.
func queryInt64(r *http.Request, name string) int64 {
	v := strings.TrimSpace(r.URL.Query().Get(name))
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
