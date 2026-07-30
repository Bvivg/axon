-- Auth schema. Applied as auth_service, whose search_path is pinned to `auth`,
-- so nothing here needs to qualify a name.

-- ---------------------------------------------------------------------------
-- users
-- ---------------------------------------------------------------------------
-- Emails are stored already normalized (trimmed and lowercased) rather than in
-- a case-insensitive column type. That keeps the citext extension — which needs
-- superuser rights to install — out of the picture, and the CHECK below makes
-- the invariant the database's business rather than the application's promise.
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

-- ---------------------------------------------------------------------------
-- credentials
-- ---------------------------------------------------------------------------
-- Separate from users because an account created through a provider has no
-- password at all. As a nullable column on users that state would be easy to
-- overlook; as a missing row it is impossible to read past by accident.
CREATE TABLE credentials (
    user_id       uuid        PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    -- Full PHC string: algorithm, parameters and salt travel with the hash, so
    -- the cost can be raised later without invalidating existing passwords.
    password_hash text        NOT NULL,
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- refresh_tokens
-- ---------------------------------------------------------------------------
-- Only the hash is stored. A leaked dump has to be useless for signing in.
--
-- family_id ties together every token descended from one sign-in. Refreshing
-- marks the presented token used and issues a successor in the same family;
-- a token coming back after it was used means theft or replay, and the whole
-- family is revoked.
CREATE TABLE refresh_tokens (
    id         uuid        PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    family_id  uuid        NOT NULL,
    token_hash text        NOT NULL,
    issued_at  timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    -- NULL means unspent / not revoked. Nullable on purpose: a zero timestamp
    -- would be a real instant and would need constant care not to be read as one.
    used_at    timestamptz,
    revoked_at timestamptz
);

-- Lookup on refresh is by hash, and the uniqueness is what makes a collision
-- between two live tokens impossible rather than merely unlikely.
CREATE UNIQUE INDEX refresh_tokens_token_hash_key ON refresh_tokens (token_hash);

-- Revoking a family on reuse touches every row sharing family_id.
CREATE INDEX refresh_tokens_family_id_idx ON refresh_tokens (family_id);

-- "Sign out everywhere" and account deletion both walk a user's tokens.
CREATE INDEX refresh_tokens_user_id_idx ON refresh_tokens (user_id);

-- Partial index for the sweep that deletes dead tokens: it only ever looks at
-- rows that are still live, so the index stays small as history accumulates.
CREATE INDEX refresh_tokens_expiry_sweep_idx
    ON refresh_tokens (expires_at)
    WHERE used_at IS NULL AND revoked_at IS NULL;

-- ---------------------------------------------------------------------------
-- oauth_accounts
-- ---------------------------------------------------------------------------
-- The primary key is (provider, provider_user_id): two people cannot claim one
-- provider identity, and the same person signing in twice resolves to the same
-- account.
--
-- provider_user_id is the provider's stable subject identifier, never the email.
-- People change their email at the provider, and matching on it would hand an
-- account to whoever claims the address next.
CREATE TABLE oauth_accounts (
    provider         text        NOT NULL,
    provider_user_id text        NOT NULL,
    user_id          uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    email            text        NOT NULL DEFAULT '',
    linked_at        timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (provider, provider_user_id)
);

CREATE INDEX oauth_accounts_user_id_idx ON oauth_accounts (user_id);
