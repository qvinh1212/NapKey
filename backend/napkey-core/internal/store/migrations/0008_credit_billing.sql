-- Bill requests from Kiro's measured credits while keeping the VND wallet.
ALTER TABLE usage_records
    ADD COLUMN credits_micros bigint NOT NULL DEFAULT 0,
    ADD COLUMN credit_price_micros_per_credit bigint NOT NULL DEFAULT 0,
    ADD COLUMN upstream_credit_price_micros_per_credit bigint NOT NULL DEFAULT 0;

ALTER TABLE usage_records
    ADD CONSTRAINT usage_records_credits_check CHECK (credits_micros >= 0),
    ADD CONSTRAINT usage_records_credit_price_check CHECK (credit_price_micros_per_credit >= 0),
    ADD CONSTRAINT usage_records_upstream_credit_price_check CHECK (upstream_credit_price_micros_per_credit >= 0);

ALTER TABLE usage_records DROP CONSTRAINT usage_records_pricing_coherent_check;
ALTER TABLE usage_records ADD CONSTRAINT usage_records_pricing_coherent_check CHECK (
    (unpriced AND priced_with IS NULL AND cost_micros = 0 AND credit_price_micros_per_credit = 0)
    OR
    (NOT unpriced AND (credit_price_micros_per_credit > 0 OR priced_with IS NOT NULL OR cost_micros = 0))
);

CREATE INDEX usage_records_credit_time_idx ON usage_records (user_id, created_at DESC)
    WHERE credits_micros > 0;
