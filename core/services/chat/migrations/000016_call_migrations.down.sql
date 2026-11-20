DO $$
BEGIN
    IF EXISTS(SELECT 1 FROM call_migrations) THEN
        RAISE EXCEPTION 'Migration history exists; disable new operations and drain instead';
    END IF;
END $$;
DROP TABLE call_migrations;
ALTER TABLE call_endpoints DROP COLUMN supports_migration;
