CREATE TABLE files (
    id          uuid        PRIMARY KEY,
    kind        text        NOT NULL,
    sha256      bytea       NOT NULL,
    payload     jsonb       NOT NULL,
    object_keys text[]      NOT NULL,
    size_bytes  bigint      NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    linked_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT files_kind_known CHECK (kind IN ('voice', 'attachment', 'image', 'video')),
    CONSTRAINT files_content_key UNIQUE (kind, sha256)
);

CREATE INDEX files_linked_at_idx ON files (linked_at);

CREATE TABLE file_sources (
    mode          text  NOT NULL,
    source_sha256 bytea NOT NULL,
    file_id       uuid  NOT NULL REFERENCES files (id) ON DELETE CASCADE,

    PRIMARY KEY (mode, source_sha256)
);

CREATE INDEX file_sources_file_id_idx ON file_sources (file_id);

ALTER TABLE uploads ADD COLUMN file_id uuid REFERENCES files (id) ON DELETE RESTRICT;

CREATE INDEX uploads_file_id_idx ON uploads (file_id);

CREATE TABLE upload_sessions (
    id           uuid        PRIMARY KEY,
    uploader_id  uuid        NOT NULL,
    mode         text        NOT NULL,
    filename     text        NOT NULL,
    size_bytes   bigint      NOT NULL,
    part_size    bigint      NOT NULL,
    part_count   integer     NOT NULL,
    staging_key  text        NOT NULL,
    multipart_id text        NOT NULL,
    status       text        NOT NULL DEFAULT 'uploading',
    failure      text        NOT NULL DEFAULT '',
    upload_id    uuid        REFERENCES uploads (id) ON DELETE SET NULL,
    claimed_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT upload_sessions_mode_known CHECK (mode IN ('media', 'file', 'voice')),
    CONSTRAINT upload_sessions_status_known CHECK (status IN ('uploading', 'processing', 'ready', 'failed')),
    CONSTRAINT upload_sessions_shape CHECK (size_bytes > 0 AND part_size > 0 AND part_count BETWEEN 1 AND 10000)
);

CREATE INDEX upload_sessions_uploader_idx ON upload_sessions (uploader_id, created_at);
CREATE INDEX upload_sessions_status_idx ON upload_sessions (status, updated_at);
