DROP TABLE IF EXISTS deleted_records;

ALTER TABLE audit_log DROP COLUMN IF EXISTS changes;

DROP TRIGGER IF EXISTS users_version ON users;
DROP FUNCTION IF EXISTS users_bump_version();
ALTER TABLE users DROP COLUMN IF EXISTS version;

ALTER TABLE roles DROP CONSTRAINT IF EXISTS roles_name_format;
ALTER TABLE roles
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS created_at,
    DROP COLUMN IF EXISTS version,
    DROP COLUMN IF EXISTS system,
    DROP COLUMN IF EXISTS title;
