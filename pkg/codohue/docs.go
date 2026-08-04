// Package codohue provides the client integration with Codohue services.
//
// Darkvoid targets Codohue v0.8.0 through the Codohue Go SDK modules at
// v0.5.0. Runtime traffic (recommendations, rank, trending, embeddings,
// events) goes to the data-plane API (cmd/api); one-time namespace
// provisioning authenticates against the separate admin plane (cmd/admin)
// via session login — see ProvisionNamespaceConfig for why it has not moved to
// v0.8.0's bearer auth or its sdk/go/admin wrapper.
// Object vectors come from one of two selectable sources
// (CODOHUE_DENSE_SOURCE): "byoe" pushes locally computed TF-IDF vectors,
// "catalog" ships raw post content for server-side auto-embedding.
// The feed integration expects paginated recommendation items with
// object_id, score, rank, limit, offset, and total metadata.
//
// Two v0.8.0 semantics matter to callers. Rankings now carry a per-item
// scored flag, surfaced on RankedItem: an unscored item was excluded (seen,
// authored, unindexed) rather than judged irrelevant, so it must not be read
// as score 0. And relevance scores changed normalization from per-request
// min-max to a batch-independent x/(x+k) map — ordering is unchanged and
// values are now comparable across calls, but not against anything recorded
// under v0.4.0. Nothing in darkvoid thresholds on the value today; the feed
// carries it through to RecommendationScore for display only.
//
// Not yet adopted from v0.8.0: catalog batch ingest (100 items per request),
// catalog reconciliation reads (changed_since paging), and the durable catalog
// Redis Stream transport. Ingest still goes one post per HTTP request with no
// retry queue, which is why an outage needs `darkvoidctl codohue reindex`.
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
