#!/usr/bin/env bash

set -euo pipefail

repo_root="$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)"
compose_file="${repo_root}/compose.prod.yml"
dev_compose_file="${repo_root}/compose.dev.yml"
workflow_file="${repo_root}/.github/workflows/bot-schema-retirement.yml"
makefile="${repo_root}/Makefile"
migration_file="${repo_root}/migrations/bot/000009_drop_bot_schema.up.sql"

fail() {
	printf 'destructive migration policy: %s\n' "$*" >&2
	exit 1
}

for required in \
	'migrate-bot-destructive:' \
	'profiles: [destructive-migration]' \
	'entrypoint: ["/bin/sh", "/migration-guard/run-bot-destructive.sh"]' \
	'backup_state:/backup-state:ro' \
	'BOT_SCHEMA_DROP_APPROVAL: ${BOT_SCHEMA_DROP_APPROVAL:-}' \
	'BOT_DATA_HANDOFF_REFERENCE: ${BOT_DATA_HANDOFF_REFERENCE:-}'; do
	grep -Fq "$required" "$compose_file" || fail "production Compose is missing: $required"
done

# Both deployment modes use the same ordered runner and guarded bot path.
for file in "$compose_file" "$dev_compose_file"; do
	grep -Fq 'entrypoint: ["/bin/sh", "/migration-guard/run-all-safe.sh"]' "$file" \
		|| fail 'Compose is missing the ordered safe migration runner'
	grep -Fq '${DARKVOID_RELEASE_DIR:-.}/scripts/migrations:/migration-guard:ro' "$file" \
		|| fail 'Compose does not mount release migration scripts read-only'
done
grep -Fq 'sh "$script_dir/run-bot-safe.sh"' "${repo_root}/scripts/migrations/run-all-safe.sh" \
	|| fail 'ordered runner does not invoke the bot guard'

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
