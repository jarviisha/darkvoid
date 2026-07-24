// Package feed provides core feed domain logic such as ranking, scoring, reader
// ports, recommendation provider contracts, no-version feed cursors, prepared
// timeline storage contracts, refresh coordination, and fanout orchestration.
//
// Prepared timelines are ranked at write time: ZSET scores are packed rank
// scores (see PackTimelineScore — rank bucket over createdAt seconds), written
// by fan-out (NX, write-time constant) and by the background refresher
// (upsert, local formula). The read path serves the materialized order and
// never re-ranks.
package feed
