# Contract: Feed Cursor No-Version

## Encoding

The cursor remains opaque to clients. The server may encode it as base64 JSON, but clients must store and resend it without modifying individual fields.

## Logical Shape

```json
{
  "tl_score": 1234567890123,
  "tl_post_id": "post-uuid",
  "tl_user": "user-uuid",
  "rec_offset": 4,
  "trend_score": 987654321,
  "trend_post_id": "post-uuid"
}
```

Fields are omitted when the corresponding source has no remaining continuation state.

## Field Semantics

| Field | Description |
|-------|-------------|
| `tl_score` | Prepared timeline continuation score for the last returned primary timeline item. |
| `tl_post_id` | Tie-breaker for primary timeline continuation when multiple posts share `tl_score`. |
| `tl_user` | Feed owner validation/observability field. It must match the authenticated user but must not select the feed by itself. |
| `rec_offset` | Next recommendation source offset. This is internal source state inside the opaque cursor, not a public offset pagination parameter. |
| `trend_score` | Trending source continuation score for the last returned trending item. |
| `trend_post_id` | Tie-breaker for trending continuation when multiple trending posts share `trend_score`. |

## Validation Rules

- Missing cursor starts a new feed sequence.
- Cursors containing `v` are rejected.
- Cursors containing nested `timeline` are rejected.
- Old session-based cursor payloads are rejected.
- Numeric positions must be non-negative.
- Post ID and user ID fields must be valid UUID strings when present.
- `tl_post_id` is required when `tl_score` is present.
- `trend_post_id` is required when `trend_score` is present.
- `tl_user`, when present, must match the authenticated user.

## Paging Rules

- The cursor advances active feed sources together.
- The cursor must not require server-side feed session state.
- Recommendation continuation uses `rec_offset`.
- Timeline and trending continuation use score plus post ID tie-breakers.
- A response without `next_cursor` marks completion for the current feed sequence.
