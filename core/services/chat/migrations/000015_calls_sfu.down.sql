DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM call_transports) OR EXISTS (SELECT 1 FROM call_webhook_receipts) OR EXISTS (SELECT 1 FROM calls WHERE room_id IS NULL OR predecessor_call_id IS NOT NULL OR replaced_by_call_id IS NOT NULL) THEN
        RAISE EXCEPTION 'SFU history exists; disable and drain SFU instead of destructive down migration';
    END IF;
END $$;
DROP TABLE call_webhook_receipts;
DROP TABLE call_transports;
DROP INDEX calls_replacement_unique;
DROP INDEX calls_predecessor_unique;
ALTER TABLE calls DROP CONSTRAINT calls_replacement_not_self;
ALTER TABLE calls DROP CONSTRAINT calls_predecessor_not_self;
ALTER TABLE calls DROP COLUMN replaced_by_call_id;
ALTER TABLE calls DROP COLUMN predecessor_call_id;
ALTER TABLE calls ALTER COLUMN room_id SET NOT NULL;
ALTER TABLE call_participants DROP CONSTRAINT call_participants_endpoint_user_fk;
ALTER TABLE call_endpoints DROP CONSTRAINT call_endpoints_identity_user_unique;
ALTER TABLE call_endpoints DROP COLUMN supports_sfu;
