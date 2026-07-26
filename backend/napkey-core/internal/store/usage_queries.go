package store

import (
	"context"
	"fmt"
	"time"

	"napkey-core/internal/pricing"
)

// billingTimeZone is the zone day boundaries are cut on.
//
// Customers are in Vietnam, so "today" has to mean today in Hanoi, not in UTC.
// Bucketing by UTC day would put traffic from 07:00 local onward into the previous
// day's total and make the console disagree with the customer's own clock.
const billingTimeZone = "Asia/Ho_Chi_Minh"

// BillingTimeZone exposes that zone so the transport layer parses a bare date on
// the same boundary the aggregation groups by. Two different zones between parsing
// and grouping would make a day's total exclude traffic the console asked for.
func BillingTimeZone() string { return billingTimeZone }

// UsageRange bounds a usage query.
type UsageRange struct {
	// From is inclusive, To is exclusive. Half-open so adjacent ranges tile without
	// double-counting the boundary row.
	From time.Time
	To   time.Time
}

// UsageFilter narrows aggregates without changing their time window.
type UsageFilter struct {
	// Empty means all of the user's keys. The user_id predicate remains mandatory,
	// so a caller cannot use another tenant's key id to read their usage.
	KeyID string
}

// Normalize fills in a sensible window when either end is missing.
func (r UsageRange) Normalize() UsageRange {
	if r.To.IsZero() {
		r.To = time.Now()
	}
	if r.From.IsZero() {
		r.From = r.To.AddDate(0, 0, -30)
	}
	return r
}

// UsageTotals is the aggregate over a range.
type UsageTotals struct {
	Requests   int64
	Tokens     pricing.Tokens
	CostMicros int64
	// UnpricedRequests is traffic served with no price on file. Surfaced rather
	// than hidden: it is revenue that was not charged and it needs a human.
	UnpricedRequests int64
	// EstimatedRequests is how many rows carry an estimated output token count.
	// A customer is entitled to know how much of their bill is measured and how
	// much is inferred.
	EstimatedRequests int64
	ErrorRequests     int64
}

// GetUserUsageTotals aggregates one user's usage over a range.
func (s *Store) GetUserUsageTotals(ctx context.Context, userID string, r UsageRange, filter UsageFilter) (*UsageTotals, error) {
	r = r.Normalize()
	var out UsageTotals
	err := s.db.QueryRowContext(ctx, `
		SELECT count(*)::bigint,
		       coalesce(sum(input_tokens), 0)::bigint,
		       coalesce(sum(output_tokens), 0)::bigint,
		       coalesce(sum(cache_read_tokens), 0)::bigint,
		       coalesce(sum(cache_write_tokens), 0)::bigint,
		       coalesce(sum(cost_micros), 0)::bigint,
		       count(*) FILTER (WHERE unpriced)::bigint,
		       count(*) FILTER (WHERE tokens_estimated)::bigint,
		       count(*) FILTER (WHERE status <> 'success')::bigint
		FROM usage_records
		WHERE user_id = $1 AND created_at >= $2 AND created_at < $3
		  AND ($4 = '' OR api_key_id::text = $4)`,
		userID, r.From.UTC(), r.To.UTC(), filter.KeyID).Scan(
		&out.Requests,
		&out.Tokens.Input, &out.Tokens.Output, &out.Tokens.CacheRead, &out.Tokens.CacheWrite,
		&out.CostMicros, &out.UnpricedRequests, &out.EstimatedRequests, &out.ErrorRequests)
	if err != nil {
		return nil, fmt.Errorf("store: totalling usage for user %s: %w", userID, err)
	}
	return &out, nil
}

// UsageBucket is one day of usage.
type UsageBucket struct {
	// Day is midnight in the billing time zone.
	Day        time.Time
	Requests   int64
	Tokens     pricing.Tokens
	CostMicros int64
}

// GetUserUsageDaily returns a per-day series for charting.
//
// Days with no traffic are absent rather than zero-filled; the console draws the
// gap. Generating a dense series in SQL would mean a generate_series join for a
// detail the frontend can handle from the range it already asked for.
func (s *Store) GetUserUsageDaily(ctx context.Context, userID string, r UsageRange, filter UsageFilter) ([]UsageBucket, error) {
	r = r.Normalize()
	rows, err := s.db.QueryContext(ctx, `
		SELECT date_trunc('day', created_at AT TIME ZONE $4) AS day,
		       count(*)::bigint,
		       coalesce(sum(input_tokens), 0)::bigint,
		       coalesce(sum(output_tokens), 0)::bigint,
		       coalesce(sum(cache_read_tokens), 0)::bigint,
		       coalesce(sum(cache_write_tokens), 0)::bigint,
		       coalesce(sum(cost_micros), 0)::bigint
		FROM usage_records
		WHERE user_id = $1 AND created_at >= $2 AND created_at < $3
		  AND ($5 = '' OR api_key_id::text = $5)
		GROUP BY day
		ORDER BY day`,
		userID, r.From.UTC(), r.To.UTC(), billingTimeZone, filter.KeyID)
	if err != nil {
		return nil, fmt.Errorf("store: building the daily usage series: %w", err)
	}
	defer rows.Close()

	var out []UsageBucket
	for rows.Next() {
		var b UsageBucket
		if err := rows.Scan(&b.Day, &b.Requests,
			&b.Tokens.Input, &b.Tokens.Output, &b.Tokens.CacheRead, &b.Tokens.CacheWrite,
			&b.CostMicros); err != nil {
			return nil, fmt.Errorf("store: scanning a usage bucket: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterating usage buckets: %w", err)
	}
	return out, nil
}

// ModelUsage is one model's share of a user's usage.
type ModelUsage struct {
	Model      string
	Requests   int64
	Tokens     pricing.Tokens
	CostMicros int64
}

// GetUserUsageByModel breaks usage down by model, most expensive first.
func (s *Store) GetUserUsageByModel(ctx context.Context, userID string, r UsageRange, filter UsageFilter) ([]ModelUsage, error) {
	r = r.Normalize()
	rows, err := s.db.QueryContext(ctx, `
		SELECT model,
		       count(*)::bigint,
		       coalesce(sum(input_tokens), 0)::bigint,
		       coalesce(sum(output_tokens), 0)::bigint,
		       coalesce(sum(cache_read_tokens), 0)::bigint,
		       coalesce(sum(cache_write_tokens), 0)::bigint,
		       coalesce(sum(cost_micros), 0)::bigint
		FROM usage_records
		WHERE user_id = $1 AND created_at >= $2 AND created_at < $3
		  AND ($4 = '' OR api_key_id::text = $4)
		GROUP BY model
		ORDER BY coalesce(sum(cost_micros), 0) DESC, model`,
		userID, r.From.UTC(), r.To.UTC(), filter.KeyID)
	if err != nil {
		return nil, fmt.Errorf("store: breaking usage down by model: %w", err)
	}
	defer rows.Close()

	var out []ModelUsage
	for rows.Next() {
		var m ModelUsage
		if err := rows.Scan(&m.Model, &m.Requests,
			&m.Tokens.Input, &m.Tokens.Output, &m.Tokens.CacheRead, &m.Tokens.CacheWrite,
			&m.CostMicros); err != nil {
			return nil, fmt.Errorf("store: scanning a model breakdown row: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterating the model breakdown: %w", err)
	}
	return out, nil
}

// UsageRecord is one row of the ledger, as the console shows it.
type UsageRecord struct {
	ID          int64
	RequestID   string
	APIKeyID    *string
	KeyName     string
	KeyPrefix   string
	KeyLastFour string
	Model       string
	Tokens      pricing.Tokens
	CostMicros  int64
	Unpriced    bool
	Estimated   bool
	LatencyMS   *int
	Status      string
	CreatedAt   time.Time
}

// ListUserUsage returns the ledger for a user, newest first.
//
// The key is joined in so the console can label a row with the key that produced
// it. A LEFT JOIN because api_key_id is nullable: usage outlives a deleted key by
// design, and those rows still have to be listable.
func (s *Store) ListUserUsage(ctx context.Context, userID string, r UsageRange, keyID string, limit, offset int) ([]UsageRecord, int64, error) {
	r = r.Normalize()
	if limit <= 0 {
		limit = 50
	}

	// The empty-string sentinel for keyID keeps this to one statement instead of
	// concatenating a WHERE clause, which is how SQL injection gets introduced.
	var total int64
	err := s.db.QueryRowContext(ctx, `
		SELECT count(*)::bigint FROM usage_records
		WHERE user_id = $1 AND created_at >= $2 AND created_at < $3
		  AND ($4 = '' OR api_key_id::text = $4)`,
		userID, r.From.UTC(), r.To.UTC(), keyID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("store: counting usage records: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, r.request_id, r.api_key_id,
		       coalesce(k.name, ''), coalesce(k.key_prefix, ''), coalesce(k.last_four, ''),
		       r.model, r.input_tokens, r.output_tokens, r.cache_read_tokens, r.cache_write_tokens,
		       r.cost_micros, r.unpriced, r.tokens_estimated, r.latency_ms, r.status, r.created_at
		FROM usage_records r
		LEFT JOIN api_keys k ON k.id = r.api_key_id
		WHERE r.user_id = $1 AND r.created_at >= $2 AND r.created_at < $3
		  AND ($4 = '' OR r.api_key_id::text = $4)
		ORDER BY r.created_at DESC, r.id DESC
		LIMIT $5 OFFSET $6`,
		userID, r.From.UTC(), r.To.UTC(), keyID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("store: listing usage records: %w", err)
	}
	defer rows.Close()

	var out []UsageRecord
	for rows.Next() {
		var rec UsageRecord
		if err := rows.Scan(&rec.ID, &rec.RequestID, &rec.APIKeyID,
			&rec.KeyName, &rec.KeyPrefix, &rec.KeyLastFour,
			&rec.Model, &rec.Tokens.Input, &rec.Tokens.Output,
			&rec.Tokens.CacheRead, &rec.Tokens.CacheWrite,
			&rec.CostMicros, &rec.Unpriced, &rec.Estimated,
			&rec.LatencyMS, &rec.Status, &rec.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("store: scanning a usage record: %w", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("store: iterating usage records: %w", err)
	}
	return out, total, nil
}
