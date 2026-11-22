DROP TABLE audit_log;
DROP FUNCTION audit_log_append_only();

DROP INDEX users_suspended_idx;
DROP INDEX users_created_at_idx;

ALTER TABLE users
    DROP COLUMN suspended_reason,
    DROP COLUMN suspended_at;

DELETE FROM role_permissions WHERE permission_id IN (
    SELECT id FROM permissions WHERE namespace IN ('admin.users', 'admin.games', 'admin.chat', 'admin.audit')
);
DELETE FROM role_permissions WHERE role_id = '00000000-0000-0000-0000-000000000003';
DELETE FROM user_roles WHERE role_id = '00000000-0000-0000-0000-000000000003';
DELETE FROM permissions WHERE namespace IN ('admin.users', 'admin.games', 'admin.chat', 'admin.audit') AND parent_id IS NOT NULL;
DELETE FROM permissions WHERE namespace IN ('admin.users', 'admin.games', 'admin.chat', 'admin.audit');
DELETE FROM roles WHERE id = '00000000-0000-0000-0000-000000000003';
