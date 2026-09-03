# Legacy bot schema retirement runbook

Migration `migrations/bot/000009_drop_bot_schema.up.sql` permanently drops the
legacy `bot` schema. A normal deploy stops bot migrations at `000008`; databases
already at `000009` are left there and are never automatically downgraded.

Do not run `migrate up` directly for the bot module. The SQL migration itself
requires a session-only approval parameter, so an accidental direct invocation
fails before `DROP SCHEMA`. Because golang-migrate records a version as dirty
before executing its SQL, such an attempt can leave `schema_migrations_bot` at
dirty version 9 even though the schema remains intact. Stop and verify the bot
tables before an approved DBA resets metadata to version 8; do not blindly
`force` the version.

## One-time production setup

Create a GitHub environment named `production` for the
`Retire legacy bot schema` workflow:

1. Configure required reviewers and enable prevention of self-review.
2. Add `VPS_HOST`, `VPS_USER`, `VPS_SSH_KEY` and `VPS_PORT` secrets.
3. Set the `DEPLOY_DIR` environment variable to the directory containing the
   deployed Compose files and `.env`.

Without environment protection, `workflow_dispatch` remains explicit but does
not provide independent human approval, so the retirement workflow must not be
used until reviewers are configured. The workflow queries the GitHub environment
API and fails before SSH unless at least one required reviewer and prevention of
self-review are both present.

## Preconditions

Before requesting approval, attach this evidence to a change record:

- the external bot owns every bot identity/configuration still required;
- any required `bot.bots`, `bot.config`, `bot.topics` and `bot.runs` data has
  been transferred and validated in the external system;
- `dv ps pg-backup` reports healthy;
- the most recent backup succeeded within 48 hours;
- a restore drill succeeded within 7 days;
- the Restic password and repository credentials are available to the recovery
  operators independently of the database host.

The workflow requires the change/ticket identifier as
`data_handoff_reference`. Boolean acknowledgements such as `true` are rejected.
The migration container independently reads the backup scheduler's read-only
state volume and rejects missing, future-dated or stale evidence.

## Execute

From GitHub Actions, choose `Retire legacy bot schema` → `Run workflow`:

1. Enter `drop-bot-schema-000009` as the exact confirmation.
2. Enter the approved handoff/change reference.
3. Have a different authorized reviewer approve the `production` environment.
4. Preserve the workflow URL and logs in the change record.

The guarded service requires bot migration version 8, passes the SQL approval
only for its own database session, applies exactly one migration, then verifies
version 9. Re-running after success is a no-op.

## Verification

After the workflow succeeds, verify:

```sh
dv exec postgres psql -U "$DB_USER" -d "$DB_NAME" -c \
  "SELECT to_regnamespace('bot') IS NULL AS bot_schema_removed;"
dv exec postgres psql -U "$DB_USER" -d "$DB_NAME" -c \
  "SELECT version, dirty FROM schema_migrations_bot;"
```

Expected results are `bot_schema_removed = t`, `version = 9` and
`dirty = false`. Then run the normal application health and smoke checks.

## Recovery and rollback

Migration `000009` down recreates empty tables only. It is structural
compatibility for the migration ladder and must never be described or used as a
data rollback.

If legacy bot data is needed after retirement:

1. Stop changes that could conflict with recovery and open an incident/change.
2. Restore the pre-retirement Restic snapshot into an isolated PostgreSQL 16
   database according to `production-backup-runbook.md`.
3. Validate the restored `bot` schema and row counts against the handoff record.
4. Decide whether to re-export data to the external bot or restore the schema
   during a maintenance window. Do not overwrite the live database wholesale.
5. If the schema is restored to live, reconcile `schema_migrations_bot` under an
   explicit DBA plan; never use `migrate force` as a substitute for restoring
   data.

Loss of the Restic password makes snapshot recovery impossible. Confirm access
to it before approving the destructive migration.
