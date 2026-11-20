#!/bin/sh
set -eu

until pg_isready -q; do
	sleep 1
done

psql -v ON_ERROR_STOP=1 --no-psqlrc \
	--set "password=$REPLICATION_PASSWORD" \
	--set "slot=$REPLICATION_SLOT" <<'SQL'
SELECT 'CREATE ROLE replicator WITH REPLICATION LOGIN'
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'replicator') \gexec
ALTER ROLE replicator WITH REPLICATION LOGIN PASSWORD :'password';
SELECT slot_name FROM pg_create_physical_replication_slot(:'slot')
WHERE NOT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = :'slot');
SQL

echo "replication: role replicator and slot $REPLICATION_SLOT are ready"
