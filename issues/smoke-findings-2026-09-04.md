# Smoke-test findings — 2026-09-04

## Conclusion

Three defects surfaced while smoke-testing a boot-order change on `main` (`17b79f2`). None was introduced by that change; all three predate it and each was reproduced directly.

SM-01 blocks `make docker-up` outright on a fresh volume and leaves the database dirty, so it is the one that has to be fixed before the documented local-development path works again. SM-02 and SM-03 are both observability defects of the same shape: a real failure that the system reports as health.

All three are open.

## Status

| ID | Status | Severity | Summary |
|---|---|---|---|
| SM-01 | Open | High | `make docker-up` cannot complete on a fresh volume: the guarded bot migration blocks the chain and dirties the database |
| SM-02 | Open | Medium | Missing module migrations leave `/health` reporting `healthy` |
| SM-03 | Open | Medium | Fatal boot errors are logged at `INFO` |

## Scope and method

The stack was brought up with `make docker-up` against a fresh volume, then the API binary was run directly against the host PostgreSQL and Redis that `.env` targets. Findings are recorded from the observed output, not from reading alone.

---

## SM-01: `make docker-up` cannot reach a healthy stack on a fresh volume

**Severity:** High
**Class:** Operations / migration gating

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

## SM-02: Missing module migrations leave `/health` reporting `healthy`

**Severity:** Medium
**Class:** Observability / deploy consistency

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

## SM-03: Fatal boot errors are logged at `INFO`

**Severity:** Medium
**Class:** Observability

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
