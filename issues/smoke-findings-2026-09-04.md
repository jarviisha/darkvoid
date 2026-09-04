# Smoke-test findings — 2026-09-04

## Conclusion

Three defects surfaced while smoke-testing a boot-order change on `main` (`17b79f2`). None was introduced by that change; all three predate it and each was reproduced directly. All three are now fixed.

SM-04 was found afterwards, while verifying the SM-01 fix end-to-end. SM-05 was found while fixing SM-04. It is only visible on a rebuild, which is why it had not been seen: `make docker-up` reuses an existing image rather than rebuilding, and the image on the development machine was five weeks old.

SM-02 and SM-03 were the same shape as each other: a real failure the system reported as health. SM-01 turned out to be narrower than first written up — see the correction in its entry.

## Status

| ID | Status | Severity | Summary |
|---|---|---|---|
| SM-01 | Resolved | High | `make docker-up` cannot complete on a fresh volume: the guarded bot migration blocks the chain and dirties the database |
| SM-02 | Resolved | Medium | Missing module migrations leave `/health` reporting `healthy` |
| SM-03 | Resolved | Medium | Fatal boot errors are logged at `INFO` |
| SM-04 | Resolved | Medium | An uploads volume created before the non-root image cannot be written by it, and the app refuses to boot |
| SM-05 | Resolved | Low | The golangci-lint exclusions block sat at the wrong nesting level and was silently ignored |

## Scope and method

The stack was brought up with `make docker-up` against a fresh volume, then the API binary was run directly against the host PostgreSQL and Redis that `.env` targets. Findings are recorded from the observed output, not from reading alone.

---

## SM-01: `make docker-up` cannot reach a healthy stack on a fresh volume — Resolved

**Severity:** High
**Class:** Operations / migration gating

> **Correction to the original write-up.** This entry first recommended giving Compose "a way to run" the retirement migration, on the assumption none existed. That was wrong: `docker-compose.prod.yml` already ran the module through `scripts/migrations/run-bot-safe.sh`, which advances to 000008 and stops, and already carried a separate `migrate-bot-destructive` service behind a `destructive-migration` profile. The Makefile used the safe runner too. `docker-compose.yml` was the only place still issuing an unrestricted `up` — the development file had simply never been given the treatment the production one had.
>
> Fixed by pointing the development `migrate-bot` at the same `run-bot-safe.sh`, with the same environment. No destructive profile was added for development: retiring the bot schema is a protected production workflow, not something a local `up` should offer.
>
> `scripts/ci/destructive_migration_policy_test.sh` — which asserted this contract against the production file only — now asserts it against the development file as well, which is what would have caught the gap.
>
> Note for existing environments: this prevents a database from being dirtied, it does not clean one already dirty. A volume that reached version 9 still needs `make migrate-force module=bot version=8` before it will come up.

### Evidence

- `migrations/bot/000009_drop_bot_schema.up.sql` guards the drop and aborts unless it recognises the runner: [`migrations/bot/000009_drop_bot_schema.up.sql:32`](../migrations/bot/000009_drop_bot_schema.up.sql#L32) raises `bot schema retirement requires the approved destructive migration runner`.
- The compose service that runs this module is a plain `migrate/migrate:4` with no such runner: [`docker-compose.yml:167`](../docker-compose.yml#L167).
- It is declared `restart: on-failure`, so the failure becomes a loop rather than a stop.
- The rest of the chain is gated behind it: `migrate-settings` waits on `migrate-bot: service_completed_successfully` ([`docker-compose.yml:190`](../docker-compose.yml#L190)), and `app` waits on `migrate-settings` the same way ([`docker-compose.yml:209`](../docker-compose.yml#L209)).

Observed on a fresh volume:

```
migrate-bot-1 |  (details: pq: bot schema retirement requires the approved destructive migration runner)
migrate-bot-1 | error: Dirty database version 9. Fix and force version.
migrate-bot-1 | error: Dirty database version 9. Fix and force version.
...
```

`docker compose ps` at that point: `postgres` healthy, `redis` healthy, `migrate-bot` `Restarting (1)`, no `app`.

### Impact

- `make docker-up` never produces a running API on a fresh volume. The documented local-development path is broken.
- The first failed attempt leaves `schema_migrations_bot` dirty at version 9, so recovery needs `make migrate-force module=bot version=…` before any retry can proceed — a step the failure message does not name.
- The same chain runs in `docker-compose.prod.yml`, so this is not confined to local development.

### Recommendation

- `CLAUDE.md` is explicit that the `bot` module must stay in the chain until every environment has run `000009`, so skipping it in compose is not the fix — compose needs a way to *run* it.
- Give the compose migrate step the approved destructive runner, or gate `000009` on a value the compose service can legitimately supply, so the retirement migration can complete exactly once per environment.
- Reconsider `restart: on-failure` for a step whose failure is permanent: a guard rejection will never succeed on retry, and looping hides the single line that explains why.
- Have the guard's exception name the runner or the command that satisfies it, rather than only the fact that the current one does not.

---

## SM-02: Missing module migrations leave `/health` reporting `healthy` — Resolved

**Severity:** Medium
**Class:** Observability / deploy consistency

> Fixed by comparing versions rather than by watching for symptoms. The migration tree is embedded in the binary (`migrations.Embedded`), and `/health` reads `schema_migrations_<module>` for each module this build reads tables from, reporting `schema: behind` and 503 when any is behind, dirty, or absent. Migrations run before the app in every deployment path here, so a module that has not caught up means the rollout is broken rather than early.
>
> `bot` is excluded by design: its tree ships 000009 while the safe runner stops at 000008, so checking it would report every correct deployment as broken. A database *ahead* of the binary is not reported either — that is a rollback, and these migrations are additive.
>
> A failure of the probe itself reports `schema: unknown` and does not unseat the instance: connectivity is already covered by the database probe, and failing here would take a working deployment out of rotation over the checker rather than over the thing checked.

### Evidence

Booting against a database missing two modules' tables, `GET /health` answered `200`:

```json
{ "status": "healthy", "database": "up", "redis": "up", "storage": "up",
  "codohue": "degraded" }
```

while the log carried:

```
"msg":"feed settings load failed, serving defaults until the next refresh",
"error":"Internal server error: ERROR: relation \"settings.feed\" does not exist (SQLSTATE 42P01)"

"msg":"feed outbox claim failed",
"error":"claim feed outbox events: ERROR: relation \"usr.feed_outbox\" does not exist (SQLSTATE 42P01)"
```

- `/health` probes connectivity only — PostgreSQL and Redis are pinged, and neither probe knows anything about schema: [`internal/app/server.go:173`](../internal/app/server.go#L173).
- The settings read is deliberately non-fatal and keeps the defaults: [`internal/app/settings_wiring.go:34`](../internal/app/settings_wiring.go#L34).

### Impact

- A deployment that missed a module's migrations reports itself healthy and serves traffic.
- With `settings.feed` absent, the feed ranks on compiled defaults while `PATCH /admin/settings/feed` fails — the admin API and the running feed disagree, and only a log line every `SETTINGS_REFRESH_INTERVAL` says so.
- With `usr.feed_outbox` absent, the outbox consumer errors on every tick and durable feed events are never drained. Post and follow mutations still commit, so nothing user-facing fails; followers' timelines simply stop being updated.
- Both failures are exactly the shape the health endpoint exists to catch, and it does not catch them.

### Recommendation

- Report per-module migration version in `/health`, or fail boot when a module's `schema_migrations_<module>` version is behind the migrations present in the image.
- At minimum, distinguish "settings row absent" (a first boot, benign) from "settings relation absent" (a migration was never run), and treat only the second as unhealthy.
- The outbox consumer's repeated failure should surface as a health signal rather than only as a log line, since nothing else reports that feed events have stopped draining.

---

## SM-03: Fatal boot errors are logged at `INFO` — Resolved

**Severity:** Medium
**Class:** Observability

> Fixed at both levels. The four `log.Fatalf` calls in `cmd/api` now go through `pkg/logger` and `os.Exit(1)`, and a `depguard` rule denies the standard `log` and `log/slog` packages everywhere except `pkg/logger` itself and `cmd/seed` — which is exempt because it never installs the slog default, so its `log.Printf` is ordinary CLI output rather than a downgraded record. `make lint` is the guard, which is the right mechanism for a repo-wide convention that `CLAUDE.md` already states.

### Evidence

A refused boot produced:

```
{"level":"INFO","msg":"Failed to initialize application: failed to setup contexts: …"}
```

- `cmd/api/main.go` uses the standard library `log` package for all four fatal paths: [`cmd/api/main.go:41`](../cmd/api/main.go#L41), [`:47`](../cmd/api/main.go#L47), [`:53`](../cmd/api/main.go#L53), [`:64`](../cmd/api/main.go#L64).
- `logger.SetDefault` calls `slog.SetDefault`: [`pkg/logger/context.go:128`](../pkg/logger/context.go#L128). `slog.SetDefault` also redirects the standard `log` package through the slog handler **at `Info` level**, which is where the level comes from.
- This is also the one rule `CLAUDE.md` states without exception for logging: always use `pkg/logger`, never the raw logger.

The process does exit `1`, so orchestrators still see the failure.

### Impact

- Any alert or log pipeline that filters on `level >= ERROR` sees nothing when the API refuses to boot.
- The severity of the most severe event the process can report is indistinguishable from routine startup chatter.
- The four call sites are the only remaining users of the standard `log` package in `cmd/api`, so the inconsistency is confined and cheap to remove.

### Recommendation

- Replace each `log.Fatalf` with a `pkg/logger` error followed by `os.Exit(1)`.
- Consider whether `slog.SetDefault`'s standard-library redirect is wanted at all: it silently downgrades every `log.Print`/`log.Fatal` in any dependency to `INFO`.

---

## SM-04: An uploads volume predating the non-root image blocks boot — Resolved

**Severity:** Medium
**Class:** Operations / upgrade hazard

> Fixed on three fronts, because the failure had three separate causes.
>
> The message now names the owner instead of the path: `pkg/storage` reports which uid owns the directory, which uid the process runs as, and the `chown` that reconciles them — and distinguishes that from a directory the process already owns whose mode denies the write, where recommending a chown would be advice that cannot work.
>
> The `Dockerfile` pins `uid 100` / `gid 101` rather than letting `adduser -S` allocate whatever is free, because a runbook that hard-codes the numbers and an image that allocates them will drift. They are the values already in use, so no existing volume needs a second chown. `scripts/ci/container_user_test.sh` fails if the Dockerfile and the runbook ever disagree.
>
> `docs/uploads-volume-ownership-runbook.md` carries the remedy, since the container cannot perform it.
>
> And `make docker-rebuild` was added: `make docker-up` starts whatever image already exists, which is why this went unseen for five weeks and why a `/health` body read earlier in this session was missing fields the source had long had.

### Evidence

- The image adds an unprivileged user and runs as it: [`Dockerfile:25`](../Dockerfile#L25) and [`Dockerfile:33`](../Dockerfile#L33). In the current build that user is `uid 100`, `gid 101`.
- The local storage provider proves writability by creating a dotfile in `STORAGE_LOCAL_DIR`, and `setupInfrastructure` runs that probe during boot: [`internal/app/infrastructure_setup.go:50`](../internal/app/infrastructure_setup.go#L50).
- A named volume takes its ownership from the image directory the first time it is populated. The `darkvoid_uploads` volume on the development machine was created on 28 April by an image that still ran as root, and is owned `0:0`:

```
drwxr-xr-x    4 0        0             4096 Apr 28 07:22 /v
drwxr-x---    2 0        0             4096 Apr 28 07:21 avatars
```

- Rebuilding the image and starting it against that volume refuses the boot:

```
"level":"ERROR","msg":"failed to initialize application",
"error":"failed to setup contexts: storage health check failed: storage/local:
 create health probe in \"/app/uploads\": open /app/uploads/.storage-health-…: permission denied"
```

### Impact

- Any environment whose uploads volume predates the non-root hardening fails to start on the next deploy that rebuilds the image. The container enters a restart loop.
- Refusing to boot is the correct behaviour — a storage backend the process cannot write to would fail every upload — but the message names the symptom without naming the remedy, and the remedy is not something the container can perform: it runs unprivileged, so it cannot `chown` its own volume.
- Development and staging only in practice: `validateStorage` refuses the `local` provider when `ENVIRONMENT=production`, so a production deployment is on S3 and unaffected.
- It stayed hidden because `make docker-up` starts an existing image rather than rebuilding one. The gap between the running image and the source can be arbitrarily large, and nothing reports it.

### Recommendation

- Document the one-off in the upgrade runbook, since it has to run from outside the container:
  `docker run --rm -v darkvoid_uploads:/v alpine chown -R 100:101 /v`
- Pin the uid/gid in the `Dockerfile` explicitly rather than letting `adduser -S` allocate them, so the runbook command cannot drift from the image.
- Consider having the storage probe distinguish "directory not writable by this user" from other failures and say what owns it, so the error names the cause rather than the symptom.
- Separately: `make docker-up` never rebuilds. A target that does, or a note in `CLAUDE.md`, would stop a stale image from being mistaken for the current source — this finding, and the misleading `/health` body observed just before it, were both that.

---

## SM-05: The lint exclusions were silently ignored — Resolved

**Severity:** Low
**Class:** Tooling / silent misconfiguration

### Evidence

`.golangci.yml` carried an `exclusions` block at the top level, holding the sqlc-generated-file exclusion and three test-file rules. golangci-lint v2 expects it under `linters:`. The running binary accepted the file anyway:

```
$ golangci-lint config verify
jsonschema: "" does not validate with "/additionalProperties": additional properties 'exclusions' not allowed
jsonschema: "output.formats.text" does not validate with ...: additional properties 'color' not allowed
```

while `golangci-lint run` — what `make lint` and CI invoke — reported no complaint about the configuration at all. The block was inert: every exclusion in it, including `internal/feature/.*/db/.*\.go`, had no effect.

Surfaced by a new test that legitimately chmods a directory to `0500`: gosec flagged it under `G302` despite the `.*_test\.go` exclusion that should have covered it.

### Impact

- Generated sqlc code and every test file were being linted with the full rule set. Nothing failed, so the repo passed — but the configuration and the enforcement had disagreed for as long as the file has been in this shape, and the first genuinely-excluded case would have looked like a real finding.
- `make lint` gates commits and CI (`aff9168`). A configuration it cannot validate is one nobody is checking.

### Recommendation

- Applied: the block now nests under `linters:`, and `output.formats.text.color` is the correct `colors`. `golangci-lint config verify` exits 0.
- Consider adding `golangci-lint config verify` to `make lint` or to CI. An invalid configuration that `run` tolerates is exactly the kind of thing that stays wrong until something unrelated trips over it — which is how this was found.
- Worth a decision separately: with the exclusions now live, test files are no longer linted. They have in fact been passing the full rule set all along, so keeping them in scope is an option the original config did not intend but the evidence supports.
