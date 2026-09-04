# Smoke-test findings — 2026-09-04

## Conclusion

Three defects surfaced while smoke-testing a boot-order change on `main` (`17b79f2`). None was introduced by that change; all three predate it and each was reproduced directly. All three are now fixed.

SM-02 and SM-03 were the same shape as each other: a real failure the system reported as health. SM-01 turned out to be narrower than first written up — see the correction in its entry.

## Status

| ID | Status | Severity | Summary |
|---|---|---|---|
| SM-01 | Resolved | High | `make docker-up` cannot complete on a fresh volume: the guarded bot migration blocks the chain and dirties the database |
| SM-02 | Resolved | Medium | Missing module migrations leave `/health` reporting `healthy` |
| SM-03 | Resolved | Medium | Fatal boot errors are logged at `INFO` |

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
