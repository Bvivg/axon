CREATE INDEX game_sessions_live ON game.sessions (updated_at DESC, id DESC)
    WHERE opponent_id IS NOT NULL AND state->>'status' = 'in_progress';
