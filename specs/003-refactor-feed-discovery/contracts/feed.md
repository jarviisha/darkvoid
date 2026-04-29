# Contract: GET /feed

## Purpose

Return the authenticated user's stable personalized feed, mixing followed posts, recommendation-backed posts, trending posts, and discovery fallback without duplicate or skipped items across cursor pages.

## Request

```http
GET /feed?cursor={opaque_cursor}
Authorization: Bearer <access_token>
```

**Query Parameters**:

- `cursor` optional opaque continuation token returned by the previous response.

## Success Response

```json
{
  "data": [
    {
      "id": "post_uuid",
      "author_id": "user_uuid",
      "author": {
        "id": "user_uuid",
        "username": "alice",
        "display_name": "Alice",
        "avatar_url": "https://example.test/avatar.jpg"
      },
      "content": "post content",
      "visibility": "public",
      "media": [],
      "like_count": 12,
      "comment_count": 3,
      "is_liked": false,
      "is_following_author": true,
      "created_at": "2026-04-28T10:00:00Z",
      "updated_at": "2026-04-28T10:00:00Z",
      "score": 42.3,
      "source": "recommendation",
      "recommendation_score": 0.91,
      "recommendation_rank": 1
    }
  ],
  "next_cursor": "opaque_cursor"
}
```

**Response Rules**:

- `data` is always present.
- `next_cursor` is omitted when no further feed items are available.
- `recommendation_score` and `recommendation_rank` are optional and only present when the item has Codohue recommendation metadata.
- `source` must identify the effective feed source used for ranking and observability.

## Error Responses

- `400` with standard error envelope when `cursor` is malformed, expired, or incompatible.
- `401` with standard error envelope when authentication is missing or invalid.
- `500` with standard error envelope for unexpected server errors.

## Pagination Contract

- Clients treat `next_cursor` as opaque.
- Clients must not inspect or construct cursor values.
- The server must not expose client-facing offset pagination for `/feed`.
- Reusing a valid cursor should continue the same browsing sequence without returning post IDs already emitted earlier in that sequence.
- The cursor may reference short-lived server-side continuation state; clients still only store and resend the opaque `next_cursor`.

## Compatibility Notes

- Cursor encoding may change from the current following-only cursor format.
- Existing clients that pass returned cursors unchanged remain compatible.
- API documentation must be regenerated if optional recommendation metadata fields are added to the response.
