ALTER TABLE room_members
    ADD COLUMN last_read_seq bigint NOT NULL DEFAULT 0;
