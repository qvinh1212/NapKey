-- Price the nine models the pool serves that 0018 and 0019 never named.
--
-- 0018 seeded five models and a '*' fallback. The catalog is not a fixed list,
-- though: publicModelsFromUpstream keeps every id the upstream publishes under the
-- Viberouter/ pool, so kiro-go has been selling sixteen. Eleven of them had no price
-- row and settled at the '*' rate.
--
-- No customer was mischarged, because '*' carries the same rate. But '*' exists to
-- stop an unrecognized id being served for free, not to price the catalog: while
-- models sit behind it, a future change to the fallback rate silently reprices most
-- of what NapKey sells, and the per-model price history those rows should have
-- simply does not exist.
--
-- Rates carry over from 0019 unchanged. This migration only widens the price book to
-- match what is actually on sale; it is not a repricing, and it opens no new period
-- for a model that already has one.
--
-- MEASURED, NOT ASSUMED
--
-- Each id below was probed against the live upstream on 2026-08-06 (three request
-- sizes, solving billed = overhead + rate * size) rather than inheriting the Claude
-- measurement 0018 recorded. What the probe found:
--
--   model                overhead    tok/para    margin
--   claude-haiku-4-5        2,050        76.7     65.9%
--   claude-haiku-4.5        2,580       141.7     66.2%
--   claude-opus-4-6         2,050        76.7     66.1%
--   claude-opus-4.6         2,623       141.7     66.6%
--   claude-opus-4-7         2,623       141.7     66.4%
--   claude-opus-4-8         2,623       141.7     66.5%
--   claude-sonnet-4-6       2,557        65.0     66.5%
--   claude-sonnet-4.6       2,607       141.7     66.3%
--   claude-sonnet-4.7       2,607       141.7     66.5%
--
-- Every model lands between 65.9% and 66.6%, so the single 2,097 VND/1M basis holds
-- across the catalog and no model needs its own rate. The overhead column is the
-- prompt the upstream prepends before counting: 2,050-2,623 tokens a caller never
-- sent. It does not erode margin, because kiro-go bills the customer from the
-- prompt_tokens the upstream reports, so the overhead is resold at retail rather
-- than absorbed. It is recorded here because it is the reason a one-line chat is
-- billed as a few thousand tokens, which is otherwise inexplicable from the logs.
--
-- The dotted and dashed spellings of one version (claude-opus-4.6 and
-- claude-opus-4-6) are published as separate ids upstream and measured differently:
-- 2,623 tokens of overhead against 2,050. They are distinct backends behind
-- equivalent names, so both are priced rather than one being treated as an alias.
--
-- NOT PRICED HERE
--
-- claude-sonnet-4.8 and gpt-image-2 are in the catalog but were not priced: the
-- first returned no usable response when probed, and the second is an image model
-- that the chat endpoint cannot serve. Pricing a model that cannot answer would
-- record an intent to sell it. They are removed from the served catalog instead, in
-- the same change that adds these rows.

-- Only ids with no open period are inserted. The five models 0019 priced keep the
-- period it opened: re-inserting them would need that period closed first, and
-- closing it would reprice settled traffic for no reason. The NOT EXISTS guard makes
-- this migration describe the gap rather than the whole book, so it stays correct if
-- a model is priced by some other path before it runs.
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
    candidate.model,
    12000000, 12000000, 12000000, 12000000,
    2097000, 2097000, 2097000, 2097000,
    300000000, 110000000,
    'kiro pool via 9router; tokens 12000 vnd/1m over 2097 vnd/1m, '
      || 'plus 300 vnd/request over a measured 110 vnd upstream call; '
      || 'per-model cost measured 2026-08-06',
    now()
FROM (VALUES
    ('claude-haiku-4-5'),
    ('claude-haiku-4.5'),
    ('claude-opus-4-6'),
    ('claude-opus-4.6'),
    ('claude-opus-4-7'),
    ('claude-opus-4-8'),
    ('claude-sonnet-4-6'),
    ('claude-sonnet-4.6'),
    ('claude-sonnet-4.7')
) AS candidate(model)
WHERE NOT EXISTS (
    SELECT 1
      FROM model_prices existing
     WHERE existing.model = candidate.model
       AND existing.effective_to IS NULL
);