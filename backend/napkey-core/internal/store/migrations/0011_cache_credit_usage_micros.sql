-- Keep the overview meter O(number of keys), while usage_records remains the source of truth.
ALTER TABLE api_key_usage
    ADD COLUMN credits_micros bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT api_key_usage_credits_micros_check CHECK (credits_micros >= 0);

UPDATE api_key_usage u
SET credits_micros = ledger.credits_micros
FROM (
    SELECT api_key_id, coalesce(sum(credits_micros), 0)::bigint AS credits_micros
    FROM usage_records
    WHERE api_key_id IS NOT NULL
    GROUP BY api_key_id
) ledger
WHERE u.api_key_id = ledger.api_key_id;
