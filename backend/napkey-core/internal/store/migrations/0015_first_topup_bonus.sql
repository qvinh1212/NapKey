CREATE TABLE first_topup_bonus_grants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE UNIQUE,
    topup_order_id uuid NOT NULL REFERENCES topup_orders(id) ON DELETE RESTRICT UNIQUE,
    amount_micros bigint NOT NULL CHECK (amount_micros > 0),
    remaining_micros bigint NOT NULL CHECK (remaining_micros >= 0 AND remaining_micros <= amount_micros),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX first_topup_bonus_grants_expiry_idx
    ON first_topup_bonus_grants(expires_at) WHERE remaining_micros > 0;

-- Earlier settlement code tracked trial remaining too aggressively. Before the
-- new promotion exists, the wallet promotional amount is entirely trial credit.
UPDATE trial_grants t SET remaining_micros = w.promotional_micros
FROM wallets w WHERE w.user_id = t.user_id;

ALTER TABLE ledger_entries DROP CONSTRAINT ledger_kind_check;
ALTER TABLE ledger_entries ADD CONSTRAINT ledger_kind_check
    CHECK (kind IN ('topup','trial','promotion','usage','refund','adjustment','hold','hold_release'));
