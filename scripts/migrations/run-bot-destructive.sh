#!/bin/sh

set -eu

script_dir="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
# shellcheck source=bot-migration-common.sh
. "${script_dir}/bot-migration-common.sh"

approval_token="drop-bot-schema-000009"
safe_version="${BOT_SAFE_MIGRATION_VERSION:-8}"
retired_version="${BOT_RETIRED_MIGRATION_VERSION:-9}"
backup_state_dir="${BACKUP_STATE_DIR:-/backup-state}"
backup_max_age="${BOT_DROP_BACKUP_MAX_AGE_SECONDS:-172800}"
restore_max_age="${BOT_DROP_RESTORE_MAX_AGE_SECONDS:-604800}"

require_fresh_state() {
	state_name="$1"
	state_path="$2"
	max_age="$3"
	if [ ! -r "$state_path" ]; then
		echo "$state_name evidence is missing: $state_path" >&2
		return 1
	fi
	state_epoch="$(cat "$state_path")"
	case "$state_epoch" in
		''|*[!0-9]*)
			echo "$state_name evidence is not a Unix timestamp" >&2
			return 1
			;;
	esac
	now_epoch="$(date +%s)"
	state_age="$((now_epoch - state_epoch))"
	if [ "$state_age" -lt 0 ]; then
		echo "$state_name evidence is dated in the future" >&2
		return 1
	fi
	if [ "$state_age" -gt "$max_age" ]; then
		echo "$state_name evidence is stale: age=${state_age}s maximum=${max_age}s" >&2
		return 1
	fi
}

require_migration_config
require_positive_integer BOT_SAFE_MIGRATION_VERSION "$safe_version"
require_positive_integer BOT_RETIRED_MIGRATION_VERSION "$retired_version"
require_positive_integer BOT_DROP_BACKUP_MAX_AGE_SECONDS "$backup_max_age"
require_positive_integer BOT_DROP_RESTORE_MAX_AGE_SECONDS "$restore_max_age"

current_version="$(current_migration_version)"
if [ "$current_version" -eq "$retired_version" ]; then
	echo "bot schema retirement migration $retired_version is already applied"
	exit 0
fi
if [ "$current_version" -ne "$safe_version" ]; then
	echo "bot migrations must be at version $safe_version before retirement; current=$current_version" >&2
	exit 1
fi

if [ "${BOT_SCHEMA_DROP_APPROVAL:-}" != "$approval_token" ]; then
	echo "BOT_SCHEMA_DROP_APPROVAL must exactly equal $approval_token" >&2
	exit 1
fi
case "${BOT_DATA_HANDOFF_REFERENCE:-}" in
	''|true|TRUE|yes|YES)
		echo "BOT_DATA_HANDOFF_REFERENCE must identify the approved external-bot handoff/change record" >&2
		exit 1
		;;
esac

require_fresh_state "successful backup" "${backup_state_dir}/last-success" "$backup_max_age"
require_fresh_state "restore drill" "${backup_state_dir}/last-restore-drill" "$restore_max_age"

echo "approved bot schema retirement: handoff=${BOT_DATA_HANDOFF_REFERENCE}"
PGOPTIONS="${PGOPTIONS:-} -c darkvoid.bot_schema_drop_approval=${approval_token}"
export PGOPTIONS
run_migrate up 1

applied_version="$(current_migration_version)"
if [ "$applied_version" -ne "$retired_version" ]; then
	echo "bot retirement ended at unexpected migration version $applied_version" >&2
	exit 1
fi
echo "bot schema retirement migration $retired_version completed; data rollback requires snapshot restore"
