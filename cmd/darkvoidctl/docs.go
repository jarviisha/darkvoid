// Package main implements darkvoidctl, an operator CLI for DarkVoid.
//
// It connects directly to Postgres (reusing the feature service/repository
// layer) so operators can manage users without a running API — useful for
// recovery (e.g. resetting a forgotten root password) and day-to-day admin.
//
// Usage:
//
//	darkvoidctl user reset-password -username root -password 'NewPass123'
//	darkvoidctl user list [-q alice] [-limit 50] [-active-only]
//	darkvoidctl user create -username bob -email bob@x.com -display-name Bob -password 'Secret123'
//	darkvoidctl user promote -username alice      # grant the admin role
//	darkvoidctl user demote  -username alice      # revoke the admin role
//	darkvoidctl user deactivate -username spammer
//
// Configuration comes from the same environment/.env as the API server
// (DB_HOST, DB_USER, ...). Run it on the host or point it at a DATABASE via env.
package main
