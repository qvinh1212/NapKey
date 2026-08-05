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

// ErrPriceOverlap means the requested period collides with an existing price for
// the same model. Surfaced as a conflict rather than swallowed, because a price
// book with two prices for one instant cannot be audited.
var ErrPriceOverlap = errors.New("a price for this model already covers that period")

// priceColumns is the shared select list, ordered to match scanRate.
const priceColumns = `
	SELECT id, model, input_micros_per_1k, output_micros_per_1k,
	       cache_read_micros_per_1k, cache_write_micros_per_1k,
	       upstream_input_micros_per_1k, upstream_output_micros_per_1k,
	       upstream_cache_read_micros_per_1k, upstream_cache_write_micros_per_1k,
	       request_fee_micros, upstream_request_fee_micros,
	       effective_from, effective_to, source_note
	FROM model_prices`

func scanRate(sc interface{ Scan(...any) error }) (*pricing.Rate, error) {
	var r pricing.Rate
	err := sc.Scan(&r.ID, &r.Model, &r.InputPer1k, &r.OutputPer1k,
		&r.CacheReadPer1k, &r.CacheWritePer1k,
		&r.UpstreamInputPer1k, &r.UpstreamOutputPer1k,
		&r.UpstreamCacheReadPer1k, &r.UpstreamCacheWritePer1k,
		&r.RequestFee, &r.UpstreamRequestFee,
		&r.EffectiveFrom, &r.EffectiveTo, &r.SourceNote)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// findRateTx resolves the price for a model at a point in time, inside a caller's
// transaction.
//
// Two things matter here. The period is half-open, [effective_from, effective_to),
// so the instant a price is superseded belongs to exactly one row. And when the
// model has no row of its own, the lookup falls back to the '*' sentinel, which is
// priced at the most expensive tier: an unrecognized model id from upstream would
// otherwise be served for free.
//
// Returns nil with no error when nothing matches at all, including the fallback.
// That is a real state (a fresh database with no seeded prices) and the caller
// records the usage as unpriced rather than rejecting traffic it already served.
func findRateTx(ctx context.Context, tx *sql.Tx, model string, at time.Time) (*pricing.Rate, error) {
	normalized := pricing.NormalizeModel(model)

	// One statement covering both the exact model and the fallback, ordered so an
	// exact match always wins. Doing it in two round trips would leave a window
	// where a price inserted between them changes the answer.
	row := tx.QueryRowContext(ctx, priceColumns+`
		WHERE model IN ($1, $2)
		  AND effective_from <= $3
		  AND (effective_to IS NULL OR effective_to > $3)
		ORDER BY (model = $1) DESC, effective_from DESC
		LIMIT 1`, normalized, pricing.FallbackModel, at.UTC())

	rate, err := scanRate(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: looking up the price for %q: %w", normalized, err)
	}
	return rate, nil
}

// FindRate resolves the price for a model at a point in time.
func (s *Store) FindRate(ctx context.Context, model string, at time.Time) (*pricing.Rate, error) {
	normalized := pricing.NormalizeModel(model)
	row := s.db.QueryRowContext(ctx, priceColumns+`
		WHERE model IN ($1, $2)
		  AND effective_from <= $3
		  AND (effective_to IS NULL OR effective_to > $3)
		ORDER BY (model = $1) DESC, effective_from DESC
		LIMIT 1`, normalized, pricing.FallbackModel, at.UTC())

	rate, err := scanRate(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: looking up the price for %q: %w", normalized, err)
	}
	return rate, nil
}

// ListRates returns the price book, newest period first.
func (s *Store) ListRates(ctx context.Context, model string, includeExpired bool) ([]pricing.Rate, error) {
	query := priceColumns + ` WHERE ($1 = '' OR model = $1)`
	if !includeExpired {
		query += ` AND (effective_to IS NULL OR effective_to > now())`
	}
	query += ` ORDER BY model, effective_from DESC`

	rows, err := s.db.QueryContext(ctx, query, pricing.NormalizeModel(model))
	if err != nil {
		return nil, fmt.Errorf("store: listing prices: %w", err)
	}
	defer rows.Close()

	var out []pricing.Rate
	for rows.Next() {
		rate, err := scanRate(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scanning price: %w", err)
		}
		out = append(out, *rate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterating prices: %w", err)
	}
	return out, nil
}

// SetRateParams carries a new price.
type SetRateParams struct {
	Model                   string
	InputPer1k              int64
	OutputPer1k             int64
	CacheReadPer1k          int64
	CacheWritePer1k         int64
	UpstreamInputPer1k      int64
	UpstreamOutputPer1k     int64
	UpstreamCacheReadPer1k  int64
	UpstreamCacheWritePer1k int64
	// RequestFee is the flat per-request charge in micro-VND; UpstreamRequestFee is
	// its cost basis, kept so margin reporting does not treat the retail fee as cost.
	RequestFee         int64
	UpstreamRequestFee int64
	SourceNote         string
	// EffectiveFrom defaults to now when zero. A future value schedules a price
	// change; a past value is allowed but only affects usage recorded after this
	// call, since cost_micros on existing rows is frozen at insert time.
	EffectiveFrom time.Time
}

// SetRate opens a new price period for a model and closes the previous one.
//
// The close-then-insert pair runs in one transaction, and the currently open row is
// locked before being read. Without the lock, two concurrent price changes both
// read the same open row, both close it, and both insert, leaving two open periods
// for one model. The unique partial index would catch that, but as a failed
// deploy rather than as correct behaviour.
//
// Existing usage_records are untouched on purpose: their cost was computed from the
// rate in effect when the request was served, and repricing history would break
// every invoice already sent (DESIGN.md section 5).
func (s *Store) SetRate(ctx context.Context, p SetRateParams) (*pricing.Rate, error) {
	model := pricing.NormalizeModel(p.Model)
	if model == "" {
		return nil, fmt.Errorf("store: a price needs a model")
	}
	if p.InputPer1k < 0 || p.OutputPer1k < 0 || p.CacheReadPer1k < 0 || p.CacheWritePer1k < 0 ||
		p.UpstreamInputPer1k < 0 || p.UpstreamOutputPer1k < 0 || p.UpstreamCacheReadPer1k < 0 || p.UpstreamCacheWritePer1k < 0 ||
		p.RequestFee < 0 || p.UpstreamRequestFee < 0 {
		return nil, fmt.Errorf("store: prices cannot be negative")
	}
	from := p.EffectiveFrom
	if from.IsZero() {
		from = time.Now()
	}
	from = from.UTC()

	var created *pricing.Rate
	err := s.withTx(ctx, nil, func(tx *sql.Tx) error {
		// Lock the open period for this model, if any. FOR UPDATE on the specific
		// row serializes concurrent price changes per model without blocking other
		// models.
		var openID int64
		var openFrom time.Time
		err := tx.QueryRowContext(ctx, `
			SELECT id, effective_from FROM model_prices
			WHERE model = $1 AND effective_to IS NULL
			FOR UPDATE`, model).Scan(&openID, &openFrom)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			// First price for this model.
		case err != nil:
			return fmt.Errorf("store: locking the open price for %q: %w", model, err)
		default:
			// The new period cannot start before the one it replaces, or the two
			// would overlap and the exclusion constraint would reject it with a
			// message that does not explain the cause.
			if !from.After(openFrom) {
				return fmt.Errorf("%w: the new period starts at or before the current one (%s)",
					ErrPriceOverlap, openFrom.Format(time.RFC3339))
			}
			if _, err := tx.ExecContext(ctx,
				`UPDATE model_prices SET effective_to = $2 WHERE id = $1`, openID, from); err != nil {
				return fmt.Errorf("store: closing the previous price for %q: %w", model, err)
			}
		}

		row := tx.QueryRowContext(ctx, `
			INSERT INTO model_prices (model, input_micros_per_1k, output_micros_per_1k,
			                          cache_read_micros_per_1k, cache_write_micros_per_1k,
			                          upstream_input_micros_per_1k, upstream_output_micros_per_1k,
			                          upstream_cache_read_micros_per_1k, upstream_cache_write_micros_per_1k,
			                          request_fee_micros, upstream_request_fee_micros,
			                          source_note, effective_from)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
			RETURNING id, model, input_micros_per_1k, output_micros_per_1k,
			          cache_read_micros_per_1k, cache_write_micros_per_1k,
			          upstream_input_micros_per_1k, upstream_output_micros_per_1k,
			          upstream_cache_read_micros_per_1k, upstream_cache_write_micros_per_1k,
			          request_fee_micros, upstream_request_fee_micros,
			          effective_from, effective_to, source_note`,
			model, p.InputPer1k, p.OutputPer1k, p.CacheReadPer1k, p.CacheWritePer1k,
			p.UpstreamInputPer1k, p.UpstreamOutputPer1k, p.UpstreamCacheReadPer1k, p.UpstreamCacheWritePer1k,
			p.RequestFee, p.UpstreamRequestFee,
			p.SourceNote, from)
		rate, err := scanRate(row)
		if err != nil {
			if pgwire.IsExclusionViolation(err) {
				return fmt.Errorf("%w: for model %q", ErrPriceOverlap, model)
			}
			return fmt.Errorf("store: inserting the price for %q: %w", model, err)
		}
		created = rate
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}
