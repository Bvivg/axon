DROP TABLE game.match_queue;
DROP TABLE game.rating_changes;
DROP TABLE game.ratings;
ALTER TABLE game.sessions DROP COLUMN rated, DROP COLUMN resigned;
