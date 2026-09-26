DROP INDEX IF EXISTS messages_forwarded_from_id_idx;
DROP INDEX IF EXISTS messages_reply_to_id_idx;

ALTER TABLE messages DROP CONSTRAINT messages_body_present;
ALTER TABLE messages ADD CONSTRAINT messages_body_present CHECK (length(btrim(body)) > 0);

ALTER TABLE messages
    DROP CONSTRAINT messages_kind_known,
    DROP COLUMN forwarded_from_id,
    DROP COLUMN reply_to_id,
    DROP COLUMN payload,
    DROP COLUMN kind;

ALTER TABLE room_members
    DROP COLUMN cleared_through_seq,
    DROP COLUMN hidden_at;

ALTER TABLE rooms DROP CONSTRAINT rooms_name_present;
ALTER TABLE rooms ADD CONSTRAINT rooms_name_present CHECK (length(btrim(name)) > 0);

DROP INDEX IF EXISTS rooms_direct_pair_key;

ALTER TABLE rooms
    DROP CONSTRAINT rooms_direct_pair_bounded,
    DROP CONSTRAINT rooms_kind_known,
    DROP COLUMN direct_user_max,
    DROP COLUMN direct_user_min,
    DROP COLUMN kind;
