ALTER TABLE roles
    ADD COLUMN title text NOT NULL DEFAULT '',
    ADD COLUMN system boolean NOT NULL DEFAULT false,
    ADD COLUMN version bigint NOT NULL DEFAULT 1,
    ADD COLUMN created_at timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

UPDATE roles SET system = true, title = initcap(name)
WHERE id IN ('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000003');

ALTER TABLE roles ADD CONSTRAINT roles_name_format CHECK (name ~ '^[a-z][a-z0-9_]{1,31}$');

ALTER TABLE users ADD COLUMN version bigint NOT NULL DEFAULT 1;

CREATE FUNCTION users_bump_version() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF (OLD.email, OLD.email_verified, OLD.username, OLD.display_name, OLD.first_name, OLD.last_name, OLD.nickname,
        OLD.avatar_url, OLD.suspended_at, OLD.suspended_reason)
       IS DISTINCT FROM
       (NEW.email, NEW.email_verified, NEW.username, NEW.display_name, NEW.first_name, NEW.last_name, NEW.nickname,
        NEW.avatar_url, NEW.suspended_at, NEW.suspended_reason) THEN
        NEW.version := OLD.version + 1;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER users_version
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION users_bump_version();

ALTER TABLE audit_log ADD COLUMN changes jsonb NOT NULL DEFAULT '[]'::jsonb;

CREATE TABLE deleted_records (
    id         uuid PRIMARY KEY,
    kind       text NOT NULL,
    record_id  uuid NOT NULL,
    record     jsonb NOT NULL,
    deleted_by uuid,
    deleted_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX deleted_records_kind_idx ON deleted_records (kind, deleted_at DESC);
