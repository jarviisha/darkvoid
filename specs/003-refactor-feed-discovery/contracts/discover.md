# Contract: GET /discover

## Purpose

Return public discovery posts using deterministic chronological cursor pagination. Discovery remains independent from personalized recommendation ranking.

## Request

```http
GET /discover?cursor={opaque_cursor}&limit=20
Authorization: Bearer <access_token>  # optional
```

**Query Parameters**:

- `cursor` optional opaque continuation token returned by the previous response.
- `limit` optional positive integer, capped at the existing maximum.

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
        "display_name": "Alice"
      },
      "content": "post content",
      "visibility": "public",
      "media": [],
      "like_count": 12,
      "comment_count": 3,
      "is_liked": false,
      "is_following_author": false,
      "created_at": "2026-04-28T10:00:00Z",
      "updated_at": "2026-04-28T10:00:00Z"
    }
  ],
  "next_cursor": "opaque_cursor"
}
```

## Error Responses

- `400` with standard error envelope when `cursor` is malformed.
- `500` with standard error envelope for unexpected server errors.

## Pagination Contract

- Discovery order is descending by post creation time, then post ID.
- Discovery only returns public, non-deleted posts.
- Authentication may enrich viewer-specific flags but must not change page membership or ordering.
- `next_cursor` is omitted when no further public posts are available.
