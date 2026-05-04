# Contract: GET /feed

## Summary

Returns a personalized feed page using the new precomputed timeline contract. This contract is breaking relative to the previous session-based feed cursor.

## Authentication

Required. The authenticated user determines the feed owner. Cursor payloads must not be trusted to identify the user.

## Request

```http
GET /feed?cursor=<opaque_cursor>
Authorization: Bearer <token>
```

### Query Parameters

| Name | Required | Description |
|------|----------|-------------|
| `cursor` | No | Opaque cursor returned by the previous `/feed` response. Must be a valid cursor for the new contract. |

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
  "next_cursor": "opaque-v2-cursor"
}
```

### Response Rules

- `data` is always present and is an array.
- `next_cursor` is omitted when there are no additional feed items.
- `source` values are `following`, `recommendation`, `trending`, or `discover`.
- Posts that are deleted, hidden, or no longer eligible due to follow state must not be returned.
- Supplemental source failure must still return a valid feed response using available sources.

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

- Previous session-based feed cursor.
- Unsupported cursor version.
- Malformed base64 or JSON.
- Negative source positions.
- Tampered cursor payload.

## Compatibility

No backward compatibility is provided for old feed cursors. Clients must start without a cursor after upgrading and must use only `next_cursor` values returned by this contract.
