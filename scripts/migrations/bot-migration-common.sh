#!/bin/sh

MIGRATE_BIN="${MIGRATE_BIN:-migrate}"
MIGRATION_PATH="${MIGRATION_PATH:-/migrations/bot}"

require_migration_config() {
	if [ -z "${MIGRATION_DATABASE_URL:-}" ]; then
		echo "MIGRATION_DATABASE_URL is required" >&2
		return 1
	fi
	if [ ! -d "$MIGRATION_PATH" ]; then
		echo "migration path does not exist: $MIGRATION_PATH" >&2
		return 1
	fi
}

run_migrate() {
	"$MIGRATE_BIN" \
		-path "$MIGRATION_PATH" \
		-database "$MIGRATION_DATABASE_URL" \
		"$@"
}

current_migration_version() {
	version_output="$(run_migrate version 2>&1)" && version_status=0 || version_status=$?
	if [ "$version_status" -ne 0 ]; then
		case "$version_output" in
			*"no migration"*)
				printf '0\n'
				return 0
				;;
		esac
		printf '%s\n' "$version_output" >&2
		return "$version_status"
	fi
	case "$version_output" in
		''|*[!0-9]*)
			echo "unexpected migration version output: $version_output" >&2
			return 1
			;;
	esac
	printf '%s\n' "$version_output"
}

require_positive_integer() {
	integer_name="$1"
	integer_value="$2"
	case "$integer_value" in
		''|*[!0-9]*|0)
			echo "$integer_name must be a positive integer" >&2
			return 1
			;;
	esac
}
