ALTER TABLE call_outbox
    ADD COLUMN push_next_attempt_at timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN push_attempts integer NOT NULL DEFAULT 0,
    ADD COLUMN push_completed boolean NOT NULL DEFAULT false,
    ADD COLUMN push_receipts jsonb NOT NULL DEFAULT '{}'::jsonb;
CREATE INDEX call_outbox_push_idx ON call_outbox (push_next_attempt_at) WHERE NOT push_completed;
