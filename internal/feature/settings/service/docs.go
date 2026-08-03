// Package service owns the runtime settings: reading them, validating and
// applying operator edits, and publishing each resulting snapshot to the
// registered sinks.
//
// It publishes on read as well as on write, which is what makes the design
// survive more than one API instance. The instance serving a PATCH applies the
// new snapshot at once; every other instance picks it up when its refresh loop
// calls Refresh. Publishing only on write would leave a two-instance deployment
// serving two different rollout percentages until the next restart — the exact
// failure that moving these knobs out of the environment was meant to end.
//
// The sink interface is declared here rather than in the feed package because it
// is a callback, not a dependency: this context does not know what a feed is, and
// the adapter that connects the two lives in internal/app.
package service
