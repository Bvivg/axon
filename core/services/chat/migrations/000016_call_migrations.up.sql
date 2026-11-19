ALTER TABLE call_endpoints ADD COLUMN supports_migration boolean NOT NULL DEFAULT false;
CREATE TABLE call_migrations (
    id uuid PRIMARY KEY,
    call_id uuid NOT NULL REFERENCES calls(id),
    source_generation bigint NOT NULL CHECK(source_generation > 0),
    target_generation bigint NOT NULL CHECK(target_generation > source_generation),
    phase text NOT NULL CHECK(phase IN ('preparing','committing','cleaning','aborting','completed','aborted')),
    deadline timestamptz,
    outcome text NOT NULL,
    created_at timestamptz NOT NULL,
    finished_at timestamptz,
    document jsonb NOT NULL,
    UNIQUE(call_id,target_generation),
    CHECK((document->>'id')::uuid=id),
    CHECK(document->>'phase'=phase),
    CHECK((document->>'source_generation')::bigint=source_generation),
    CHECK((document->>'target_generation')::bigint=target_generation)
);
CREATE INDEX call_migrations_pending_idx ON call_migrations(deadline) WHERE phase IN ('preparing','committing','cleaning','aborting');
