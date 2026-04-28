# Data Model: Refactor Feed and Discovery Pagination

## FeedPageState

Represents the opaque continuation state for `/feed`.

**Fields**:

- `version`: cursor/session schema version.
- `mode`: current feed phase, such as mixed, following, or discovery fallback.
- `following_cursor`: chronological local DB cursor for followed/self posts.
- `recommendation_offset`: next Codohue recommendation offset to request.
- `recommendation_total`: provider-reported total when available.
- `discovery_cursor`: chronological local DB cursor for public discovery fallback.
- `pending_items`: candidate post IDs and source metadata fetched in a previous request but not returned yet.
- `seen_post_ids`: bounded set of post IDs already returned in this browsing sequence.
- `created_at`: time the state was created.
- `expires_at`: time after which the cursor/session is invalid.

**Validation Rules**:

- `version` must be recognized.
- `mode` must be one of the known feed phases.
- Local post cursors must contain valid timestamps and UUIDs.
- `recommendation_offset` must be non-negative.
- `pending_items` and `seen_post_ids` must be bounded by configured caps.
- Expired or malformed state returns a client-correctable invalid cursor error.

**State Transitions**:

- Empty cursor starts mixed feed state.
- Mixed state advances source cursors and stores unreturned candidates as pending.
- When following and recommendation sources are exhausted, state transitions to discovery fallback.
- Empty result with no remaining source state ends pagination by omitting `next_cursor`.

## FeedCandidate

Represents an item being considered for a feed page before response enrichment.

**Fields**:

- `post_id`: candidate post identity.
- `source`: following, recommendation, trending, or discovery.
- `source_rank`: rank from provider or source order when available.
- `provider_score`: Codohue recommendation score when available.
- `local_score`: local scoring result.
- `blended_score`: final ordering score.
- `created_at`: post creation time for deterministic tie-breaking.

**Validation Rules**:

- Candidate must resolve to an eligible visible post before being returned.
- Duplicate `post_id` candidates collapse to one item using source precedence and strongest available ranking signal.
- Invalid provider IDs are filtered and logged.

## RecommendationPage

Represents a Codohue recommendations response consumed by Darkvoid.

**Fields**:

- `items`: ordered recommendation items.
- `limit`: provider page size.
- `offset`: provider page offset used for the response.
- `total`: provider-reported total count.

**Relationships**:

- Each `items[]` entry becomes a `FeedCandidate` if the object ID is valid and resolves to an eligible post.

## RecommendationItem

Represents a single provider recommendation.

**Fields**:

- `object_id`: provider object ID, expected to be a post ID.
- `score`: provider score where values above zero indicate personalized signal.
- `rank`: provider rank within the response.

**Validation Rules**:

- `object_id` must parse to a valid post UUID before DB resolution.
- `rank` must be positive when present.
- `score` below zero is treated as unusable provider data.

## DiscoveryCursor

Represents the public discovery continuation point.

**Fields**:

- `created_at`: timestamp of the last returned public post.
- `post_id`: UUID of the last returned public post.

**Validation Rules**:

- Must use deterministic descending order by `created_at`, then `post_id`.
- Must only apply to public, non-deleted posts.
- Viewer-specific fields do not alter cursor order.

## FeedItemResponse

Represents a post returned by `/feed`.

**Fields**:

- Existing post response fields.
- `score`: final feed score.
- `source`: feed source.
- `recommendation_score`: optional provider score for recommendation-backed items.
- `recommendation_rank`: optional provider rank for recommendation-backed items.

**Validation Rules**:

- Response shape remains wrapped in `data`.
- Optional provider fields are omitted when not applicable.
- `next_cursor` is omitted when no further items are available.
