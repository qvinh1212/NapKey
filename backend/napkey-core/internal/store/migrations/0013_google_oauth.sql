CREATE TABLE oauth_identities (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider         text NOT NULL,
    provider_subject text NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE(provider, provider_subject),
    UNIQUE(user_id, provider),
    CONSTRAINT oauth_identities_provider_check CHECK (provider IN ('google'))
);

CREATE INDEX oauth_identities_user_id_idx ON oauth_identities(user_id);
