# Data Model: Precomputed Feed Timeline

## Feed Timeline

Represents a user's prepared primary feed entries for posts from followed authors.

**Fields**:

- `user_id`: owner of the prepared timeline.
- `post_id`: referenced post.
- `score`: ordering score derived from post creation time in microseconds for MVP.
- `author_id`: author of the post, available after batch load and used for follow-state filtering.
- `created_at`: post creation timestamp after batch load.
- `expires_at`: derived retention boundary for prepared timeline content.

**Relationships**:

- Belongs to one user.
- References many posts by ID.
- Depends on current follow relationships and post visibility for response eligibility.

**Validation rules**:

- A timeline must contain no more than 1,000 primary followed-author items for one user.
- Prepared content must not be retained beyond 7 days.
- Duplicate `post_id` members for the same user are not allowed.
- A prepared entry is not sufficient for response eligibility; the post must still be visible and the author must still be followed at response time.

**State transitions**:

- `missing` -> `refreshing`: feed request or rollout backfill detects no usable prepared timeline.
- `refreshing` -> `ready`: enough eligible followed-author posts are prepared.
- `ready` -> `stale`: post/follow/visibility changes make some prepared entries stale.
- `stale` -> `ready`: lazy cleanup or response-time filtering leaves only eligible returned items.
- `ready|stale` -> `expired`: retention window or eviction removes prepared content.

## Feed Cursor V2

Represents the next continuation position for the new `/feed` contract.

**Fields**:

- `version`: fixed feed cursor version for this contract.
- `timeline_score`: last primary timeline score returned or considered.
- `timeline_post_id`: last primary timeline post ID used as a tie-breaker.
- `recommendation_offset`: next recommendation source offset.
- `trending_cursor`: next trending source cursor or score.
- `fallback_cursor`: next fallback/discovery-like cursor for feed continuation.
- `issued_at`: cursor issue timestamp.

**Relationships**:

- Used only with authenticated `/feed` requests.
- Does not reference `feed:session` or server-side continuation state.

**Validation rules**:

- Missing cursor starts a new feed browsing sequence.
- Unsupported versions, old session-based cursor payloads, malformed payloads, and tampered payloads are client validation errors.
- Cursor positions must be non-negative where numeric.
- Cursor must not authorize access to a different user's feed; the server derives user identity from authentication.

**State transitions**:

- `absent` -> `issued`: first page returns more data.
- `issued` -> `advanced`: next page returns more data.
- `issued|advanced` -> `complete`: response has no next cursor.
- `any` -> `rejected`: cursor is malformed, unsupported, or legacy.

## Feed Propagation Event

Represents a feed-impacting mutation that may update or invalidate prepared timelines.

**Fields**:

- `event_type`: `post_created`, `follow_created`, `follow_deleted`, `post_visibility_changed`, or `post_deleted`.
- `post_id`: affected post when applicable.
- `author_id`: affected author when applicable.
- `actor_id`: user who performed the mutation when applicable.
- `score`: ordering score for post-created fanout.
- `created_at`: event time.

**Relationships**:

- `post_created` targets followers of the author.
- `follow_created` targets the follower's own timeline refresh.
- `follow_deleted` invalidates the follower's current eligibility for that author's prepared entries.
- `post_deleted` and `post_visibility_changed` create stale prepared entries that response-time filtering must suppress.

**Validation rules**:

- Propagation must run only after the source mutation succeeds.
- Propagation failure is non-fatal for the source mutation.
- Fanout must be bounded for hot authors.

## Timeline Refresh

Represents a refresh process that rebuilds or warms a user's prepared timeline.

**Fields**:

- `user_id`: timeline owner.
- `trigger`: `lazy_feed_request`, `backfill`, `follow_created`, or `manual_rollout`.
- `requested_limit`: number of source candidates to consider.
- `prepared_count`: number of entries added.
- `started_at`, `finished_at`: timing for operational measurement.
- `error`: sanitized failure reason for logs/metrics.

**Relationships**:

- Reads current following list.
- Reads eligible recent posts from followed authors.
- Writes prepared timeline entries.

**Validation rules**:

- Lazy refresh must be available when the user requests feed and the timeline is missing, expired, or incomplete.
- Background refresh for active users is optional.
- Refresh must not block the feed response beyond the response target; if refresh cannot complete quickly, the response uses bounded fallback or empty-state behavior.

## Stale Prepared Entry

Represents a prepared entry that no longer matches authoritative visibility or follow state.

**Fields**:

- `user_id`
- `post_id`
- `stale_reason`: `post_deleted`, `post_hidden`, `visibility_changed`, or `unfollowed_author`.
- `detected_at`: when the response path or cleanup detected staleness.

**Relationships**:

- Derived from Feed Timeline entries and authoritative source data.

**Validation rules**:

- Stale prepared entries must never be returned to users.
- Physical cleanup can be lazy or best-effort.
- Detection should be measurable through operational signals.

## Supplemental Feed Candidate

Represents non-primary content that can fill a feed page.

**Fields**:

- `post_id`
- `source`: `recommendation`, `trending`, or `fallback`
- `source_score`: provider or local score when available.
- `source_rank`: provider or local rank when available.
- `cursor_position`: continuation position for the source.

**Relationships**:

- May reference the same post as a primary timeline entry; duplicates must be collapsed before response.
- Subject to the same response-time visibility checks as primary entries.

**Validation rules**:

- Supplemental content must be bounded per feed page.
- Supplemental provider failure must not fail the feed response.
- Supplemental source continuation is represented in Feed Cursor V2, not in server-side session state.
