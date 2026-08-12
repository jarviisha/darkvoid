#!/usr/bin/env bash

set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
backup_script="${script_dir}/postgres-restic.sh"
test_state="$(mktemp -d)"
trap 'rm -rf "$test_state"' EXIT INT TERM

run_validation() {
	repository="$1"
	password="$2"
	webhook="$3"
	env \
		BACKUP_SOURCE_ONLY=1 \
		BACKUP_STATE_DIR="$test_state" \
		BACKUP_RESTORE_DATABASE=darkvoid_restore_drill \
		BACKUP_INTERVAL_SECONDS=86400 \
		BACKUP_RESTORE_DRILL_INTERVAL_SECONDS=604800 \
		BACKUP_KEEP_DAILY=14 \
		BACKUP_KEEP_WEEKLY=8 \
		BACKUP_KEEP_MONTHLY=12 \
		BACKUP_MAX_AGE_SECONDS=172800 \
		BACKUP_RESTIC_TAG=darkvoid-postgres \
		BACKUP_RESTIC_HOST=darkvoid-production \
		RESTIC_REPOSITORY="$repository" \
		RESTIC_PASSWORD="$password" \
		RESTIC_PASSWORD_FILE= \
		BACKUP_ALERT_WEBHOOK_URL="$webhook" \
		PGHOST=postgres \
		PGUSER=postgres \
		PGDATABASE=darkvoid \
		bash -c '. "$1"; set_defaults; validate_config' bash "$backup_script"
}

run_validation 's3:https://objects.example/darkvoid-backup' 'test-password' 'https://alerts.example/hooks/backup'

if run_validation '/state/local-repository' 'test-password' 'https://alerts.example/hooks/backup' 2>/dev/null; then
	echo 'local Restic repository unexpectedly passed validation' >&2
	exit 1
fi

if run_validation 's3:https://objects.example/darkvoid-backup' '' 'https://alerts.example/hooks/backup' 2>/dev/null; then
	echo 'missing Restic encryption password unexpectedly passed validation' >&2
	exit 1
fi

if run_validation 's3:https://objects.example/darkvoid-backup' 'test-password' 'http://alerts.example/hooks/backup' 2>/dev/null; then
	echo 'insecure alert webhook unexpectedly passed validation' >&2
	exit 1
fi

BACKUP_SOURCE_ONLY=1 bash -c '
	. "$1"
	set_defaults
	BACKUP_STATE_DIR="$2"
	BACKUP_RESTORE_DRILL_INTERVAL_SECONDS=60
	restore_drill_due
' bash "$backup_script" "$test_state"

date +%s > "${test_state}/last-restore-drill"
if BACKUP_SOURCE_ONLY=1 bash -c '
	. "$1"
	set_defaults
	BACKUP_STATE_DIR="$2"
	BACKUP_RESTORE_DRILL_INTERVAL_SECONDS=60
	restore_drill_due
' bash "$backup_script" "$test_state"; then
	echo 'fresh restore drill state unexpectedly reported due' >&2
	exit 1
fi

echo 'postgres-restic tests passed'
