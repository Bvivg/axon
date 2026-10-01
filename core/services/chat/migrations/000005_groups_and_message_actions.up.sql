ALTER TABLE rooms DROP CONSTRAINT rooms_kind_known;
ALTER TABLE rooms ADD CONSTRAINT rooms_kind_known CHECK (kind IN ('open', 'direct', 'group'));

ALTER TABLE rooms DROP CONSTRAINT rooms_direct_pair_bounded;
ALTER TABLE rooms ADD CONSTRAINT rooms_direct_pair_bounded CHECK (
    (kind = 'direct' AND direct_user_min IS NOT NULL AND direct_user_max IS NOT NULL AND direct_user_min < direct_user_max)
    OR (kind <> 'direct' AND direct_user_min IS NULL AND direct_user_max IS NULL)
);

ALTER TABLE room_members
    ADD COLUMN role text NOT NULL DEFAULT 'member',
    ADD CONSTRAINT room_members_role_known CHECK (role IN ('owner', 'member'));

ALTER TABLE messages
    ADD COLUMN edited_at timestamptz,
    ADD COLUMN deleted_at timestamptz,
    ADD COLUMN forward_origin_author_id uuid,
    ADD COLUMN upload_id uuid REFERENCES uploads (id) ON DELETE SET NULL;

ALTER TABLE messages DROP CONSTRAINT messages_body_present;
ALTER TABLE messages ADD CONSTRAINT messages_body_present
    CHECK (deleted_at IS NOT NULL OR kind <> 'text' OR length(btrim(body)) > 0);

CREATE INDEX messages_upload_id_idx ON messages (upload_id) WHERE upload_id IS NOT NULL;
CREATE INDEX uploads_created_at_idx ON uploads (created_at);

UPDATE messages m
SET upload_id = u.id
FROM uploads u
WHERE m.upload_id IS NULL
    AND m.kind = u.kind
    AND m.payload ->> 'url' = u.payload ->> 'url';

UPDATE messages m
SET forward_origin_author_id = s.author_id
FROM messages s
WHERE m.forwarded_from_id = s.id AND m.forward_origin_author_id IS NULL;
