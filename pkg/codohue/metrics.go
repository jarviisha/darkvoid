package codohue

import "sync/atomic"

// The two counters below cover the integration's silent degradation paths.
// Every failure they count is deliberately logged-and-continued — the feed
// must serve through a Codohue outage — but that policy means nothing louder
// than a log line says the index is drifting stale (posts created during an
// outage stay missing until `ctl codohue reindex`) or that the model is
// training on gappy behavior data. A counter on /metrics turns that drift
// into something a graph can alarm on.
var cmetrics codohueMetrics

type codohueMetrics struct {
	indexErrors        atomic.Uint64
	eventPublishErrors atomic.Uint64
}

// MetricsSnapshot is a point-in-time copy of Codohue integration counters.
type MetricsSnapshot struct {
	// IndexErrors counts failed index-maintenance calls: embedding upserts,
	// catalog ingests and object deletes. Each one is a post whose index
	// entry is now stale or missing.
	IndexErrors uint64 `json:"index_errors"`
	// EventPublishErrors counts behavior events that never reached the
	// codohue:events stream.
	EventPublishErrors uint64 `json:"event_publish_errors"`
}

// SnapshotMetrics returns current Codohue integration counters.
func SnapshotMetrics() MetricsSnapshot {
	return MetricsSnapshot{
		IndexErrors:        cmetrics.indexErrors.Load(),
		EventPublishErrors: cmetrics.eventPublishErrors.Load(),
	}
}

func countIndexError() { cmetrics.indexErrors.Add(1) }

func countEventPublishError() { cmetrics.eventPublishErrors.Add(1) }
