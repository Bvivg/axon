CREATE TABLE rating.ratings (
    game text NOT NULL,
    user_id uuid NOT NULL,
    rating double precision NOT NULL,
    deviation double precision NOT NULL CHECK (deviation > 0),
    volatility double precision NOT NULL CHECK (volatility > 0),
    games integer NOT NULL DEFAULT 0 CHECK (games >= 0),
    wins integer NOT NULL DEFAULT 0 CHECK (wins >= 0),
    losses integer NOT NULL DEFAULT 0 CHECK (losses >= 0),
    draws integer NOT NULL DEFAULT 0 CHECK (draws >= 0),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (game, user_id)
);

CREATE INDEX rating_ratings_leaderboard ON rating.ratings (game, rating DESC, user_id);

CREATE TABLE rating.changes (
    session_id uuid NOT NULL,
    user_id uuid NOT NULL,
    game text NOT NULL,
    event_id uuid NOT NULL,
    rating_before double precision NOT NULL,
    rating_after double precision NOT NULL,
    deviation_before double precision NOT NULL,
    deviation_after double precision NOT NULL,
    volatility_after double precision NOT NULL,
    finished_at timestamptz NOT NULL,
    PRIMARY KEY (session_id, user_id),
    FOREIGN KEY (game, user_id) REFERENCES rating.ratings (game, user_id)
);

CREATE INDEX rating_changes_user ON rating.changes (game, user_id, finished_at DESC);

CREATE TABLE rating.processed_events (
    event_id uuid PRIMARY KEY,
    session_id uuid NOT NULL,
    processed_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
