# Database migrations

The application uses golang-migrate v4.19.1, with one ordered history and one
`public.schema_migrations_<module>` table per module. SQL files are the schema
source for SQLC v1.30.0. Tool versions are pinned in the Makefile.

The September 2026 baseline consolidates the final schema into one pair per
active module, preserving tables, column order/types/defaults, constraints,
indexes, functions, triggers, extensions and the singleton feed settings row:

| Module | PostgreSQL schema | Old final version | Baseline |
| --- | --- | --- | --- |
| user | usr | 15 | 1 |
| post | post | 15 | 1 |
| notification | notification | 3 | 1 |
| settings | settings | 2 | 1 |

Apply in order: user → post → notification → settings. Roll back in reverse.
Rolling back `000001_init` deletes that module's data. Shared extensions remain
installed on rollback because other schemas may depend on them.

`bot/` is frozen legacy history, not an active baseline module. Fresh databases
skip it. Existing bot versions 1–7 still advance to 8; version 8 stays there and
version 9 is left retired. Deleting legacy data still uses the
[bot retirement runbook](../docs/bot-schema-retirement-runbook.md). New bot
migrations cannot be created with `make migrate-create`.

## Fresh database

Configure `DB_*` in `.env`, then run:

```sh
make migrate-up
make migrate-status
```

Each active module should report version 1, clean. A fresh bot module has no
applied migration and no `bot` schema. Docker Compose uses the same ordered
runner. Application readiness derives required versions from embedded files.

## Existing database with data to preserve

This is a history reset, not an incremental upgrade. Do not run the new
baseline against existing tables, and do not use `migrate-down` to reset version
numbers. No database is automatically rebased by this repository change.

1. Keep the preceding release available. Stop application writers, take a
   database backup and verify it can be restored.
2. Using that release's original migration files, finish all active migrations:
   user 15, post 15, notification 3, settings 2, all clean. Resolve drift or
   failed migrations before proceeding. Leave bot on its own legacy history.
3. Create a separate empty database with the new baseline. Compare schema-only
   `pg_dump --no-owner --no-privileges` output for `usr`, `post`, `notification`,
   `settings` against the existing database, using the same PostgreSQL version.
   Also compare required public extensions (`pg_trgm`, `pgcrypto`, `unaccent`).
   Ignore only dump-generated random `\restrict`/`\unrestrict` tokens and the
   migration tracking tables. The schemas must match; version numbers alone
   do not establish this. Preserve existing settings values and business data.
4. Only after verification, run the following transaction on the existing
   database with `psql -X -v ON_ERROR_STOP=1`. It changes tracking metadata only:

```sql
BEGIN;
LOCK TABLE public.schema_migrations_user, public.schema_migrations_post,
    public.schema_migrations_notification, public.schema_migrations_settings
    IN ACCESS EXCLUSIVE MODE;
DO $$
BEGIN
    IF (SELECT count(*) FROM public.schema_migrations_user) <> 1
        OR NOT EXISTS (SELECT 1 FROM public.schema_migrations_user WHERE version = 15 AND NOT dirty)
        OR (SELECT count(*) FROM public.schema_migrations_post) <> 1
        OR NOT EXISTS (SELECT 1 FROM public.schema_migrations_post WHERE version = 15 AND NOT dirty)
        OR (SELECT count(*) FROM public.schema_migrations_notification) <> 1
        OR NOT EXISTS (SELECT 1 FROM public.schema_migrations_notification WHERE version = 3 AND NOT dirty)
        OR (SELECT count(*) FROM public.schema_migrations_settings) <> 1
        OR NOT EXISTS (SELECT 1 FROM public.schema_migrations_settings WHERE version = 2 AND NOT dirty)
    THEN
        RAISE EXCEPTION 'expected clean legacy heads 15/15/3/2; aborting baseline adoption';
    END IF;
END;
$$;
UPDATE public.schema_migrations_user SET version = 1;
UPDATE public.schema_migrations_post SET version = 1;
UPDATE public.schema_migrations_notification SET version = 1;
UPDATE public.schema_migrations_settings SET version = 1;
COMMIT;
```

5. Deploy the baseline release, run `make migrate-up` (no changes expected on
   the four active modules), verify migration status and `/health`, then resume
   traffic. Coordinate this cutover: old binaries/migration runners expect the
   old version sequence and must not run against the rebased tracking tables.
   Returning to the old release requires restoring its matching database state.

For a disposable development database, recreate only that database and apply
the baseline. `make db-reset` removes Compose volumes, including other local
state; use it only when all that data may be discarded.

## Subsequent changes

```sh
make migrate-create module=post name=add_example_field
# Edit 000002_add_example_field.up.sql and its down.sql counterpart.
make migrate-check
make test-migrations
make sqlc-generate
make test
make lint
```

Creation is offline: it produces the next six-digit sequence and a matching
up/down pair. It validates snake_case names, refuses gaps/duplicate versions or
incomplete pairs, and serializes concurrent creates. It does not infer SQL
from a running database or Go structs. Fill both files explicitly; an intentional
no-op rollback must contain SQL (for example `SELECT 1;`) and explain why.

Once shared or deployed, migrations are immutable: add a new migration instead
of editing `000001_init`. Reconcile sequence collisions when merging branches
before deployment. Keep schema changes in these SQL files, then regenerate SQLC;
never hand-edit generated Go files or maintain a competing schema snapshot.

`make migrate-check` validates names, pairs, sequences and nonempty SQL.
`make test-migrations` uses disposable PostgreSQL 16.14 and golang-migrate
containers, ignores local DB credentials, and checks fresh up, repeat up with
data, full down/up schema equality, constraints, counter triggers, Vietnamese
search and initial settings. CI runs both and checks SQLC produces no diff.
For changes that transform existing data, add a dedicated upgrade test with
representative rows before applying the new migration.
