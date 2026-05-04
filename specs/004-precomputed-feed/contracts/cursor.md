# Contract: Feed Cursor V2

## Encoding

The cursor is opaque to clients. The server may encode it as base64 JSON, but clients must treat it as an uninterpreted token.

## Logical Shape

```json
{
  "v": 2,
  "tl_score": 1777701600000000,
  "tl_post_id": "post-uuid",
  "rec_offset": 20,
  "trend_cursor": "source-specific",
  "fallback_cursor": "source-specific",
  "issued_at": "2026-05-02T10:00:00Z"
}
```

## Field Semantics

| Field | Description |
|-------|-------------|
| `v` | Cursor contract version. Must be `2` for this feature. |
| `tl_score` | Primary timeline continuation score. |
| `tl_post_id` | Tie-breaker for primary timeline continuation when multiple posts share a score. |
| `rec_offset` | Next recommendation source offset. |
| `trend_cursor` | Next trending source position. |
| `fallback_cursor` | Next fallback/discovery-like source position inside `/feed`. |
| `issued_at` | Time the cursor was issued, used for validation and observability. |

## Validation Rules

- Missing cursor starts a new feed sequence.
- `v` values other than `2` are rejected.
- Old `FeedPageState` payloads are rejected.
- Malformed, tampered, or unsupported source positions are rejected.
- Numeric positions must be non-negative.
- The authenticated user, not the cursor, determines feed ownership.

## Paging Rules

- The cursor advances all active feed sources together.
- The cursor must not require server-side `feed:session` state.
- The cursor must work when the previous page included fallback or supplemental content.
- A response without `next_cursor` marks completion for the current feed sequence.
