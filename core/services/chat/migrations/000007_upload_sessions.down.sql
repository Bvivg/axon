DROP TABLE IF EXISTS upload_sessions;

DROP INDEX IF EXISTS uploads_file_id_idx;
ALTER TABLE uploads DROP COLUMN IF EXISTS file_id;

DROP TABLE IF EXISTS file_sources;
DROP TABLE IF EXISTS files;
