#!/usr/bin/env bash

set -euo pipefail

if [ "$#" -ne 3 ]; then
	echo "usage: $0 <env-file> <app-digest> <backup-digest>" >&2
	exit 2
fi

env_file="$1"
app_digest="$2"
backup_digest="$3"

if [ ! -f "$env_file" ]; then
	echo "deployment env file does not exist: $env_file" >&2
	exit 1
fi
for digest in "$app_digest" "$backup_digest"; do
	if [[ ! "$digest" =~ ^sha256:[0-9a-f]{64}$ ]]; then
		echo "invalid image digest: $digest" >&2
		exit 1
	fi
done

env_dir="$(dirname "$env_file")"
env_name="$(basename "$env_file")"
env_tmp="$(mktemp "${env_dir}/.${env_name}.images.XXXXXX")"
trap 'rm -f "$env_tmp"' EXIT

awk '!/^APP_DIGEST=/ && !/^BACKUP_DIGEST=/' "$env_file" > "$env_tmp"
printf '\nAPP_DIGEST=%s\nBACKUP_DIGEST=%s\n' \
	"$app_digest" "$backup_digest" >> "$env_tmp"
chmod --reference="$env_file" "$env_tmp"
mv "$env_tmp" "$env_file"
trap - EXIT
