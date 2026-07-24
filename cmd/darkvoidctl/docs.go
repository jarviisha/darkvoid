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
//	darkvoidctl user promote -username alice      # grant the admin role
//	darkvoidctl user demote  -username alice      # revoke the admin role
//	darkvoidctl user deactivate -username spammer
//
// Passwords are never accepted as flags (argv is world-readable via /proc and
// persists in shell history). reset-password and create prompt interactively
// with echo disabled, or read DARKVOIDCTL_PASSWORD / a single piped stdin line.
//
// Configuration comes from the same environment/.env as the API server
// (DB_HOST, DB_USER, ...). Run it on the host or point it at a DATABASE via env.
package main
