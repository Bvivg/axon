ALTER TABLE messages DROP CONSTRAINT messages_body_bounded;
ALTER TABLE messages ADD CONSTRAINT messages_body_bounded CHECK (length(body) <= 65536);
