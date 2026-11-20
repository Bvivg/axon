ALTER TABLE game.sessions ADD COLUMN partition smallint;

CREATE INDEX game_sessions_open_partition ON game.sessions (partition) WHERE state IS NULL OR state->>'status' = 'in_progress';

ALTER TABLE game.match_queue
    DROP CONSTRAINT match_queue_session_id_fkey,
    ADD COLUMN started boolean NOT NULL DEFAULT true;

CREATE TABLE game.owners (
    partition smallint PRIMARY KEY CHECK (partition >= 0),
    owner_id text NOT NULL,
    address text NOT NULL,
    generation bigint NOT NULL CHECK (generation > 0),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE game.projector_offsets (
    partition smallint PRIMARY KEY CHECK (partition >= 0),
    next_offset bigint NOT NULL CHECK (next_offset >= 0),
    claim_generation bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
