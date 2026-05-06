# Data Model: Feed Cursor Contract

## Feed Cursor

Opaque continuation token returned by `/feed` and sent back by clients in the `cursor` query parameter.

### Logical Fields

| Field | Type | Required | Purpose |
|-------|------|----------|---------|
| `tl_score` | integer | Conditional | Continuation score for the prepared primary timeline. Present when timeline continuation remains active. |
| `tl_post_id` | UUID string | Conditional | Tie-breaker for primary timeline continuation when several posts share `tl_score`. Required when `tl_score` is present. |
| `tl_user` | UUID string | Required for emitted cursors | Feed owner validation/observability field. Must match the authenticated user when present. |
| `rec_offset` | integer | Conditional | Next recommendation source offset. Present when recommendation continuation remains active. |
| `trend_score` | number | Conditional | Continuation score for trending source. Present when trending continuation remains active. |
| `trend_post_id` | UUID string | Conditional | Tie-breaker for trending continuation when several posts share `trend_score`. Required when `trend_score` is present. |

### Validation Rules

- Cursor MUST NOT include `v`.
- Cursor MUST NOT include the prior nested `timeline` object.
- Cursor MUST NOT include old session-state fields such as session ID, pending items, or seen IDs.
- Missing cursor means start a new feed sequence.
- `tl_score`, `rec_offset`, and `trend_score` must be non-negative when present.
- `tl_post_id`, `trend_post_id`, and `tl_user` must be valid UUID strings when present.
- `tl_post_id` is required when `tl_score` is present.
- `trend_post_id` is required when `trend_score` is present.
- `tl_user` mismatch with the authenticated user invalidates the cursor.

## Timeline Continuation

Represents the last consumed prepared timeline item.

### Fields

- `tl_score`: timeline ordering score.
- `tl_post_id`: tie-breaker post ID.

### State Transitions

- Empty at start of feed sequence.
- Set when the response includes primary timeline items and more timeline entries may remain.
- Advanced to the last returned primary timeline item.
- Omitted when no primary timeline continuation remains.

## Recommendation Continuation

Represents the next recommendation provider position.

### Fields

- `rec_offset`: next recommendation offset.

### State Transitions

- Starts at zero when recommendation source is active.
- Advances by the number of recommendation items consumed or accepted from the provider page.
- Omitted when recommendation source is exhausted or inactive.

## Trending Continuation

Represents the last consumed trending source item.

### Fields

- `trend_score`: trending ordering score.
- `trend_post_id`: tie-breaker post ID.

### State Transitions

- Empty at start of feed sequence.
- Set when the response includes trending items and more trending candidates may remain.
- Advanced to the last returned trending item.
- Omitted when trending source is exhausted or inactive.

## Feed Owner

Authenticated account whose feed is being served.

### Rules

- Authenticated identity selects the feed.
- `tl_user` validates that a cursor was emitted for the same authenticated account.
- A mismatched `tl_user` returns the same invalid cursor error as other malformed cursor cases.
