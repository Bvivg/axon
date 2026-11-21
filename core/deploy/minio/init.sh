#!/bin/sh
set -eu

mc alias set local http://minio:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null
mc mb --ignore-existing local/avatars
mc anonymous set download local/avatars
mc mb --ignore-existing local/chat-attachments
mc anonymous set none local/chat-attachments

grant() {
	user=$1
	secret=$2
	policy=$3
	mc admin policy create local "$policy" "/minio/$policy.json" >/dev/null
	mc admin user add local "$user" "$secret" >/dev/null
	case "$(mc admin user info local "$user")" in
	*"$policy"*) ;;
	*) mc admin policy attach local "$policy" --user "$user" >/dev/null ;;
	esac
	echo "minio: $user is limited to $policy"
}

grant "$AVATARS_ACCESS_KEY" "$AVATARS_SECRET_KEY" avatars-bucket
grant "$ATTACHMENTS_ACCESS_KEY" "$ATTACHMENTS_SECRET_KEY" attachments-bucket
