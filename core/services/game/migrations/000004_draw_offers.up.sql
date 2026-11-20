ALTER TABLE game.sessions
    ADD COLUMN draw_offered_by uuid,
    ADD COLUMN agreed_draw boolean NOT NULL DEFAULT false;
