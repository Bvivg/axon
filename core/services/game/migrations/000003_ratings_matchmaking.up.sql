ALTER TABLE game.sessions
    ADD COLUMN rated boolean NOT NULL DEFAULT false,
    ADD COLUMN resigned boolean NOT NULL DEFAULT false;

CREATE TABLE game.ratings (
    user_id uuid NOT NULL,
    game text NOT NULL,
    rating integer NOT NULL DEFAULT 1200 CHECK (rating > 0),
    games integer NOT NULL DEFAULT 0 CHECK (games >= 0),
    wins integer NOT NULL DEFAULT 0 CHECK (wins >= 0),
    losses integer NOT NULL DEFAULT 0 CHECK (losses >= 0),
    draws integer NOT NULL DEFAULT 0 CHECK (draws >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, game)
);

CREATE INDEX game_ratings_leaderboard ON game.ratings (game, rating DESC);

CREATE TABLE game.rating_changes (
    session_id uuid NOT NULL REFERENCES game.sessions (id),
    user_id uuid NOT NULL,
    game text NOT NULL,
    before integer NOT NULL,
    after integer NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, user_id),
    FOREIGN KEY (user_id, game) REFERENCES game.ratings (user_id, game)
);

CREATE INDEX game_rating_changes_user ON game.rating_changes (user_id, game, created_at DESC);

CREATE TABLE game.match_queue (
    user_id uuid NOT NULL,
    game text NOT NULL,
    rating integer NOT NULL,
    request_id uuid NOT NULL,
    joined_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    last_seen_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    session_id uuid REFERENCES game.sessions (id),
    PRIMARY KEY (user_id, game)
);

CREATE INDEX game_match_queue_waiting ON game.match_queue (game, joined_at) WHERE session_id IS NULL;
