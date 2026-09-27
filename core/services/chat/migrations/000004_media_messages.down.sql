DROP TABLE uploads;

DROP INDEX messages_room_media_idx;

UPDATE messages
SET kind    = 'attachment',
    payload = jsonb_build_object(
        'url', payload ->> 'url',
        'filename', CASE kind WHEN 'image' THEN 'photo' ELSE 'video' END,
        'mime', payload ->> 'mime',
        'size_bytes', COALESCE((payload ->> 'size_bytes')::bigint, 0)
    )
WHERE kind IN ('image', 'video');

ALTER TABLE messages DROP CONSTRAINT messages_kind_known;
ALTER TABLE messages
    ADD CONSTRAINT messages_kind_known CHECK (kind IN ('text', 'voice', 'attachment', 'system'));
