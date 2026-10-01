CREATE INDEX messages_body_trgm_idx ON messages USING gin (body public.gin_trgm_ops)
    WHERE deleted_at IS NULL AND kind <> 'system';
