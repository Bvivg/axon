ALTER TABLE call_endpoints ADD COLUMN supports_sfu boolean NOT NULL DEFAULT false;
ALTER TABLE call_endpoints ADD CONSTRAINT call_endpoints_identity_user_unique UNIQUE(id,user_id);
ALTER TABLE call_participants ADD CONSTRAINT call_participants_endpoint_user_fk FOREIGN KEY(endpoint_id,user_id) REFERENCES call_endpoints(id,user_id);
ALTER TABLE calls ALTER COLUMN room_id DROP NOT NULL;
ALTER TABLE calls ADD COLUMN predecessor_call_id uuid REFERENCES calls(id);
ALTER TABLE calls ADD COLUMN replaced_by_call_id uuid REFERENCES calls(id);
ALTER TABLE calls ADD CONSTRAINT calls_predecessor_not_self CHECK (predecessor_call_id IS NULL OR predecessor_call_id <> id);
ALTER TABLE calls ADD CONSTRAINT calls_replacement_not_self CHECK (replaced_by_call_id IS NULL OR replaced_by_call_id <> id);
CREATE UNIQUE INDEX calls_predecessor_unique ON calls(predecessor_call_id) WHERE predecessor_call_id IS NOT NULL;
CREATE UNIQUE INDEX calls_replacement_unique ON calls(replaced_by_call_id) WHERE replaced_by_call_id IS NOT NULL;

CREATE TABLE call_transports (
    call_id uuid NOT NULL REFERENCES calls(id),
    generation bigint NOT NULL CHECK (generation > 0),
    kind text NOT NULL CHECK (kind IN ('p2p','sfu')),
    state text NOT NULL CHECK (state IN ('ringing','connecting','active','waiting_cleanup','ended')),
    room_name text,
    room_sid text,
    created_at timestamptz NOT NULL DEFAULT now(),
    setup_deadline timestamptz,
    ended_at timestamptz,
    reconcile_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (call_id,generation),
    CHECK ((kind='p2p' AND room_name IS NULL) OR (kind='sfu' AND room_name IS NOT NULL))
);
CREATE INDEX call_transports_reconcile_idx ON call_transports(reconcile_at) WHERE kind='sfu';

CREATE TABLE call_webhook_receipts (
    event_id text PRIMARY KEY CHECK (length(event_id) BETWEEN 1 AND 128),
    body_hash bytea NOT NULL CHECK (octet_length(body_hash)=32),
    room_name text NOT NULL,
    event_kind text NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX call_webhook_receipts_received_idx ON call_webhook_receipts(received_at);
