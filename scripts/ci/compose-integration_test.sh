#!/usr/bin/env bash
# Fresh, isolated dev stack; cleanup is limited to this generated project.
set -euo pipefail
repo_root="$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)"
test_dir="$(mktemp -d)"
project="darkvoid-compose-test-$$"
test_env="$test_dir/.env"
printf '%s\n' "LOCAL_APP_IMAGE=${APP_TEST_IMAGE:-darkvoid:ci}" \
	'APP_HOST_PORT=0' 'JWT_SECRET=integration-secret' 'ENVIRONMENT=development' \
	'STORAGE_PROVIDER=local' 'CODOHUE_ENABLED=false' > "$test_env"
printf '%s\n' "DB_PASSWORD='integration\$p#word'" >> "$test_env"
compose() {
	env -i PATH="$PATH" docker compose --env-file "$test_env" -p "$project" \
		-f "$repo_root/compose.yml" -f "$repo_root/compose.dev.yml" "$@"
}
cleanup() {
	status="$?"
	if [ "$status" -ne 0 ]; then compose logs --tail 50 || true; fi
	compose down --volumes --remove-orphans >/dev/null 2>&1 || true
	rm -rf "$test_dir"
	exit "$status"
}
trap cleanup EXIT
compose up -d --no-build --wait --wait-timeout 120 app
address="$(compose port app 8080)"
curl --fail --silent --show-error --max-time 10 "http://$address/health"
test "$(compose exec -T app printenv DB_PASSWORD)" = "integration\$p#word"
test "$(compose exec -T postgres psql -U postgres -d darkvoid -Atc \
	'SELECT version::text || chr(58) || dirty::text FROM schema_migrations_bot')" = 8:false
# The second run must be safe and idempotent against a populated schema.
compose run --rm --no-deps migrate
echo 'Fresh Compose stack and migration rerun passed'
