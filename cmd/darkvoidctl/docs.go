// Package main implements darkvoidctl, an operator CLI for DarkVoid.
//
// It connects directly to Postgres (reusing the feature service/repository
// layer) so operators can manage users without a running API — useful for
// recovery (e.g. resetting a forgotten root password) and day-to-day admin.
//
// Usage:
//
//	darkvoidctl user reset-password -username root   # prompts for the password
//	darkvoidctl user list [-q alice] [-limit 50] [-active-only]
//	darkvoidctl user create -username bob -email bob@x.com -display-name Bob
//	darkvoidctl user roles                                  # the assignable roles
//	darkvoidctl user grant-role  -username alice -role admin
//	darkvoidctl user revoke-role -username alice -role admin
//	darkvoidctl user deactivate -username spammer
//	darkvoidctl codohue reindex [-since 24h] [-limit 500] [-dry-run]
//
// codohue reindex is the repair pass for the recommendation index. Posts are
// indexed once, at creation, and a failed ingest is logged and dropped rather
// than queued — so an outage leaves those posts absent from recommendations
// permanently. It walks the public corpus newest first through the same cursor
// query the discover feed uses, and shares post/service.IndexText with the live
// path so a backfill cannot index posts under different text than new ones get.
//
// grant-role/revoke-role replace the earlier admin-only promote/demote pair, which
// could reach only the admin role. This is the only way to assign the bot role,
// which marks a machine account and has no API endpoint behind it. An unknown role
// name is rejected before it reaches the database, where it would fail the CHECK
// constraint as an opaque error.
//
// Passwords are never accepted as flags (argv is world-readable via /proc and
// persists in shell history). reset-password and create prompt interactively
// with echo disabled, or read DARKVOIDCTL_PASSWORD / a single piped stdin line.
//
// Configuration comes from the same environment/.env as the API server
// (DB_HOST, DB_USER, ...). Run it on the host or point it at a DATABASE via env.
package main
