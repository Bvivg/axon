#!/bin/sh
set -eu

bootstrap=${KAFKA_BOOTSTRAP:-kafka:29092}
partitions=${GAME_JOURNAL_PARTITIONS:-24}
replication=${GAME_JOURNAL_REPLICATION:-1}
insync=${GAME_JOURNAL_MIN_INSYNC:-1}
existing=$(kafka-topics --bootstrap-server "$bootstrap" --list)

create() {
	topic=$1
	shift
	if printf '%s\n' "$existing" | grep -qxF "$topic"; then
		return 0
	fi
	kafka-topics --bootstrap-server "$bootstrap" --create --if-not-exists --topic "$topic" "$@"
}

create game.session-events.v1 --partitions "$partitions" --replication-factor "$replication" \
	--config min.insync.replicas="$insync" --config retention.ms=-1 --config cleanup.policy=delete
create game.partition-leases.v1 --partitions "$partitions" --replication-factor "$replication" \
	--config min.insync.replicas="$insync"

create game.analysis_requested.v1 --partitions 6

for topic in auth.audit.v1 game.finished.v1 rating.changed.v1 tournament.finished.v1; do
	create "$topic" --config retention.ms=-1 --config cleanup.policy=delete
done

for topic in auth.user.registered.v1 chat.message chat.message.v1 call.ended.v1 \
	tournament.round_started.v1 tournament.forfeit.v1 tournament.cancelled.v1 cdc.rejected.v1 __debezium-heartbeat.axon; do
	create "$topic"
done
