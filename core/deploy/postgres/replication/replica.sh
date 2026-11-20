#!/bin/sh
set -eu

if [ ! -s "$PGDATA/PG_VERSION" ]; then
	until pg_isready -q -h "$PRIMARY_HOST" -p "$PRIMARY_PORT"; do
		sleep 1
	done
	rm -rf "$PGDATA" "$PGDATA.partial"
	PGPASSWORD="$REPLICATION_PASSWORD" pg_basebackup \
		--host="$PRIMARY_HOST" --port="$PRIMARY_PORT" --username=replicator --no-password \
		--pgdata="$PGDATA.partial" --slot="$REPLICATION_SLOT" \
		--wal-method=stream --write-recovery-conf --checkpoint=fast
	mv "$PGDATA.partial" "$PGDATA"
	echo "replica: base backup from $PRIMARY_HOST:$PRIMARY_PORT restored"
fi

exec docker-entrypoint.sh postgres "$@"
