ALTER TABLE messages DROP CONSTRAINT messages_kind_known;
ALTER TABLE messages
    ADD CONSTRAINT messages_kind_known CHECK (kind IN ('text', 'voice', 'attachment', 'image', 'video', 'album', 'system'));

DROP INDEX messages_room_media_idx;
CREATE INDEX messages_room_media_idx ON messages (room_id, seq)
    WHERE kind IN ('voice', 'attachment', 'image', 'video', 'album');

CREATE TABLE message_uploads (
    message_id uuid    NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    position   integer NOT NULL,
    upload_id  uuid    NOT NULL REFERENCES uploads (id) ON DELETE CASCADE,

    PRIMARY KEY (message_id, position),
    CONSTRAINT message_uploads_position_bounded CHECK (position BETWEEN 0 AND 9)
);

CREATE INDEX message_uploads_upload_id_idx ON message_uploads (upload_id);
