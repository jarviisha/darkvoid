#!/usr/bin/env bash

set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
safe_runner="${script_dir}/run-bot-safe.sh"
destructive_runner="${script_dir}/run-bot-destructive.sh"
fake_migrate="${script_dir}/testdata/fake-migrate.sh"
test_dir="$(mktemp -d)"
trap 'rm -rf "$test_dir"' EXIT

state_file="${test_dir}/version"
log_file="${test_dir}/migrate.log"
backup_state="${test_dir}/backup-state"
mkdir -p "$backup_state" "${test_dir}/migrations"

run_safe() {
	env \
		MIGRATE_BIN="$fake_migrate" \
		MIGRATION_PATH="${test_dir}/migrations" \
		MIGRATION_DATABASE_URL='postgres://test' \
		FAKE_MIGRATION_STATE="$state_file" \
		FAKE_MIGRATION_LOG="$log_file" \
		sh "$safe_runner"
}

run_destructive() {
	env \
		MIGRATE_BIN="$fake_migrate" \
		MIGRATION_PATH="${test_dir}/migrations" \
		MIGRATION_DATABASE_URL='postgres://test' \
		FAKE_MIGRATION_STATE="$state_file" \
		FAKE_MIGRATION_LOG="$log_file" \
		BACKUP_STATE_DIR="$backup_state" \
		BOT_SCHEMA_DROP_APPROVAL="${BOT_SCHEMA_DROP_APPROVAL:-}" \
		BOT_DATA_HANDOFF_REFERENCE="${BOT_DATA_HANDOFF_REFERENCE:-}" \
		BOT_DROP_BACKUP_MAX_AGE_SECONDS=120 \
		BOT_DROP_RESTORE_MAX_AGE_SECONDS=120 \
		sh "$destructive_runner"
}

printf '7\n' > "$state_file"
: > "$log_file"
run_safe >/dev/null
[ "$(cat "$state_file")" = 8 ] || { echo 'safe runner did not advance version 7 to 8' >&2; exit 1; }
grep -Fq 'command=goto arg=8' "$log_file" || { echo 'safe runner did not use goto 8' >&2; exit 1; }

for version in 8 9; do
	printf '%s\n' "$version" > "$state_file"
	: > "$log_file"
	run_safe >/dev/null
	if grep -Fq 'command=goto' "$log_file"; then
		echo "safe runner attempted to move database from version $version" >&2
		exit 1
	fi
done

printf '10\n' > "$state_file"
if run_safe >/dev/null 2>&1; then
	echo 'safe runner accepted an unsupported newer migration version' >&2
	exit 1
fi

printf 'dirty\n' > "$state_file"
if run_safe >/dev/null 2>&1; then
	echo 'safe runner accepted a dirty migration state' >&2
	exit 1
fi

printf '8\n' > "$state_file"
now="$(date +%s)"
printf '%s\n' "$now" > "${backup_state}/last-success"
printf '%s\n' "$now" > "${backup_state}/last-restore-drill"
: > "$log_file"
if run_destructive >/dev/null 2>&1; then
	echo 'destructive runner accepted missing approval and handoff reference' >&2
	exit 1
fi
[ "$(cat "$state_file")" = 8 ] || { echo 'failed gate changed migration state' >&2; exit 1; }

BOT_SCHEMA_DROP_APPROVAL=drop-bot-schema-000009
BOT_DATA_HANDOFF_REFERENCE=change-1234
export BOT_SCHEMA_DROP_APPROVAL BOT_DATA_HANDOFF_REFERENCE
printf '%s\n' "$((now - 121))" > "${backup_state}/last-success"
if run_destructive >/dev/null 2>&1; then
	echo 'destructive runner accepted stale backup evidence' >&2
	exit 1
fi

printf '%s\n' "$now" > "${backup_state}/last-success"
run_destructive >/dev/null
[ "$(cat "$state_file")" = 9 ] || { echo 'approved runner did not apply migration 9' >&2; exit 1; }
grep -Eq 'command=up arg=1 pgoptions=.*darkvoid\.bot_schema_drop_approval=drop-bot-schema-000009' "$log_file" \
	|| { echo 'approved runner did not pass the SQL session guard' >&2; exit 1; }

: > "$log_file"
unset BOT_SCHEMA_DROP_APPROVAL BOT_DATA_HANDOFF_REFERENCE
run_destructive >/dev/null
if grep -Fq 'command=up' "$log_file"; then
	echo 'already-applied destructive runner attempted another migration' >&2
	exit 1
fi

grep -Fq "current_setting('darkvoid.bot_schema_drop_approval', TRUE)" \
	"${script_dir}/../../migrations/bot/000009_drop_bot_schema.up.sql" \
	|| { echo 'migration 000009 is missing its SQL approval guard' >&2; exit 1; }

echo 'bot migration gate tests passed'
