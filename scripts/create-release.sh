#!/usr/bin/env bash
# Export one committed source tree and its exact image pair. No host secrets.
set -euo pipefail
if [ "$#" -ne 5 ]; then
	echo 'usage: create-release.sh <new-directory> <commit> <sequence> <app-digest> <backup-digest>' >&2
	exit 2
fi
output="$1" commit="$2" sequence="$3" app_digest="$4" backup_digest="$5"
[[ "$commit" =~ ^[0-9a-f]{40}$ && "$sequence" =~ ^[1-9][0-9]*$ ]] || exit 2
for digest in "$app_digest" "$backup_digest"; do
	[[ "$digest" =~ ^sha256:[0-9a-f]{64}$ ]] || exit 2
done
app_image="${APP_IMAGE:-ghcr.io/${GITHUB_REPOSITORY:-jarviisha/darkvoid}}"
app_image="${app_image,,}"
backup_image="${BACKUP_IMAGE:-$app_image-backup}"
for image in "$app_image" "$backup_image"; do
	[[ "$image" =~ ^[a-z0-9][a-z0-9./_-]+$ ]] || exit 2
done
mkdir "$output"
git archive "$commit" compose.yml compose.dev.yml compose.prod.yml compose.codohue.yml \
	scripts migrations | tar -x -C "$output"
git rev-list "$commit" > "$output/ancestors"
printf 'RELEASE_COMMIT=%s\nRELEASE_SEQUENCE=%s\nAPP_IMAGE=%s\nAPP_DIGEST=%s\nBACKUP_IMAGE=%s\nBACKUP_DIGEST=%s\n' \
	"$commit" "$sequence" "$app_image" "$app_digest" "$backup_image" "$backup_digest" > "$output/release.env"
(
	cd "$output"
	find compose*.yml scripts migrations ancestors release.env -type f -print0 \
		| sort -z | xargs -0 sha256sum > SHA256SUMS
)
