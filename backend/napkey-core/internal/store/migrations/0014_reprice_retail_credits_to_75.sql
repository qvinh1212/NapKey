-- Raise the retail rate from 60 to 75 VND without reducing credit quantities
-- already owned by customers. Settled usage and historical top-up orders keep
-- their original rate; new and unsettled top-up orders use the new rate.

LOCK TABLE wallets, wallet_holds, trial_grants, topup_orders
    IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM wallets
        WHERE balance_micros::numeric + floor(balance_micros::numeric / 4) > 9223372036854775807
           OR held_micros::numeric + floor(held_micros::numeric / 4) > 9223372036854775807
           OR promotional_micros::numeric + floor(promotional_micros::numeric / 4) > 9223372036854775807
    ) OR EXISTS (
        SELECT 1 FROM wallet_holds
        WHERE status = 'open'
          AND amount_micros::numeric + floor(amount_micros::numeric / 4) > 9223372036854775807
    ) OR EXISTS (
        SELECT user_id FROM wallet_holds
        WHERE status = 'open'
        GROUP BY user_id
        HAVING SUM(amount_micros::numeric + floor(amount_micros::numeric / 4)) > 9223372036854775807
    ) OR EXISTS (
        SELECT 1 FROM trial_grants
        WHERE amount_micros::numeric + floor(amount_micros::numeric / 4) > 9223372036854775807
           OR remaining_micros::numeric + floor(remaining_micros::numeric / 4) > 9223372036854775807
    ) THEN
        RAISE EXCEPTION 'retail credit reprice would overflow bigint';
    END IF;
END $$;

ALTER TABLE topup_orders
    ALTER COLUMN retail_vnd_per_credit SET DEFAULT 75;

-- Quotes with no received payment move to the new rate. Paid and underpaid
-- orders retain the exact rate used for their historical credit grants.
UPDATE topup_orders
SET retail_vnd_per_credit = 75
WHERE status <> 'paid'
  AND status <> 'underpaid'
  AND received_amount_micros = 0;

UPDATE wallet_holds
SET amount_micros = amount_micros + amount_micros / 4
WHERE status = 'open';

UPDATE trial_grants
SET amount_micros = amount_micros + amount_micros / 4,
    remaining_micros = remaining_micros + remaining_micros / 4
WHERE remaining_micros > 0;

WITH before_reprice AS (
    SELECT user_id, balance_micros, promotional_micros
    FROM wallets
    WHERE balance_micros > 0
    FOR UPDATE
), repriced AS (
    UPDATE wallets AS wallet
    SET balance_micros = before_reprice.balance_micros + before_reprice.balance_micros / 4,
        held_micros = COALESCE((
            SELECT SUM(hold.amount_micros)
            FROM wallet_holds AS hold
            WHERE hold.user_id = wallet.user_id AND hold.status = 'open'
        ), 0),
        promotional_micros = before_reprice.promotional_micros + before_reprice.promotional_micros / 4,
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
       'pricing_change', '60-to-75',
       'wallet-credit-reprice-60-to-75:' || user_id::text,
       '{"oldVndPerCredit":60,"newVndPerCredit":75,"reason":"preserve_existing_credits"}'::jsonb
FROM repriced;
