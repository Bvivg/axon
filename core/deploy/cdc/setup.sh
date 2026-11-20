#!/bin/sh
set -eu

until pg_isready -q; do
	sleep 1
done

psql -v ON_ERROR_STOP=1 --no-psqlrc \
	--set "password=$DEBEZIUM_DB_PASSWORD" \
	--set "publication=$CDC_PUBLICATION" <<'SQL'
SELECT 'CREATE ROLE debezium WITH LOGIN REPLICATION'
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'debezium') \gexec
ALTER ROLE debezium WITH LOGIN REPLICATION PASSWORD :'password';

CREATE TEMP TABLE outbox_tables AS
SELECT n.nspname AS schema_name, c.oid AS relid
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relname = 'outbox' AND c.relkind = 'r' AND n.nspname IN ('auth', 'chat', 'game', 'rating', 'tournament');

SELECT format('GRANT USAGE ON SCHEMA %I TO debezium', nspname)
FROM pg_namespace
WHERE nspname IN ('auth', 'chat', 'game', 'rating', 'tournament') \gexec

SELECT format('GRANT SELECT ON TABLE %I.outbox TO debezium', schema_name)
FROM outbox_tables \gexec

SELECT format('CREATE PUBLICATION %I WITH (publish = %L)', :'publication', 'insert')
WHERE NOT EXISTS (SELECT 1 FROM pg_publication WHERE pubname = :'publication') \gexec
SELECT format('ALTER PUBLICATION %I SET (publish = %L)', :'publication', 'insert') \gexec

SELECT format('ALTER PUBLICATION %I ADD TABLE %I.outbox', :'publication', t.schema_name)
FROM outbox_tables t
WHERE NOT EXISTS (
	SELECT 1 FROM pg_publication_rel pr
	JOIN pg_publication p ON p.oid = pr.prpubid
	WHERE p.pubname = :'publication' AND pr.prrelid = t.relid
) \gexec

SELECT schemaname || '.' || tablename AS published
FROM pg_publication_tables
WHERE pubname = :'publication'
ORDER BY 1;
SQL

echo "cdc: role debezium and publication $CDC_PUBLICATION are ready"
