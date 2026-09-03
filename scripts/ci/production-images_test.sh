#!/usr/bin/env bash

set -euo pipefail

repo_root="$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)"
compose_file="${repo_root}/docker-compose.prod.yml"
cd_file="${repo_root}/.github/workflows/cd.yml"
digest_writer="${repo_root}/scripts/set-deployment-image-digests.sh"

fail() {
	printf 'production image policy: %s\n' "$*" >&2
	exit 1
}

assert_pinned_reference() {
	context="$1"
	reference="$2"
	if [[ ! "$reference" =~ ^[^[:space:]@]+:[^[:space:]@]+@sha256:[0-9a-f]{64}$ ]]; then
		fail "$context is not pinned as an exact tag and sha256 digest: $reference"
	fi
}

compose_image_count=0
while IFS= read -r reference; do
	compose_image_count=$((compose_image_count + 1))
	case "$reference" in
		'${APP_IMAGE:-ghcr.io/jarviisha/darkvoid}@${APP_DIGEST:?set APP_DIGEST to the deployed sha256 digest}') ;;
		'${BACKUP_IMAGE:-ghcr.io/jarviisha/darkvoid-backup}@${BACKUP_DIGEST:?set BACKUP_DIGEST to the deployed sha256 digest}') ;;
		*) assert_pinned_reference docker-compose.prod.yml "$reference" ;;
	esac
done < <(sed -n 's/^[[:space:]]*image:[[:space:]]*//p' "$compose_file")
if [ "$compose_image_count" -ne 7 ]; then
	fail "expected 7 production image declarations, found $compose_image_count"
fi

if grep -Eq 'APP_TAG|:[[:space:]]*latest([@[:space:]]|$)|:-latest' "$compose_file"; then
	fail 'production Compose still contains a mutable application tag'
fi

base_image_count=0
while IFS='=' read -r name reference; do
	base_image_count=$((base_image_count + 1))
	assert_pinned_reference "$name" "$reference"
done < <(sed -nE 's/^ARG ((GO|RUNTIME|POSTGRES)_IMAGE)=(.*)$/\1=\3/p' \
	"${repo_root}/Dockerfile" "${repo_root}/docker/backup/Dockerfile")
if [ "$base_image_count" -ne 3 ]; then
	fail "expected 3 Dockerfile base image declarations, found $base_image_count"
fi

if [ "$(grep -c 'format=long' "$cd_file")" -ne 2 ]; then
	fail 'CD must publish full commit SHA tags for both images'
fi
if grep -Eq 'type=raw,value=latest|APP_TAG' "$cd_file"; then
	fail 'CD still publishes or deploys a mutable application tag'
fi
for required in \
	'app_digest: ${{ steps.build.outputs.digest }}' \
	'backup_digest: ${{ steps.build_backup.outputs.digest }}' \
	'bash scripts/set-deployment-image-digests.sh .env "$APP_DIGEST" "$BACKUP_DIGEST"'; do
	grep -Fq "$required" "$cd_file" || fail "CD is missing: $required"
done

test_dir="$(mktemp -d)"
trap 'rm -rf "$test_dir"' EXIT
test_env="${test_dir}/.env"
cat > "$test_env" <<'EOF'
DB_PASSWORD=keep-this-secret
APP_DIGEST=sha256:old-app
BACKUP_DIGEST=sha256:old-backup
EOF
chmod 640 "$test_env"
old_mode="$(stat -c '%a' "$test_env")"
app_digest="sha256:$(printf 'a%.0s' {1..64})"
backup_digest="sha256:$(printf 'b%.0s' {1..64})"

bash "$digest_writer" "$test_env" "$app_digest" "$backup_digest"
grep -Fxq 'DB_PASSWORD=keep-this-secret' "$test_env" || fail 'digest writer changed an unrelated secret'
[ "$(grep -c '^APP_DIGEST=' "$test_env")" -eq 1 ] || fail 'digest writer did not store exactly one app digest'
[ "$(grep -c '^BACKUP_DIGEST=' "$test_env")" -eq 1 ] || fail 'digest writer did not store exactly one backup digest'
grep -Fxq "APP_DIGEST=$app_digest" "$test_env" || fail 'digest writer stored the wrong app digest'
grep -Fxq "BACKUP_DIGEST=$backup_digest" "$test_env" || fail 'digest writer stored the wrong backup digest'
[ "$(stat -c '%a' "$test_env")" = "$old_mode" ] || fail 'digest writer changed .env permissions'

before_invalid="$(sha256sum "$test_env")"
if bash "$digest_writer" "$test_env" 'sha256:short' "$backup_digest" >/dev/null 2>&1; then
	fail 'digest writer accepted an invalid digest'
fi
[ "$(sha256sum "$test_env")" = "$before_invalid" ] || fail 'invalid input changed .env'

echo 'production image pinning tests passed'
