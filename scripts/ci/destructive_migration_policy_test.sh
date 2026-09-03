#!/usr/bin/env bash

set -euo pipefail

repo_root="$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)"
compose_file="${repo_root}/docker-compose.prod.yml"
workflow_file="${repo_root}/.github/workflows/bot-schema-retirement.yml"
makefile="${repo_root}/Makefile"
migration_file="${repo_root}/migrations/bot/000009_drop_bot_schema.up.sql"

fail() {
	printf 'destructive migration policy: %s\n' "$*" >&2
	exit 1
}

for required in \
	'entrypoint: ["/bin/sh", "/migration-guard/run-bot-safe.sh"]' \
	'migrate-bot-destructive:' \
	'profiles: [destructive-migration]' \
	'entrypoint: ["/bin/sh", "/migration-guard/run-bot-destructive.sh"]' \
	'backup_state:/backup-state:ro' \
	'BOT_SCHEMA_DROP_APPROVAL: ${BOT_SCHEMA_DROP_APPROVAL:-}' \
	'BOT_DATA_HANDOFF_REFERENCE: ${BOT_DATA_HANDOFF_REFERENCE:-}'; do
	grep -Fq "$required" "$compose_file" || fail "production Compose is missing: $required"
done

normal_bot_block="$(sed -n '/^  migrate-bot:$/,/^  migrate-bot-destructive:$/p' "$compose_file")"
if grep -Eq '^[[:space:]]+"?up"?,?$' <<< "$normal_bot_block"; then
	fail 'normal migrate-bot service can still execute unrestricted up'
fi

for required in \
	'workflow_dispatch:' \
	'environment: production' \
	'deployments: read' \
	'protection_rules[]?' \
	'prevent_self_review' \
	'drop-bot-schema-000009' \
	'data_handoff_reference:' \
	'envs: DEPLOY_DIR,BOT_SCHEMA_DROP_APPROVAL,BOT_DATA_HANDOFF_REFERENCE' \
	'--profile destructive-migration run --rm migrate-bot-destructive'; do
	grep -Fq -- "$required" "$workflow_file" || fail "protected workflow is missing: $required"
done

if grep -Fq 'bot notification post user' "$makefile"; then
	fail 'generic Make migration down chain still includes bot'
fi
grep -Fq 'sh scripts/migrations/run-bot-safe.sh' "$makefile" \
	|| fail 'Make safe bot migration target does not use the guard runner'

grep -Fq "current_setting('darkvoid.bot_schema_drop_approval', TRUE)" "$migration_file" \
	|| fail 'migration 000009 is missing its session approval guard'
guard_line="$(grep -n "current_setting('darkvoid.bot_schema_drop_approval', TRUE)" "$migration_file" | cut -d: -f1)"
drop_line="$(grep -n '^DROP SCHEMA IF EXISTS bot CASCADE;' "$migration_file" | cut -d: -f1)"
if [ -z "$guard_line" ] || [ -z "$drop_line" ] || [ "$guard_line" -ge "$drop_line" ]; then
	fail 'migration SQL guard does not precede DROP SCHEMA'
fi

if grep -Fq 'destructive-migration' "${repo_root}/.github/workflows/cd.yml"; then
	fail 'normal CD workflow invokes the destructive migration profile'
fi

echo 'destructive migration policy tests passed'
