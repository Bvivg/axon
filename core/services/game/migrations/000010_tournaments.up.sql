ALTER TABLE game.sessions ADD COLUMN tournament_id uuid;

CREATE INDEX game_sessions_tournament ON game.sessions (tournament_id) WHERE tournament_id IS NOT NULL;

CREATE TABLE game.cancelled_tournaments (
    tournament_id uuid PRIMARY KEY,
    cancelled_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
