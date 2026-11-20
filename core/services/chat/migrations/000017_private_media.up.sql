CREATE INDEX files_object_keys_idx ON files USING gin (object_keys);
