#!/usr/bin/env bash

# Pins the production Compose defaults whose failure modes are quiet.
#
# Everything here was a live incident shape rather than a style preference: an
# empty trusted-proxy list rate-limits a whole deployment into one bucket, and a
# `${VAR:?}` on a value the application already validates breaks every Compose
# command on the box — including the ones used to diagnose it.

set -euo pipefail

repo_root="$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)"
compose_file="${repo_root}/docker-compose.prod.yml"

fail() {
	printf 'deployment defaults: %s\n' "$*" >&2
	exit 1
}

# The app publishes on loopback and is reached through a proxy, so the peer it
# sees is the bridge gateway, never the client. An empty list makes TrustedRealIP
# a no-op: one shared httprate bucket for everyone and the gateway address in
# every access log.
proxy_line="$(grep -E '^[[:space:]]*TRUSTED_PROXY_CIDRS:' "$compose_file" || true)"
[ -n "$proxy_line" ] || fail 'TRUSTED_PROXY_CIDRS is not set for the app service'
case "$proxy_line" in
	*'TRUSTED_PROXY_CIDRS:-}'*|*'TRUSTED_PROXY_CIDRS:-}"'*)
		fail 'TRUSTED_PROXY_CIDRS defaults to empty, which disables forwarded-IP resolution behind the proxy'
		;;
esac
for required_range in '127.0.0.0/8' '10.0.0.0/8' '172.16.0.0/12' '192.168.0.0/16'; do
	case "$proxy_line" in
		*"$required_range"*) ;;
		*) fail "TRUSTED_PROXY_CIDRS default does not cover $required_range" ;;
	esac
done

# Storage and backup configuration is validated where it is used —
# pkg/config.validateStorage refuses a non-s3 provider under
# ENVIRONMENT=production, and the backup scheduler names every missing value at
# once. A required variable here would additionally fail `dv ps` and `dv logs`,
# and would fail on the non-production deployments that share this file.
while IFS= read -r required_variable; do
	case "$required_variable" in
		APP_DIGEST|BACKUP_DIGEST|DB_PASSWORD|JWT_SECRET) ;;
		*) fail "$required_variable is required by Compose; validate it in the process that uses it instead" ;;
	esac
done < <(grep -vE '^[[:space:]]*#' "$compose_file" \
	| grep -oE '\$\{[A-Z0-9_]+:\?' \
	| sed -E 's/^\$\{([A-Z0-9_]+):\?$/\1/' \
	| sort -u)

# Non-production deployments run on this file with the local provider. Without
# the mount their uploads live in the container's writable layer and disappear
# on the next deploy.
grep -Fq 'uploads:/app/uploads' "$compose_file" || fail 'the app service does not mount the uploads volume'
grep -Eq '^  uploads:$' "$compose_file" || fail 'the uploads volume is not declared'
grep -Fq 'STORAGE_LOCAL_DIR: /app/uploads' "$compose_file" \
	|| fail 'STORAGE_LOCAL_DIR does not point at the mounted uploads volume'

echo 'deployment defaults tests passed'
