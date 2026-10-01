DROP INDEX users_username_key;

ALTER TABLE users
    DROP COLUMN username;
