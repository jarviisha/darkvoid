#!/usr/bin/env bash

# Pins the identity the runtime image runs as, and the one remedy that depends
# on it.
#
# A Docker named volume takes its ownership from the image that first populated
# it. When the image gained an unprivileged user, every uploads volume created
# before that became unwritable, and the app refuses to boot against it — the
# correct outcome, reached through a message that reads as a path problem. The
# fix has to run from outside the container, which means a number written down
# in a runbook, which means that number and the image must not drift apart.
#
# adduser -S allocates whatever system uid happens to be free, so pinning is the
# only thing that keeps them together.

set -euo pipefail

repo_root="$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)"
dockerfile="${repo_root}/Dockerfile"
runbook="${repo_root}/docs/uploads-volume-ownership-runbook.md"
makefile="${repo_root}/Makefile"

fail() {
	printf 'container user: %s\n' "$*" >&2
	exit 1
}

uid="$(grep -oE 'adduser -S -u [0-9]+' "$dockerfile" | grep -oE '[0-9]+$' || true)"
gid="$(grep -oE 'addgroup -S -g [0-9]+' "$dockerfile" | grep -oE '[0-9]+$' || true)"

[ -n "$uid" ] || fail 'Dockerfile does not pin a numeric uid for the runtime user'
[ -n "$gid" ] || fail 'Dockerfile does not pin a numeric gid for the runtime group'

[ -f "$runbook" ] || fail "no runbook at ${runbook#"$repo_root"/}"

grep -Fq "chown -R ${uid}:${gid}" "$runbook" \
	|| fail "runbook chown does not use the pinned ${uid}:${gid} from the Dockerfile"

# make docker-up starts whatever image already exists. Without a target that
# rebuilds, a stale image is indistinguishable from current source — which is
# how this failure went unseen for five weeks.
grep -Eq '^docker-rebuild:' "$makefile" \
	|| fail 'Makefile has no docker-rebuild target, so nothing rebuilds the app image'
grep -Fq 'test-container-user' "$makefile" \
	|| fail 'test-ops does not run the container user contract'

printf 'container user tests passed\n'
