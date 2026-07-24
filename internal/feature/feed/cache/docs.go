// Package cache defines feed cache and prepared timeline store implementations.
// It no longer owns feed browsing session state; feed continuation is encoded in
// no-version cursors and prepared timeline positions.
//
// Timeline keys are versioned (feed:tl:v2) because score semantics changed
// from UnixMicro timestamps to packed rank scores — the numeric ranges
// overlap, so legacy keys are never read again and expire via TTL. Writes have
// two modes: AddPost (ZADD NX, fan-out — never downgrades a refreshed score)
// and SetPostsBatch (plain upsert, background re-rank).
package cache
