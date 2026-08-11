// Package feed provides core feed domain logic such as ranking, scoring, reader
// ports, recommendation provider contracts, no-version feed cursors, prepared
// timeline storage contracts, refresh coordination, fanout orchestration, and
// a transactional PostgreSQL outbox with retry/dead-letter delivery for post
// and follow mutations.
//
// Prepared timelines are ranked at write time: ZSET scores are packed rank
// scores (see PackTimelineScore — rank bucket over createdAt seconds), written
// by fan-out (NX, write-time constant) and by the background refresher
// (atomic snapshot replacement, local formula). The read path serves the
// materialized order and never re-ranks.
//
// The knobs that shape all of the above — whether timelines are served and to
// whom, how many entries they hold and for how long, whether fanout runs, its
// follower cap, and the three ranking weights — are not captured at construction.
// They live in one *Settings holder that the read path, the ranker, the timeline
// store, the refresher and the dispatcher all read, and the settings context
// swaps in a new snapshot when an operator edits them. That is what makes a
// rollout percent something you can raise without a restart, which is the only
// way a staged rollout is worth staging.
//
// The two exceptions stay environment-fed and construction-time: the dispatcher's
// worker count and queue size, because they allocate goroutines and a channel.
package feed
