CREATE TABLE analysis.reports (
    session_id uuid PRIMARY KEY,
    event_id uuid NOT NULL,
    status text NOT NULL CHECK (status IN ('pending', 'ready', 'failed')),
    depth integer NOT NULL CHECK (depth > 0),
    engine text NOT NULL,
    moves integer NOT NULL CHECK (moves >= 0),
    game_finished_at timestamptz NOT NULL,
    started_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    completed_at timestamptz,
    duration_ms integer CHECK (duration_ms >= 0),
    error text,
    CHECK ((status = 'pending') = (completed_at IS NULL)),
    CHECK ((status = 'pending') = (duration_ms IS NULL)),
    CHECK ((status = 'failed') = (error IS NOT NULL))
);

CREATE INDEX analysis_reports_pending ON analysis.reports (started_at) WHERE status = 'pending';

CREATE TABLE analysis.positions (
    session_id uuid NOT NULL REFERENCES analysis.reports (session_id) ON DELETE CASCADE,
    ply integer NOT NULL CHECK (ply >= 0),
    eval_cp integer NOT NULL CHECK (eval_cp BETWEEN -10000 AND 10000),
    eval_mate integer,
    best_move text,
    played_move text,
    loss integer NOT NULL CHECK (loss >= 0),
    class text NOT NULL CHECK (class IN ('none', 'best', 'good', 'inaccuracy', 'mistake', 'blunder')),
    PRIMARY KEY (session_id, ply),
    CHECK ((ply = 0) = (played_move IS NULL)),
    CHECK ((ply = 0) = (class = 'none'))
);

CREATE TABLE analysis.processed_events (
    event_id uuid PRIMARY KEY,
    session_id uuid NOT NULL,
    processed_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
