// Package codohue provides the client integration with Codohue services.
//
// Darkvoid targets Codohue v0.4.0 through the Codohue Go SDK modules at
// v0.4.0. Runtime traffic (recommendations, rank, trending, embeddings,
// events) goes to the data-plane API (cmd/api); one-time namespace
// provisioning authenticates against the separate admin plane (cmd/admin)
// via session login, since the runtime SDK does not wrap operator routes.
// Object vectors come from one of two selectable sources
// (CODOHUE_DENSE_SOURCE): "byoe" pushes locally computed TF-IDF vectors,
// "catalog" ships raw post content for server-side auto-embedding.
// The feed integration expects paginated recommendation items with
// object_id, score, rank, limit, offset, and total metadata.
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
