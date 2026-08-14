#!/usr/bin/env bash

set -euo pipefail

log() {
	level="$1"
	shift
	printf '%s level=%s service=postgres-backup message=%s\n' \
		"$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$level" "$*" >&2
}

set_defaults() {
	BACKUP_INTERVAL_SECONDS="${BACKUP_INTERVAL_SECONDS:-86400}"
	BACKUP_RESTORE_DRILL_INTERVAL_SECONDS="${BACKUP_RESTORE_DRILL_INTERVAL_SECONDS:-604800}"
	BACKUP_KEEP_DAILY="${BACKUP_KEEP_DAILY:-14}"
	BACKUP_KEEP_WEEKLY="${BACKUP_KEEP_WEEKLY:-8}"
	BACKUP_KEEP_MONTHLY="${BACKUP_KEEP_MONTHLY:-12}"
	BACKUP_MAX_AGE_SECONDS="${BACKUP_MAX_AGE_SECONDS:-172800}"
	BACKUP_RESTIC_TAG="${BACKUP_RESTIC_TAG:-darkvoid-postgres}"
	BACKUP_RESTIC_HOST="${BACKUP_RESTIC_HOST:-darkvoid-production}"
	BACKUP_STATE_DIR="${BACKUP_STATE_DIR:-/state}"
	BACKUP_RESTORE_DATABASE="${BACKUP_RESTORE_DATABASE:-${PGDATABASE:-darkvoid}_restore_drill}"
	RESTIC_CACHE_DIR="${RESTIC_CACHE_DIR:-${BACKUP_STATE_DIR}/cache}"
	export RESTIC_CACHE_DIR
}

require_positive_integer() {
	name="$1"
	value="$2"
	case "$value" in
		''|*[!0-9]*)
			log error "$name must be a positive integer"
			return 1
			;;
	esac
	if [ "$value" -le 0 ]; then
		log error "$name must be greater than zero"
		return 1
	fi
}

validate_remote_repository() {
	case "$RESTIC_REPOSITORY" in
		s3:*|sftp:*|rest:https://*|azure:*|gs:*)
			return 0
			;;
		*)
			log error "RESTIC_REPOSITORY must use a supported off-host backend"
			return 1
			;;
	esac
}

validate_config() {
	if [ -z "${RESTIC_REPOSITORY:-}" ]; then
		log error "RESTIC_REPOSITORY is required"
		return 1
	fi
	validate_remote_repository || return 1

	if [ -z "${RESTIC_PASSWORD:-}" ] && [ -z "${RESTIC_PASSWORD_FILE:-}" ]; then
		log error "RESTIC_PASSWORD or RESTIC_PASSWORD_FILE is required"
		return 1
	fi
	if [ -z "${BACKUP_ALERT_WEBHOOK_URL:-}" ]; then
		log error "BACKUP_ALERT_WEBHOOK_URL is required"
		return 1
	fi
	case "$BACKUP_ALERT_WEBHOOK_URL" in
		https://*) ;;
		*)
			log error "BACKUP_ALERT_WEBHOOK_URL must use HTTPS"
			return 1
			;;
	esac

	if [ -z "${PGHOST:-}" ] || [ -z "${PGUSER:-}" ] || [ -z "${PGDATABASE:-}" ]; then
		log error "PGHOST, PGUSER and PGDATABASE are required"
		return 1
	fi
	case "$PGDATABASE" in
		*[!A-Za-z0-9_]*)
			log error "PGDATABASE must contain only letters, numbers and underscores"
			return 1
			;;
	esac
	case "$BACKUP_RESTORE_DATABASE" in
		''|*[!A-Za-z0-9_]*)
			log error "BACKUP_RESTORE_DATABASE must contain only letters, numbers and underscores"
			return 1
			;;
	esac
	if [ "$BACKUP_RESTORE_DATABASE" = "$PGDATABASE" ]; then
		log error "BACKUP_RESTORE_DATABASE must not be the production database"
		return 1
	fi
	if [ "${#BACKUP_RESTORE_DATABASE}" -gt 63 ]; then
		log error "BACKUP_RESTORE_DATABASE exceeds PostgreSQL's 63-byte identifier limit"
		return 1
	fi
	case "$BACKUP_RESTIC_TAG" in
		''|*[!A-Za-z0-9_.-]*)
			log error "BACKUP_RESTIC_TAG contains unsupported characters"
			return 1
			;;
	esac
	case "$BACKUP_RESTIC_HOST" in
		''|*[!A-Za-z0-9_.-]*)
			log error "BACKUP_RESTIC_HOST contains unsupported characters"
			return 1
			;;
	esac

	require_positive_integer BACKUP_INTERVAL_SECONDS "$BACKUP_INTERVAL_SECONDS" || return 1
	require_positive_integer BACKUP_RESTORE_DRILL_INTERVAL_SECONDS "$BACKUP_RESTORE_DRILL_INTERVAL_SECONDS" || return 1
	require_positive_integer BACKUP_KEEP_DAILY "$BACKUP_KEEP_DAILY" || return 1
	require_positive_integer BACKUP_KEEP_WEEKLY "$BACKUP_KEEP_WEEKLY" || return 1
	require_positive_integer BACKUP_KEEP_MONTHLY "$BACKUP_KEEP_MONTHLY" || return 1
	require_positive_integer BACKUP_MAX_AGE_SECONDS "$BACKUP_MAX_AGE_SECONDS" || return 1
}

send_alert() {
	status="$1"
	message="$2"
	host="$(hostname 2>/dev/null || printf unknown)"
	payload="$(jq -cn \
		--arg service postgres-backup \
		--arg status "$status" \
		--arg message "$message" \
		--arg host "$host" \
		--arg timestamp "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
		'{service:$service,status:$status,message:$message,host:$host,timestamp:$timestamp}')" || return 1

	curl --fail --silent --show-error \
		--retry 3 --retry-all-errors --max-time 15 \
		-H 'Content-Type: application/json' \
		--data-binary "$payload" \
		"$BACKUP_ALERT_WEBHOOK_URL" >/dev/null
}

initialize_repository() {
	if restic cat config >/dev/null 2>&1; then
		return 0
	fi
	log info "initializing encrypted Restic repository"
	restic init || return 1
}

perform_backup() {
	archive_path="postgres/${PGDATABASE}.dump"
	log info "starting PostgreSQL backup"
	if ! backup_output="$(
		pg_dump \
			--format=custom \
			--compress=9 \
			--no-owner \
			--no-privileges \
		| restic --retry-lock 5m backup \
			--stdin \
			--stdin-filename "$archive_path" \
			--host "$BACKUP_RESTIC_HOST" \
			--tag "$BACKUP_RESTIC_TAG" \
			--json
	)"; then
		log error "pg_dump or Restic upload failed"
		return 1
	fi

	snapshot_id="$(printf '%s\n' "$backup_output" \
		| jq -r 'select(.message_type == "summary") | .snapshot_id // empty' \
		| tail -n 1)"
	if [ -z "$snapshot_id" ]; then
		log error "Restic did not return a snapshot ID"
		return 1
	fi
	log info "backup snapshot $snapshot_id completed"
	printf '%s\n' "$snapshot_id"
}

apply_retention() {
	log info "applying daily/weekly/monthly retention policy"
	restic --retry-lock 5m forget \
		--host "$BACKUP_RESTIC_HOST" \
		--tag "$BACKUP_RESTIC_TAG" \
		--keep-daily "$BACKUP_KEEP_DAILY" \
		--keep-weekly "$BACKUP_KEEP_WEEKLY" \
		--keep-monthly "$BACKUP_KEEP_MONTHLY" \
		--prune || return 1
}

restore_drill_due() {
	state_file="${BACKUP_STATE_DIR}/last-restore-drill"
	if [ ! -r "$state_file" ]; then
		return 0
	fi
	last_drill="$(cat "$state_file" 2>/dev/null || printf 0)"
	case "$last_drill" in
		''|*[!0-9]*) return 0 ;;
	esac
	now="$(date +%s)"
	[ "$((now - last_drill))" -ge "$BACKUP_RESTORE_DRILL_INTERVAL_SECONDS" ]
}

run_restore_drill() {
	snapshot_id="$1"
	drill_database="$BACKUP_RESTORE_DATABASE"
	archive_path="postgres/${PGDATABASE}.dump"
	result=0

	log info "starting restore drill into isolated database $drill_database"
	dropdb --if-exists --force --maintenance-db=postgres "$drill_database" || return 1
	createdb --template=template0 --maintenance-db=postgres "$drill_database" || return 1

	if ! restic dump "$snapshot_id" "$archive_path" \
		| pg_restore \
			--exit-on-error \
			--single-transaction \
			--no-owner \
			--no-privileges \
			--dbname="$drill_database"; then
		log error "restore drill could not restore snapshot $snapshot_id"
		result=1
	fi

	if [ "$result" -eq 0 ]; then
		if ! psql --dbname="$drill_database" --no-psqlrc --quiet \
			--set=ON_ERROR_STOP=1 \
			--command="SELECT count(*) FROM usr.users;" >/dev/null; then
			log error "restore drill could not read the critical usr.users table"
			result=1
		fi
	fi

	if ! dropdb --if-exists --force --maintenance-db=postgres "$drill_database"; then
		log error "restore drill database cleanup failed"
		result=1
	fi
	if [ "$result" -ne 0 ]; then
		return "$result"
	fi
	log info "restore drill for snapshot $snapshot_id completed"
}

write_epoch() {
	date +%s > "$1"
}

run_cycle() {
	initialize_repository || return 1
	snapshot_id="$(perform_backup)" || return 1
	apply_retention || return 1

	if restore_drill_due; then
		run_restore_drill "$snapshot_id" || return 1
		write_epoch "${BACKUP_STATE_DIR}/last-restore-drill" || return 1
	fi
	write_epoch "${BACKUP_STATE_DIR}/last-success" || return 1
}

main() {
	set_defaults
	if ! validate_config; then
		case "${BACKUP_ALERT_WEBHOOK_URL:-}" in
			https://*)
				send_alert configuration_failed "PostgreSQL backup configuration is invalid; inspect pg-backup container logs" \
					|| log error "could not deliver backup configuration alert"
				;;
		esac
		exit 1
	fi
	mkdir -p "$BACKUP_STATE_DIR" "$RESTIC_CACHE_DIR"

	trap 'log info "shutdown requested"; exit 0' INT TERM
	log info "scheduler started; interval=${BACKUP_INTERVAL_SECONDS}s restore_drill_interval=${BACKUP_RESTORE_DRILL_INTERVAL_SECONDS}s"

	while :; do
		if run_cycle; then
			if [ -e "${BACKUP_STATE_DIR}/failure-active" ]; then
				send_alert recovered "PostgreSQL backup cycle recovered" \
					|| log error "could not deliver recovery alert"
				rm -f "${BACKUP_STATE_DIR}/failure-active"
			fi
		else
			touch "${BACKUP_STATE_DIR}/failure-active"
			send_alert failed "PostgreSQL backup cycle failed; inspect pg-backup container logs" \
				|| log error "could not deliver backup failure alert"
		fi
		log info "next backup cycle in ${BACKUP_INTERVAL_SECONDS}s"
		sleep "$BACKUP_INTERVAL_SECONDS" &
		wait "$!"
	done
}

if [ "${BACKUP_SOURCE_ONLY:-0}" != "1" ]; then
	main "$@"
fi
