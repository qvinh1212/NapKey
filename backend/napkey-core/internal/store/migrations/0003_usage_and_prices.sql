-- Stage 3 schema: the usage ledger and the price book behind it.
--
-- The Stage 2 api_key_usage table is a counter: three numbers that only ever go
-- up. It can tell a customer they spent 40k tokens but it cannot answer "on which
-- request", "at what price", or "prove it". This migration adds the row-per-request
-- ledger that makes usage auditable, which DESIGN.md section 9 requires to be
-- correct *before* Stage 4 attaches money to it.
--
-- api_key_usage is deliberately kept. It stays the fast path for the limit checks
-- kiro-go already performs, and it becomes a derived cache of this ledger rather
-- than the source of truth.

-- Prices are versioned over time, never overwritten.
--
-- Cost is computed from the price in effect at the usage row's created_at, not the
-- current price. Without this, raising a price would retroactively change what
-- last month's traffic cost and every invoice already sent would stop reconciling.
CREATE TABLE model_prices (
    id          bigserial PRIMARY KEY,
    -- model is matched case-insensitively against the upstream model id. It is
    -- stored lowercase, enforced by the check constraint below.
    model       text NOT NULL,
    -- All four rates are micro-VND per 1,000 tokens (DESIGN.md section 5).
    -- Integers, not floats: a float accumulates representation error across
    -- millions of rows, and money that does not add up is worse than money that is
    -- slightly wrong.
    input_micros_per_1k       bigint NOT NULL,
    output_micros_per_1k      bigint NOT NULL,
    -- Cache reads are an order of magnitude cheaper than fresh input, and cache
    -- writes cost a premium over it. Collapsing them into one rate is how a
    -- reseller quietly loses money on cache-heavy traffic (DESIGN.md section 5).
    cache_read_micros_per_1k  bigint NOT NULL,
    cache_write_micros_per_1k bigint NOT NULL,
    -- How this row was derived: USD list price, exchange rate, margin. Audit trail
    -- only, never used in arithmetic, so that a recorded rate cannot silently
    -- reprice history. A price change inserts a new row instead of editing this one.
    source_note text NOT NULL DEFAULT '',
    effective_from timestamptz NOT NULL DEFAULT now(),
    -- NULL means "still in effect". Closing a period sets this to the moment the
    -- successor takes over, so the two never overlap.
    effective_to   timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT model_prices_model_lower_check CHECK (model = lower(model)),
    CONSTRAINT model_prices_model_not_blank_check CHECK (btrim(model) <> ''),
    CONSTRAINT model_prices_input_check       CHECK (input_micros_per_1k >= 0),
    CONSTRAINT model_prices_output_check      CHECK (output_micros_per_1k >= 0),
    CONSTRAINT model_prices_cache_read_check  CHECK (cache_read_micros_per_1k >= 0),
    CONSTRAINT model_prices_cache_write_check CHECK (cache_write_micros_per_1k >= 0),
    -- An open-ended row (effective_to IS NULL) is fine; a closed one must cover a
    -- positive span. Zero-length or inverted ranges would make the lookup below
    -- ambiguous or empty.
    CONSTRAINT model_prices_period_check CHECK (effective_to IS NULL OR effective_to > effective_from)
);

-- The pricing lookup is "newest row for this model whose period contains ts", so
-- the index leads with model and orders by period start descending.
CREATE INDEX model_prices_lookup_idx ON model_prices (model, effective_from DESC);

-- At most one open-ended price per model. This constraint is what makes the lookup
-- deterministic: without it, two concurrent "set price" calls both leave
-- effective_to NULL and cost depends on which row the planner happens to return.
CREATE UNIQUE INDEX model_prices_one_open_per_model_idx ON model_prices (model)
    WHERE effective_to IS NULL;

-- btree_gist is required to combine plain equality on model with range overlap in
-- one exclusion constraint.
CREATE EXTENSION IF NOT EXISTS btree_gist;

-- Overlapping periods for the same model are rejected outright rather than
-- resolved by precedence. A price book that can express two prices for one instant
-- is a book nobody can audit.
ALTER TABLE model_prices ADD CONSTRAINT model_prices_no_overlap
    EXCLUDE USING gist (
        model WITH =,
        tstzrange(effective_from, effective_to, '[)') WITH &&
    );

-- One row per proxied request.
--
-- Append-only. Rows are never updated after insert: a correction is a new
-- compensating row, which is what keeps a reconciliation run reproducible.
CREATE TABLE usage_records (
    id          bigserial PRIMARY KEY,
    -- user_id is denormalized from api_keys on purpose. Usage has to outlive the
    -- key it was billed through, and it has to be queryable per user without a join
    -- on the hot path. Set from the key's owner at insert time, never from the
    -- request body.
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- ON DELETE SET NULL, not CASCADE: deleting a key must not erase the record of
    -- what was spent through it. Revocation keeps the row (api_keys are only
    -- hard-deleted after a failed create push), but the weaker reference is the
    -- honest one for an accounting table.
    api_key_id  uuid REFERENCES api_keys (id) ON DELETE SET NULL,
    -- request_id is the data plane's idempotency key, and UNIQUE is the entire
    -- deduplication mechanism. kiro-go retries a failed usage report; without this
    -- a network blip while responding would bill the same request twice.
    request_id  text NOT NULL UNIQUE,
    model       text NOT NULL,
    input_tokens       integer NOT NULL DEFAULT 0,
    output_tokens      integer NOT NULL DEFAULT 0,
    cache_read_tokens  integer NOT NULL DEFAULT 0,
    cache_write_tokens integer NOT NULL DEFAULT 0,
    -- Computed and frozen at insert time from priced_with. Recomputing on read
    -- would let a later price edit change historical cost, the exact failure the
    -- versioned price book exists to prevent.
    cost_micros bigint NOT NULL DEFAULT 0,
    -- Which model_prices row produced cost_micros. This is what makes a disputed
    -- charge answerable: the rate is recoverable, not inferred. NULL means no price
    -- was on file, in which case cost_micros is 0 and unpriced is set.
    priced_with bigint REFERENCES model_prices (id) ON DELETE SET NULL,
    -- Real traffic that was served but not charged, because the model had no price.
    -- Flagged rather than dropped so it stays findable and fixable.
    unpriced    boolean NOT NULL DEFAULT false,
    -- Which upstream account served the request. Needed to attribute cost to a pool
    -- member and to notice one account carrying the whole load.
    upstream_account_id text NOT NULL DEFAULT '',
    latency_ms  integer,
    status      text NOT NULL DEFAULT 'success',
    -- Whether output_tokens was measured upstream or estimated by kiro-go's
    -- token estimator. Billing an estimate as though it were measured is not
    -- defensible, so the distinction is recorded per row and surfaced in the
    -- reconciliation view rather than averaged away.
    tokens_estimated boolean NOT NULL DEFAULT false,
    -- When the request happened, per the data plane. Distinct from recorded_at
    -- because a retried report can land minutes later, and pricing must use the
    -- moment of service.
    created_at  timestamptz NOT NULL DEFAULT now(),
    recorded_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT usage_records_input_check       CHECK (input_tokens >= 0),
    CONSTRAINT usage_records_output_check      CHECK (output_tokens >= 0),
    CONSTRAINT usage_records_cache_read_check  CHECK (cache_read_tokens >= 0),
    CONSTRAINT usage_records_cache_write_check CHECK (cache_write_tokens >= 0),
    -- Cost is never negative. A refund is a wallet entry in Stage 4, not a negative
    -- usage row: usage describes what was consumed, and consumption cannot be undone.
    CONSTRAINT usage_records_cost_check        CHECK (cost_micros >= 0),
    CONSTRAINT usage_records_latency_check     CHECK (latency_ms IS NULL OR latency_ms >= 0),
    CONSTRAINT usage_records_status_check      CHECK (status IN ('success', 'error', 'cancelled')),
    CONSTRAINT usage_records_request_id_check  CHECK (btrim(request_id) <> '' AND char_length(request_id) <= 200),
    CONSTRAINT usage_records_model_check       CHECK (char_length(model) <= 200),
    -- An unpriced row carries no cost, and a costed row names the rate it used.
    -- Cost with no attributable rate is exactly the number that cannot be defended
    -- in a billing dispute.
    CONSTRAINT usage_records_pricing_coherent_check CHECK (
        (unpriced AND priced_with IS NULL AND cost_micros = 0)
        OR (NOT unpriced AND (priced_with IS NOT NULL OR cost_micros = 0))
    )
);

-- The console's main query: one user's usage, newest first, over a date range.
CREATE INDEX usage_records_user_time_idx ON usage_records (user_id, created_at DESC);
-- Per-key drilldown and the per-key rollup.
CREATE INDEX usage_records_key_time_idx ON usage_records (api_key_id, created_at DESC);
-- Cost-by-model reporting across all users.
CREATE INDEX usage_records_model_time_idx ON usage_records (model, created_at DESC);
-- Partial index for the reconciliation job, which only looks at rows served
-- without a price. Stays empty in normal operation.
CREATE INDEX usage_records_unpriced_idx ON usage_records (created_at DESC) WHERE unpriced;
-- Seed prices.
--
-- Derived from Anthropic's published USD list prices at an operator-set exchange
-- rate plus a margin, per DESIGN.md section 5. The rate is a deliberate constant
-- rather than a live API call: pricing a request must not depend on a third party
-- being reachable, and a rate that moves mid-month makes invoices irreproducible.
--
-- Worked example, Sonnet input at $3.00 per 1M tokens:
--   $3.00 / 1M tokens             = $0.003 per 1k tokens
--   x 26,000 VND/USD              = 78 VND per 1k tokens
--   x 1,000,000 micros/VND        = 78,000,000 micro-VND per 1k tokens
--   x 1.30 retail multiplier      = 101,400,000
-- The multiplier folds gross margin and currency buffer into one number. It is a
-- business input, not a technical one, and it is the single value to revisit when
-- the exchange rate drifts past the operational threshold.
--
-- effective_from is set far in the past so usage arriving during the deploy window
-- prices correctly instead of landing unpriced.
INSERT INTO model_prices (
    model,
    input_micros_per_1k, output_micros_per_1k,
    cache_read_micros_per_1k, cache_write_micros_per_1k,
    source_note, effective_from
) VALUES
    -- Sonnet 4.x: $3 / $15 per 1M, cache read $0.30, cache write $3.75.
    ('claude-sonnet-4-20250514',
     101400000, 507000000, 10140000, 126750000,
     'anthropic list 3/15 usd per 1m, cache 0.30/3.75; fx 26000; retail x1.30',
     '2020-01-01 00:00:00+00'),
    ('claude-sonnet-4-5-20250929',
     101400000, 507000000, 10140000, 126750000,
     'anthropic list 3/15 usd per 1m, cache 0.30/3.75; fx 26000; retail x1.30',
     '2020-01-01 00:00:00+00'),
    -- Opus 4.x: $15 / $75 per 1M, cache read $1.50, cache write $18.75.
    ('claude-opus-4-20250514',
     507000000, 2535000000, 50700000, 633750000,
     'anthropic list 15/75 usd per 1m, cache 1.50/18.75; fx 26000; retail x1.30',
     '2020-01-01 00:00:00+00'),
    ('claude-opus-4-1-20250805',
     507000000, 2535000000, 50700000, 633750000,
     'anthropic list 15/75 usd per 1m, cache 1.50/18.75; fx 26000; retail x1.30',
     '2020-01-01 00:00:00+00'),
    -- Haiku 3.5: $0.80 / $4 per 1M, cache read $0.08, cache write $1.00.
    ('claude-3-5-haiku-20241022',
     27040000, 135200000, 2704000, 33800000,
     'anthropic list 0.80/4 usd per 1m, cache 0.08/1.00; fx 26000; retail x1.30',
     '2020-01-01 00:00:00+00');

-- A catch-all so an unrecognized model still bills instead of silently costing
-- nothing. Priced at Opus rates: if the model is unknown, the safe assumption is
-- the expensive one. The alternative, defaulting to cheap, means a new model id
-- from upstream is served at a loss until someone notices.
--
-- '*' is the sentinel the lookup falls back to when the request's own model has no
-- row. It is a legal value for the model column (lower('*') = '*' and it is not
-- blank), and it can never collide with a real model id.
INSERT INTO model_prices (
    model,
    input_micros_per_1k, output_micros_per_1k,
    cache_read_micros_per_1k, cache_write_micros_per_1k,
    source_note, effective_from
) VALUES (
    '*',
    507000000, 2535000000, 50700000, 633750000,
    'fallback for unknown models, priced at opus rates so an unknown model is never served free',
    '2020-01-01 00:00:00+00'
);
-- Give the Stage 2 counter table an integer money column.
--
-- api_key_usage.credits_used is double precision, inherited from kiro-go's
-- CreditsUsed float64. Float money drifts: summed across millions of requests the
-- error is real, and a balance that disagrees with the sum of its own rows cannot
-- be defended. The column stays for kiro-go compatibility, but cost_micros is the
-- number napkey-core reads.
--
-- Both are now derived values. usage_records is the source of truth, and these
-- counters are a cache so the console and kiro-go's limit check can read one row
-- instead of aggregating a ledger on every request.
ALTER TABLE api_key_usage
    ADD COLUMN cost_micros bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT api_key_usage_cost_micros_check CHECK (cost_micros >= 0);