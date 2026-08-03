// Package settings is the bounded context for application settings an operator
// changes while the process is running.
//
// It exists because the alternative for every knob under it was a redeploy. That
// is an acceptable price for a database password and an unacceptable one for a
// staged rollout percent: the restart is exactly the risk the staged rollout is
// there to avoid.
//
// The shape is the same as the bot control plane's: a single-row table per
// settings group, a partial-update admin endpoint, and consumers that read a
// snapshot rather than a value captured at construction. Where it differs is the
// delivery — the bot polls GET /bot/plan over HTTP because it is a separate
// process, while these settings are consumed in-process, so the service pushes
// each new snapshot into the feed's holder and a background refresh loop repeats
// the read for instances that did not serve the write.
//
// What does not belong here: anything needed to reach the database, bind the
// port, or authenticate a request. Those stay in the environment — a setting
// stored in the database cannot be the setting that tells the process how to
// reach the database, and a knob that only takes effect at construction (the
// fanout worker count, the channel size) would offer a change that silently does
// nothing until the next restart.
package settings
