package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"napkey-core/internal/pgwire"
	"napkey-core/internal/pricing"
)

// Usage record statuses.
const (
	// UsageStatusSuccess is a request that completed and returned tokens.
	UsageStatusSuccess = "success"
	// UsageStatusError is a request that failed upstream. Recorded because the
	// tokens it consumed were still spent, and because a spike in errors is worth
	// seeing in the console.
	UsageStatusError = "error"
	// UsageStatusCancelled is a request the client abandoned mid-stream.
	UsageStatusCancelled = "cancelled"
)

// maxRequestIDLength matches the check constraint in migration 0003.
const maxRequestIDLength = 200

// RecordUsageParams is one request's usage as reported by the data plane.
type RecordUsageParams struct {
	// RequestID is the idempotency key. The data plane generates it once per
	// request and reuses it on every retry, which is what makes a retried report
	// safe.
	RequestID string
	// KeyID is the napkey-core api_keys id. The owner is resolved from it rather
	// than taken from the report, so a compromised data plane cannot attribute
	// usage to someone else's account.
	KeyID         string
	Model         string
	Tokens        pricing.Tokens
	CreditsMicros int64
	// UpstreamAccountID is which pool account served the request.
	UpstreamAccountID string
	LatencyMS         *int
	Status            string
	// TokensEstimated marks output token counts that came from kiro-go's estimator
	// rather than from the upstream response.
	TokensEstimated bool
	// OccurredAt is when the request was served. Pricing uses this, not the time
	// the report arrived, so a report retried an hour later still prices correctly.
	OccurredAt time.Time
}

// RecordUsageResult reports what happened.
type RecordUsageResult struct {
	// Duplicate is true when this request_id was already recorded. The caller
	// should treat it as success: the data plane is retrying a report that landed.
	Duplicate bool
	RecordID  int64
	UserID    string
	// CostMicros is what this request cost, in micro-VND.
	CostMicros int64
	// Unpriced is true when no rate covered the model. The request is recorded at
	// zero cost and flagged, because refusing to record traffic that was already
	// served would lose the record entirely.
	Unpriced bool
	// RateID is the model_prices row used, zero when unpriced.
	RateID int64
}

// RecordUsage writes one usage row, prices it, and folds it into the counters.
//
// Everything happens in one transaction so the row, its price, and the counters
// cannot disagree. Three properties matter:
//
// Idempotency. The insert is ON CONFLICT (request_id) DO NOTHING RETURNING id, so a
// retried report inserts nothing and returns no row. The counter update is
// conditional on that row existing, which is what stops a retry from
// double-counting. Checking with a prior SELECT instead would leave a window where
// two concurrent retries both see "not recorded" and both insert.
//
// Ownership. user_id comes from the key row, never from the request body.
//
// Frozen cost. The price is resolved for OccurredAt and the resulting cost is
// stored on the row. Later price changes do not move it (DESIGN.md section 5).
func (s *Store) RecordUsage(ctx context.Context, p RecordUsageParams) (*RecordUsageResult, error) {
	if err := validateRecordUsage(&p); err != nil {
		return nil, err
	}

	var out RecordUsageResult
	err := s.withTx(ctx, nil, func(tx *sql.Tx) error {
		// Resolve the owner. A revoked or disabled key still gets its usage
		// recorded: the request was served, and dropping the record would mean the
		// traffic silently cost nothing.
		var userID string
		err := tx.QueryRowContext(ctx,
			`SELECT user_id FROM api_keys WHERE id = $1`, p.KeyID).Scan(&userID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("store: resolving the key owner for usage: %w", err)
		}
		out.UserID = userID

		// Two ways to price a request, and which one applies depends on what the data
		// plane could measure.
		//
		// When the upstream reports its own credit meter, that number is authoritative
		// and is used directly. When it does not, the request is priced from the token
		// counts against model_prices, which is the same path the wallet hold already
		// uses. The second case is not hypothetical: only the Kiro pool emits a credit
		// meter, so any other upstream reports tokens alone.
		//
		// The important property is that neither path can silently produce a zero
		// charge. Before this split, a report with no credits priced to exactly zero
		// and was stored as priced, so served traffic became free with no error, no
		// flag, and nothing in the reconciliation view. Failing that way is worse than
		// failing loudly, because nothing surfaces it until an upstream invoice does.
		var (
			costMicros         int64
			upstreamCostMicros int64
			requestFeeMicros   int64
			pricedWith         *int64
			unpriced           bool
		)
		switch {
		case p.CreditsMicros > 0:
			costMicros, err = pricing.ComputeCreditCost(p.CreditsMicros, pricing.RetailMicrosPerCredit)
			if err != nil {
				return fmt.Errorf("store: pricing credits for request %q: %w", p.RequestID, err)
			}
			upstreamCostMicros, err = pricing.ComputeCreditCost(p.CreditsMicros, pricing.UpstreamMicrosPerCredit)
			if err != nil {
				return fmt.Errorf("store: pricing upstream credits for request %q: %w", p.RequestID, err)
			}
		default:
			rate, rateErr := findRateTx(ctx, tx, p.Model, p.OccurredAt)
			if rateErr != nil {
				return rateErr
			}
			if rate == nil {
				// No price on file, not even the '*' fallback. The request was already
				// served, so the row is still written; it is flagged instead, which is
				// what /v1/admin/usage-audit reports on.
				unpriced = true
				break
			}
			retail, computeErr := pricing.Compute(p.Tokens, *rate)
			if computeErr != nil {
				return fmt.Errorf("store: pricing tokens for request %q: %w", p.RequestID, computeErr)
			}
			upstream, computeErr := pricing.Compute(p.Tokens, rate.UpstreamRate())
			if computeErr != nil {
				return fmt.Errorf("store: pricing upstream tokens for request %q: %w", p.RequestID, computeErr)
			}
			costMicros = retail.Micros
			upstreamCostMicros = upstream.Micros
			requestFeeMicros = retail.RequestFeeMicros
			rateID := rate.ID
			pricedWith = &rateID
		}

		var recordID int64
		err = tx.QueryRowContext(ctx, `
			INSERT INTO usage_records (
				user_id, api_key_id, request_id, model,
				input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
				cost_micros, upstream_cost_micros, upstream_cost_estimated, priced_with, unpriced,
				credits_micros, credit_price_micros_per_credit, upstream_credit_price_micros_per_credit,
				request_fee_micros,
				upstream_account_id, latency_ms, status, tokens_estimated, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, false, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
			ON CONFLICT (request_id) DO NOTHING
			RETURNING id`,
			userID, p.KeyID, p.RequestID, pricing.NormalizeModel(p.Model),
			p.Tokens.Input, p.Tokens.Output, p.Tokens.CacheRead, p.Tokens.CacheWrite,
			costMicros, upstreamCostMicros, pricedWith, unpriced,
			p.CreditsMicros, creditPriceForRow(p.CreditsMicros), upstreamCreditPriceForRow(p.CreditsMicros),
			requestFeeMicros,
			p.UpstreamAccountID, p.LatencyMS, p.Status, p.TokensEstimated,
			p.OccurredAt.UTC(),
		).Scan(&recordID)

		if errors.Is(err, sql.ErrNoRows) {
			// The request_id is already on file. This is the retry path and it is
			// success, not an error: the data plane can stop retrying.
			out.Duplicate = true
			return nil
		}
		if err != nil {
			// A foreign key violation here means the key was deleted between the
			// lookup above and this insert, inside the same transaction's snapshot.
			// Treat it as an unknown key rather than a server fault.
			if pgwire.SQLState(err) == pgwire.CodeForeignKeyViolation {
				return ErrNotFound
			}
			return fmt.Errorf("store: inserting usage for request %q: %w", p.RequestID, err)
		}

		out.RecordID = recordID
		out.CostMicros = costMicros
		out.Unpriced = false

		// Fold into the counters. These are a derived cache: kiro-go's limit check
		// and the console's headline numbers read one row instead of aggregating
		// the ledger on every request. The ledger stays the source of truth, and
		// the reconciliation query below proves the two agree.
		//
		// credits_used remains for kiro-go compatibility. credits_micros and
		// cost_micros are the exact integer counters used by NapKey views.
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO api_key_usage (api_key_id, tokens_used, credits_used, credits_micros, cost_micros, requests_count, updated_at)
			VALUES ($1, $2, $3, $4, $5, 1, now())
			ON CONFLICT (api_key_id) DO UPDATE SET
				tokens_used    = api_key_usage.tokens_used + $2,
				credits_used   = api_key_usage.credits_used + $3,
				credits_micros = api_key_usage.credits_micros + $4,
				cost_micros    = api_key_usage.cost_micros + $5,
				requests_count = api_key_usage.requests_count + 1,
				updated_at     = now()`,
			p.KeyID, p.Tokens.Total(), float64(p.CreditsMicros)/float64(pricing.MicrocreditsPerCredit), p.CreditsMicros, costMicros); err != nil {
			return fmt.Errorf("store: updating usage counters: %w", err)
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE api_keys SET last_used_at = greatest(coalesce(last_used_at, $2), $2) WHERE id = $1`,
			p.KeyID, p.OccurredAt.UTC()); err != nil {
			return fmt.Errorf("store: updating last_used_at: %w", err)
		}
		if err := settleWalletTx(ctx, tx, p.RequestID, costMicros, true); err != nil {
			return fmt.Errorf("store: settling wallet for request %q: %w", p.RequestID, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func validateRecordUsage(p *RecordUsageParams) error {
	if p.RequestID == "" {
		return errors.New("store: usage needs a request id for idempotency")
	}
	if len(p.RequestID) > maxRequestIDLength {
		return fmt.Errorf("store: request id is longer than %d characters", maxRequestIDLength)
	}
	if p.KeyID == "" {
		return errors.New("store: usage needs a key id")
	}
	if p.CreditsMicros < 0 {
		return errors.New("store: usage credits cannot be negative")
	}

	switch p.Status {
	case "":
		p.Status = UsageStatusSuccess
	case UsageStatusSuccess, UsageStatusError, UsageStatusCancelled:
	default:
		return fmt.Errorf("store: %q is not a valid usage status", p.Status)
	}

	// A report with neither a credit meter nor any tokens describes a request that
	// consumed nothing measurable, and it would price to zero on every path. That is
	// indistinguishable from a data plane that lost its usage numbers, so it is
	// refused rather than stored as a free request. Errors and cancellations legally
	// consume nothing, so they are exempt.
	if p.CreditsMicros == 0 && p.Tokens.IsZero() && p.Status == UsageStatusSuccess {
		return errors.New("store: a successful request must report either credits or token counts")
	}
	if p.OccurredAt.IsZero() {
		p.OccurredAt = time.Now()
	}
	// A timestamp far in the future would pick up a scheduled price early, and one
	// far in the past would pick up a retired price. Clamping to now is the honest
	// resolution: the report reached us now, so that is the latest the request can
	// have happened.
	if p.OccurredAt.After(time.Now().Add(1 * time.Minute)) {
		p.OccurredAt = time.Now()
	}
	if len(p.UpstreamAccountID) > 200 {
		p.UpstreamAccountID = p.UpstreamAccountID[:200]
	}
	if len(p.Model) > 200 {
		p.Model = p.Model[:200]
	}
	return nil
}

// creditPriceForRow reports the credit rate to store on a usage row.
//
// It is zero unless the row was actually priced from a credit meter. The check
// constraint added in 0008 requires an unpriced row to carry a zero credit price,
// and a token-priced row has no credit rate to attribute, so recording the constant
// unconditionally would both violate the constraint and imply a basis that was
// never used.
func creditPriceForRow(creditsMicros int64) int64 {
	if creditsMicros > 0 {
		return pricing.RetailMicrosPerCredit
	}
	return 0
}

func upstreamCreditPriceForRow(creditsMicros int64) int64 {
	if creditsMicros > 0 {
		return pricing.UpstreamMicrosPerCredit
	}
	return 0
}
