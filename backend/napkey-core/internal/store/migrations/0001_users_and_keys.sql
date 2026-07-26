-- Stage 2 schema: users, sessions, API keys, audit log.
--
-- Money tables (wallets, ledger_entries, topup_orders, payment_events) belong to
-- Stage 4 and are deliberately absent: DESIGN.md section 9 puts usage accounting
-- before billing, and creating empty money tables now would invite code that
-- writes to them before the hold/settle flow in section 6 exists.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- citext makes email uniqueness case-insensitive at the database level. Doing it
-- in application code instead leaves a race where two concurrent registrations
-- both pass a SELECT check and both insert.
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE users (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email            citext NOT NULL UNIQUE,
    password_hash    text NOT NULL,
    email_verified_at timestamptz,
    status           text NOT NULL DEFAULT 'active',
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_status_check CHECK (status IN ('active', 'suspended')),
    -- Cheap sanity check only. Real validation is deliverability, which only
    -- sending mail proves; this just stops obvious garbage.
    CONSTRAINT users_email_shape_check CHECK (email ~ '^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$')
);

CREATE INDEX users_created_at_idx ON users (created_at DESC);

-- Sessions are stored server-side so a logout, a password change, or an admin
-- suspension can revoke access immediately. A stateless JWT cannot be revoked
-- before it expires, which for a console that manages billable keys is the wrong
-- trade.
CREATE TABLE sessions (
    -- token_hash is SHA-256 of the cookie value. Storing the raw token would mean
    -- a database leak hands over every live session.
    token_hash   bytea PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    user_agent   text,
    ip           inet
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

-- Email verification and password reset tokens share a table because they have
-- identical mechanics: single use, short lived, hashed at rest.
CREATE TABLE email_tokens (
    token_hash bytea PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose    text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    CONSTRAINT email_tokens_purpose_check CHECK (purpose IN ('verify_email', 'reset_password'))
);

CREATE INDEX email_tokens_user_purpose_idx ON email_tokens (user_id, purpose);
CREATE INDEX email_tokens_expires_at_idx ON email_tokens (expires_at);

CREATE TABLE api_keys (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         text NOT NULL DEFAULT '',
    -- key_prefix is the human-readable head ("nk_live_"), kept separate so the
    -- console can show live/test mode without unmasking anything.
    key_prefix   text NOT NULL,
    -- key_hash is SHA-256 of the full key. The cleartext key is shown once at
    -- creation and never stored, which is the core difference from kiro-go's
    -- config.json (DESIGN.md section 5).
    key_hash     bytea NOT NULL UNIQUE,
    last_four    text NOT NULL,
    enabled      boolean NOT NULL DEFAULT true,
    -- Stage 5 turns these into real rate limits; nullable now so a key created
    -- today does not silently gain a limit later.
    rpm_limit    integer,
    tpm_limit    integer,
    -- Manual quota for Stage 2. 0 means unlimited, matching kiro-go semantics so
    -- the two services agree on what an absent limit means.
    token_limit  bigint NOT NULL DEFAULT 0,
    credit_limit double precision NOT NULL DEFAULT 0,
    revoked_at   timestamptz,
    last_used_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    -- Sync state toward the kiro-go data plane. A key is only usable once
    -- kiro-go knows about it, so the failure has to be visible and retryable
    -- rather than swallowed at creation time.
    sync_state       text NOT NULL DEFAULT 'pending',
    sync_error       text,
    synced_at        timestamptz,
    sync_attempts    integer NOT NULL DEFAULT 0,
    next_sync_at     timestamptz NOT NULL DEFAULT now(),
    -- The id kiro-go assigned, needed to update or delete the key there later.
    remote_id        text,
    CONSTRAINT api_keys_sync_state_check CHECK (sync_state IN ('pending', 'synced', 'failed', 'delete_pending')),
    CONSTRAINT api_keys_token_limit_check CHECK (token_limit >= 0),
    CONSTRAINT api_keys_credit_limit_check CHECK (credit_limit >= 0),
    CONSTRAINT api_keys_rpm_limit_check CHECK (rpm_limit IS NULL OR rpm_limit > 0),
    CONSTRAINT api_keys_tpm_limit_check CHECK (tpm_limit IS NULL OR tpm_limit > 0),
    CONSTRAINT api_keys_last_four_check CHECK (char_length(last_four) = 4)
);

CREATE INDEX api_keys_user_id_idx ON api_keys (user_id, created_at DESC);
-- Partial index: the reconciler only ever scans rows that still need work, so
-- the index stays small no matter how many keys are settled.
CREATE INDEX api_keys_sync_pending_idx ON api_keys (next_sync_at)
    WHERE sync_state IN ('pending', 'failed', 'delete_pending');

-- Counters live apart from api_keys because they are written on a completely
-- different cadence: the key row is nearly immutable while usage is appended
-- constantly. Splitting them keeps a usage update from competing with a key edit
-- for the same row lock.
--
-- Stage 3 replaces this with usage_records + model_prices (DESIGN.md section 5).
-- These columns are a stopgap so the console can show something real in Stage 2,
-- not the accounting ledger.
CREATE TABLE api_key_usage (
    api_key_id     uuid PRIMARY KEY REFERENCES api_keys (id) ON DELETE CASCADE,
    tokens_used    bigint NOT NULL DEFAULT 0,
    credits_used   double precision NOT NULL DEFAULT 0,
    requests_count bigint NOT NULL DEFAULT 0,
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT api_key_usage_tokens_check CHECK (tokens_used >= 0),
    CONSTRAINT api_key_usage_credits_check CHECK (credits_used >= 0),
    CONSTRAINT api_key_usage_requests_check CHECK (requests_count >= 0)
);

CREATE TABLE audit_logs (
    id          bigserial PRIMARY KEY,
    actor_type  text NOT NULL,
    actor_id    text,
    action      text NOT NULL,
    target_type text,
    target_id   text,
    metadata    jsonb NOT NULL DEFAULT '{}'::jsonb,
    ip          inet,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT audit_logs_actor_type_check CHECK (actor_type IN ('user', 'admin', 'system'))
);

CREATE INDEX audit_logs_created_at_idx ON audit_logs (created_at DESC);
CREATE INDEX audit_logs_actor_idx ON audit_logs (actor_type, actor_id, created_at DESC);
CREATE INDEX audit_logs_target_idx ON audit_logs (target_type, target_id, created_at DESC);

-- Rate limiting for the auth endpoints, keyed by identifier (email or IP).
-- Kept in Postgres rather than memory because napkey-core will run more than one
-- replica eventually, and a per-process counter is trivially bypassed by
-- spreading attempts across replicas.
CREATE TABLE auth_attempts (
    id         bigserial PRIMARY KEY,
    scope      text NOT NULL,
    identifier text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX auth_attempts_lookup_idx ON auth_attempts (scope, identifier, created_at DESC);