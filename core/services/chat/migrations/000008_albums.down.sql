DROP TABLE IF EXISTS message_uploads;

DELETE FROM messages WHERE kind = 'album';

DROP INDEX IF EXISTS messages_room_media_idx;
CREATE INDEX messages_room_media_idx ON messages (room_id, seq)
    WHERE kind IN ('voice', 'attachment', 'image', 'video');

ALTER TABLE messages DROP CONSTRAINT messages_kind_known;
ALTER TABLE messages
    ADD CONSTRAINT messages_kind_known CHECK (kind IN ('text', 'voice', 'attachment', 'image', 'video', 'system'));
