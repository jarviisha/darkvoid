#!/bin/sh

set -eu

script_dir="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
# shellcheck source=bot-migration-common.sh
. "${script_dir}/bot-migration-common.sh"

safe_version="${BOT_SAFE_MIGRATION_VERSION:-8}"
retired_version="${BOT_RETIRED_MIGRATION_VERSION:-9}"

require_migration_config
require_positive_integer BOT_SAFE_MIGRATION_VERSION "$safe_version"
require_positive_integer BOT_RETIRED_MIGRATION_VERSION "$retired_version"
if [ "$safe_version" -ge "$retired_version" ]; then
	echo "safe migration version must be below retired migration version" >&2
	exit 1
fi

current_version="$(current_migration_version)"
if [ "$current_version" -lt "$safe_version" ]; then
	echo "advancing bot migrations from $current_version to safe version $safe_version"
	run_migrate goto "$safe_version"
	exit 0
fi
if [ "$current_version" -eq "$safe_version" ]; then
	echo "bot migrations are at safe version $safe_version; destructive version $retired_version requires approval"
	exit 0
fi
if [ "$current_version" -eq "$retired_version" ]; then
	echo "bot schema retirement migration $retired_version is already applied; no automatic downgrade"
	exit 0
fi

echo "bot migration version $current_version is newer than this release supports; refusing automatic downgrade" >&2
exit 1
