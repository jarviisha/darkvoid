// Package migrations embeds versioned SQL for database readiness checks.
// Active modules start at 000001_init, the consolidated schema baseline;
// subsequent changes are append-only numbered up/down pairs per module.
// The bot tree retains its original versions solely for legacy installations
// and the separately guarded retirement workflow.
package migrations
