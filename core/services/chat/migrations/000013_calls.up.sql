CREATE TABLE call_endpoints (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL,
    family_id uuid NOT NULL,
    proof_hash bytea NOT NULL CHECK (octet_length(proof_hash) = 32),
    epoch bigint NOT NULL CHECK (epoch > 0),
    connection_id uuid,
    lease_expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX call_endpoints_user_idx ON call_endpoints (user_id, lease_expires_at);

CREATE TABLE calls (
    id uuid PRIMARY KEY,
    room_id uuid NOT NULL REFERENCES rooms (id),
    caller_id uuid NOT NULL,
    start_key uuid NOT NULL,
    state text NOT NULL CHECK (state IN ('ringing', 'connecting', 'active', 'ended')),
    version bigint NOT NULL CHECK (version > 0),
    deadline timestamptz,
    created_at timestamptz NOT NULL,
    document jsonb NOT NULL,
    UNIQUE (caller_id, start_key),
    CHECK (document->>'state' = state),
    CHECK ((document->>'version')::bigint = version),
    CHECK ((document->>'id')::uuid = id)
);
CREATE INDEX calls_deadline_idx ON calls (deadline) WHERE deadline IS NOT NULL;
CREATE INDEX calls_history_idx ON calls (created_at DESC, id DESC);

CREATE TABLE call_participants (
    call_id uuid NOT NULL REFERENCES calls (id) ON DELETE CASCADE,
    user_id uuid NOT NULL,
    endpoint_id uuid REFERENCES call_endpoints (id),
    PRIMARY KEY (call_id, user_id)
);
CREATE INDEX call_participants_user_idx ON call_participants (user_id, call_id);

CREATE TABLE call_user_slots (
    user_id uuid PRIMARY KEY,
    call_id uuid NOT NULL REFERENCES calls (id),
    phase text NOT NULL CHECK (phase IN ('busy', 'releasing'))
);
CREATE INDEX call_user_slots_call_idx ON call_user_slots (call_id);

CREATE TABLE call_requests (
    user_id uuid NOT NULL,
    request_id uuid NOT NULL,
    fingerprint bytea NOT NULL CHECK (octet_length(fingerprint) = 32),
    result jsonb NOT NULL,
    call_id uuid NOT NULL REFERENCES calls (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, request_id)
);

CREATE TABLE call_outbox (
    sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id uuid NOT NULL UNIQUE,
    call_id uuid NOT NULL REFERENCES calls (id) ON DELETE CASCADE,
    user_id uuid NOT NULL,
    version bigint NOT NULL,
    body jsonb NOT NULL,
    acknowledgements jsonb NOT NULL DEFAULT '{}'::jsonb,
    attempts integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (call_id, user_id, version)
);
CREATE INDEX call_outbox_user_idx ON call_outbox (user_id, sequence);
CREATE INDEX call_outbox_retry_idx ON call_outbox (next_attempt_at);
