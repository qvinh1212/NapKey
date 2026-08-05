-- Charge a flat fee per request on the token-priced path.
--
-- 0018 seeded this price book with no per-request fee, and gave a sound reason: on
-- the Kiro path the credit meter already separates an expensive model from a cheap
-- one, so a flat fee would charge for that difference twice.
--
-- That reason does not hold for the 9Router upstream, which is what now serves
-- customer traffic. It speaks the OpenAI protocol, which carries no credit meter, so
-- kiro-go reports credits=0 and settlement prices from token counts alone. The
-- upstream still bills roughly one credit per call regardless of the call's size,
-- and a token-only price cannot see that fixed cost:
--
--   request           tokens    revenue @12k/1M    upstream cost    result
--   chat, one line       350               4 VND         110 VND    loses 106
--   agent step         1,800              22 VND         110 VND    loses  88
--   agent, mid-size    8,800             106 VND         110 VND    loses   4
--   large context     46,200             554 VND         110 VND    earns 444
--
-- Break-even was ~33,000 tokens per request. Coding-agent traffic sits an order of
-- magnitude below that, so the models NapKey sells were priced below cost on exactly
-- the workload they are sold for.
--
-- The fee is 300 VND retail against a 110 VND basis, the measured cost of one
-- upstream call. Across the traffic mix that yields a 72.3% blended margin, which
-- matches the 72.5% the credit path already earns: the point is to make the two
-- upstreams cost a customer the same, not to raise prices.
--
-- Why a flat fee and not higher token rates: rates high enough to cover a 1,800-token
-- request would need to be ~5x, which would make a 200,000-token request absurd. The
-- cost being recovered is per-call, so the charge is per-call. Token rates are left
-- untouched, so a large request still pays mostly for its tokens (the fee is 11% of a
-- 200,000-token charge) while a small one stops being served at a loss.

-- Close the open period for every model this reprices. now() is frozen for the whole
-- transaction, so each period ends at exactly the instant its successor begins: the
-- '[)' range in model_prices_no_overlap requires the boundary to belong to one period
-- only, and an inclusive end would overlap and abort the migration.
--
-- Scoped to the rows 0018 opened. A model priced by some later migration keeps its
-- own period rather than being silently replaced by this one.
UPDATE model_prices
   SET effective_to = now()
 WHERE effective_to IS NULL
   AND model IN ('claude-fable-5', 'claude-opus-5', 'claude-opus-4.7',
                 'claude-opus-4.8', 'claude-sonnet-5', '*');

-- Token rates carry over from 0018 unchanged; only the fee columns are new. Existing
-- usage_records keep the cost they were charged, because cost_micros and
-- request_fee_micros are frozen at insert time (DESIGN.md section 5).
INSERT INTO model_prices (
    model,
    input_micros_per_1k, output_micros_per_1k,
    cache_read_micros_per_1k, cache_write_micros_per_1k,
    upstream_input_micros_per_1k, upstream_output_micros_per_1k,
    upstream_cache_read_micros_per_1k, upstream_cache_write_micros_per_1k,
    request_fee_micros, upstream_request_fee_micros,
    source_note, effective_from
)
SELECT
    model,
    12000000, 12000000, 12000000, 12000000,
    2097000, 2097000, 2097000, 2097000,
    300000000, 110000000,
    'kiro pool via 9router; tokens 12000 vnd/1m over 2097 vnd/1m, '
      || 'plus 300 vnd/request over a measured 110 vnd upstream call',
    now()
FROM (VALUES
    ('claude-fable-5'),
    ('claude-opus-5'),
    ('claude-opus-4.7'),
    ('claude-opus-4.8'),
    ('claude-sonnet-5')
) AS priced(model);

-- The '*' fallback keeps the job it has had since 0003: an unrecognized model id must
-- never be served for free. It carries the fee too, or routing to an unknown id would
-- be a way to avoid it.
INSERT INTO model_prices (
    model,
    input_micros_per_1k, output_micros_per_1k,
    cache_read_micros_per_1k, cache_write_micros_per_1k,
    upstream_input_micros_per_1k, upstream_output_micros_per_1k,
    upstream_cache_read_micros_per_1k, upstream_cache_write_micros_per_1k,
    request_fee_micros, upstream_request_fee_micros,
    source_note, effective_from
) VALUES (
    '*',
    12000000, 12000000, 12000000, 12000000,
    2097000, 2097000, 2097000, 2097000,
    300000000, 110000000,
    'fallback for unknown model ids; same tier and fee as the named models, never free',
    now()
);
