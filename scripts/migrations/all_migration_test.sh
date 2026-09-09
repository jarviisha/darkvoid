#!/usr/bin/env bash
set -euo pipefail
script_dir="$(CDPATH='' cd -- "$(dirname "$0")" && pwd)"
test_dir="$(mktemp -d)"
trap 'rm -rf "$test_dir"' EXIT
mkdir -p "$test_dir/bot"
export MIGRATE_BIN="$script_dir/testdata/fake-migrate.sh"
export MIGRATION_ROOT="$test_dir" FAKE_MULTI_MODULE=true
export FAKE_MIGRATION_STATE="$test_dir/version" FAKE_MIGRATION_LOG="$test_dir/log"
for version in none 7 8 9; do
	printf '%s\n' "$version" > "$FAKE_MIGRATION_STATE"
	: > "$FAKE_MIGRATION_LOG"
	sh "$script_dir/run-all-safe.sh" >/dev/null
	actual="$(sed -n 's/^module=\([^ ]*\).*/\1/p' "$FAKE_MIGRATION_LOG" | uniq | paste -sd ,)"
	[ "$actual" = user,post,notification,bot,settings ]
	if grep -q 'module=bot command=up' "$FAKE_MIGRATION_LOG"; then exit 1; fi
	if [ "$version" = 9 ]; then
		[ "$(cat "$FAKE_MIGRATION_STATE")" = 9 ]
	else
		[ "$(cat "$FAKE_MIGRATION_STATE")" = 8 ]
	fi
done
for failed in user post notification settings; do
	printf '8\n' > "$FAKE_MIGRATION_STATE"
	: > "$FAKE_MIGRATION_LOG"
	if FAKE_FAIL_MODULE="$failed" sh "$script_dir/run-all-safe.sh" >/dev/null 2>&1; then
		echo "runner ignored failure in $failed" >&2; exit 1
	fi
	grep -q "module=$failed " < <(tail -n 1 "$FAKE_MIGRATION_LOG")
done
for version in dirty 10; do
	printf '%s\n' "$version" > "$FAKE_MIGRATION_STATE"
	: > "$FAKE_MIGRATION_LOG"
	if sh "$script_dir/run-all-safe.sh" >/dev/null 2>&1; then exit 1; fi
	if grep -q 'module=settings ' "$FAKE_MIGRATION_LOG"; then exit 1; fi
done
echo 'Ordered migration tests passed'
