-- Reprice the catalog to a tiered 30% margin table.
--
-- WHY THIS REPRICING
--
-- Until now every model carried one flat rate (12,000 VND/1M tokens) regardless of
-- how expensive it actually is upstream. That worked while the pool was small and
-- homogeneous, but the eight models NapKey now sells span a 6x cost range:
-- claude-sonnet-5 and gpt-5.6-luna consume roughly half the upstream credits per
-- token that claude-opus-5 does, and claude-fable-5 consumes three times as much.
-- A single rate either overcharges the cheap models or undercharges the expensive
-- ones; neither outcome survives contact with customers who can compare what they
-- pay against what the upstream actually costs.
--
-- The new table prices each model at its own upstream cost plus a uniform 30%
-- token margin. The per-request fee stays at 300 VND retail / 110 VND upstream,
-- unchanged from 0019: it recovers a fixed per-call cost that token rates cannot
-- see, and raising it here would be double-counting.
--
-- THE NEW TABLE
--
--   model               ratio    vnd/1m    micros/1k    upstream vnd/1m
--   claude-sonnet-5      0.5x     1,500    1,500,000          1,049
--   gpt-5.6-luna         0.5x     1,500    1,500,000          1,049
--   claude-opus-4.7      1.0x     3,000    3,000,000          2,097
--   claude-opus-4.8      1.0x     3,000    3,000,000          2,097
--   gpt-5.6-terra        1.2x     3,600    3,600,000          2,516
--   claude-opus-5        1.5x     4,500    4,500,000          3,146
--   gpt-5.6-sol          2.0x     6,000    6,000,000          4,194
--   claude-fable-5       3.3x    10,000   10,000,000          6,920
--   *                    3.3x    10,000   10,000,000          6,920
--
-- Ratios come from the provider's credit-per-token consumption observed on the
-- live key (1000 credits = 50,000 VND). Upstream VND/1M = 2,097 x ratio, rounded.
-- Retail VND/1M = upstream / 0.7, rounded to the nearest hundred. Every named
-- model lands at ~30.1% token margin; the '*' fallback matches fable-5 so an
-- unrecognized id is never cheaper than the most expensive named one.
--
-- Cache reads and writes carry the same rate as fresh input. The upstream bills
-- a flat per-token cost with no cache tier on this key, so a discount here would
-- give away margin that is not actually being saved. This matches 0018's reasoning
-- and remains true.
--
-- MODELS REMOVED FROM SALE
--
-- Seven ids priced in 0020 are no longer offered: claude-haiku-4.5,
-- claude-haiku-4-5, claude-sonnet-4.6, claude-sonnet-4-6, claude-sonnet-4.7,
-- claude-opus-4.6, claude-opus-4-6. They are closed here alongside everything
-- else and excluded from the new rows. kiro-go's nineRouterUnservable() is
-- updated in the same change to drop them from /v1/models, so no client can
-- request a model that has no open price period.
--
-- GPT-5.6-LUNA / TERRA / SOL ARE NEW
--
-- These three ids have never had a price row. They are inserted fresh below with
-- no NOT EXISTS guard needed (there is nothing to guard against). If some other
-- path prices them before this migration runs, the INSERT will still succeed
-- because their periods were never opened; the only risk would be a duplicate
-- open period, which the no-overlap constraint catches and aborts.
--
-- CLAUDE-FABLE-5 IS WITHDRAWN FROM SALE
--
-- claude-fable-5 keeps its price row here: it anchors the '*' fallback at the top
-- tier, so an unrecognized id settles at the same rate as the most expensive named
-- model. But it is not offered for sale -- fifteen probes on 2026-08-09 returned
-- Cloudflare 524 or closed streams without usage, never a completion, while
-- claude-sonnet-5 answered in nine seconds on the same key. The pool key is not
-- entitled to it upstream. kiro-go's nineRouterUnservable() drops it from /v1/models
-- and refuses requests that name it before they reach the wallet hold. The price
-- row stays so settled traffic is not silently repriced when we stop selling it.

-- Close every open period. Scoped to all rows so the new table fully replaces
-- the old one: leaving any prior period open would mean two active rates for the
-- same model, which the no-overlap constraint forbids.
UPDATE model_prices SET effective_to = now() WHERE effective_to IS NULL;

INSERT INTO model_prices (
    model,
    input_micros_per_1k, output_micros_per_1k,
    cache_read_micros_per_1k, cache_write_micros_per_1k,
    upstream_input_micros_per_1k, upstream_output_micros_per_1k,
    upstream_cache_read_micros_per_1k, upstream_cache_write_micros_per_1k,
    request_fee_micros, upstream_request_fee_micros,
    source_note, effective_from
) VALUES
    ('claude-sonnet-5',
     1500000, 1500000, 1500000, 1500000,
     1049000, 1049000, 1049000, 1049000,
     300000000, 110000000,
     'tiered 30pct margin; ratio 0.5x; 1500 vnd/1m over 1049 vnd/1m upstream',
     now()),
    ('gpt-5.6-luna',
     1500000, 1500000, 1500000, 1500000,
     1049000, 1049000, 1049000, 1049000,
     300000000, 110000000,
     'tiered 30pct margin; ratio 0.5x; 1500 vnd/1m over 1049 vnd/1m upstream',
     now()),
    ('claude-opus-4.7',
     3000000, 3000000, 3000000, 3000000,
     2097000, 2097000, 2097000, 2097000,
     300000000, 110000000,
     'tiered 30pct margin; ratio 1.0x; 3000 vnd/1m over 2097 vnd/1m upstream',
     now()),
    ('claude-opus-4.8',
     3000000, 3000000, 3000000, 3000000,
     2097000, 2097000, 2097000, 2097000,
     300000000, 110000000,
     'tiered 30pct margin; ratio 1.0x; 3000 vnd/1m over 2097 vnd/1m upstream',
     now()),
    ('gpt-5.6-terra',
     3600000, 3600000, 3600000, 3600000,
     2516000, 2516000, 2516000, 2516000,
     300000000, 110000000,
     'tiered 30pct margin; ratio 1.2x; 3600 vnd/1m over 2516 vnd/1m upstream',
     now()),
    ('claude-opus-5',
     4500000, 4500000, 4500000, 4500000,
     3146000, 3146000, 3146000, 3146000,
     300000000, 110000000,
     'tiered 30pct margin; ratio 1.5x; 4500 vnd/1m over 3146 vnd/1m upstream',
     now()),
    ('gpt-5.6-sol',
     6000000, 6000000, 6000000, 6000000,
     4194000, 4194000, 4194000, 4194000,
     300000000, 110000000,
     'tiered 30pct margin; ratio 2.0x; 6000 vnd/1m over 4194 vnd/1m upstream',
     now()),
    ('claude-fable-5',
     10000000, 10000000, 10000000, 10000000,
     6920000, 6920000, 6920000, 6920000,
     300000000, 110000000,
     'tiered 30pct margin; ratio 3.3x; 10000 vnd/1m over 6920 vnd/1m upstream',
     now());

-- The '*' fallback matches fable-5 rather than the cheapest tier: an unrecognized
-- id must never be cheaper than the most expensive named model, or routing to an
-- unknown id becomes a way to get discounted access. Same job it has had since 0003.
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
    10000000, 10000000, 10000000, 10000000,
    6920000, 6920000, 6920000, 6920000,
    300000000, 110000000,
    'fallback for unknown model ids; matches fable-5 tier, never cheaper than named models',
    now()
);
