ALTER TABLE game.sessions
    ADD COLUMN revision integer NOT NULL DEFAULT 0 CHECK (revision >= 0),
    ADD COLUMN clock jsonb,
    ADD COLUMN timed_out boolean NOT NULL DEFAULT false;

UPDATE game.sessions
SET revision = COALESCE((state->>'ply')::integer, 0) + CASE WHEN opponent_id IS NULL THEN 0 ELSE 1 END,
    clock = CASE WHEN opponent_id IS NOT NULL THEN jsonb_build_object(
        'remaining_ms', jsonb_build_array(300000, 300000),
        'started_at', clock_timestamp()
    ) END;

ALTER TABLE game.moves
    ADD COLUMN state_before jsonb,
    ADD COLUMN clock_before jsonb;

CREATE INDEX game_sessions_active ON game.sessions (id) WHERE state->>'status' = 'in_progress';
