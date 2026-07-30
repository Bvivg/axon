#!/bin/sh
# One database, one schema per service, one login role per schema.
#
# Each role gets USAGE and CREATE on its own schema and nothing anywhere else,
# so a cross-schema JOIN fails at the database instead of at review time. That
# is the difference between schema isolation being a rule and being enforced.
#
# Tables are not created here. Migrations own them, per service, under
# services/<name>/migrations/, applied independently of each other.
#
# The Postgres entrypoint runs this once, when the data volume is first
# created. An existing volume will not pick up changes — `make reset` drops the
# volume and replays it.
set -eu

# Passwords come from the environment via docker-compose. Compose supplies
# development defaults, so an empty value here means something is actually
# misconfigured rather than merely unset.
for var in AUTH_DB_PASSWORD CHAT_DB_PASSWORD GAME_DB_PASSWORD CALLING_DB_PASSWORD; do
	eval "value=\${$var:-}"
	if [ -z "$value" ]; then
		echo "init: $var is empty — check core/deploy/.env against .env.example" >&2
		exit 1
	fi
done

# ON_ERROR_STOP matters: without it a half-initialised volume would look
# healthy, and the failure would surface much later as a missing role.
#
# The schema name and role name are interpolated as identifiers (:"name") and
# the password as a literal (:'name'), so psql quotes each correctly.
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

			CREATE ROLE :"role" LOGIN PASSWORD :'password';
			GRANT USAGE, CREATE ON SCHEMA :"schema" TO :"role";

			-- Pin the role's search_path so a query that forgets to qualify a
			-- table cannot silently resolve somewhere else.
			ALTER ROLE :"role" SET search_path TO :"schema";

			COMMIT;
		SQL
}

init_service_schema auth "$AUTH_DB_PASSWORD"
init_service_schema chat "$CHAT_DB_PASSWORD"
init_service_schema game "$GAME_DB_PASSWORD"
init_service_schema calling "$CALLING_DB_PASSWORD"

# Nobody should be writing to public. Leaving it writable only invites tables
# parked there "for now".
psql -v ON_ERROR_STOP=1 \
	--username "$POSTGRES_USER" \
	--dbname "$POSTGRES_DB" \
	-c 'REVOKE CREATE ON SCHEMA public FROM PUBLIC;'

echo "init: created schemas auth, chat, game, calling and their service roles"
