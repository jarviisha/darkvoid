# Research: Feed Cursor Contract

## Decision: Use a flat no-version feed cursor

**Rationale**: The product is still in development and the client team has confirmed the breaking change. Keeping a `v` field or accepting old shapes would preserve ambiguous compatibility behavior with no current product value. A flat logical shape also matches the client-confirmed design and is easier to inspect during integration.

**Alternatives considered**:

- Keep `v` and increment later: rejected because the explicit product direction is no version/fallback compatibility for this stage.
- Accept both old and new shapes temporarily: rejected because stale clients should fail fast during development.
- Keep the current nested `timeline` object: rejected because it does not match the confirmed client contract.

## Decision: Reject obsolete cursor payloads strictly

**Rationale**: Cursor payloads that contain `v`, nested `timeline`, old `fallback_cursor`-only continuation from the previous shipped shape, or old session fields can produce ambiguous continuation state. The new contract should return the standard invalid cursor response instead of attempting partial interpretation.

**Alternatives considered**:

- Silent restart when an old cursor appears: rejected because it can hide client integration mistakes and show unexpected repeated items.
- Best-effort migration from old shape to new shape: rejected because the user explicitly does not need fallback compatibility.

## Decision: Include tie-breaker post IDs for timeline and trending scores

**Rationale**: Scores alone are insufficient when multiple posts share the same score. Timeline scores can share a microsecond, and local trending scores can share like counts. The cursor needs `tl_post_id` and `trend_post_id` alongside `tl_score` and `trend_score` so the server can continue deterministically without duplicate or skipped items.

**Alternatives considered**:

- Score-only cursor: rejected because same-score items are a known edge case.
- Use only rank offsets for all sources: rejected because timeline and local trending naturally sort by score plus post identity, while recommendation already has provider offset semantics.

## Decision: Treat `tl_user` as validation/observability, not feed ownership authority

**Rationale**: The authenticated user must remain the source of truth for which feed is served. `tl_user` is useful to detect accidental cross-account cursor use and to aid debugging, but it must never allow a client to select another user's feed.

**Alternatives considered**:

- Omit user state entirely: simpler, but loses a useful validation signal requested by the client-confirmed shape.
- Trust cursor user identity for feed selection: rejected as unsafe because cursors are client input.

## Decision: Trending continuation is source continuation, not public offset pagination

**Rationale**: Clients should continue storing one opaque `next_cursor`. Trending continuation can be represented internally by score and post ID without exposing a separate client-facing trending list or offset parameter. Provider-backed trending may still use provider pagination internally, but the public contract remains a single cursor.

**Alternatives considered**:

- Keep trending page-1 only: rejected because the new cursor contract explicitly asks for trending state.
- Expose a separate `trend_cursor` query parameter: rejected because it leaks source assembly details and complicates clients.

## Decision: Keep `/discover` unchanged

**Rationale**: This feature changes `/feed` cursor contract only. Discovery remains its own chronological cursor endpoint and was explicitly kept out of scope by the closed precomputed feed spec.

**Alternatives considered**:

- Align `/discover` with the new feed cursor: rejected as out of scope and unnecessary for client-confirmed feed pagination.
