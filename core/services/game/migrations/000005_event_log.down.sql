DROP TABLE game.projector_offsets;
DROP TABLE game.owners;
ALTER TABLE game.match_queue DROP COLUMN started;
ALTER TABLE game.match_queue ADD CONSTRAINT match_queue_session_id_fkey FOREIGN KEY (session_id) REFERENCES game.sessions (id);
DROP INDEX game.game_sessions_open_partition;
ALTER TABLE game.sessions DROP COLUMN partition;
