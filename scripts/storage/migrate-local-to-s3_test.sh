#!/usr/bin/env bash

set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
migrate_script="${script_dir}/migrate-local-to-s3.sh"
workspace="$(mktemp -d)"
trap 'rm -rf "$workspace"' EXIT INT TERM

uploads="${workspace}/uploads"
mkdir -p "${uploads}/media" "${uploads}/avatars"
printf 'image\n' > "${uploads}/media/11111111-1111-1111-1111-111111111111.jpg"
printf 'image\n' > "${uploads}/avatars/22222222-2222-2222-2222-222222222222.png"
printf 'probe\n' > "${uploads}/.storage-health-probe"

# A stub aws that records its arguments and answers `s3 ls` with one line per
# file the sync was asked to copy, so the verification step has something to
# count without a bucket.
stub_bin="${workspace}/bin"
mkdir -p "$stub_bin"
cat > "${stub_bin}/aws" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${AWS_STUB_LOG}"
if [ "${2:-}" = "ls" ]; then
	cat "${AWS_STUB_LISTING}"
fi
STUB
chmod +x "${stub_bin}/aws"
export AWS_STUB_LOG="${workspace}/aws.log"
export AWS_STUB_LISTING="${workspace}/listing.txt"
printf '2026-09-03 00:00:00 6 media/11111111-1111-1111-1111-111111111111.jpg\n2026-09-03 00:00:00 6 avatars/22222222-2222-2222-2222-222222222222.png\n' \
	> "$AWS_STUB_LISTING"
export PATH="${stub_bin}:${PATH}"

fail() {
	printf 'migrate-local-to-s3 tests: %s\n' "$*" >&2
	exit 1
}

# Without --apply nothing may be written: the sync must carry --dryrun.
: > "$AWS_STUB_LOG"
bash "$migrate_script" --source "$uploads" --bucket darkvoid-media >/dev/null 2>&1 \
	|| fail 'dry run failed'
grep -q -- '--dryrun' "$AWS_STUB_LOG" || fail 'dry run did not pass --dryrun to aws'
if grep -q ' s3 ls ' "$AWS_STUB_LOG"; then
	fail 'dry run listed the bucket'
fi

: > "$AWS_STUB_LOG"
bash "$migrate_script" --source "$uploads" --bucket darkvoid-media --apply >/dev/null 2>&1 \
	|| fail 'apply run failed'
if grep -q -- '--dryrun' "$AWS_STUB_LOG"; then
	fail 'apply run still passed --dryrun'
fi
# Keys must land at the bucket root, or every media_key already in the database
# resolves to a URL the bucket does not answer.
grep -Fq "s3 sync ${uploads} s3://darkvoid-media/" "$AWS_STUB_LOG" \
	|| fail 'sync destination is not the bucket root'
grep -Fq -- "--exclude .*" "$AWS_STUB_LOG" || fail 'apply run did not exclude dotfiles'

# The prefix has to be normalized, or --prefix media and --prefix /media/ build
# two different key spaces out of the same intent.
: > "$AWS_STUB_LOG"
bash "$migrate_script" --source "$uploads" --bucket darkvoid-media --prefix /media/ --apply >/dev/null 2>&1 \
	|| fail 'prefixed apply run failed'
grep -Fq "s3://darkvoid-media/media/ " "$AWS_STUB_LOG" || fail 'prefix was not normalized'

# A bucket that came back holding fewer objects than were copied is a failure,
# not a silent success.
printf '2026-09-03 00:00:00 6 media/11111111-1111-1111-1111-111111111111.jpg\n' > "$AWS_STUB_LISTING"
if bash "$migrate_script" --source "$uploads" --bucket darkvoid-media --apply >/dev/null 2>&1; then
	fail 'a short object count was accepted'
fi

if bash "$migrate_script" --bucket darkvoid-media >/dev/null 2>&1; then
	fail 'a missing --source was accepted'
fi
if bash "$migrate_script" --source "$uploads" >/dev/null 2>&1; then
	fail 'a missing bucket was accepted'
fi
if bash "$migrate_script" --source "${workspace}/absent" --bucket darkvoid-media >/dev/null 2>&1; then
	fail 'a nonexistent source directory was accepted'
fi
mkdir -p "${workspace}/empty"
if bash "$migrate_script" --source "${workspace}/empty" --bucket darkvoid-media >/dev/null 2>&1; then
	fail 'an empty source directory was accepted'
fi

echo 'migrate-local-to-s3 tests passed'
