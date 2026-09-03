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
		BACKUP_RESTORE_DRILL_ENABLED="${drill_enabled:-true}" \
		BACKUP_RESTORE_PGPORT="${drill_port:-5432}" \
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

if drill_enabled=sometimes run_validation 's3:https://objects.example/darkvoid-backup' 'test-password' 'https://alerts.example/hooks/backup' 2>/dev/null; then
	echo 'non-boolean BACKUP_RESTORE_DRILL_ENABLED unexpectedly passed validation' >&2
	exit 1
fi

if drill_port=0 run_validation 's3:https://objects.example/darkvoid-backup' 'test-password' 'https://alerts.example/hooks/backup' 2>/dev/null; then
	echo 'invalid restore drill port unexpectedly passed validation' >&2
	exit 1
fi

# A disabled drill has no target to validate, so an unusable port must not stop
# the deployment from taking backups.
drill_enabled=false drill_port=0 run_validation 's3:https://objects.example/darkvoid-backup' 'test-password' 'https://alerts.example/hooks/backup'

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

# A failed restore drill must not withhold last-success: the healthcheck reads
# that file to answer "is there a recent off-host snapshot", and the snapshot in
# this cycle uploaded fine. The drill reports through its own alert instead.
cycle_state="$(mktemp -d)"
trap 'rm -rf "$test_state" "$cycle_state"' EXIT INT TERM

run_stubbed_cycle() {
	drill_result="$1"
	BACKUP_SOURCE_ONLY=1 bash -c '
		. "$1"
		set_defaults
		BACKUP_STATE_DIR="$2"
		BACKUP_RESTORE_DRILL_ENABLED=true
		BACKUP_RESTORE_DRILL_INTERVAL_SECONDS=1
		drill_exit="$3"
		initialize_repository() { return 0; }
		perform_backup() { printf "%s\n" 0123abcd; }
		apply_retention() { return 0; }
		run_restore_drill() { return "$drill_exit"; }
		send_alert() { printf "%s\n" "$1" >> "${BACKUP_STATE_DIR}/alerts"; }
		run_cycle
	' bash "$backup_script" "$cycle_state" "$drill_result"
}

if ! run_stubbed_cycle 1; then
	echo 'a failed restore drill unexpectedly failed the whole backup cycle' >&2
	exit 1
fi
if [ ! -r "${cycle_state}/last-success" ]; then
	echo 'a failed restore drill suppressed last-success' >&2
	exit 1
fi
if [ ! -e "${cycle_state}/restore-drill-failure-active" ]; then
	echo 'a failed restore drill did not record its own failure state' >&2
	exit 1
fi
if [ -e "${cycle_state}/last-restore-drill" ]; then
	echo 'a failed restore drill was recorded as completed' >&2
	exit 1
fi
if ! grep -qx restore_drill_failed "${cycle_state}/alerts"; then
	echo 'a failed restore drill did not alert' >&2
	exit 1
fi

run_stubbed_cycle 0
if [ ! -r "${cycle_state}/last-restore-drill" ]; then
	echo 'a passing restore drill was not recorded' >&2
	exit 1
fi
if [ -e "${cycle_state}/restore-drill-failure-active" ]; then
	echo 'a passing restore drill did not clear the previous failure state' >&2
	exit 1
fi
if ! grep -qx restore_drill_recovered "${cycle_state}/alerts"; then
	echo 'a recovered restore drill did not alert' >&2
	exit 1
fi

# A disabled drill leaves the backup half of the cycle untouched.
disabled_state="$(mktemp -d)"
trap 'rm -rf "$test_state" "$cycle_state" "$disabled_state"' EXIT INT TERM
BACKUP_SOURCE_ONLY=1 bash -c '
	. "$1"
	set_defaults
	BACKUP_STATE_DIR="$2"
	BACKUP_RESTORE_DRILL_ENABLED=false
	initialize_repository() { return 0; }
	perform_backup() { printf "%s\n" 0123abcd; }
	apply_retention() { return 0; }
	run_restore_drill() { printf "drill ran\n" > "${BACKUP_STATE_DIR}/drill-ran"; return 0; }
	run_cycle
' bash "$backup_script" "$disabled_state"
if [ -e "${disabled_state}/drill-ran" ]; then
	echo 'the restore drill ran while disabled' >&2
	exit 1
fi
if [ ! -r "${disabled_state}/last-success" ]; then
	echo 'a cycle with the drill disabled did not record last-success' >&2
	exit 1
fi

echo 'postgres-restic tests passed'
