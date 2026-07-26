-- Stage 4 schema: prepaid wallets, append-only ledger, holds, and Casso events.

CREATE TABLE wallets (
    user_id         uuid PRIMARY KEY REFERENCES users(id) ON DELETE RESTRICT,
    balance_micros  bigint NOT NULL DEFAULT 0,
    held_micros     bigint NOT NULL DEFAULT 0,
    currency        text NOT NULL DEFAULT 'VND',
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT wallets_balance_check CHECK (balance_micros >= 0),
    CONSTRAINT wallets_held_check CHECK (held_micros >= 0 AND held_micros <= balance_micros),
    CONSTRAINT wallets_currency_check CHECK (currency = 'VND')
);

CREATE TABLE ledger_entries (
    id                    bigserial PRIMARY KEY,
    user_id               uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    kind                  text NOT NULL,
    amount_micros         bigint NOT NULL,
    balance_after_micros  bigint NOT NULL,
    held_after_micros     bigint NOT NULL,
    ref_type              text NOT NULL,
    ref_id                text NOT NULL,
    idempotency_key       text NOT NULL UNIQUE,
    metadata              jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at            timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ledger_kind_check CHECK (kind IN ('topup','usage','refund','adjustment','hold','hold_release')),
    CONSTRAINT ledger_balance_check CHECK (balance_after_micros >= 0),
    CONSTRAINT ledger_held_check CHECK (held_after_micros >= 0 AND held_after_micros <= balance_after_micros)
);
CREATE INDEX ledger_entries_user_time_idx ON ledger_entries(user_id, created_at DESC, id DESC);

CREATE TABLE wallet_holds (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    api_key_id      uuid REFERENCES api_keys(id) ON DELETE SET NULL,
    request_id      text NOT NULL UNIQUE,
    amount_micros   bigint NOT NULL,
    status          text NOT NULL DEFAULT 'open',
    expires_at      timestamptz NOT NULL,
    settled_at      timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT wallet_holds_amount_check CHECK (amount_micros > 0),
    CONSTRAINT wallet_holds_status_check CHECK (status IN ('open','settled','released','expired'))
);
CREATE INDEX wallet_holds_open_expiry_idx ON wallet_holds(expires_at) WHERE status = 'open';

CREATE TABLE topup_orders (
    id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                  uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    memo_code                text NOT NULL UNIQUE,
    expected_amount_micros   bigint NOT NULL,
    received_amount_micros   bigint NOT NULL DEFAULT 0,
    provider                 text NOT NULL DEFAULT 'casso',
    bank_account_number      text NOT NULL,
    status                   text NOT NULL DEFAULT 'pending',
    expires_at               timestamptz NOT NULL,
    paid_at                  timestamptz,
    created_at               timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT topup_expected_check CHECK (expected_amount_micros >= 20000000000),
    CONSTRAINT topup_received_check CHECK (received_amount_micros >= 0),
    CONSTRAINT topup_status_check CHECK (status IN ('pending','paid','underpaid','expired','cancelled'))
);
CREATE INDEX topup_orders_user_time_idx ON topup_orders(user_id, created_at DESC);

CREATE TABLE payment_events (
    id                  bigserial PRIMARY KEY,
    provider            text NOT NULL DEFAULT 'casso',
    provider_tx_id      text NOT NULL,
    bank_reference      text,
    signature_verified  boolean NOT NULL,
    payload             jsonb NOT NULL,
    matched_order_id    uuid REFERENCES topup_orders(id) ON DELETE SET NULL,
    status              text NOT NULL,
    received_at         timestamptz NOT NULL DEFAULT now(),
    processed_at        timestamptz,
    error_message       text,
    CONSTRAINT payment_events_status_check CHECK (status IN ('received','processing','credited','duplicate','unmatched','rejected')),
    UNIQUE(provider, provider_tx_id)
);
CREATE INDEX payment_events_work_idx ON payment_events(received_at) WHERE status = 'received';
