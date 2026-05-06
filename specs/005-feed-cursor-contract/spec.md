# Feature Specification: Feed Cursor Contract

**Feature Branch**: `005-feed-cursor-contract`  
**Created**: 2026-05-06  
**Status**: Closed  
**Input**: User description: "Đổi thiết kế feed cursor contract theo client đã xác nhận. Không cần field `v`/version vì sản phẩm vẫn đang phát triển và không cần giữ fallback compatibility. Cursor mới cần dùng các field như `tl_score`, `tl_user`, `rec_offset`, `trend_score` để tiếp tục các feed sources."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Continue Feed With New Cursor (Priority: P1)

As a client developer, I want the feed endpoint to return and accept the newly agreed cursor fields so that the client can paginate the feed using the latest contract without carrying legacy cursor handling.

**Why this priority**: This is the breaking contract change the client team has confirmed. Without it, clients and backend will disagree on the cursor payload and feed pagination can fail after the first page.

**Independent Test**: Can be fully tested by requesting the first authenticated feed page, reading the returned cursor, requesting the next page with that cursor, and verifying that the next page continues without duplicates or a version field.

**Acceptance Scenarios**:

1. **Given** an authenticated client requests the first feed page, **When** more feed items are available, **Then** the response includes an opaque cursor whose decoded logical fields include timeline, recommendation, and trending continuation state without a version field.
2. **Given** an authenticated client sends a cursor returned by the new contract, **When** it requests the next feed page, **Then** the response continues from the prior page without repeating already consumed feed items from tracked sources.
3. **Given** the feed has no additional items, **When** the client requests a page, **Then** the response omits the next cursor.

---

### User Story 2 - Reject Obsolete Cursor Shapes (Priority: P2)

As a client developer, I want obsolete feed cursor shapes to fail clearly so that stale clients surface integration errors during development instead of silently receiving ambiguous feed results.

**Why this priority**: The product is still in development and the client team has accepted the breaking change, so supporting old cursor variants would add ambiguity without user value.

**Independent Test**: Can be tested by sending cursors that use the prior nested timeline shape, include a version field, or use old session state, and verifying they are rejected with the standard invalid cursor response.

**Acceptance Scenarios**:

1. **Given** a client sends a cursor containing a `v` field, **When** it requests the feed, **Then** the server rejects the cursor as invalid.
2. **Given** a client sends the previously shipped nested timeline cursor shape, **When** it requests the feed, **Then** the server rejects the cursor as invalid.
3. **Given** a client sends an old session-based cursor, **When** it requests the feed, **Then** the server rejects the cursor as invalid.
4. **Given** a client starts without a cursor, **When** it requests the feed, **Then** the server starts a fresh feed sequence successfully.

---

### User Story 3 - Document Cursor Field Meaning (Priority: P3)

As a client developer, I want the new cursor fields documented with their purpose and ownership rules so that client code treats the cursor correctly and does not rely on unsafe assumptions.

**Why this priority**: The cursor remains opaque in normal client behavior, but field-level documentation is needed for coordinated backend/client development while the product is still evolving.

**Independent Test**: Can be tested by reviewing the published contract and confirming every new cursor field has a defined purpose, validation expectation, and client responsibility.

**Acceptance Scenarios**:

1. **Given** the cursor contract is documented, **When** a developer reviews it, **Then** each field has a clear explanation and pagination purpose.
2. **Given** the cursor includes user identity state, **When** the server processes it, **Then** authenticated identity remains the source of truth for feed ownership.
3. **Given** the client stores a cursor, **When** it sends a later feed request, **Then** the client treats the cursor as opaque and does not mutate individual fields.

### Edge Cases

- Client sends an empty cursor or omits the cursor parameter.
- Client sends a malformed, non-decodable, or partially missing cursor.
- Client sends a cursor with negative timeline, recommendation, or trending positions.
- Client sends a cursor whose user field does not match the authenticated user.
- Client sends a cursor from the previously shipped nested timeline shape.
- Client sends a cursor containing `v` or any old session-style fields.
- Multiple timeline or trending items share the same score.
- Recommendation or trending sources are temporarily unavailable while a valid cursor is present.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST use a new feed cursor contract that does not include a version field.
- **FR-002**: System MUST return only the new cursor shape for feed continuation after this feature is complete.
- **FR-003**: System MUST reject cursors containing the obsolete `v` field.
- **FR-004**: System MUST reject the previously shipped nested timeline cursor shape.
- **FR-005**: System MUST reject old session-based cursor payloads and unsupported legacy cursor fields.
- **FR-006**: System MUST allow a missing cursor to start a new feed sequence.
- **FR-007**: System MUST include a timeline continuation score in the cursor so the next page can continue from the primary prepared feed position.
- **FR-008**: System MUST include a feed owner field in the cursor for validation or observability, while authenticated identity remains the authority for which feed is served.
- **FR-009**: System MUST include a recommendation continuation offset in the cursor when recommendation pagination state remains active.
- **FR-010**: System MUST include a trending continuation score in the cursor when trending pagination state remains active.
- **FR-011**: System MUST validate that numeric cursor positions are non-negative.
- **FR-012**: System MUST produce a clear invalid cursor error for malformed, obsolete, or inconsistent cursor payloads.
- **FR-013**: System MUST avoid duplicate posts across consecutive feed pages requested with the new cursor contract.
- **FR-014**: System MUST omit the next cursor when no tracked feed source has additional items.
- **FR-015**: System MUST document each cursor field, including whether clients may inspect it, mutate it, or must treat it as opaque.
- **FR-016**: System MUST keep discovery endpoint pagination outside the scope of this feed cursor contract change.

### Key Entities *(include if feature involves data)*

- **Feed Cursor**: The opaque continuation token returned by the feed endpoint and sent back by clients to request the next feed page. Its logical fields include timeline score, feed owner, recommendation offset, and trending score.
- **Timeline Continuation**: The position of the last consumed primary feed item, represented by a score and any additional tie-breaking behavior needed to prevent duplicates.
- **Recommendation Continuation**: The next position in the recommendation source, represented by an offset.
- **Trending Continuation**: The next position in the trending source, represented by a score and any additional tie-breaking behavior needed to prevent duplicates.
- **Feed Owner**: The authenticated account whose feed is being served. Any cursor user field is secondary validation or observability state, not the authority for selecting a feed.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of feed cursors returned after this feature use the new no-version cursor contract.
- **SC-002**: 100% of obsolete cursor shapes tested during contract verification are rejected with the standard invalid cursor response.
- **SC-003**: Consecutive feed pages requested with the new cursor show no duplicate posts in contract tests covering timeline, recommendation, and trending continuation.
- **SC-004**: Client integration can request at least three consecutive feed pages using only the returned cursor, without special handling for old cursor formats.
- **SC-005**: Cursor contract documentation names and explains every field required by the client/backend agreement.

## Assumptions

- The product is still in development, so backward compatibility with previously shipped feed cursors is not required.
- Client developers have confirmed they will update to the new cursor contract.
- The feed endpoint remains authenticated, and authenticated identity remains the source of truth for feed ownership.
- The cursor remains opaque for normal client behavior even though its logical fields are documented for development coordination.
- Discovery pagination remains unchanged and is not part of this feature.
