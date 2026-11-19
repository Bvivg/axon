DROP INDEX game.game_sessions_active;
ALTER TABLE game.moves DROP COLUMN state_before, DROP COLUMN clock_before;
ALTER TABLE game.sessions DROP COLUMN revision, DROP COLUMN clock, DROP COLUMN timed_out;
