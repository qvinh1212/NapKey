-- Price the five models NapKey sells, and make settlement work without a credit meter.
--
-- Two things change here.
--
-- First, the price book gains rows for the models actually on sale. Until now
-- model_prices only held the Anthropic metered list prices seeded in 0003, none of
-- which match a model NapKey serves, so every lookup fell through to the '*'
-- sentinel priced at metered Opus rates. That is roughly forty times the price the
-- credit model has been charging, which made the wallet hold quote a number far
-- above the real cost of the request.
--
-- Second, those metered rows are closed. They describe pay-as-you-go Anthropic API
-- pricing, which is not the cost basis of this business: the upstream is a Kiro
-- pool billed per credit, not per token at list price.
--
-- Token rates are 12,000 VND per 1M tokens retail against a measured 2,097 VND per
-- 1M cost basis, matching the sibling deployment that shares this upstream. Both
-- figures are per 1,000 tokens in micro-VND here, so 12,000 VND/1M = 12,000,000
-- micros/1k.
--
-- No per-request fee. The credit meter already separates expensive models from
-- cheap ones, because an Opus request consumes more upstream credits than a Sonnet
-- one; layering a flat fee on top would charge for that difference twice.
--
-- Superseded by 0019 for the token-priced path. That reasoning holds only while a
-- credit meter is doing the separating, and the 9Router upstream reports no meter, so
-- token-only pricing there settles small requests below cost. Credit-metered traffic
-- is still fee-free for exactly the reason given above.

-- The fee is part of a price period, so it is versioned with one: changing it opens
-- a new period through the same path as a token rate, and no settled request is
-- repriced. Every row seeded below leaves it at zero per the decision above; the
-- column exists so a later fee is a price change rather than a schema change.
ALTER TABLE model_prices
    ADD COLUMN request_fee_micros bigint NOT NULL DEFAULT 0,
    ADD COLUMN upstream_request_fee_micros bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT model_prices_request_fee_check CHECK (request_fee_micros >= 0),
    ADD CONSTRAINT model_prices_upstream_request_fee_check CHECK (upstream_request_fee_micros >= 0);

-- usage_records keeps the fee each request was actually charged, frozen like every
-- other cost component, so a charge stays explainable after the price moves: without
-- it nothing records which part of a total was flat and which was per-token. Adding
-- the column later would mean backfilling rows whose charge is already settled, and
-- zero is the honest value for every row written before a fee exists.
ALTER TABLE usage_records
    ADD COLUMN request_fee_micros bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT usage_records_request_fee_check CHECK (request_fee_micros >= 0);

-- Close every open period. now() is frozen for the whole transaction, so these end
-- at exactly the instant the rows below begin: the '[)' range in
-- model_prices_no_overlap requires the boundary to belong to one period only, and
-- an inclusive end would make the two overlap and abort the migration.
UPDATE model_prices SET effective_to = now() WHERE effective_to IS NULL;

-- Cache reads and writes carry the same rate as fresh input. The upstream bills a
-- flat per-token cost with no cache tier, so a discount here would give away margin
-- that is not actually being saved.
INSERT INTO model_prices (
    model,
    input_micros_per_1k, output_micros_per_1k,
    cache_read_micros_per_1k, cache_write_micros_per_1k,
    upstream_input_micros_per_1k, upstream_output_micros_per_1k,
    upstream_cache_read_micros_per_1k, upstream_cache_write_micros_per_1k,
    source_note, effective_from
) VALUES
    ('claude-fable-5',
     12000000, 12000000, 12000000, 12000000,
     2097000, 2097000, 2097000, 2097000,
     'kiro pool; retail 12000 vnd/1m tokens, cost 2097 vnd/1m measured 2026-08-03',
     now()),
    ('claude-opus-5',
     12000000, 12000000, 12000000, 12000000,
     2097000, 2097000, 2097000, 2097000,
     'kiro pool; retail 12000 vnd/1m tokens, cost 2097 vnd/1m measured 2026-08-03',
     now()),
    ('claude-opus-4.7',
     12000000, 12000000, 12000000, 12000000,
     2097000, 2097000, 2097000, 2097000,
     'kiro pool; retail 12000 vnd/1m tokens, cost 2097 vnd/1m measured 2026-08-03',
     now()),
    ('claude-opus-4.8',
     12000000, 12000000, 12000000, 12000000,
     2097000, 2097000, 2097000, 2097000,
     'kiro pool; retail 12000 vnd/1m tokens, cost 2097 vnd/1m measured 2026-08-03',
     now()),
    ('claude-sonnet-5',
     12000000, 12000000, 12000000, 12000000,
     2097000, 2097000, 2097000, 2097000,
     'kiro pool; retail 12000 vnd/1m tokens, cost 2097 vnd/1m measured 2026-08-03',
     now());

-- The '*' fallback keeps its job from 0003: an unrecognized model id must never be
-- served for free, so it is priced at the tier NapKey sells. What changes is the
-- reference point, from Anthropic metered rates to this price book. Same rate as
-- the named models, because they all share one upstream and one cost basis.
INSERT INTO model_prices (
    model,
    input_micros_per_1k, output_micros_per_1k,
    cache_read_micros_per_1k, cache_write_micros_per_1k,
    upstream_input_micros_per_1k, upstream_output_micros_per_1k,
    upstream_cache_read_micros_per_1k, upstream_cache_write_micros_per_1k,
    source_note, effective_from
) VALUES (
    '*',
    12000000, 12000000, 12000000, 12000000,
    2097000, 2097000, 2097000, 2097000,
    'fallback for unknown model ids; same tier as the named models, never free',
    now()
);