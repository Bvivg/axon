CREATE TABLE game.sessions (
    id uuid PRIMARY KEY,
    game text NOT NULL,
    creator_id uuid NOT NULL,
    opponent_id uuid CHECK (opponent_id <> creator_id),
    request_id uuid NOT NULL,
    state jsonb,
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (creator_id, request_id),
    CHECK ((opponent_id IS NULL) = (state IS NULL))
);

CREATE INDEX game_sessions_creator ON game.sessions (creator_id, updated_at DESC);
CREATE INDEX game_sessions_opponent ON game.sessions (opponent_id, updated_at DESC);

CREATE TABLE game.moves (
    session_id uuid NOT NULL REFERENCES game.sessions (id),
    ply integer NOT NULL CHECK (ply > 0),
    player_id uuid NOT NULL,
    data jsonb NOT NULL,
    PRIMARY KEY (session_id, ply)
);
