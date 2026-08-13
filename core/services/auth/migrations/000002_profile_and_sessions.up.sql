ALTER TABLE users
    ADD COLUMN first_name text NOT NULL DEFAULT '',
    ADD COLUMN last_name  text NOT NULL DEFAULT '',
    ADD COLUMN nickname   text NOT NULL DEFAULT '';

ALTER TABLE users
    ADD COLUMN avatar_is_custom boolean NOT NULL DEFAULT false;

ALTER TABLE refresh_tokens
    ADD COLUMN user_agent text NOT NULL DEFAULT '',
    ADD COLUMN ip         text NOT NULL DEFAULT '';
