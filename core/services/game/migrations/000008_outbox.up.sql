CREATE TABLE game.outbox (
    id uuid PRIMARY KEY,
    aggregatetype text NOT NULL,
    aggregateid text NOT NULL,
    type text NOT NULL,
    payload bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX game_outbox_created_at ON game.outbox (created_at);
