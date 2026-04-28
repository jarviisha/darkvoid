# Contract: Codohue Provider Integration

## Purpose

Define the Darkvoid-side expectations for Codohue service/container `v0.4.0` consumed through the three Codohue Go SDK modules at tag `v0.2.0`.

## SDK Modules

All three modules must be upgraded together:

- `github.com/jarviisha/codohue/pkg/codohuetypes v0.2.0`
- `github.com/jarviisha/codohue/sdk/go v0.2.0`
- `github.com/jarviisha/codohue/sdk/go/redistream v0.2.0`

## Recommendations

Darkvoid expects recommendation responses to include:

```json
{
  "items": [
    {
      "object_id": "post_uuid",
      "score": 0.91,
      "rank": 1
    }
  ],
  "limit": 20,
  "offset": 0,
  "total": 100
}
```

**Rules**:

- `object_id` maps to a Darkvoid post ID.
- `score > 0` means personalized signal is available.
- `score = 0` means fallback or cold-start signal.
- `rank` is used as a deterministic provider ordering signal.
- `offset` is stored only inside Darkvoid's opaque feed continuation state.

## Trending

Darkvoid may use provider trending items for first-page or candidate-pool enrichment when enabled. Trending items must be treated as public-only candidates after DB resolution.

## Failure Handling

- Provider errors must not fail valid feed requests when local fallback content exists.
- Invalid object IDs, deleted posts, private posts, and duplicates are filtered.
- Provider usage, fallback usage, and filtered item counts must be observable through structured logs.
