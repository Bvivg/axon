ALTER TABLE users
    ADD COLUMN username text;

WITH stems AS (
    SELECT id,
           created_at,
           rtrim(left(
               regexp_replace(
                   regexp_replace(lower(split_part(email, '@', 1)), '[^a-z0-9]+', '_', 'g'),
                   '^[^a-z]+', ''),
               32), '_') AS stem
    FROM users
), named AS (
    SELECT id,
           created_at,
           CASE WHEN length(stem) >= 3 THEN stem ELSE 'user' END AS stem
    FROM stems
), ranked AS (
    SELECT id,
           stem,
           row_number() OVER (PARTITION BY stem ORDER BY created_at, id) AS rank
    FROM named
)
UPDATE users
SET username = CASE
        WHEN ranked.rank = 1 THEN ranked.stem
        ELSE rtrim(left(ranked.stem, 25), '_') || '_' || left(replace(users.id::text, '-', ''), 6)
    END
FROM ranked
WHERE ranked.id = users.id;

ALTER TABLE users
    ALTER COLUMN username SET NOT NULL,
    ADD CONSTRAINT users_username_format CHECK (username ~ '^[a-z][a-z0-9_]{2,31}$');

CREATE UNIQUE INDEX users_username_key ON users (username);
