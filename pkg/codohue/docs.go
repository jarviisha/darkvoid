// Package codohue provides the client integration with Codohue services.
//
// Darkvoid speaks the wire shipped with Codohue v0.12.1, through the Codohue Go
// SDK modules at v0.7.0. Runtime traffic (recommendations, rank, trending, catalog ingest,
// events) goes to the data-plane API (cmd/api); one-time namespace
// provisioning authenticates against the separate admin plane (cmd/admin)
// via session login — see ProvisionNamespaceConfig for why it has not moved to
// v0.8.0's bearer auth or its sdk/go/admin wrapper.
// Object vectors are Codohue's to produce: darkvoid ships raw post content to
// the catalog pipeline (dense_source "catalog") and the server embeds it. There
// is no local vectorizer and no bring-your-own-embedding path, so nothing here
// has to keep a text representation in step with an embedding model.
// The feed integration expects paginated recommendation items with
// object_id, score, rank, limit, offset, and total metadata.
//
// Two v0.8.0 semantics matter to callers. Rankings now carry a per-item
// scored flag, surfaced on RankedItem: an unscored item was excluded (seen,
// authored, unindexed) rather than judged irrelevant, so it must not be read
// as score 0. And relevance scores changed normalization from per-request
// min-max to a batch-independent x/(x+k) map — ordering is unchanged and
// values are now comparable across calls, but not against anything recorded
// under v0.4.0. The value is not display-only, despite what this said before:
// mixed feed adds it to the local score with a weight of 20, so its scale is
// load-bearing for ordering. v0.12.0 replaced the serve-time x/(x+k) curve with
// a clamped cosine, which moves that scale — the weight has not been
// re-baselined against it.
//
// Not yet adopted from v0.8.0: catalog batch ingest (100 items per request),
// catalog reconciliation reads (changed_since paging), and the durable catalog
// Redis Stream transport. Ingest still goes one post per HTTP request with no
// retry queue, which is why an outage needs `darkvoidctl codohue reindex`.
//
// Ping reads the namespace instead of calling the SDK's Ping. /ping needs no
// credentials, so it answers 200 from a deployment that rejects every real call —
// which is not hypothetical: a namespace deleted out from under this service left
// GET /health reporting "active" for six days while every recommendation, rank
// and ingest returned 401. For the same reason 401 and 403 are the two 4xx
// statuses that open the circuit: they are not one call site's malformed request,
// they fail every caller identically until an operator intervenes, and an open
// circuit is what lets /health override a stale "active".
//
// Behavior events are the exception to "runtime traffic goes over HTTP": they
// are published to the codohue:events Redis Stream, which Codohue's consumer
// reads from whichever Redis Codohue owns. The Redis client is therefore passed
// in rather than derived from BaseURL, and it is not necessarily the same client
// the rest of darkvoid caches with — see config.CodohueConfig.EventsRedis. Two
// consequences: the circuit breaker does not cover PublishBehaviorEvent, which
// fails independently over Redis, and a nil client disables event publishing
// while leaving the HTTP surface fully working.
package codohue
