DROP TABLE IF EXISTS game.cancelled_tournaments;
DROP INDEX IF EXISTS game.game_sessions_tournament;
ALTER TABLE game.sessions DROP COLUMN IF EXISTS tournament_id;
