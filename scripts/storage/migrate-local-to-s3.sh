#!/usr/bin/env bash

# Copies local-provider media into the shared S3 bucket, preserving keys.
#
# The database stores bare storage keys ("media/<uuid>.jpg", "avatars/<uuid>.jpg")
# and the provider turns a key into a URL at read time. Switching
# STORAGE_PROVIDER from local to s3 therefore changes where every existing key is
# looked up without changing any row: unless the objects are in the bucket under
# the same keys first, every image and video already posted 404s. Run this before
# the cutover, not after.
#
# Reports what it would copy and exits without writing unless --apply is given.

set -euo pipefail

usage() {
	cat >&2 <<'USAGE'
usage: migrate-local-to-s3.sh --source <dir> [options]

  --source <dir>      Local upload directory (STORAGE_LOCAL_DIR), the parent of
                      the media/ and avatars/ key prefixes.
  --bucket <name>     Target bucket. Defaults to $STORAGE_S3_BUCKET.
  --prefix <prefix>   Key prefix inside the bucket. Must match the path in
                      STORAGE_BASE_URL, or the copied objects answer on a
                      different URL than the app builds. Defaults to none.
  --endpoint <url>    S3-compatible endpoint. Defaults to $STORAGE_S3_ENDPOINT.
  --region <region>   Defaults to $STORAGE_S3_REGION.
  --apply             Actually copy. Without it this is a dry run.

The upload directory usually lives in a Docker volume rather than on the host.
Mount it read-only to get at it. The aws-cli image entrypoint is `aws`, so the
shell has to be named explicitly:

  docker run --rm -v darkvoid_uploads:/uploads:ro -v "$PWD:/work" \
    --env-file .env --entrypoint bash amazon/aws-cli:latest \
    /work/scripts/storage/migrate-local-to-s3.sh --source /uploads --apply
USAGE
}

fail() {
	printf 'storage migration: %s\n' "$*" >&2
	exit 1
}

source_dir=""
bucket="${STORAGE_S3_BUCKET:-}"
prefix=""
endpoint="${STORAGE_S3_ENDPOINT:-}"
region="${STORAGE_S3_REGION:-}"
apply=false

while [ "$#" -gt 0 ]; do
	case "$1" in
		--source) source_dir="${2:-}"; shift 2 ;;
		--bucket) bucket="${2:-}"; shift 2 ;;
		--prefix) prefix="${2:-}"; shift 2 ;;
		--endpoint) endpoint="${2:-}"; shift 2 ;;
		--region) region="${2:-}"; shift 2 ;;
		--apply) apply=true; shift ;;
		-h|--help) usage; exit 0 ;;
		*) usage; fail "unknown argument: $1" ;;
	esac
done

[ -n "$source_dir" ] || { usage; fail "--source is required"; }
[ -d "$source_dir" ] || fail "source directory does not exist: $source_dir"
[ -n "$bucket" ] || { usage; fail "--bucket or STORAGE_S3_BUCKET is required"; }
command -v aws >/dev/null 2>&1 || fail "the aws CLI is required"

file_count="$(find "$source_dir" -type f ! -name '.*' | wc -l | tr -d '[:space:]')"
if [ "$file_count" -eq 0 ]; then
	fail "source directory holds no files: $source_dir"
fi

# Trailing and leading slashes are normalized so that --prefix media and
# --prefix /media/ cannot produce two different key spaces.
prefix="${prefix#/}"
prefix="${prefix%/}"
destination="s3://${bucket}"
if [ -n "$prefix" ]; then
	destination="${destination}/${prefix}"
fi

sync_arguments=("$source_dir" "${destination}/" --no-progress)
# The local provider writes nothing but uploads, except for the storage health
# probe. Excluding dotfiles keeps that out of the bucket even if a probe is
# caught mid-cycle.
sync_arguments+=(--exclude '.*' --exclude '*/.*')
if [ -n "$endpoint" ]; then
	sync_arguments+=(--endpoint-url "$endpoint")
fi
if [ -n "$region" ]; then
	sync_arguments+=(--region "$region")
fi

if [ "$apply" != true ]; then
	printf 'storage migration: dry run, %s files under %s -> %s\n' \
		"$file_count" "$source_dir" "$destination" >&2
	aws s3 sync "${sync_arguments[@]}" --dryrun
	printf 'storage migration: nothing written; re-run with --apply\n' >&2
	exit 0
fi

printf 'storage migration: copying %s files from %s to %s\n' \
	"$file_count" "$source_dir" "$destination" >&2
aws s3 sync "${sync_arguments[@]}"

# A sync that silently copied nothing looks identical to a successful one, so the
# object count is checked rather than trusted. It is a lower bound: the bucket may
# already hold objects this directory never had.
list_arguments=(s3 ls "${destination}/" --recursive)
if [ -n "$endpoint" ]; then
	list_arguments+=(--endpoint-url "$endpoint")
fi
if [ -n "$region" ]; then
	list_arguments+=(--region "$region")
fi
object_count="$(aws "${list_arguments[@]}" | grep -c . || true)"
if [ "$object_count" -lt "$file_count" ]; then
	fail "bucket holds $object_count objects under $destination but $file_count files were copied"
fi

printf 'storage migration: %s files copied, %s objects present under %s\n' \
	"$file_count" "$object_count" "$destination" >&2
printf 'storage migration: STORAGE_BASE_URL must now serve %s\n' "$destination" >&2
