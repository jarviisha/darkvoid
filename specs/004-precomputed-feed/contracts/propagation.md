# Contract: Feed Propagation

## Purpose

Defines feed-impacting events emitted after successful post and follow mutations. Feed propagation updates or refreshes prepared timelines without making the source mutation depend on propagation success.

## Events

### PostCreated

Emitted after a post creation transaction commits.

Fields:

- `post_id`
- `author_id`
- `visibility`
- `created_at`
- `score`

Rules:

- Public and follower-visible posts are eligible for propagation according to existing visibility rules.
- Propagation must be non-blocking or bounded so post creation latency is protected.
- Followers beyond the fanout cap may rely on lazy refresh.

### FollowCreated

Emitted after a follow relationship is created.

Fields:

- `follower_id`
- `followee_id`
- `created_at`

Rules:

- The follower's following-ID cache is invalidated.
- The follower's prepared timeline should be refreshed lazily on next feed request and may be warmed proactively.

### FollowDeleted

Emitted after a follow relationship is removed.

Fields:

- `follower_id`
- `followee_id`
- `created_at`

Rules:

- The follower's following-ID cache is invalidated.
- Response-time follow filtering must prevent posts by the unfollowed author from being returned as primary following items.
- Physical timeline cleanup is best-effort.

### PostDeletedOrVisibilityChanged

Emitted after a post is deleted or visibility changes.

Fields:

- `post_id`
- `author_id`
- `new_visibility`
- `created_at`

Rules:

- Response-time visibility filtering must prevent ineligible posts from being returned.
- Supplemental recommendation indexes and trending caches may be invalidated as they are today.
- Prepared timeline cleanup is best-effort.

## Worker Behavior

- Initial implementation uses in-process workers.
- Queue size, worker count, max fanout followers, timeline max items, and timeline retention are configurable.
- Worker errors are logged and measured, but source mutations remain successful.
- Missed fanout after process restart is recovered by lazy refresh on feed request.

## Rollout Signals

The rollout must measure:

- Fanout enqueue failures.
- Fanout processing latency.
- Prepared timeline read hit rate.
- Lazy refresh count and failures.
- Fallback usage rate.
- Cursor rejection rate.
- Redis memory pressure.
