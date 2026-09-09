#!/usr/bin/env bash
# Only disposable containers are used; the developer's DB_* settings are ignored.
set -euo pipefail
repo_root="$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)"
test_dir="$(mktemp -d)"
container="darkvoid-migrations-test-$$"
postgres_image='postgres:16.14-alpine@sha256:57c72fd2a128e416c7fcc499958864df5301e940bca0a56f58fddf30ffc07777'
migrate_image='migrate/migrate:v4.19.1@sha256:cc4ad8e19d66791e3689405d9a028ce6e9614f32032db14acda1469f7201d6e4'
cleanup() {
	status="$?"
	if [ "$status" -ne 0 ]; then docker logs --tail 30 "$container" || true; fi
	docker stop "$container" >/dev/null 2>&1 || true
	rm -rf "$test_dir"
	exit "$status"
}
trap cleanup EXIT
docker run --detach --rm --name "$container" --tmpfs /var/lib/postgresql/data \
	-e POSTGRES_HOST_AUTH_METHOD=trust "$postgres_image" >/dev/null
ready=false
# The initialization server accepts Unix sockets before TCP is available.
# Probe the same TCP endpoint used by the migration runner.
for _attempt in {1..60}; do
	if docker exec "$container" pg_isready -h 127.0.0.1 -U postgres >/dev/null 2>&1; then ready=true; break; fi
	sleep 1
done
[ "$ready" = true ] || { echo 'PostgreSQL did not become ready' >&2; exit 1; }
migrate_module() {
	module="$1"; shift
	docker run --rm --network "container:$container" \
		-v "$repo_root/migrations:/migrations:ro" "$migrate_image" \
		-path "/migrations/$module" \
		-database "postgres://postgres@127.0.0.1/postgres?sslmode=disable&x-migrations-table=schema_migrations_$module" "$@"
}
run_all() {
	docker run --rm --network "container:$container" \
		-e PGHOST=127.0.0.1 -e PGUSER=postgres -e PGDATABASE=postgres -e PGSSLMODE=disable \
		-v "$repo_root/migrations:/migrations:ro" \
		-v "$repo_root/scripts/migrations:/migration-guard:ro" \
		--entrypoint /bin/sh "$migrate_image" /migration-guard/run-all-safe.sh
}
dump_schema() {
	docker exec "$container" pg_dump -U postgres --schema-only --no-owner --no-privileges \
		--schema=usr --schema=post --schema=notification --schema=settings postgres \
		| sed '/^\\restrict /d; /^\\unrestrict /d'
}
run_all
dump_schema > "$test_dir/before.sql"
docker exec -i "$container" psql -X -U postgres -v ON_ERROR_STOP=1 < "$repo_root/scripts/migrations/testdata/assert-baseline.sql"
# A second up must preserve existing rows, settings and trigger effects.
run_all
docker exec "$container" psql -X -U postgres -v ON_ERROR_STOP=1 -c \
	"DO \$\$ BEGIN IF NOT EXISTS (SELECT 1 FROM usr.users WHERE username = 'baseline_author') THEN RAISE EXCEPTION 'rerun lost data'; END IF; END \$\$;"
for module in settings notification post user; do migrate_module "$module" down -all; done
remaining="$(docker exec "$container" psql -X -U postgres -Atc \
	"SELECT count(*) FROM pg_namespace WHERE nspname IN ('usr', 'post', 'notification', 'settings', 'bot')")"
[ "$remaining" = 0 ] || { echo 'down left application schemas behind' >&2; exit 1; }
run_all
dump_schema > "$test_dir/after.sql"
diff -u "$test_dir/before.sql" "$test_dir/after.sql"
docker exec -i "$container" psql -X -U postgres -v ON_ERROR_STOP=1 < "$repo_root/scripts/migrations/testdata/assert-baseline.sql"
echo 'Migration up/rerun/down/up, schema round trip and SQL behavior passed'
