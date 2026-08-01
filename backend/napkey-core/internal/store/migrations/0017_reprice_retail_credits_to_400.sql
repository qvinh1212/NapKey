-- Reprice future credit purchases to 400 VND while preserving existing quantities.
LOCK TABLE wallets, wallet_holds, trial_grants, first_topup_bonus_grants, topup_orders IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM wallets WHERE balance_micros::numeric * 400 / 75 > 9223372036854775807 OR held_micros::numeric * 400 / 75 > 9223372036854775807 OR promotional_micros::numeric * 400 / 75 > 9223372036854775807)
     OR EXISTS (SELECT 1 FROM wallet_holds WHERE amount_micros::numeric * 400 / 75 > 9223372036854775807)
     OR EXISTS (SELECT 1 FROM trial_grants WHERE amount_micros::numeric * 400 / 75 > 9223372036854775807 OR remaining_micros::numeric * 400 / 75 > 9223372036854775807)
     OR EXISTS (SELECT 1 FROM first_topup_bonus_grants WHERE amount_micros::numeric * 400 / 75 > 9223372036854775807 OR remaining_micros::numeric * 400 / 75 > 9223372036854775807) THEN
    RAISE EXCEPTION 'retail credit reprice would overflow bigint';
  END IF;
END $$;

ALTER TABLE topup_orders ALTER COLUMN retail_vnd_per_credit SET DEFAULT 400;
UPDATE topup_orders SET retail_vnd_per_credit = 400 WHERE status <> 'paid' AND status <> 'underpaid' AND received_amount_micros = 0;

UPDATE wallet_holds SET amount_micros = floor(amount_micros::numeric * 400 / 75)::bigint WHERE status = 'open';
UPDATE trial_grants SET amount_micros = floor(amount_micros::numeric * 400 / 75)::bigint, remaining_micros = floor(remaining_micros::numeric * 400 / 75)::bigint;
UPDATE first_topup_bonus_grants SET amount_micros = floor(amount_micros::numeric * 400 / 75)::bigint, remaining_micros = floor(remaining_micros::numeric * 400 / 75)::bigint;

WITH before_reprice AS (SELECT user_id, balance_micros, promotional_micros FROM wallets FOR UPDATE), repriced AS (
  UPDATE wallets w SET balance_micros=floor(b.balance_micros::numeric*400/75)::bigint,
    held_micros=COALESCE((SELECT SUM(amount_micros) FROM wallet_holds h WHERE h.user_id=w.user_id AND h.status='open'),0),
    promotional_micros=floor(b.promotional_micros::numeric*400/75)::bigint, updated_at=now()
  FROM before_reprice b WHERE w.user_id=b.user_id
  RETURNING w.user_id,w.balance_micros,w.held_micros,w.balance_micros-b.balance_micros AS adjustment_micros)
INSERT INTO ledger_entries(user_id,kind,amount_micros,balance_after_micros,held_after_micros,ref_type,ref_id,idempotency_key,metadata)
SELECT user_id,'adjustment',adjustment_micros,balance_micros,held_micros,'pricing_change','75-to-400','wallet-credit-reprice-75-to-400:'||user_id::text,
  '{"oldVndPerCredit":75,"newVndPerCredit":400,"reason":"preserve_existing_credits"}'::jsonb FROM repriced WHERE adjustment_micros <> 0;
