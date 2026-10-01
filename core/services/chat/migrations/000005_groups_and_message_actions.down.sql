DROP INDEX IF EXISTS uploads_created_at_idx;
DROP INDEX IF EXISTS messages_upload_id_idx;

UPDATE messages
SET body = 'Message deleted'
WHERE deleted_at IS NOT NULL AND kind = 'text' AND length(btrim(body)) = 0;

ALTER TABLE messages DROP CONSTRAINT messages_body_present;
ALTER TABLE messages ADD CONSTRAINT messages_body_present CHECK (kind <> 'text' OR length(btrim(body)) > 0);

ALTER TABLE messages
    DROP COLUMN upload_id,
    DROP COLUMN forward_origin_author_id,
    DROP COLUMN deleted_at,
    DROP COLUMN edited_at;

ALTER TABLE room_members
    DROP CONSTRAINT room_members_role_known,
    DROP COLUMN role;

DELETE FROM rooms WHERE kind = 'group';

ALTER TABLE rooms DROP CONSTRAINT rooms_direct_pair_bounded;
ALTER TABLE rooms ADD CONSTRAINT rooms_direct_pair_bounded CHECK (
    (kind = 'direct' AND direct_user_min IS NOT NULL AND direct_user_max IS NOT NULL AND direct_user_min < direct_user_max)
    OR (kind = 'open' AND direct_user_min IS NULL AND direct_user_max IS NULL)
);

ALTER TABLE rooms DROP CONSTRAINT rooms_kind_known;
ALTER TABLE rooms ADD CONSTRAINT rooms_kind_known CHECK (kind IN ('open', 'direct'));
