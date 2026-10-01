CREATE TABLE read_marks (
    room_id  uuid        NOT NULL,
    user_id  uuid        NOT NULL,
    from_seq bigint      NOT NULL,
    to_seq   bigint      NOT NULL,
    read_at  timestamptz NOT NULL DEFAULT now(),

    FOREIGN KEY (room_id, user_id) REFERENCES room_members (room_id, user_id) ON DELETE CASCADE,
    CONSTRAINT read_marks_range CHECK (from_seq >= 0 AND to_seq > from_seq)
);

CREATE INDEX read_marks_reader_idx ON read_marks (room_id, user_id, to_seq);
