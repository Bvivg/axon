ALTER TABLE upload_sessions ADD COLUMN expected_sha256 bytea
    CHECK (expected_sha256 IS NULL OR octet_length(expected_sha256) = 32);

ALTER TABLE file_sources ADD COLUMN source_size_bytes bigint
    CHECK (source_size_bytes IS NULL OR source_size_bytes > 0);
