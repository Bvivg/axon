ALTER TABLE rooms
    ADD COLUMN kind text NOT NULL DEFAULT 'open',
    ADD COLUMN direct_user_min uuid,
    ADD COLUMN direct_user_max uuid;

ALTER TABLE rooms
    ADD CONSTRAINT rooms_kind_known CHECK (kind IN ('open', 'direct')),
    ADD CONSTRAINT rooms_direct_pair_bounded CHECK (
        (kind = 'direct' AND direct_user_min IS NOT NULL AND direct_user_max IS NOT NULL AND direct_user_min < direct_user_max)
        OR (kind = 'open' AND direct_user_min IS NULL AND direct_user_max IS NULL)
    );

CREATE UNIQUE INDEX rooms_direct_pair_key ON rooms (direct_user_min, direct_user_max) WHERE kind = 'direct';

ALTER TABLE rooms DROP CONSTRAINT rooms_name_present;
ALTER TABLE rooms ADD CONSTRAINT rooms_name_present CHECK (kind = 'direct' OR length(btrim(name)) > 0);

ALTER TABLE room_members
    ADD COLUMN hidden_at timestamptz,
    ADD COLUMN cleared_through_seq bigint NOT NULL DEFAULT 0;

ALTER TABLE messages
    ADD COLUMN kind text NOT NULL DEFAULT 'text',
    ADD COLUMN payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN reply_to_id uuid REFERENCES messages (id) ON DELETE SET NULL,
    ADD COLUMN forwarded_from_id uuid REFERENCES messages (id) ON DELETE SET NULL;

ALTER TABLE messages
    ADD CONSTRAINT messages_kind_known CHECK (kind IN ('text', 'voice', 'attachment', 'system'));

ALTER TABLE messages DROP CONSTRAINT messages_body_present;
ALTER TABLE messages ADD CONSTRAINT messages_body_present CHECK (kind <> 'text' OR length(btrim(body)) > 0);

CREATE INDEX messages_reply_to_id_idx ON messages (reply_to_id) WHERE reply_to_id IS NOT NULL;
CREATE INDEX messages_forwarded_from_id_idx ON messages (forwarded_from_id) WHERE forwarded_from_id IS NOT NULL;
