#!/bin/sh
set -eu

for var in AUTH_DB_PASSWORD CHAT_DB_PASSWORD GAME_DB_PASSWORD CALLING_DB_PASSWORD RATING_DB_PASSWORD; do
	eval "value=\${$var:-}"
	if [ -z "$value" ]; then
		echo "init: $var is empty — check core/deploy/.env against .env.example" >&2
		exit 1
	fi
done

init_service_schema() {
	schema="$1"
	password="$2"

	psql -v ON_ERROR_STOP=1 \
		--username "$POSTGRES_USER" \
		--dbname "$POSTGRES_DB" \
		--set "schema=$schema" \
		--set "role=${schema}_service" \
		--set "password=$password" <<-'SQL'
			BEGIN;

			CREATE SCHEMA IF NOT EXISTS :"schema";

			SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'role', :'password')
			WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'role') \gexec
			GRANT USAGE, CREATE ON SCHEMA :"schema" TO :"role";

			ALTER ROLE :"role" SET search_path TO :"schema";

			COMMIT;
		SQL
}

init_service_schema auth "$AUTH_DB_PASSWORD"
init_service_schema chat "$CHAT_DB_PASSWORD"
init_service_schema game "$GAME_DB_PASSWORD"
init_service_schema calling "$CALLING_DB_PASSWORD"
init_service_schema rating "$RATING_DB_PASSWORD"

psql -v ON_ERROR_STOP=1 \
	--username "$POSTGRES_USER" \
	--dbname "$POSTGRES_DB" \
	-c 'REVOKE CREATE ON SCHEMA public FROM PUBLIC;'

echo "init: ensured schemas auth, chat, game, calling, rating and their service roles"
