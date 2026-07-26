package store

import (
	"context"
	"fmt"
	"time"

	"napkey-core/internal/pricing"
)

// CounterDrift is one key whose cached counters disagree with the ledger.
type CounterDrift struct {
	APIKeyID string
	UserID   string
	// Counter* is what api_key_usage claims.
	CounterRequests int64
	CounterTokens   int64
	CounterCost     int64
	// Ledger* is what usage_records actually sums to.
	LedgerRequests int64
	LedgerTokens   int64
	LedgerCost     int64
}

// CostDelta is the signed difference between the counter and the ledger, in micros.
func (d CounterDrift) CostDelta() int64 { return d.CounterCost - d.LedgerCost }

// FindCounterDrift lists keys whose cached counters disagree with the ledger.
//
// This is the check DESIGN.md section 9 calls for before money is attached in
// Stage 4: "usage phải đo chính xác trước khi tính tiền". api_key_usage is a
// derived cache maintained by RecordUsage in the same transaction as the ledger
// insert, so in normal operation this returns nothing. A non-empty result means
// either a bug in that transaction or a manual UPDATE, and both need a human before
// the numbers are billed.
//
// The comparison is deliberately whole-table rather than windowed: counters are
// lifetime totals, so a drift introduced last month is still a drift today.
func (s *Store) FindCounterDrift(ctx context.Context, limit int) ([]CounterDrift, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH ledger AS (
			SELECT api_key_id,
			       count(*)::bigint AS requests,
			       coalesce(sum(input_tokens + output_tokens + cache_read_tokens + cache_write_tokens), 0)::bigint AS tokens,
			       coalesce(sum(cost_micros), 0)::bigint AS cost
			FROM usage_records
			WHERE api_key_id IS NOT NULL
			GROUP BY api_key_id
		)
		SELECT k.id, k.user_id,
		       u.requests_count, u.tokens_used, u.cost_micros,
		       coalesce(l.requests, 0), coalesce(l.tokens, 0), coalesce(l.cost, 0)
		FROM api_key_usage u
		JOIN api_keys k ON k.id = u.api_key_id
		LEFT JOIN ledger l ON l.api_key_id = u.api_key_id
		WHERE u.requests_count <> coalesce(l.requests, 0)
		   OR u.tokens_used    <> coalesce(l.tokens, 0)
		   OR u.cost_micros    <> coalesce(l.cost, 0)
		ORDER BY abs(u.cost_micros - coalesce(l.cost, 0)) DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: checking counter drift: %w", err)
	}
	defer rows.Close()

	var out []CounterDrift
	for rows.Next() {
		var d CounterDrift
		if err := rows.Scan(&d.APIKeyID, &d.UserID,
			&d.CounterRequests, &d.CounterTokens, &d.CounterCost,
			&d.LedgerRequests, &d.LedgerTokens, &d.LedgerCost); err != nil {
			return nil, fmt.Errorf("store: scanning counter drift: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterating counter drift: %w", err)
	}
	return out, nil
}

// RebuildKeyCounters recomputes one key's cached counters from the ledger.
//
// The repair for what FindCounterDrift reports. It rewrites the cache from the
// append-only ledger, which is the only direction that is safe: the ledger is the
// source of truth and the counter is disposable.
//
// credits_used is left alone. It is kiro-go's float64 field, kept for compatibility
// with the data plane's own limit check, and this function has no float value to
// write that would not reintroduce the drift the integer column exists to avoid.
func (s *Store) RebuildKeyCounters(ctx context.Context, keyID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE api_key_usage u
		SET requests_count = coalesce(l.requests, 0),
		    tokens_used    = coalesce(l.tokens, 0),
		    cost_micros    = coalesce(l.cost, 0),
		    updated_at     = now()
		FROM (
			SELECT count(*)::bigint AS requests,
			       coalesce(sum(input_tokens + output_tokens + cache_read_tokens + cache_write_tokens), 0)::bigint AS tokens,
			       coalesce(sum(cost_micros), 0)::bigint AS cost
			FROM usage_records
			WHERE api_key_id = $1
		) l
		WHERE u.api_key_id = $1`, keyID)
	if err != nil {
		return fmt.Errorf("store: rebuilding counters for key %s: %w", keyID, err)
	}
	return nil
}

// UnpricedModel is a model that served traffic with no price on file.
type UnpricedModel struct {
	Model     string
	Requests  int64
	Tokens    int64
	FirstSeen time.Time
	LastSeen  time.Time
}

// ListUnpricedModels reports traffic that was served but not charged.
//
// Every row here is revenue that was given away, so this is the query an operator
// should see on a dashboard rather than discover at month end. The fallback '*'
// price in migration 0003 means this should normally stay empty; a non-empty result
// means even the fallback was missing.
func (s *Store) ListUnpricedModels(ctx context.Context, since time.Time) ([]UnpricedModel, error) {
	if since.IsZero() {
		since = time.Now().AddDate(0, 0, -30)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT model, count(*)::bigint,
		       coalesce(sum(input_tokens + output_tokens + cache_read_tokens + cache_write_tokens), 0)::bigint,
		       min(created_at), max(created_at)
		FROM usage_records
		WHERE unpriced AND created_at >= $1
		GROUP BY model
		ORDER BY 2 DESC`, since.UTC())
	if err != nil {
		return nil, fmt.Errorf("store: listing unpriced models: %w", err)
	}
	defer rows.Close()

	var out []UnpricedModel
	for rows.Next() {
		var m UnpricedModel
		if err := rows.Scan(&m.Model, &m.Requests, &m.Tokens, &m.FirstSeen, &m.LastSeen); err != nil {
			return nil, fmt.Errorf("store: scanning an unpriced model: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterating unpriced models: %w", err)
	}
	return out, nil
}

// RevenueByModel is platform-wide usage for one model.
type RevenueByModel struct {
	Model      string
	Requests   int64
	Tokens     pricing.Tokens
	CostMicros int64
	// EstimatedRequests is how many of these rows priced an estimated output token
	// count. A high share means the revenue figure rests on kiro-go's estimator
	// rather than on measured usage, which is worth knowing before trusting a
	// margin calculation.
	EstimatedRequests int64
}

// GetRevenueByModel aggregates billed usage across all users.
//
// This is the input to the margin view in Stage 5: what customers were charged, per
// model. It is not profit, because the upstream cost side lives in kiro-go's
// account pool and is not in this database.
func (s *Store) GetRevenueByModel(ctx context.Context, r UsageRange) ([]RevenueByModel, error) {
	r = r.Normalize()
	rows, err := s.db.QueryContext(ctx, `
		SELECT model,
		       count(*)::bigint,
		       coalesce(sum(input_tokens), 0)::bigint,
		       coalesce(sum(output_tokens), 0)::bigint,
		       coalesce(sum(cache_read_tokens), 0)::bigint,
		       coalesce(sum(cache_write_tokens), 0)::bigint,
		       coalesce(sum(cost_micros), 0)::bigint,
		       count(*) FILTER (WHERE tokens_estimated)::bigint
		FROM usage_records
		WHERE created_at >= $1 AND created_at < $2
		GROUP BY model
		ORDER BY coalesce(sum(cost_micros), 0) DESC, model`,
		r.From.UTC(), r.To.UTC())
	if err != nil {
		return nil, fmt.Errorf("store: aggregating revenue by model: %w", err)
	}
	defer rows.Close()

	var out []RevenueByModel
	for rows.Next() {
		var m RevenueByModel
		if err := rows.Scan(&m.Model, &m.Requests,
			&m.Tokens.Input, &m.Tokens.Output, &m.Tokens.CacheRead, &m.Tokens.CacheWrite,
			&m.CostMicros, &m.EstimatedRequests); err != nil {
			return nil, fmt.Errorf("store: scanning a revenue row: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterating revenue rows: %w", err)
	}
	return out, nil
}

// PruneUsageRecords deletes ledger rows older than the retention window.
//
// Not called by the janitor. It exists so a retention policy can be applied
// deliberately, because deleting usage history is a decision with legal and
// accounting weight, not housekeeping. DESIGN.md section 5 suggests monthly
// partitioning past ~10M rows, which is the better answer at scale.
func (s *Store) PruneUsageRecords(ctx context.Context, olderThan time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM usage_records WHERE created_at < $1`, olderThan.UTC())
	if err != nil {
		return 0, fmt.Errorf("store: pruning usage records: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return n, nil
}
