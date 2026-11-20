#!/bin/sh
set -eu

content=$(sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' -e "s/$(printf '\t')/\\\\t/g" "$SCHEMA_FILE" | sed -e ':a' -e 'N' -e '$!ba' -e 's/\n/\\n/g')
body=$(mktemp)
printf '{"artifactId":"%s","artifactType":"PROTOBUF","firstVersion":{"content":{"content":"%s\\n","contentType":"application/x-protobuf"}}}' "$SCHEMA_ARTIFACT" "$content" > "$body"

response=$(curl -fsS -X POST -H 'Content-Type: application/json' --data @"$body" \
	"$REGISTRY_URL/apis/registry/v3/groups/$SCHEMA_GROUP/artifacts?ifExists=FIND_OR_CREATE_VERSION")
rm -f "$body"

version=$(printf '%s' "$response" | sed -n 's/.*"version":"\([^"]*\)".*/\1/p')
echo "schema: $SCHEMA_GROUP/$SCHEMA_ARTIFACT is registered as version ${version:-unknown}"
