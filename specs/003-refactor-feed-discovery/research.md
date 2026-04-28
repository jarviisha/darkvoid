# Research: Refactor Feed and Discovery Pagination

## Decision: Upgrade all Codohue Go SDK modules to tag v0.2.0

**Rationale**: Darkvoid currently depends on three Codohue modules at `v0.1.0`, while the target service/container behavior is Codohue `v0.4.0`. The user clarified that the compatible SDK tag for all three Go modules is `v0.2.0`. Planning against `v0.2.0` avoids adapter shims for obsolete ID-only recommendation responses and lets the feed consume item-level `object_id`, `score`, `rank`, `limit`, `offset`, and `total`.

**Alternatives considered**:

- Keep SDKs at `v0.1.0` and manually call the newer HTTP response shape: rejected because it duplicates SDK responsibility and increases drift.
- Support both old ID-only and new object responses long term: rejected because the requested target is full compatibility with the new SDK/service contract.

## Decision: Keep client-facing `/feed` pagination as an opaque cursor, not offset

**Rationale**: The constitution requires cursor pagination for feed/list endpoints. Codohue recommendation `offset` is acceptable only as internal provider continuation state. Clients continue to pass a single opaque `cursor`; Darkvoid decodes it or resolves it to feed session state and uses DB cursors for local post sources.

**Alternatives considered**:

- Expose `offset` or `recommendation_offset` query parameters to clients: rejected because it violates the endpoint pagination model and leaks provider mechanics.
- Keep the current following-only cursor: rejected because it duplicates or skips items when page 1 ranking mixes following, trending, and recommendations.

## Decision: Introduce versioned feed continuation state with source cursors, pending candidates, and seen IDs

**Rationale**: Mixed-source ranking fetches more candidates than it returns. If the system advances a source cursor past candidates that were fetched but not shown, those candidates become unreachable. If it advances only to the last shown following item, already-shown items can repeat. Feed state must therefore track:

- per-source cursor positions
- Codohue recommendation offset and total
- pending candidates fetched but not shown
- seen post IDs already returned in the browsing sequence
- mode transitions into discovery fallback

This state can be represented by a versioned cursor token and, when Redis is available, a short-lived feed session record keyed by that token.

**Alternatives considered**:

- Stateless cursor containing every field and all pending/seen IDs: viable for small sessions but risks large URLs after several pages.
- Redis-only feed sessions: best for compact cursors, but requires clear fallback behavior when Redis is disabled.
- Recompute every page from scratch and filter by score cursor: rejected because scores change over time and local DB sources are ordered chronologically, not by blended score.

## Decision: Use bounded short-lived feed session state, not reusable per-user feed cache

**Rationale**: The constitution prohibits per-user feed caching without capacity justification. This feature needs continuation state, not a cache of rendered feed pages. The plan limits state to active browsing sequences:

- TTL measured in minutes, aligned with normal infinite-scroll sessions
- capped pending IDs and seen IDs
- no cached rendered responses
- no long-lived precomputed per-user feed
- fallback path when Redis is unavailable

This keeps memory use bounded while preserving correctness across mixed-source pagination.

**Alternatives considered**:

- Existing Redis feed cache for full feed pages: rejected because it would become per-user feed caching and stale quickly.
- No session state: rejected because it cannot guarantee no duplicates/no skips with ranked mixed sources.

## Decision: Preserve `/discover` as chronological public-post DB cursor pagination

**Rationale**: Discovery already matches the desired deterministic behavior: public posts ordered by `(created_at DESC, id DESC)` and continued by `(created_at, id)`. The refactor should not personalize discovery ordering. Feed fallback to discovery should reuse the discovery cursor but filter out feed-session seen IDs.

**Alternatives considered**:

- Make discovery use Codohue trending/recommendations: rejected because discovery is the anonymous public fallback and must stay predictable.
- Keep feed fallback starting discovery from the newest post without seen filtering: rejected because it can repeat public posts already shown as recommendations or trending.

## Decision: Use Codohue `score` and `rank` as recommendation signals, then blend with local score

**Rationale**: Codohue v0.4.0 distinguishes personalized signal (`score > 0`) from fallback/cold responses (`score = 0`) and returns rank. Darkvoid should preserve those signals in feed scoring, avoid flat presence-only boosts, and use deterministic tie-breakers for posts with equal blended scores.

**Alternatives considered**:

- Continue flat `cfBonus`: rejected because it ignores provider ranking and score semantics.
- Fully trust Codohue ordering and ignore local recency/relationship signals: rejected because feed still includes followed content and must remain useful when Codohue returns fallback scores.

## Decision: Preserve provider order when resolving post details by IDs

**Rationale**: DB `WHERE id = ANY($1)` does not guarantee output order. Recommendation and trending metadata must be joined back to fetched posts by object ID, then ordered by provider rank or blended score. Missing, deleted, private, or invalid IDs are filtered and logged.

**Alternatives considered**:

- Trust DB return order: rejected because it is not deterministic.
- Add database-specific ordering to every ID lookup: useful for some cases, but feed still needs metadata joins and filtering by source.

## Decision: Restore follower-visible content in following feed if product rules require it

**Rationale**: Existing cursor query only returns `public` posts for followed authors, while older feed queries included `public` and `followers`. If the product rule is that followers see followers-only posts, the refactor must include that in the following lane while keeping discovery/recommendation/trending public-only.

**Alternatives considered**:

- Keep following lane public-only: rejected unless product explicitly redefines follower feed visibility.
- Allow followers-only posts in discovery/recommendation/trending: rejected because public surfaces should not expose non-public content.
