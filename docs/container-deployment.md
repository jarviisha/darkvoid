# Container deployment

Darkvoid builds two images: API/seed/operator CLI together, and PostgreSQL backup.
The API and background workers remain one process. PostgreSQL and Redis belong to
this project; Codohue remains an external service.

## Compose modes

| Mode | Files | Normal services |
| --- | --- | --- |
| App-only | `compose.yml` | app, with external DB/Redis |
| Local development | `compose.yml` + `compose.dev.yml` | app, postgres, redis, migrate |
| Deployed development/production | `compose.yml` + `compose.prod.yml` | app, postgres, redis, migrate, pg-backup |
| Codohue network integration | Add `compose.codohue.yml` | Join its existing network; no Codohue containers created |

`ctl` and `seed` use the `tools` profile and the same image as app. The guarded
destructive migration is available only in the production overlay, under its
separate profile. A normal migration runs user → post → notification → bot-safe →
settings and stops at the first error. Module version tables and SQL are unchanged.

Use the Makefile locally:

```sh
make docker-up
make docker-rebuild
make docker-seed
make docker-up-app
make docker-down-app
```

`docker-up` reuses the existing image; `docker-rebuild` builds the current source.
Seed and app-only targets build app first, so all commands use one local image.
`docker-down-app` stops app without taking down infrastructure. A bare
`docker compose up` now selects app-only, not the development stack. For local
database clients, copy `docker-compose.override.yml.example` to
`docker-compose.override.yml`; `make docker-up` includes it automatically.

`scripts/dv` is the common entrypoint for Make, CD and the installed `dv`/`dvctl`.
It uses Compose's native dotenv parsing, including quoted values and comments.
`DARKVOID_COMPOSE` takes precedence over `COMPOSE_FILE`. Use colon-separated file
lists, for example `compose.yml:compose.prod.yml:compose.codohue.yml`. Relative
bundled files resolve in the release; additional server-local overrides resolve
in the deployment directory. Explicit absolute paths also work.

For app-only mode set container-reachable DB/Redis endpoints. Legacy `EXTERNAL_*`
values take precedence over the corresponding base DB/Redis settings. Local
infrastructure overlays always use `darkvoid-postgres` and `darkvoid-redis`, avoiding
DNS collisions when Codohue uses the same service names. Its network and event
Redis defaults remain compatible with the old stack; `CODOHUE_NETWORK_NAME` and
`CODOHUE_EVENTS_REDIS_HOST` can select stable aliases supplied by its operator.

## Configuration boundaries

The container listener is always `0.0.0.0:8080`. `APP_HOST_PORT` selects the host
port; `SERVER_PORT` remains a legacy host-port fallback and still controls native
Go processes. Set a fixed, nonzero host port on deployed environments. The API
binds to host loopback by default; `SERVER_BIND` controls that binding. Narrow
`TRUSTED_PROXY_CIDRS` to the actual reverse proxy addresses when known.

`dv` reads the following files from `DARKVOID_DIR`, in order:

1. `.env`: deployment configuration; existing installations can keep their values.
2. `.env.app`, if present: optional application configuration and credentials.
3. `.env.backup`, if present: backup-only keys; start from `.env.backup.example`.
4. The selected release's `release.env`: image repositories, digests and identity.

These are interpolation inputs. No service receives an entire env file. App,
seed and ctl share an explicit application allowlist; backup keys are mapped only
into pg-backup, and migration jobs receive database credentials. Standard AWS
credential-chain variables belong to app; backup credentials use `BACKUP_AWS_*`.
Web-identity credential files still require an appropriate operator-owned mount.

Operator shell values override dotenv settings, except that a selected release
owns its app/backup image pair. Add newly introduced application variables to
the allowlist in `compose.yml`. Do not combine Docker and native migration Make
targets in one invocation: Docker targets delegate dotenv parsing to Compose,
whereas native targets retain Make's existing environment interface.

Production requires S3 through application validation. The uploads volume remains
for deployed development environments; no media migration occurs in this change.
Backup has an initial 512 MiB / 1 CPU limit, configurable with
`BACKUP_MEMORY_LIMIT`/`BACKUP_CPU_LIMIT`. Tune these against actual backup size and
duration. Set `BACKUP_RESTORE_PG*` to a scratch PostgreSQL instance when restore
drills should not consume production disk and I/O. See the
[backup runbook](production-backup-runbook.md).

## Releases and deployment

The host needs Docker Compose 2.24 or newer, Bash, jq, curl, flock, and GNU coreutils.
CD currently targets the GitHub `development` environment at `/opt/darkvoid-dev`.
No production environment or infrastructure is provisioned by this refactor.

```text
/opt/darkvoid-dev/
  .env, .env.app, .env.backup       operator-owned configuration
  .deploy.lock                    serializes deploy and bot retirement
  incoming/<commit>-<run>-<attempt>/
  releases/<commit>-<run>-<attempt>/
    compose*.yml, scripts/, migrations/
    release.env, ancestors, SHA256SUMS
  current -> releases/<verified release>
  previous -> releases/<previous verified release>
```

CI builds and smoke-tests both images and boots an isolated development stack
before publishing. CD exports the exact tested commit with image digests and
checksums. It uploads into a unique incoming directory without overwriting the
running release. The host takes a lock, verifies the bundle, rejects older CI
sequences and commits that do not descend from the current release, then makes
the candidate directory read-only.

The resolved app/backup references must match the candidate manifest; server
overrides cannot silently substitute another image or a local build. Deployed
environments must publish exactly one fixed HTTP host port.

Deployment pulls images, waits for PostgreSQL/Redis, executes a fresh migration
job, and waits for app readiness. It also probes the configured published HTTP
port. Production always waits for pg-backup health; deployed development can
request that gate with `DEPLOY_REQUIRE_BACKUP=true`. Defaults are 180 seconds for
app/infra and 1800 seconds for backup, configurable with `DEPLOY_APP_TIMEOUT` and
`DEPLOY_BACKUP_TIMEOUT`. Backup health represents snapshot freshness, not successful
restore drills; those retain their separate alert and timestamp contract.

Only after these checks does CD atomically update `current`. That one pointer
selects Compose, scripts, migrations and both image digests together. `.env` is not
rewritten. GitHub concurrency serializes deployment jobs, while the host lock also
coordinates the protected bot-retirement workflow. Do not run manual lifecycle
commands concurrently with deploy; take the same lock, for example:

```sh
flock -x /opt/darkvoid-dev/.deploy.lock dv up -d
```

## First upgrade from the old layout

Before deploying this commit:

1. Confirm host prerequisites and preserve a copy of the current `.env`, Compose
   files and app/backup digests as the legacy recovery record. The first release
   has no `previous` pointer because the old flat directory was not a bundle.
2. Record the existing Compose project name, network and named volumes. Keep the
   deployment directory unchanged, and preserve `COMPOSE_PROJECT_NAME` if set.
   `dv --project-directory` always refers to that stable directory, not a release
   directory. Production keeps the network name `${COMPOSE_NETWORK_NAME:-darkvoid}`.
3. Existing `.env` file lists using `docker-compose.yml`,
   `docker-compose.prod.yml` or `docker-compose.codohue.yml` are translated by dv.
   Use dv on deployed hosts so files resolve through `current`; a bare Docker CLI
   command in the old flat directory can still select its stale files. Update
   file lists to the new names for checkout-based use. The old tracked files have
   been replaced; additional local overrides are not silently discarded.
4. If this project previously ran `app-external`, identify and stop that old
   container before starting the new `app` service, because both publish the same
   port. Never remove its uploads volume. The deprecated `external` env profile
   selects base Compose in dv; it no longer creates a second app.
5. Deploy through CD, verify `dv ps`, `dv logs app`, and the backup state. Old
   exited `migrate-*` containers may remain as orphans; they are not started or
   automatically removed. Remove only those confirmed obsolete containers later.

The database, uploads and backup-state volume names and UID/GID remain unchanged.
Do not use `down -v` when upgrading a real deployment. No destructive SQL is added
to normal deployment; bot retirement remains a separately protected workflow.

## Failed rollout and recovery

A failure leaves `current` on the last verified release, but containers may
already be running the candidate and migrations may already have committed.
Inspect the candidate explicitly using the release path printed by the deploy:

```sh
DARKVOID_RELEASE_DIR=/opt/darkvoid-dev/releases/<candidate> dv ps
DARKVOID_RELEASE_DIR=/opt/darkvoid-dev/releases/<candidate> dv logs --tail 100 app migrate pg-backup
```

The one-off migration container is removed after execution; its output is
retained in the CD job log. An older stopped `migrate` service can have unrelated logs.

Fix configuration and rerun CD with a new run attempt, or deploy a forward fix.
Release directories are never overwritten. If an application-only rollback is
appropriate, first check that the old binary is compatible with the current
schema, then take the host lock and use `DARKVOID_RELEASE_DIR` to explicitly select
the recovery release when recreating app/backup. Verify health before changing
the `current` pointer. Do not run old migrations or downgrade database metadata as
an automatic rollback. The normal deployment script intentionally refuses older
history; recovery is a deliberate operator operation.

Server-local config and overrides remain outside the immutable bundle; back them
up separately. TLS/proxy routing, remote backup/restore, production sizing and
availability of the external Codohue network still require host-level validation.

## Verification commands

```sh
make test-ops
make test-container-runtime APP_TEST_IMAGE=darkvoid:ci BACKUP_TEST_IMAGE=darkvoid-backup:ci
make test-compose-integration APP_TEST_IMAGE=darkvoid:ci
```

The integration test creates a unique Compose project with a random loopback
port and fresh volumes, verifies API health and migration idempotency, then
removes only its own containers/volumes. Release tests use real Compose rendering
and recording substitutes for container lifecycle calls, covering readiness
failures, lock ordering, checksum failures and release promotion without a VPS.
