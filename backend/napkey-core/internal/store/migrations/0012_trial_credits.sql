-- One-time promotional credit grants. IP fingerprints are keyed hashes produced
-- by the application; raw client addresses never enter this table.
CREATE TABLE trial_grants (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL UNIQUE REFERENCES users(id) ON DELETE RESTRICT,
    ip_hash bytea NOT NULL UNIQUE,
    amount_micros     bigint NOT NULL,
    remaining_micros  bigint NOT NULL,
    expires_at        timestamptz NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT trial_grants_amount_check CHECK (amount_micros > 0),
    CONSTRAINT trial_grants_remaining_check CHECK (remaining_micros >= 0 AND remaining_micros <= amount_micros)
);
CREATE INDEX trial_grants_expiry_idx ON trial_grants(expires_at) WHERE remaining_micros > 0;

ALTER TABLE wallets
    ADD COLUMN promotional_micros bigint NOT NULL DEFAULT 0,
    ADD COLUMN promotional_expires_at timestamptz,
    ADD CONSTRAINT wallets_promotional_check CHECK (
        promotional_micros >= 0 AND promotional_micros <= balance_micros
    );

ALTER TABLE ledger_entries DROP CONSTRAINT ledger_kind_check;
ALTER TABLE ledger_entries ADD CONSTRAINT ledger_kind_check
    CHECK (kind IN ('topup','trial','usage','refund','adjustment','hold','hold_release'));
