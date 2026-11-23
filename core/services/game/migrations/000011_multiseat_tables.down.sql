ALTER TABLE game.match_queue
    DROP COLUMN IF EXISTS options_key,
    DROP COLUMN IF EXISTS options,
    DROP COLUMN IF EXISTS seats;

DROP INDEX IF EXISTS game.game_sessions_open;
DROP INDEX IF EXISTS game.game_sessions_players;

ALTER TABLE game.sessions
    DROP COLUMN IF EXISTS left_seats,
    DROP COLUMN IF EXISTS options,
    DROP COLUMN IF EXISTS players,
    DROP COLUMN IF EXISTS seats;
