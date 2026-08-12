# Production PostgreSQL backup runbook

Production runs `pg-backup` as a normal Compose service. It takes a PostgreSQL
custom-format dump immediately after startup and then every 24 hours by default.
Restic encrypts each dump before sending it to a required off-host repository.
No dump is retained in a Docker volume; `backup_state` contains only timestamps
and a disposable Restic cache.

## Required configuration

Set these values in the deployment `.env` before running `dv up -d`:

```dotenv
BACKUP_RESTIC_REPOSITORY=s3:https://s3.example.com/darkvoid-backups/production
BACKUP_RESTIC_PASSWORD=<long independent encryption secret>
BACKUP_ALERT_WEBHOOK_URL=https://alerts.example.com/hooks/darkvoid-backup
BACKUP_AWS_ACCESS_KEY_ID=<backup-only access key>
BACKUP_AWS_SECRET_ACCESS_KEY=<backup-only secret key>
BACKUP_AWS_REGION=us-east-1
```

The repository validator rejects filesystem repositories so losing the database
host cannot also lose the backups. For S3, grant the backup principal read,
write, list and delete access only to the repository bucket/prefix. Keep the
Restic password outside that storage account: losing it makes every snapshot
unrecoverable, while exposing it together with the repository defeats the
separation between encrypted data and its key.

CD publishes both images with a full commit-SHA tag for traceability, then
records the immutable `APP_DIGEST` and `BACKUP_DIGEST` returned by the registry
in the deployment `.env`. Rollbacks must restore both digests from the same
verified deployment. Other Restic remote backends (`sftp`, REST over HTTPS,
Azure and GCS) are accepted by the scheduler, but need a deployment-specific
Compose override for their credential files or environment variables.

## Schedule, retention and health

Defaults are:

- one backup every 86,400 seconds;
- 14 daily, 8 weekly and 12 monthly snapshots;
- one real restore drill every 604,800 seconds (and immediately on first boot);
- backup health becomes stale after 172,800 seconds.

Override these with `BACKUP_INTERVAL_SECONDS`,
`BACKUP_RESTORE_DRILL_INTERVAL_SECONDS`, `BACKUP_KEEP_DAILY`,
`BACKUP_KEEP_WEEKLY`, `BACKUP_KEEP_MONTHLY` and
`BACKUP_MAX_AGE_SECONDS`. Keep maximum age greater than the backup interval plus
the longest expected dump/upload time. `BACKUP_RESTIC_HOST` is deliberately
stable across container replacement so Restic retention applies to one history;
give each environment a distinct value when they share a repository.

The restore drill creates `<production-db>_restore_drill` by default, restores the
snapshot produced by the same cycle, verifies that it contains application
data readable from the critical `usr.users` table, then drops the database. A non-superuser `DB_USER` therefore needs
`CREATEDB`; override `BACKUP_RESTORE_DATABASE` if that name is reserved. The
scheduler refuses to use the production database as its drill target.

Inspect runtime state with:

```sh
dv ps pg-backup
dv logs --tail 100 pg-backup
dv exec pg-backup restic snapshots --tag darkvoid-postgres
```

The configured webhook receives `application/json` events with `service`,
`status`, `message`, `host` and `timestamp`. Alert on `failed` and
`configuration_failed`; `recovered` closes an active failure. Compose also marks
the service unhealthy when no successful full cycle has completed within the
maximum age.

## Manual recovery drill

Automated drills prove that the latest dump can be downloaded, decrypted and
loaded. For a disaster-recovery exercise, restore into a new isolated PostgreSQL
instance instead of the live database:

1. Record the target snapshot with
   `dv exec pg-backup restic snapshots --tag darkvoid-postgres`.
2. Start an empty PostgreSQL 16 instance on an isolated network.
3. From a trusted machine with the Restic password and repository credentials,
   stream `restic dump <snapshot-id> postgres/<production-db>.dump` into
   `pg_restore --exit-on-error --single-transaction --dbname=<recovery-db>`.
4. Run application-level checks against the recovery database: migrations,
   row counts for critical tables, login/read flows and referential integrity.
5. Record snapshot ID, duration, validation results and operator in the incident
   or change record, then destroy the isolated recovery resources.

Never point a restore command at the live database. Restoring production in
place requires an approved incident plan, a maintenance window and a separately
verified snapshot.

## Secret rotation

Rotating S3 credentials does not rewrite snapshots; update `.env` and recreate
`pg-backup`. Restic password rotation changes repository key material and must be
performed with `restic key` while the old password is still available. Verify a
snapshot and complete a restore drill before deleting the old key.
