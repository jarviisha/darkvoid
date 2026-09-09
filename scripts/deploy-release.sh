#!/usr/bin/env bash
# Stage under <root>/incoming/<id>; promote only after migration and readiness.
set -euo pipefail
if [ "$#" -ne 2 ]; then
	echo 'usage: deploy-release.sh <deployment-directory> <incoming-release-directory>' >&2
	exit 2
fi
root="$(CDPATH='' cd -- "$1" && pwd -P)"
incoming="$(CDPATH='' cd -- "$2" && pwd -P)"
fail() { echo "deploy: $*" >&2; exit 1; }
[[ "$incoming" == "$root/incoming/"* ]] || fail 'release must be staged in incoming/'
release_id="${incoming##*/}"
[[ "$release_id" =~ ^[0-9a-f]{40}-[0-9]+-[0-9]+$ ]] || fail 'invalid release directory name'
exec 9> "$root/.deploy.lock"
flock -x 9
for link in current previous; do
	if [ -e "$root/$link" ] && [ ! -L "$root/$link" ]; then fail "$link must be a symlink"; fi
done
if [ -n "$(find "$incoming" -type l -print -quit)" ]; then fail 'release contains a symlink'; fi
(cd "$incoming" && sha256sum --check --status SHA256SUMS) || fail 'release checksum mismatch'

read_manifest() {
	local key="$1" file="$2" value
	value="$(sed -n "s/^${key}=//p" "$file")"
	[ -n "$value" ] || fail "missing $key in release manifest"
	printf '%s\n' "$value"
}
commit="$(read_manifest RELEASE_COMMIT "$incoming/release.env")"
sequence="$(read_manifest RELEASE_SEQUENCE "$incoming/release.env")"
[[ "$commit" =~ ^[0-9a-f]{40}$ && "$sequence" =~ ^[1-9][0-9]*$ ]] || fail 'invalid release identity'
[[ "$release_id" == "$commit-"* ]] || fail 'release directory does not match commit'
for key in APP_DIGEST BACKUP_DIGEST; do
	digest="$(read_manifest "$key" "$incoming/release.env")"
	[[ "$digest" =~ ^sha256:[0-9a-f]{64}$ ]] || fail "invalid $key"
done
old_release=""
if [ -L "$root/current" ]; then
	old_release="$(readlink -f "$root/current")"
	old_commit="$(read_manifest RELEASE_COMMIT "$old_release/release.env")"
	old_sequence="$(read_manifest RELEASE_SEQUENCE "$old_release/release.env")"
	[[ "$old_sequence" =~ ^[1-9][0-9]*$ ]] || fail 'invalid current release sequence'
	[ "$sequence" -ge "$old_sequence" ] || fail 'refusing an older CI release'
	grep -Fxq "$old_commit" "$incoming/ancestors" || fail 'current commit is not an ancestor of the candidate'
fi
release="$root/releases/$release_id"
[ ! -e "$release" ] || fail 'release already exists; upload with a new run attempt'
mkdir -p "$root/releases"
mv "$incoming" "$release"
chmod -R a-w "$release"
export DARKVOID_DIR="$root" DARKVOID_RELEASE_DIR="$release" COMPOSE_PROFILES=""
unset APP_IMAGE APP_DIGEST BACKUP_IMAGE BACKUP_DIGEST
dv() { bash "$release/scripts/dv" "$@"; }
trap 'echo "Deploy failed. current still names the last verified release; containers may have changed. Inspect with DARKVOID_RELEASE_DIR=$release dv ps/logs before recovery." >&2' ERR

config="$(dv config --format json)"
jq -e '.services.postgres and .services.redis and .services.migrate and .services["pg-backup"]' \
	<<< "$config" >/dev/null || fail 'deploy requires the production infrastructure overlay'
# Server overrides may tune resources, but must not substitute another image
# while current records the candidate's manifest as verified.
app_reference="$(read_manifest APP_IMAGE "$release/release.env")@$(read_manifest APP_DIGEST "$release/release.env")"
backup_reference="$(read_manifest BACKUP_IMAGE "$release/release.env")@$(read_manifest BACKUP_DIGEST "$release/release.env")"
jq -e --arg app "$app_reference" --arg backup "$backup_reference" \
	'.services.app.image == $app and .services["pg-backup"].image == $backup and
	 (.services.app.build == null) and (.services["pg-backup"].build == null)' \
	<<< "$config" >/dev/null || fail 'resolved images do not match the release manifest'
app_timeout="$(jq -r '."x-deployment".app_timeout' <<< "$config")"
backup_timeout="$(jq -r '."x-deployment".backup_timeout' <<< "$config")"
require_backup="$(jq -r '."x-deployment".require_backup' <<< "$config")"
environment="$(jq -r '.services.app.environment.ENVIRONMENT' <<< "$config")"
[ "$environment" != production ] || require_backup=true
for timeout in "$app_timeout" "$backup_timeout"; do
	[[ "$timeout" =~ ^[1-9][0-9]*$ ]] || fail 'readiness timeouts must be positive integers'
done
[[ "$require_backup" == true || "$require_backup" == false ]] || fail 'DEPLOY_REQUIRE_BACKUP must be true or false'
jq -e '[.services.app.ports[] | select(.target == 8080)] | length == 1' \
	<<< "$config" >/dev/null || fail 'deploy requires one published HTTP port'
host="$(jq -r '.services.app.ports[] | select(.target == 8080) | .host_ip // "127.0.0.1"' <<< "$config")"
port="$(jq -r '.services.app.ports[] | select(.target == 8080) | .published' <<< "$config")"
[[ "$port" =~ ^[1-9][0-9]{0,4}$ ]] && [ "$port" -le 65535 ] \
	|| fail 'deploy requires a fixed HTTP host port from 1 through 65535'
unset config
case "$host" in 0.0.0.0) host=127.0.0.1 ;; ::) host=::1 ;; esac
[[ "$host" != *:* ]] || host="[$host]"

dv pull postgres redis migrate app pg-backup
dv up -d --wait --wait-timeout "$app_timeout" postgres redis
# Explicit run guarantees migrations execute on every release, even if a prior
# migrate service completed successfully with identical container configuration.
dv run --rm --no-deps migrate
dv up -d --no-deps --wait --wait-timeout "$app_timeout" app
curl --fail --silent --show-error --max-time 10 "http://$host:$port/health" >/dev/null
if [ "$require_backup" = true ]; then
	dv up -d --no-deps --wait --wait-timeout "$backup_timeout" pg-backup
else
	dv up -d --no-deps pg-backup
fi

# A single pointer records the entire verified release, including both digests.
# .env remains operator-owned; changing this pointer never rolls back DB data.
if [ -n "$old_release" ]; then
	ln -s "$old_release" "$root/.previous.$$"
	mv -Tf "$root/.previous.$$" "$root/previous"
fi
ln -s "$release" "$root/.current.$$"
mv -Tf "$root/.current.$$" "$root/current"
trap - ERR
echo "Verified release $release_id"
