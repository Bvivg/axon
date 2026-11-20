CREATE TABLE push_subscriptions (
    endpoint   text        PRIMARY KEY,
    user_id    uuid        NOT NULL,
    p256dh     text        NOT NULL,
    auth       text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT push_subscriptions_endpoint_bounded CHECK (length(endpoint) BETWEEN 1 AND 2048)
);

CREATE INDEX push_subscriptions_user_id_idx ON push_subscriptions (user_id);
