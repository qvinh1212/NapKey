-- Raise the retail rate from 45 to 60 VND while preserving every existing
-- customer's remaining credit quantity. Wallets store VND, so their balance and
-- any open holds need a one-time 4/3 adjustment before the new rate is active.

ALTER TABLE topup_orders
    ADD COLUMN retail_vnd_per_credit bigint NOT NULL DEFAULT 45,
    ADD CONSTRAINT topup_retail_credit_rate_check CHECK (retail_vnd_per_credit > 0);

ALTER TABLE topup_orders
    ALTER COLUMN retail_vnd_per_credit SET DEFAULT 60;

UPDATE wallet_holds
SET amount_micros = amount_micros + amount_micros / 3
WHERE status = 'open';

WITH before_reprice AS (
    SELECT user_id, balance_micros, held_micros
    FROM wallets
    WHERE balance_micros > 0
    FOR UPDATE
), repriced AS (
    UPDATE wallets AS wallet
    SET balance_micros = before_reprice.balance_micros + before_reprice.balance_micros / 3,
        held_micros = before_reprice.held_micros + before_reprice.held_micros / 3,
        updated_at = now()
    FROM before_reprice
    WHERE wallet.user_id = before_reprice.user_id
    RETURNING wallet.user_id,
              wallet.balance_micros,
              wallet.held_micros,
              wallet.balance_micros - before_reprice.balance_micros AS adjustment_micros
)
INSERT INTO ledger_entries (
    user_id, kind, amount_micros, balance_after_micros, held_after_micros,
    ref_type, ref_id, idempotency_key, metadata
)
SELECT user_id, 'adjustment', adjustment_micros, balance_micros, held_micros,
       'pricing_change', '45-to-60',
       'wallet-credit-reprice-45-to-60:' || user_id::text,
       '{"oldVndPerCredit":45,"newVndPerCredit":60,"reason":"preserve_existing_credits"}'::jsonb
FROM repriced;
