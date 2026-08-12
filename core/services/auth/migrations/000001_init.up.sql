CREATE TABLE users (
    id             uuid        PRIMARY KEY,
    email          text        NOT NULL,
    email_verified boolean     NOT NULL DEFAULT false,
    display_name   text        NOT NULL DEFAULT '',
    avatar_url     text        NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT users_email_present    CHECK (length(email) > 0),
    CONSTRAINT users_email_normalized CHECK (email = lower(email))
);

CREATE UNIQUE INDEX users_email_key ON users (email);

CREATE TABLE credentials (
    user_id       uuid        PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,

    password_hash text        NOT NULL,
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE refresh_tokens (
    id         uuid        PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    family_id  uuid        NOT NULL,
    token_hash text        NOT NULL,
    issued_at  timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,

    used_at    timestamptz,
    revoked_at timestamptz
);

CREATE UNIQUE INDEX refresh_tokens_token_hash_key ON refresh_tokens (token_hash);

CREATE INDEX refresh_tokens_family_id_idx ON refresh_tokens (family_id);

CREATE INDEX refresh_tokens_user_id_idx ON refresh_tokens (user_id);

CREATE INDEX refresh_tokens_expiry_sweep_idx
    ON refresh_tokens (expires_at)
    WHERE used_at IS NULL AND revoked_at IS NULL;

CREATE TABLE oauth_accounts (
    provider         text        NOT NULL,
    provider_user_id text        NOT NULL,
    user_id          uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    email            text        NOT NULL DEFAULT '',
    linked_at        timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (provider, provider_user_id)
);

CREATE INDEX oauth_accounts_user_id_idx ON oauth_accounts (user_id);
