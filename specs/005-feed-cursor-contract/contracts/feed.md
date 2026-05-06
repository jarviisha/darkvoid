# Contract: GET /feed

## Summary

Returns an authenticated feed page using the no-version feed cursor contract. This is a breaking cursor change from the closed precomputed feed contract.

## Authentication

Required. The authenticated user determines the feed owner. Cursor payloads must not be trusted to select a user feed.

## Request

```http
GET /feed?cursor=<opaque_cursor>
Authorization: Bearer <token>
```

### Query Parameters

| Name | Required | Description |
|------|----------|-------------|
| `cursor` | No | Opaque cursor returned by the previous `/feed` response. Must use the no-version feed cursor contract. |

## Success Response

```json
{
  "data": [
    {
      "id": "post-uuid",
      "author_id": "author-uuid",
      "author": {
        "id": "author-uuid",
        "username": "alice",
        "display_name": "Alice",
        "avatar_url": "https://cdn.example/avatar.jpg"
      },
      "content": "hello",
      "visibility": "public",
      "media": [],
      "like_count": 12,
      "comment_count": 3,
      "is_liked": false,
      "is_following_author": true,
      "created_at": "2026-05-02T10:00:00Z",
      "updated_at": "2026-05-02T10:00:00Z",
      "score": 42.5,
      "source": "following",
      "recommendation_score": 0.97,
      "recommendation_rank": 2
    }
  ],
  "next_cursor": "opaque-no-version-cursor"
}
```

### Response Rules

- `data` is always present and is an array.
- `next_cursor` is omitted when no tracked feed source has additional items.
- `next_cursor`, when present, decodes to a logical shape without `v`.
- `source` values remain `following`, `recommendation`, `trending`, or `discover`.
- Posts that are deleted, hidden, or no longer eligible due to follow state must not be returned.
- Recommendation or trending source failures must still return a valid feed response using available sources.

## Error Responses

### Unauthenticated

```json
{
  "error": {
    "code": "UNAUTHORIZED",
    "message": "user not authenticated"
  }
}
```

Status: `401`

### Invalid Cursor

```json
{
  "error": {
    "code": "BAD_REQUEST",
    "message": "invalid cursor"
  }
}
```

Status: `400`

Invalid cursor cases include:

- Cursor containing `v`.
- Cursor containing the prior nested `timeline` shape.
- Previous session-based feed cursor.
- Malformed base64 or JSON.
- Negative source positions.
- Invalid or mismatched `tl_user`.
- Missing tie-breaker post ID for timeline or trending continuation.

## Compatibility

No backward compatibility is provided for old feed cursors. Clients must start without a cursor after upgrading and must use only `next_cursor` values returned by this contract.
