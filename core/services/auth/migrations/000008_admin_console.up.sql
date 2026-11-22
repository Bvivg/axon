INSERT INTO roles (id, name) VALUES
    ('00000000-0000-0000-0000-000000000003', 'moderator');

INSERT INTO permissions (id, namespace, action, parent_id) VALUES
    ('00000000-0000-0000-0007-000000000001', 'admin.users', 'manage', NULL),
    ('00000000-0000-0000-0007-000000000002', 'admin.users', 'read', '00000000-0000-0000-0007-000000000001'),
    ('00000000-0000-0000-0007-000000000003', 'admin.users', 'mutate', '00000000-0000-0000-0007-000000000001'),

    ('00000000-0000-0000-0008-000000000001', 'admin.games', 'manage', NULL),
    ('00000000-0000-0000-0008-000000000002', 'admin.games', 'read', '00000000-0000-0000-0008-000000000001'),
    ('00000000-0000-0000-0008-000000000003', 'admin.games', 'moderate', '00000000-0000-0000-0008-000000000001'),

    ('00000000-0000-0000-0009-000000000001', 'admin.chat', 'manage', NULL),
    ('00000000-0000-0000-0009-000000000002', 'admin.chat', 'read', '00000000-0000-0000-0009-000000000001'),
    ('00000000-0000-0000-0009-000000000003', 'admin.chat', 'moderate', '00000000-0000-0000-0009-000000000001'),

    ('00000000-0000-0000-000a-000000000001', 'admin.audit', 'manage', NULL),
    ('00000000-0000-0000-000a-000000000002', 'admin.audit', 'read', '00000000-0000-0000-000a-000000000001');

INSERT INTO role_permissions (role_id, permission_id) VALUES
    ('00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0007-000000000001'),
    ('00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0008-000000000001'),
    ('00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0009-000000000001'),
    ('00000000-0000-0000-0000-000000000002', '00000000-0000-0000-000a-000000000001'),

    ('00000000-0000-0000-0000-000000000003', '00000000-0000-0000-0007-000000000002'),
    ('00000000-0000-0000-0000-000000000003', '00000000-0000-0000-0008-000000000001'),
    ('00000000-0000-0000-0000-000000000003', '00000000-0000-0000-0009-000000000001'),
    ('00000000-0000-0000-0000-000000000003', '00000000-0000-0000-000a-000000000002'),
    ('00000000-0000-0000-0000-000000000003', '00000000-0000-0000-0006-000000000002');

ALTER TABLE users
    ADD COLUMN suspended_at timestamptz,
    ADD COLUMN suspended_reason text NOT NULL DEFAULT '';

CREATE INDEX users_created_at_idx ON users (created_at DESC, id DESC);
CREATE INDEX users_suspended_idx ON users (suspended_at) WHERE suspended_at IS NOT NULL;

CREATE TABLE audit_log (
    id             uuid PRIMARY KEY,
    occurred_at    timestamptz NOT NULL,
    service        text NOT NULL,
    action         text NOT NULL,
    outcome        text NOT NULL CHECK (outcome IN ('succeeded', 'denied', 'failed')),
    actor_id       uuid,
    target_type    text NOT NULL DEFAULT '',
    target_id      text NOT NULL DEFAULT '',
    reason         text NOT NULL DEFAULT '',
    correlation_id text NOT NULL DEFAULT '',
    client_ip      text NOT NULL DEFAULT '',
    user_agent     text NOT NULL DEFAULT '',
    details        jsonb NOT NULL DEFAULT '{}'::jsonb,
    recorded_at    timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE INDEX audit_log_occurred_idx ON audit_log (occurred_at DESC, id DESC);
CREATE INDEX audit_log_actor_idx ON audit_log (actor_id, occurred_at DESC, id DESC);
CREATE INDEX audit_log_target_idx ON audit_log (target_type, target_id, occurred_at DESC, id DESC);

CREATE FUNCTION audit_log_append_only() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit_log is append-only' USING ERRCODE = 'insufficient_privilege';
END;
$$;

CREATE TRIGGER audit_log_no_change
    BEFORE UPDATE OR DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_append_only();

CREATE TRIGGER audit_log_no_truncate
    BEFORE TRUNCATE ON audit_log
    FOR EACH STATEMENT EXECUTE FUNCTION audit_log_append_only();
