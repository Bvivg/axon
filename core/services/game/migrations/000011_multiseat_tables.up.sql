ALTER TABLE game.sessions
    ADD COLUMN seats smallint CHECK (seats BETWEEN 2 AND 6),
    ADD COLUMN players uuid[],
    ADD COLUMN options jsonb,
    ADD COLUMN left_seats smallint[];

CREATE INDEX game_sessions_players ON game.sessions USING gin (players);

CREATE INDEX game_sessions_open ON game.sessions (game, updated_at DESC, id DESC)
    WHERE opponent_id IS NULL AND seats IS NOT NULL;

ALTER TABLE game.match_queue
    ADD COLUMN seats smallint NOT NULL DEFAULT 2 CHECK (seats BETWEEN 2 AND 6),
    ADD COLUMN options jsonb,
    ADD COLUMN options_key text NOT NULL DEFAULT '';
