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
           row_number() OVER (PARTITION BY stem ORDER BY created_at, id) = 1
               AND stem NOT IN ('admin', 'administrator', 'axon', 'moderator', 'official',
                                'root', 'security', 'support', 'system') AS keeps_stem
    FROM named
), proposed AS (
    SELECT id,
           keeps_stem,
           CASE WHEN keeps_stem THEN stem
                ELSE rtrim(left(stem, 25), '_') || '_' || left(replace(id::text, '-', ''), 6)
           END AS username
    FROM ranked
), settled AS (
    SELECT id,
           username,
           row_number() OVER (PARTITION BY username ORDER BY keeps_stem DESC, id) AS rank
    FROM proposed
)
UPDATE users
SET username = CASE
        WHEN settled.rank = 1 THEN settled.username
        ELSE 'u' || left(replace(users.id::text, '-', ''), 31)
    END
FROM settled
WHERE settled.id = users.id;

ALTER TABLE users
    ALTER COLUMN username SET NOT NULL,
    ADD CONSTRAINT users_username_format CHECK (username ~ '^[a-z][a-z0-9_]{2,31}$');

CREATE UNIQUE INDEX users_username_key ON users (username);
