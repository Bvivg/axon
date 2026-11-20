#!/bin/sh
set -eu

userlist=/run/pgbouncer/userlist.txt

quote() {
	printf '"%s"' "$(printf '%s' "$1" | sed 's/"/""/g')"
}

add() {
	if [ -z "$2" ]; then
		echo "pgbouncer: password for $1 is empty" >&2
		exit 1
	fi
	printf '%s %s\n' "$(quote "$1")" "$(quote "$2")" >>"$userlist"
}

umask 077
: >"$userlist"
add auth_service "${AUTH_DB_PASSWORD:-}"
add chat_service "${CHAT_DB_PASSWORD:-}"
add game_service "${GAME_DB_PASSWORD:-}"
add calling_service "${CALLING_DB_PASSWORD:-}"

exec pgbouncer /etc/pgbouncer/pgbouncer.ini
