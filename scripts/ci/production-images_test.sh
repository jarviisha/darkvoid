#!/usr/bin/env bash
set -euo pipefail
repo_root="$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)"
cd "$repo_root"
fail() { printf 'production image policy: %s\n' "$*" >&2; exit 1; }
export DB_PASSWORD=test JWT_SECRET=test APP_IMAGE=ghcr.io/jarviisha/darkvoid
export BACKUP_IMAGE=ghcr.io/jarviisha/darkvoid-backup
APP_DIGEST="sha256:$(printf 'a%.0s' {1..64})"
BACKUP_DIGEST="sha256:$(printf 'b%.0s' {1..64})"
export APP_DIGEST BACKUP_DIGEST
unset COMPOSE_FILE COMPOSE_PROFILES
images="$(docker compose --env-file /dev/null -f compose.yml -f compose.prod.yml \
	--profile tools --profile destructive-migration config --images)"
while IFS= read -r reference; do
	[[ "$reference" =~ ^[^[:space:]@]+@sha256:[0-9a-f]{64}$ ]] \
		|| fail "mutable or invalid production image: $reference"
done <<< "$images"
base_image_count=0
while IFS='=' read -r name reference; do
	base_image_count=$((base_image_count + 1))
	[[ "$reference" =~ ^[^[:space:]@]+:[^[:space:]@]+@sha256:[0-9a-f]{64}$ ]] \
		|| fail "$name is not pinned"
done < <(sed -nE 's/^ARG ((GO|RUNTIME|POSTGRES)_IMAGE)=(.*)$/\1=\3/p' Dockerfile docker/backup/Dockerfile)
[ "$base_image_count" -eq 3 ] || fail 'missing pinned base image'
# Literal workflow expressions, not shell interpolation.
# shellcheck disable=SC2016
for required in \
	'app_digest: ${{ steps.build.outputs.digest }}' \
	'backup_digest: ${{ steps.build_backup.outputs.digest }}' \
	'github.event.workflow_run.head_sha' \
	'create-release.sh' \
	'deploy-release.sh' \
	'test-container-runtime'; do
	grep -Fq "$required" .github/workflows/cd.yml || fail "CD is missing: $required"
done
echo 'Production image pinning tests passed'
