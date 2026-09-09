#!/bin/sh
# One ordered job; every module keeps its existing version table.
set -eu
script_dir="$(CDPATH='' cd -- "$(dirname "$0")" && pwd)"
migration_root="${MIGRATION_ROOT:-/migrations}"
migrate_bin="${MIGRATE_BIN:-migrate}"
for module in user post notification bot settings; do
	echo "migrating $module"
	if [ "$module" = bot ]; then
		MIGRATION_PATH="$migration_root/bot" \
		MIGRATION_DATABASE_URL='postgres:///?x-migrations-table=schema_migrations_bot' \
		BOT_SAFE_MIGRATION_VERSION=8 BOT_RETIRED_MIGRATION_VERSION=9 \
		sh "$script_dir/run-bot-safe.sh"
	else
		"$migrate_bin" -path "$migration_root/$module" \
			-database "postgres:///?x-migrations-table=schema_migrations_$module" up
	fi
done
