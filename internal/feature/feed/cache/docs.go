// Package cache defines feed cache and prepared timeline store implementations.
// It no longer owns feed browsing session state; feed continuation is encoded in
// no-version cursors and prepared timeline positions.
//
// Timeline keys are versioned (feed:tl:v2) because score semantics changed
// from UnixMicro timestamps to packed rank scores — the numeric ranges
// overlap, so legacy keys are never read again and expire via TTL. Writes have
// two modes: AddPost (ZADD NX, fan-out — never downgrades a refreshed score)
// and SetPostsBatch (plain upsert, background re-rank). Refresher rebuilds use
// an atomic replacement script that removes stale members while preserving
// entries whose companion write-time marker shows fanout happened during the
// database read, including delayed outbox events for older posts.
//
// The trim bound and TTL are read from the live feed.Settings snapshot on each
// write rather than captured when the store is built, so lowering either starts
// reclaiming memory on the next fanout. Note the asymmetry: a lowered TTL only
// applies to keys written after the change, because Redis holds one expiry per
// key and existing timelines keep theirs until something writes to them again.
package cache
