#!/usr/bin/env bash
# Preserve the existing storage identity and runtime contract across refactors.
set -euo pipefail
repo_root="$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)"
cd "$repo_root"
export DB_PASSWORD=characterization JWT_SECRET=characterization
APP_DIGEST="sha256:$(printf 'a%.0s' {1..64})"
BACKUP_DIGEST="sha256:$(printf 'b%.0s' {1..64})"
export APP_DIGEST BACKUP_DIGEST
export SERVER_PORT=8080 APP_HOST_PORT=8080 SERVER_BIND=127.0.0.1 COMPOSE_NETWORK_NAME=darkvoid
unset COMPOSE_FILE COMPOSE_PROFILES
if [ -f compose.prod.yml ]; then
	files=(-f compose.yml -f compose.prod.yml)
else
	files=(-f docker-compose.prod.yml)
fi
docker compose --env-file /dev/null -p darkvoid-dev "${files[@]}" --profile destructive-migration \
	config --no-env-resolution --format json | jq -e '
	.services.app.stop_grace_period == "40s" and
	.services.app.healthcheck.test == ["CMD", "wget", "-qO-", "http://localhost:8080/health"] and
	.services.app.ports[0].host_ip == "127.0.0.1" and
	.services.app.ports[0].target == 8080 and
	.services["pg-backup"].read_only == true and
	.services["migrate-bot-destructive"].profiles == ["destructive-migration"] and
	.volumes.postgres_data.name == "darkvoid-dev_postgres_data" and
	.volumes.redis_data.name == "darkvoid-dev_redis_data" and
	.volumes.uploads.name == "darkvoid-dev_uploads" and
	.volumes.backup_state.name == "darkvoid-dev_backup_state" and
	.networks.default.name == "darkvoid"
' >/dev/null
echo 'Compose characterization tests passed'
