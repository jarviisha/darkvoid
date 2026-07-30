// Package redis provides Redis configuration and client construction helpers.
//
// Two constructors, differing only in what a server that is down at
// construction time costs. New pings and returns an error, for a Redis the
// caller cannot run correctly without. NewLazy skips the ping and hands back a
// client that dials on first use, for an optional dependency — there,
// discarding the client on a failed boot probe would disable the feature for
// the life of the process instead of for the length of the outage.
package redis
