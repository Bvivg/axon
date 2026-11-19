DROP INDEX call_outbox_push_idx;
ALTER TABLE call_outbox DROP COLUMN push_next_attempt_at, DROP COLUMN push_attempts, DROP COLUMN push_completed, DROP COLUMN push_receipts;
