// Package config loads, validates, and exposes the configuration a process needs
// before it can reach anything else: the database, the port, the signing keys,
// the mail provider and fail-closed local storage configuration, and the two knobs that size the feed's fanout
// worker pool at construction.
//
// It is deliberately not where every setting lives. Anything an operator changes
// while watching a graph — the feed's rollout gates, its retention limits, its
// ranking weights — is in the settings.feed table and served by the settings
// context, because a redeploy per adjustment is the cost this package cannot
// avoid and that one does not have to pay. What stays here is what a stored value
// could not describe: how to reach the database, and how often to re-read the
// things that are in it.
package config
