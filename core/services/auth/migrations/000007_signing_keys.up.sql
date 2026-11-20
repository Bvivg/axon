CREATE TABLE signing_keys (
    kid          text PRIMARY KEY,
    private_key  bytea NOT NULL,
    activates_at timestamptz NOT NULL,
    retires_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX signing_keys_retires_at ON signing_keys (retires_at);
