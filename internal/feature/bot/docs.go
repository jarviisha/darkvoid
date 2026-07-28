// Package bot is the control plane for the content bot that runs as a separate
// process in cmd/bot.
//
// The bot itself stays outside the API: it authenticates as an ordinary user and
// posts over the public HTTP API, so it exercises the same paths a real client
// does. What lives here is the state it used to carry as environment variables
// and compile-time slices — personas, topics, post interval, model fallback
// chain, and a pause switch — plus the log of what it has actually done.
//
// Two consumers read this context, and their split is the whole reason it exists:
//
//   - The admin plane (/api/v1/admin/bots) lets an operator edit that state and
//     read the activity log. It is mounted inside the existing /admin group, so it
//     inherits the admin role check rather than declaring its own.
//   - The agent plane (/api/v1/bot) is what the bot process polls for its desired
//     state and reports run results to. It is guarded by entity.RoleBot, reusing
//     the JWT and role machinery instead of adding a second auth scheme for
//     machine callers.
//
// Post ids recorded in the activity log are resolved to content through a narrow
// reader implemented in internal/app, never a cross-schema join — the bot tables
// hold usr.users and post.posts ids as plain UUIDs without foreign keys, matching
// how every other module keeps its schema independently migratable.
package bot
