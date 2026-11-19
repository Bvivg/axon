#!/usr/bin/env bash
set -euo pipefail
umask 077

die() { printf '%s\n' "$*" >&2; exit 1; }

[[ $# == 3 ]] || die 'Usage: backup-restore.sh backup|restore DIRECTORY COMPOSE_PROJECT'
mode=$1
input_dir=$2
project=$3
[[ "$project" =~ ^[a-z0-9][a-z0-9_-]*$ ]] || die 'Invalid Compose project name'
deploy=$(cd "$(dirname "$0")" && pwd -P)
repo=$(cd "$deploy/../.." && pwd -P)
env_file=${AXON_ENV_FILE:-"$deploy/.env"}
[[ -f "$env_file" ]] || die 'Set AXON_ENV_FILE to a Compose env file'
command -v jq >/dev/null || die 'jq is required'
sha=(shasum -a 256)
if command -v sha256sum >/dev/null; then sha=(sha256sum); fi
parent=$(cd "$(dirname "$input_dir")" && pwd -P)
folder=$(basename "$input_dir")
[[ "$folder" != . && "$folder" != .. ]] || die 'Specify a dedicated backup directory'
backup="$parent/$folder"
case "$backup" in "$repo"|"$repo"/*) die 'Choose a backup directory outside the Git workspace';; esac
[[ ! -L "$backup" ]] || die 'The backup directory must not be a symlink'

scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT
started=$SECONDS
export AXON_SNAPSHOT_PASSWORD
AXON_SNAPSHOT_PASSWORD=$(openssl rand -hex 24)
common=(docker compose --env-file "$env_file")
source_stack=("${common[@]}" -p "$project" -f "$deploy/docker-compose.yml")
recovery=("${common[@]}" -p "$project" -f "$deploy/docker-compose.backup.yml")
buckets=(avatars chat-attachments)

db_counts() {
  "$@" exec -T postgres sh -ec 'psql -XqAt -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "
    SELECT (SELECT count(*) FROM chat.messages), (SELECT count(*) FROM chat.files);
  "'
}

s3() {
  "${recovery[@]}" exec -T snapshot sh -ec '
    set -o pipefail
    cfg=$(mktemp -d)
    trap "rm -rf \"$cfg\"" EXIT
    export MC_CONFIG_DIR=$cfg
    mc alias set snap http://127.0.0.1:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null
    mc alias set peer "$AXON_PEER_MINIO_URL" "$AXON_PEER_MINIO_USER" "$AXON_PEER_MINIO_PASSWORD" >/dev/null
    if [ "$1" = hash ]; then
      mc cat "$2" | sha256sum
    else
      mc "$@"
    fi
  ' sh "$@" </dev/null
}

inventory() {
  s3 ls --json --recursive "$1/$2" |
    jq -cS 'if .status != "success" then error("MinIO listing failed") elif .type == "file" then {key,size} else empty end' |
    LC_ALL=C sort
}

copy_and_verify() {
  local from=$1 to=$2 bucket=$3 reference=$4
  local key path item
  s3 mb "$to/$bucket" >/dev/null
  s3 mirror --retry --quiet "$from/$bucket" "$to/$bucket" >"$scratch/mirror.log"
  inventory "$from" "$bucket" >"$scratch/source.jsonl"
  inventory "$to" "$bucket" >"$scratch/destination.jsonl"
  cmp -s "$scratch/source.jsonl" "$scratch/destination.jsonl" || die "$bucket inventory differs after copy"
  if [[ "$mode" == backup ]]; then
    cp "$scratch/source.jsonl" "$reference.inventory"
  else
    cmp -s "$reference.inventory" "$scratch/destination.jsonl" || die "$bucket differs from saved inventory"
  fi
  head -n 3 "$scratch/source.jsonl" >"$scratch/sample.jsonl"
  : >"$scratch/sample.sha256"
  while IFS= read -r item; do
    key=$(jq -r '.key' <<< "$item")
    for path in "$from" "$to"; do
      s3 hash "$path/$bucket/$key" >"$scratch/$path.sha256"
      s3 stat --json "$path/$bucket/$key" | jq -ceS '
        if .status != "success" then error("MinIO stat failed") else
          .metadata // {} | with_entries(select(.key | ascii_downcase | test("^content-(type|disposition|encoding)$")))
        end
      ' >"$scratch/$path.headers"
    done
    cmp -s "$scratch/$from.sha256" "$scratch/$to.sha256" || die "$bucket SHA-256 mismatch"
    cmp -s "$scratch/$from.headers" "$scratch/$to.headers" || die "$bucket media headers differ"
    cat "$scratch/$to.sha256" >>"$scratch/sample.sha256"
  done <"$scratch/sample.jsonl"
  cmp -s <(awk 'END { print NR }' "$scratch/sample.jsonl") \
    <(awk 'END { print NR }' "$scratch/sample.sha256") || die "$bucket sample was incomplete"
  if [[ "$mode" == backup ]]; then
    cp "$scratch/sample.sha256" "$reference.samples"
  else
    cmp -s "$reference.samples" "$scratch/sample.sha256" || die "$bucket SHA-256 differs from the saved sample"
  fi
  printf '%s objects=%s bytes=%s sha256_samples=%s\n' "$bucket" \
    "$(awk 'END { print NR }' "$scratch/destination.jsonl")" \
    "$(jq -s 'map(.size) | add // 0' <"$scratch/destination.jsonl")" \
    "$(awk 'END { print NR }' "$scratch/sample.sha256")"
}

case "$mode" in
  backup)
    running=$("${source_stack[@]}" ps --status running --status restarting --status paused --services)
    if grep -Eq '^(auth|chat|game)$' <<< "$running"; then
      die 'Stop source Auth, Chat and Game and quiesce external database and bucket writes first'
    fi
    mkdir -- "$backup"
    recovery=("${common[@]}" -p "axon-snapshot-$(openssl rand -hex 6)" -f "$deploy/docker-compose.backup.yml" -f "$scratch/source.yml")
    export AXON_SOURCE_NETWORK="${project}_default"
    export AXON_PEER_MINIO_URL=http://minio:9000
    AXON_PEER_MINIO_USER=$("${source_stack[@]}" exec -T minio sh -ec 'printf %s "$MINIO_ROOT_USER"')
    AXON_PEER_MINIO_PASSWORD=$("${source_stack[@]}" exec -T minio sh -ec 'printf %s "$MINIO_ROOT_PASSWORD"')
    export AXON_PEER_MINIO_USER AXON_PEER_MINIO_PASSWORD
    cat >"$scratch/source.yml" <<'YAML'
services:
  snapshot:
    networks: [default, source]
networks:
  source:
    external: true
    name: ${AXON_SOURCE_NETWORK}
YAML
    trap '"${recovery[@]}" down --volumes >/dev/null || :; rm -rf -- "$scratch"' EXIT
    "${recovery[@]}" up -d --wait snapshot
    db_counts "${source_stack[@]}" >"$backup/db-counts"
    "${source_stack[@]}" exec -T postgres sh -ec '
      exec pg_dump -Fc -U "$POSTGRES_USER" -d "$POSTGRES_DB"
    ' >"$backup/postgres.dump"
    for bucket in "${buckets[@]}"; do
      copy_and_verify peer snap "$bucket" "$backup/$bucket"
    done
    "${recovery[@]}" stop snapshot
    "${recovery[@]}" run --rm --no-deps -T archive -cf - . >"$backup/minio.tar"
    (cd "$backup" && "${sha[@]}" postgres.dump minio.tar db-counts ./*.inventory ./*.samples >SHA256SUMS)
    ;;
  restore)
    [[ "$project" == axon-restore-* ]] || die 'Restore requires a new project named axon-restore-*'
    containers=$(docker ps -aq --filter "label=com.docker.compose.project=$project")
    volumes=$(docker volume ls -q --filter "label=com.docker.compose.project=$project")
    networks=$(docker network ls -q --filter "label=com.docker.compose.project=$project")
    [[ -z "$containers$volumes$networks" ]] || die 'Restore project already exists'
    (cd "$backup" && "${sha[@]}" -c SHA256SUMS >/dev/null)
    export AXON_PEER_MINIO_URL=http://minio:9000
    "${recovery[@]}" up -d --wait postgres minio
    AXON_PEER_MINIO_USER=$("${recovery[@]}" exec -T minio sh -ec 'printf %s "$MINIO_ROOT_USER"')
    AXON_PEER_MINIO_PASSWORD=$("${recovery[@]}" exec -T minio sh -ec 'printf %s "$MINIO_ROOT_PASSWORD"')
    export AXON_PEER_MINIO_USER AXON_PEER_MINIO_PASSWORD
    "${recovery[@]}" run --rm --no-deps -T archive -xf - <"$backup/minio.tar"
    "${recovery[@]}" up -d --wait snapshot
    "${recovery[@]}" exec -T postgres sh -ec '
      exec pg_restore --clean --if-exists --single-transaction -U "$POSTGRES_USER" -d "$POSTGRES_DB"
    ' <"$backup/postgres.dump"
    db_counts "${recovery[@]}" >"$scratch/db-counts"
    cmp -s "$backup/db-counts" "$scratch/db-counts" || die 'Restored DB counts differ from backup'
    for bucket in "${buckets[@]}"; do
      copy_and_verify snap peer "$bucket" "$backup/$bucket"
    done
    s3 anonymous set download peer/avatars >/dev/null
    s3 anonymous set none peer/chat-attachments >/dev/null
    "${recovery[@]}" stop snapshot
    ;;
  *)
    die 'Expected backup or restore'
    ;;
esac

IFS='|' read -r messages files <"$backup/db-counts"
printf '%s messages=%s files=%s postgres_dump_bytes=%s minio_archive_bytes=%s elapsed_seconds=%s\n' \
  "$mode" "$messages" "$files" "$(wc -c <"$backup/postgres.dump" | tr -d ' ')" \
  "$(wc -c <"$backup/minio.tar" | tr -d ' ')" "$((SECONDS - started))"
