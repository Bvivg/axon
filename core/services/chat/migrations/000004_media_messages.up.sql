ALTER TABLE messages DROP CONSTRAINT messages_kind_known;
ALTER TABLE messages
    ADD CONSTRAINT messages_kind_known CHECK (kind IN ('text', 'voice', 'attachment', 'image', 'video', 'system'));

CREATE INDEX messages_room_media_idx ON messages (room_id, seq)
    WHERE kind IN ('voice', 'attachment', 'image', 'video');

CREATE TABLE uploads (
    id          uuid PRIMARY KEY,
    uploader_id uuid        NOT NULL,
    kind        text        NOT NULL,
    payload     jsonb       NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uploads_kind_known CHECK (kind IN ('voice', 'attachment', 'image', 'video'))
);

CREATE INDEX uploads_uploader_id_idx ON uploads (uploader_id, created_at);
