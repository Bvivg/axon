ALTER TABLE refresh_tokens
    DROP COLUMN user_agent,
    DROP COLUMN ip;

ALTER TABLE users
    DROP COLUMN avatar_is_custom,
    DROP COLUMN first_name,
    DROP COLUMN last_name,
    DROP COLUMN nickname;
