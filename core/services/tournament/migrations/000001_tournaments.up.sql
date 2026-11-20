CREATE TABLE tournament.tournaments (
    id uuid PRIMARY KEY,
    creator_id uuid NOT NULL,
    request_id uuid NOT NULL,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
    game text NOT NULL,
    format text NOT NULL CHECK (format IN ('single_elimination', 'swiss')),
    capacity integer NOT NULL CHECK (capacity BETWEEN 2 AND 64),
    requested_rounds integer NOT NULL DEFAULT 0 CHECK (requested_rounds >= 0),
    total_rounds integer NOT NULL DEFAULT 0 CHECK (total_rounds >= 0),
    status text NOT NULL CHECK (status IN ('registration', 'seeding', 'running', 'finished', 'cancelled')),
    current_round integer NOT NULL DEFAULT 0 CHECK (current_round >= 0),
    winner_id uuid,
    created_at timestamptz NOT NULL,
    started_at timestamptz,
    finished_at timestamptz,
    cancelled_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (creator_id, request_id)
);

CREATE INDEX tournament_tournaments_recent ON tournament.tournaments (created_at DESC, id DESC);
CREATE INDEX tournament_tournaments_open ON tournament.tournaments (status) WHERE status IN ('seeding', 'running');

CREATE TABLE tournament.registrations (
    tournament_id uuid NOT NULL REFERENCES tournament.tournaments (id) ON DELETE CASCADE,
    user_id uuid NOT NULL,
    rating integer NOT NULL,
    seed integer CHECK (seed > 0),
    registered_at timestamptz NOT NULL,
    returned boolean NOT NULL DEFAULT false,
    PRIMARY KEY (tournament_id, user_id)
);

CREATE TABLE tournament.rounds (
    tournament_id uuid NOT NULL REFERENCES tournament.tournaments (id) ON DELETE CASCADE,
    number integer NOT NULL CHECK (number > 0),
    started_at timestamptz NOT NULL,
    finished_at timestamptz,
    PRIMARY KEY (tournament_id, number)
);

CREATE TABLE tournament.matches (
    tournament_id uuid NOT NULL,
    round integer NOT NULL,
    board integer NOT NULL CHECK (board > 0),
    session_id uuid UNIQUE,
    first_id uuid NOT NULL,
    second_id uuid,
    result text NOT NULL CHECK (result IN ('pending', 'win', 'draw', 'bye', 'aborted')),
    winner_id uuid,
    forfeit boolean NOT NULL DEFAULT false,
    finished_at timestamptz,
    PRIMARY KEY (tournament_id, round, board),
    FOREIGN KEY (tournament_id, round) REFERENCES tournament.rounds (tournament_id, number) ON DELETE CASCADE,
    CHECK ((second_id IS NULL) = (session_id IS NULL))
);

CREATE TABLE tournament.deadlines (
    session_id uuid PRIMARY KEY,
    tournament_id uuid NOT NULL REFERENCES tournament.tournaments (id) ON DELETE CASCADE,
    due_at timestamptz NOT NULL,
    fired_at timestamptz,
    forfeited boolean NOT NULL DEFAULT false
);

CREATE INDEX tournament_deadlines_due ON tournament.deadlines (due_at) WHERE fired_at IS NULL;

CREATE TABLE tournament.ratings (
    game text NOT NULL,
    user_id uuid NOT NULL,
    rating integer NOT NULL,
    changed_at timestamptz NOT NULL,
    PRIMARY KEY (game, user_id)
);

CREATE TABLE tournament.processed_events (
    event_id uuid PRIMARY KEY,
    session_id uuid NOT NULL,
    processed_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
