# Contract: Timeline Store

## Purpose

Provides feed-owned operations for reading, writing, trimming, and refreshing prepared primary feed timelines. Implementations may use Redis sorted sets, but callers depend on this contract rather than storage-specific APIs.

## Timeline Key Semantics

- One prepared primary timeline per user.
- Entries are ordered newest-first by score.
- Entry member identity is the post ID.
- Duplicate post entries for the same user are idempotent.

## Operations

### AddPost

Adds one post to one user's prepared timeline.

Input:

- `user_id`
- `post_id`
- `score`

Rules:

- Must be idempotent for duplicate `post_id`.
- Must not exceed the configured max timeline size after add and trim.
- Must update the timeline retention window.

### AddPostsBatch

Adds many posts to one user's prepared timeline, used by lazy refresh and backfill.

Rules:

- Must be idempotent.
- Must keep newest-first ordering.
- Must trim to max size.

### ReadPage

Reads candidate post IDs for a user from a continuation position.

Input:

- `user_id`
- optional `timeline_score`
- optional `timeline_post_id`
- `limit`

Output:

- Ordered post ID candidates.
- Last timeline position read.

Rules:

- Should over-fetch enough candidates for response-time filtering.
- Must not return storage errors as client-visible internal details.
- A miss or empty timeline is not fatal; it triggers lazy refresh/fallback behavior.

### Trim

Ensures a user's prepared timeline remains within bounds.

Rules:

- Maximum primary items: 1,000.
- Maximum retention: 7 days.

### RemovePostBestEffort

Best-effort cleanup for known stale prepared entries.

Rules:

- May be used for deleted posts or local user timeline cleanup.
- Not required for user-visible correctness because response-time filtering is authoritative.

## Failure Semantics

- Timeline store failures must not fail post creation.
- Timeline read failures should cause `/feed` to use fallback behavior when possible.
- Failures must be logged with sanitized fields and counted in operational signals.
