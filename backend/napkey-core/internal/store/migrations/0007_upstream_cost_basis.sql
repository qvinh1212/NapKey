ALTER TABLE model_prices
    ADD COLUMN upstream_input_micros_per_1k bigint NOT NULL DEFAULT 0,
    ADD COLUMN upstream_output_micros_per_1k bigint NOT NULL DEFAULT 0,
    ADD COLUMN upstream_cache_read_micros_per_1k bigint NOT NULL DEFAULT 0,
    ADD COLUMN upstream_cache_write_micros_per_1k bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT model_prices_upstream_input_check CHECK (upstream_input_micros_per_1k >= 0),
    ADD CONSTRAINT model_prices_upstream_output_check CHECK (upstream_output_micros_per_1k >= 0),
    ADD CONSTRAINT model_prices_upstream_cache_read_check CHECK (upstream_cache_read_micros_per_1k >= 0),
    ADD CONSTRAINT model_prices_upstream_cache_write_check CHECK (upstream_cache_write_micros_per_1k >= 0);

-- Existing rows were seeded at retail x1.30. Persist the derived basis once so
-- operations queries never infer margin at read time again.
UPDATE model_prices SET
    upstream_input_micros_per_1k = input_micros_per_1k * 10 / 13,
    upstream_output_micros_per_1k = output_micros_per_1k * 10 / 13,
    upstream_cache_read_micros_per_1k = cache_read_micros_per_1k * 10 / 13,
    upstream_cache_write_micros_per_1k = cache_write_micros_per_1k * 10 / 13;

ALTER TABLE usage_records
    ADD COLUMN upstream_cost_micros bigint NOT NULL DEFAULT 0,
    ADD COLUMN upstream_cost_estimated boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT usage_records_upstream_cost_check CHECK (upstream_cost_micros >= 0);

UPDATE usage_records
SET upstream_cost_micros = cost_micros * 10 / 13,
    upstream_cost_estimated = true
WHERE cost_micros > 0;
