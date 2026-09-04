# Uploads volume ownership runbook

## When you need this

The application refuses to boot with a message like:

```
storage/local: cannot write to "/app/uploads": it is owned by uid 0 gid 0 and
this process runs as uid 100 gid 101; a volume created by an image that ran as
root needs `chown -R 100:101` from outside the container
```

This affects deployments using `STORAGE_PROVIDER=local` — development and
staging. Production is unaffected: `pkg/config.validateStorage` refuses the
local provider when `ENVIRONMENT=production`, so a production deployment is on
S3 and has no uploads volume.

## Why it happens

A Docker named volume takes its ownership from the image directory the first
time it is populated. Early images ran as root, so any uploads volume created
then is owned by `0:0`.

The runtime image now runs as an unprivileged user. It cannot write into a
root-owned volume, and the local storage provider proves writability at boot by
creating a probe file — so the boot is refused rather than deferred to the first
upload that would have failed.

The container cannot repair this itself. Changing the volume's ownership is
precisely what an unprivileged process cannot do to the volume it is running on,
which is why the remedy is here rather than in an entrypoint.

## Remedy

Stop the app, chown the volume, start it again. The numbers are the uid and gid
pinned in the `Dockerfile`; `scripts/ci/container_user_test.sh` fails if this
document and the image ever disagree.

```sh
docker compose stop app
docker run --rm -v darkvoid_uploads:/v alpine chown -R 100:101 /v
docker compose start app
```

Substitute the volume name if the Compose project is not `darkvoid`; `docker
volume ls` lists them. Confirm the result:

```sh
docker run --rm -v darkvoid_uploads:/v alpine ls -ldn /v
# drwxr-xr-x 4 100 101 ... /v
```

No data is moved or removed — only ownership changes.

## Verifying

```sh
curl -s localhost:8080/health
```

`"storage": "up"` means the probe now succeeds.

## Avoiding it on a new environment

Nothing to do. A volume created by the current image is owned correctly from the
start; only volumes predating the unprivileged user need this.

## Related

- A variant of the same message naming the *mode* rather than the owner means
  the directory already belongs to the process and the permission bits are the
  problem — a bind mount with restrictive permissions, not this situation.
- `make docker-up` starts whatever app image already exists. Use `make
  docker-rebuild` to build the working tree; a stale image is otherwise
  indistinguishable from current source.
