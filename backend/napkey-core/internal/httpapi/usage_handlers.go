package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"napkey-core/internal/auth"
	"napkey-core/internal/logger"
	"napkey-core/internal/pricing"
	"napkey-core/internal/store"
)

// maxUsageRangeDays bounds how far back a single console query may reach. A
// customer with a year of traffic asking for all of it would scan millions of rows
// to render one chart.
const maxUsageRangeDays = 366

// Reports slightly ahead of the control plane clock are accepted, but a distant
// future timestamp could deliberately select a later price period.
const maxUsageFutureSkew = 5 * time.Minute

// reportUsageRequest is what kiro-go posts after a proxied request.
//
// This is the Stage 3 contract from DESIGN.md section 4, replacing the Stage 2
// shape that carried only aggregate counters. Two changes
// matter: requestId makes the report idempotent so a retry cannot double-bill, and
// token counts remain split for observability, while the measured Kiro credit
// quantity is the authoritative billing input.
//
// Pricing is deliberately absent. The data plane reports what it measured; the
// control plane decides what it costs. Letting kiro-go send a cost would make the
// amount charged a function of what the data plane claims.
type reportUsageRequest struct {
	// RequestID is the data plane's idempotency key, stable across retries.
	RequestID string `json:"requestId"`
	// KeyID is napkey-core's api_keys id, which kiro-go carries in the key name.
	KeyID string `json:"keyId"`
	// RemoteID is kiro-go's own key id, used only for diagnostics.
	RemoteID string `json:"remoteId,omitempty"`
	Model    string `json:"model"`

	InputTokens      int64   `json:"inputTokens"`
	OutputTokens     int64   `json:"outputTokens"`
	CacheReadTokens  int64   `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int64   `json:"cacheWriteTokens,omitempty"`
	Credits          float64 `json:"credits"`

	UpstreamAccountID string `json:"upstreamAccountId,omitempty"`
	LatencyMS         *int   `json:"latencyMs,omitempty"`
	Status            string `json:"status,omitempty"`
	// TokensEstimated tells the truth about the numbers above: kiro-go estimates
	// output tokens from the rendered response when the upstream does not report
	// them. Recording the distinction is what keeps an estimate from being
	// presented to a customer as a measurement.
	TokensEstimated bool `json:"tokensEstimated,omitempty"`
	// OccurredAt is when the request was served, RFC3339. Pricing uses this rather
	// than arrival time, so a report retried later still prices at the rate in
	// effect when the work happened.
	OccurredAt string `json:"occurredAt,omitempty"`
}

// handleReportUsage records one request's usage.
//
// Errors are shaped around what the data plane should do next. A malformed report
// gets 400 and must not be retried; an unknown key gets 200 with a marker, because
// the key is gone from the control plane and retrying will never help; a database
// failure gets 503 so the report is retried. Returning 200 on a storage failure
// would silently discard billable usage.
func (s *Server) handleReportUsage(w http.ResponseWriter, r *http.Request) {
	var req reportUsageRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	req.RequestID = strings.TrimSpace(req.RequestID)
	req.KeyID = strings.TrimSpace(req.KeyID)
	if req.RequestID == "" {
		writeError(w, http.StatusBadRequest, codeInvalidRequest,
			"requestId is required so a retried report cannot be billed twice")
		return
	}
	if req.KeyID == "" {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "keyId is required")
		return
	}
	// Negative counts would produce a negative cost, which in Stage 4 credits the
	// wallet. That makes this a balance check, not input tidying.
	if req.InputTokens < 0 || req.OutputTokens < 0 || req.CacheReadTokens < 0 || req.CacheWriteTokens < 0 {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "token counts must not be negative")
		return
	}
	creditsMicros, err := pricing.CreditMicrosFromFloat(req.Credits)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "credits must be a finite non-negative number")
		return
	}

	occurredAt := time.Now()
	if req.OccurredAt != "" {
		parsed, err := time.Parse(time.RFC3339, req.OccurredAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, codeInvalidRequest,
				"occurredAt must be an RFC3339 timestamp")
			return
		}
		occurredAt = parsed
	}
	if occurredAt.After(time.Now().Add(maxUsageFutureSkew)) {
		writeError(w, http.StatusBadRequest, codeInvalidRequest,
			"occurredAt is too far in the future")
		return
	}

	result, err := s.store.RecordUsage(r.Context(), store.RecordUsageParams{
		RequestID: req.RequestID,
		KeyID:     req.KeyID,
		Model:     req.Model,
		Tokens: pricing.Tokens{
			Input:      req.InputTokens,
			Output:     req.OutputTokens,
			CacheRead:  req.CacheReadTokens,
			CacheWrite: req.CacheWriteTokens,
		},
		CreditsMicros:     creditsMicros,
		UpstreamAccountID: req.UpstreamAccountID,
		LatencyMS:         req.LatencyMS,
		Status:            req.Status,
		TokensEstimated:   req.TokensEstimated,
		OccurredAt:        occurredAt,
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Nothing to retry against: the key does not exist here. Reported as
			// success so the data plane stops, but logged because a stream of these
			// means the two services disagree about which keys exist.
			logger.Warnf("usage report for unknown key %s (request %s) discarded", req.KeyID, req.RequestID)
			writeJSON(w, http.StatusOK, map[string]any{"status": "ignored_unknown_key"})
			return
		}
		// Anything else is likely transient. 503 tells the data plane to retry, and
		// the request_id makes that retry safe.
		logger.Errorf("recording usage for request %s failed: %v", req.RequestID, err)
		writeError(w, http.StatusServiceUnavailable, codeInternal,
			"could not record usage, retry with the same requestId")
		return
	}

	if result.Duplicate {
		// The idempotency key did its job. Reported explicitly so a retry storm is
		// visible in the data plane's logs rather than looking like success.
		writeJSON(w, http.StatusOK, map[string]any{"status": "duplicate", "requestId": req.RequestID})
		return
	}
	if result.Unpriced {
		// Served and recorded, but charged nothing because no rate covered the
		// model. Logged at warn: this is revenue given away and it needs a human.
		logger.Warnf("usage for model %q had no price on file, recorded at zero cost (request %s)",
			req.Model, req.RequestID)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "recorded",
		"id":         result.RecordID,
		"costMicros": result.CostMicros,
		"unpriced":   result.Unpriced,
	})
}

// usageRangeFrom parses the from/to query parameters.
//
// Both are optional and default to the last 30 days. The range is clamped rather
// than rejected when too wide, so a console bug asking for ten years degrades to a
// year of data instead of an error the user cannot act on.
func usageRangeFrom(r *http.Request) (store.UsageRange, string) {
	var out store.UsageRange
	query := r.URL.Query()

	if v := strings.TrimSpace(query.Get("from")); v != "" {
		parsed, err := parseDateOrTime(v)
		if err != nil {
			return out, "from must be a date (2026-01-31) or an RFC3339 timestamp"
		}
		out.From = parsed
	}
	if v := strings.TrimSpace(query.Get("to")); v != "" {
		parsed, err := parseDateOrTime(v)
		if err != nil {
			return out, "to must be a date (2026-01-31) or an RFC3339 timestamp"
		}
		out.To = parsed
	}
	out = out.Normalize()
	if !out.To.After(out.From) {
		return out, "to must be after from"
	}
	if out.To.Sub(out.From) > maxUsageRangeDays*24*time.Hour {
		out.From = out.To.AddDate(0, 0, -maxUsageRangeDays)
	}
	return out, ""
}

// parseDateOrTime accepts a bare date or a full RFC3339 timestamp.
//
// A bare date is interpreted in the billing time zone, so "from=2026-01-31" means
// midnight in Hanoi. Parsing it as UTC would shift the boundary seven hours and put
// a customer's morning traffic in the wrong day.
func parseDateOrTime(v string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	loc, err := time.LoadLocation(store.BillingTimeZone())
	if err != nil {
		// The tzdata database is missing from the image. UTC is wrong by seven
		// hours but it is better than refusing to render the page.
		loc = time.UTC
	}
	return time.ParseInLocation("2006-01-02", v, loc)
}

// tokensView is the JSON shape for a token breakdown.
func tokensView(t pricing.Tokens) map[string]any {
	return map[string]any{
		"input":      t.Input,
		"output":     t.Output,
		"cacheRead":  t.CacheRead,
		"cacheWrite": t.CacheWrite,
		"total":      t.Total(),
	}
}

// costView renders money in both units.
//
// Micros are authoritative and the console should compute with them; the VND and
// formatted forms are provided so every client renders the same rounding. Leaving
// the conversion to the frontend is how two pages end up disagreeing by a dong.
func costView(micros int64) map[string]any {
	return map[string]any{
		"micros":    micros,
		"vnd":       pricing.VNDFromMicros(micros),
		"formatted": pricing.FormatVND(micros) + " ₫",
	}
}

func creditsView(microcredits int64) map[string]any {
	return map[string]any{
		"micros":  microcredits,
		"credits": float64(microcredits) / float64(pricing.MicrocreditsPerCredit),
	}
}

// handleUsageSummary returns the console's headline numbers.
func (s *Server) handleUsageSummary(w http.ResponseWriter, r *http.Request) {
	su := sessionFromContext(r.Context())

	summary, err := s.store.GetUsageSummary(r.Context(), su.User.ID)
	if err != nil {
		writeStoreError(w, err, "summarizing usage")
		return
	}

	// The last 30 days from the ledger, alongside the lifetime counters. Both are
	// shown because they answer different questions and a customer comparing them
	// should see consistent numbers.
	window := store.UsageRange{From: time.Now().AddDate(0, 0, -30), To: time.Now()}
	recent, err := s.store.GetUserUsageTotals(r.Context(), su.User.ID, window, store.UsageFilter{})
	if err != nil {
		writeStoreError(w, err, "totalling recent usage")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"usage": map[string]any{
			"totalTokens":   summary.TotalTokens,
			"totalRequests": summary.TotalRequests,
			"activeKeys":    summary.ActiveKeys,
			"totalCost":     costView(summary.TotalCostMicros),
			// Retained so an existing console build keeps rendering. Stage 4 removes
			// it once nothing reads it.
			"totalCredits": summary.TotalCredits,
		},
		"last30Days": map[string]any{
			"requests":          recent.Requests,
			"tokens":            tokensView(recent.Tokens),
			"cost":              costView(recent.CostMicros),
			"credits":           creditsView(recent.CreditsMicros),
			"errorRequests":     recent.ErrorRequests,
			"estimatedRequests": recent.EstimatedRequests,
			"unpricedRequests":  recent.UnpricedRequests,
		},
		"billing": map[string]any{
			"mode":    "prepaid_wallet",
			"message": "usage is priced against the prepaid wallet; top up before the available balance reaches zero",
		},
	})
}

// handleUsageDetail returns the per-day series and the per-model breakdown.
func (s *Server) handleUsageDetail(w http.ResponseWriter, r *http.Request) {
	su := sessionFromContext(r.Context())

	window, problem := usageRangeFrom(r)
	if problem != "" {
		writeFieldErrors(w, map[string]string{"range": problem})
		return
	}
	filter := store.UsageFilter{KeyID: strings.TrimSpace(r.URL.Query().Get("keyId"))}

	totals, err := s.store.GetUserUsageTotals(r.Context(), su.User.ID, window, filter)
	if err != nil {
		writeStoreError(w, err, "totalling usage")
		return
	}
	daily, err := s.store.GetUserUsageDaily(r.Context(), su.User.ID, window, filter)
	if err != nil {
		writeStoreError(w, err, "building the daily usage series")
		return
	}
	byModel, err := s.store.GetUserUsageByModel(r.Context(), su.User.ID, window, filter)
	if err != nil {
		writeStoreError(w, err, "breaking usage down by model")
		return
	}

	days := make([]map[string]any, 0, len(daily))
	for _, b := range daily {
		days = append(days, map[string]any{
			"day":      b.Day.Format("2006-01-02"),
			"requests": b.Requests,
			"tokens":   tokensView(b.Tokens),
			"cost":     costView(b.CostMicros),
			"credits":  creditsView(b.CreditsMicros),
		})
	}
	models := make([]map[string]any, 0, len(byModel))
	for _, m := range byModel {
		models = append(models, map[string]any{
			"model":    m.Model,
			"requests": m.Requests,
			"tokens":   tokensView(m.Tokens),
			"cost":     costView(m.CostMicros),
			"credits":  creditsView(m.CreditsMicros),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"range": map[string]any{
			"from": window.From.UTC().Format(time.RFC3339),
			"to":   window.To.UTC().Format(time.RFC3339),
		},
		"totals": map[string]any{
			"requests":          totals.Requests,
			"tokens":            tokensView(totals.Tokens),
			"cost":              costView(totals.CostMicros),
			"credits":           creditsView(totals.CreditsMicros),
			"errorRequests":     totals.ErrorRequests,
			"estimatedRequests": totals.EstimatedRequests,
			"unpricedRequests":  totals.UnpricedRequests,
		},
		"daily":   days,
		"byModel": models,
	})
}

// handleListUsageRecords returns the raw ledger, newest first.
//
// This is what makes a charge auditable from the customer's side: every request is
// listed with its tokens, its cost, and whether the token count was measured or
// estimated.
func (s *Server) handleListUsageRecords(w http.ResponseWriter, r *http.Request) {
	su := sessionFromContext(r.Context())

	window, problem := usageRangeFrom(r)
	if problem != "" {
		writeFieldErrors(w, map[string]string{"range": problem})
		return
	}
	limit, offset := parsePagination(r, 50, 500)
	keyID := strings.TrimSpace(r.URL.Query().Get("keyId"))

	records, total, err := s.store.ListUserUsage(r.Context(), su.User.ID, window, keyID, limit, offset)
	if err != nil {
		writeStoreError(w, err, "listing usage records")
		return
	}

	out := make([]map[string]any, 0, len(records))
	for _, rec := range records {
		row := map[string]any{
			"id":        rec.ID,
			"requestId": rec.RequestID,
			"model":     rec.Model,
			"tokens":    tokensView(rec.Tokens),
			"cost":      costView(rec.CostMicros),
			"credits":   creditsView(rec.CreditsMicros),
			"unpriced":  rec.Unpriced,
			"estimated": rec.Estimated,
			"status":    rec.Status,
			"createdAt": rec.CreatedAt.UTC().Format(time.RFC3339),
		}
		if rec.APIKeyID != nil {
			row["keyId"] = *rec.APIKeyID
			row["keyName"] = rec.KeyName
			row["keyMasked"] = auth.DisplayKey(rec.KeyPrefix, rec.KeyLastFour)
		}
		if rec.LatencyMS != nil {
			row["latencyMs"] = *rec.LatencyMS
		}
		out = append(out, row)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"records": out,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
	})
}
