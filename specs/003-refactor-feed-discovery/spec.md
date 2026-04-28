# Feature Specification: Refactor Feed and Discovery Pagination

**Feature Branch**: `003-refactor-feed-discovery`  
**Created**: 2026-04-28  
**Status**: Draft  
**Input**: User description: "Upgrade all Codohue client integrations to match Codohue service container tag v0.4.0, which adds paginated recommendation responses with score/rank per item. Plan the optimal refactor for feed and discovery so pagination and cursor behavior are stable, especially when Codohue participates."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Stable Personalized Feed Scrolling (Priority: P1)

As an authenticated user, I want to scroll my personalized feed across multiple pages without seeing duplicate posts or missing eligible posts, even when the feed combines followed authors, recommended posts, and trending posts.

**Why this priority**: The current risk is unstable cursor behavior after ranked mixing. Reliable scrolling is the minimum requirement for trusting the feed.

**Independent Test**: Can be tested by preparing a user with followed posts, recommended posts, and trending posts, then requesting several consecutive pages and verifying each eligible post appears at most once and no page jumps backward.

**Acceptance Scenarios**:

1. **Given** a user has enough eligible posts for multiple pages, **When** the user requests page after page using returned cursors, **Then** the combined result set contains no repeated post IDs.
2. **Given** recommended and trending posts outrank followed posts on the first page, **When** the user requests the next page, **Then** followed posts that were already shown do not appear again and eligible followed posts that were not shown remain reachable.
3. **Given** the recommendation provider returns item-level score and rank, **When** feed items are presented, **Then** recommended items are ordered using that signal in a predictable way instead of a flat presence-only boost.

---

### User Story 2 - Reliable Public Discovery (Priority: P2)

As any visitor or signed-in user, I want public discovery pages to remain chronological and cursor-stable so browsing public posts is predictable and does not depend on personalization availability.

**Why this priority**: Discovery is the unauthenticated fallback and also the safety net when a personalized feed has no more followed content.

**Independent Test**: Can be tested by requesting discovery pages with and without authentication and verifying order, cursor continuity, optional viewer-specific flags, and absence of duplicates.

**Acceptance Scenarios**:

1. **Given** public posts exist with overlapping timestamps, **When** a visitor paginates discovery, **Then** ordering remains deterministic across all pages.
2. **Given** a signed-in user requests discovery, **When** posts are returned, **Then** viewer-specific flags such as liked status and following-author status are populated without changing pagination order.
3. **Given** a personalized feed falls back to discovery after followed content is exhausted, **When** the fallback begins, **Then** posts already shown earlier in the feed session are not repeated.

---

### User Story 3 - Fully Compatible Recommendation Integration (Priority: P3)

As an operator, I want Darkvoid's recommendation client integrations to be aligned with the recommendation service version so feed behavior can rely on paginated score/rank responses without compatibility gaps.

**Why this priority**: Codohue service v0.4.0 has useful response metadata that is central to stable ranked pagination, so client integrations must expose the same response semantics.

**Independent Test**: Can be tested by running the feed with provider responses that include score/rank and pagination metadata, plus provider failure scenarios, and verifying the system either uses the rich metadata or falls back locally without relying on outdated response shapes.

**Acceptance Scenarios**:

1. **Given** the recommendation provider returns score, rank, limit, offset, and total, **When** Darkvoid builds feed pages, **Then** it uses those values to continue recommendation pagination and rank recommended items.
2. **Given** the recommendation client integrations are installed, **When** Darkvoid communicates with the recommendation provider, **Then** the exposed response model matches the provider's paginated score/rank item shape.
3. **Given** the recommendation provider is unavailable or returns unusable items, **When** a user requests feed or discovery, **Then** users still receive valid local results with clear operational visibility.

### Edge Cases

- Recommendation pages contain invalid, deleted, private, or inaccessible object IDs.
- Recommendation items overlap with followed posts, trending posts, or posts already shown in the same feed session.
- The recommendation provider reports a total that changes while a user is paging.
- New posts are created while a user is paging through an existing feed session.
- A cursor is malformed, expired, belongs to an incompatible mode, or references unavailable state.
- A user follows or unfollows authors while paging through feed results.
- Discovery has multiple posts with the same creation timestamp.
- The feed has fewer eligible items than the requested page size.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST provide stable feed pagination where each returned cursor continues from the exact consumed feed position without repeating posts already returned in the same browsing sequence.
- **FR-002**: The system MUST prevent eligible posts from becoming unreachable because recommended or trending posts outranked followed posts on an earlier page.
- **FR-003**: The system MUST represent feed page state across all active content sources needed for the next page, including followed content, recommended content, trending content, and discovery fallback when those sources participate.
- **FR-004**: The system MUST use recommendation item score and rank when those values are available to order and explain recommended-feed placement consistently.
- **FR-005**: The system MUST preserve deterministic ordering for recommendations even when post details are loaded separately from recommendation metadata.
- **FR-006**: The system MUST ignore recommendation or trending items that are invalid, deleted, inaccessible, or duplicate without failing the whole feed request.
- **FR-007**: The system MUST continue serving feed and discovery results when the recommendation provider is unavailable, slow, or returns unusable results.
- **FR-008**: The system MUST keep public discovery ordered by a deterministic public-post cursor and independent from personalized recommendation ranking.
- **FR-009**: The system MUST ensure discovery pagination works consistently for both anonymous visitors and authenticated users.
- **FR-010**: The system MUST populate viewer-specific state for authenticated discovery requests without changing which posts qualify for the page.
- **FR-011**: The system MUST ensure personalized feed fallback to discovery does not reintroduce posts already returned earlier in the same browsing sequence.
- **FR-012**: The system MUST return clear client errors for malformed cursors and safe empty responses when no more eligible content exists.
- **FR-013**: The system MUST provide observable outcomes for recommendation usage, fallback usage, invalid provider items, and cursor errors so operators can verify rollout health.
- **FR-014**: The system MUST align all recommendation client integrations with the provider version that exposes paginated recommendation items with score and rank.
- **FR-015**: The system MUST include characterization coverage for current feed/discovery behavior before changing pagination behavior and regression coverage for duplicate, skip, fallback, and provider-version scenarios.

### Key Entities *(include if feature involves data)*

- **Feed Page State**: The opaque continuation state returned to clients so the next feed page can resume all active content sources without duplicate or skipped items.
- **Feed Item**: A post presented in feed with source, score, rank signal when available, and viewer-specific flags.
- **Recommendation Item**: A provider-suggested object with object identity, score, rank, and page metadata when available.
- **Discovery Cursor**: The deterministic continuation state for chronological public discovery.
- **Seen Item Set**: The bounded record of posts already returned in a feed browsing sequence, used to prevent repeats across mixed sources and fallback.
- **Provider Capability State**: The verified recommendation-provider contract, used to confirm rich metadata behavior and local fallback behavior.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In a test dataset of at least 100 mixed eligible posts, five consecutive feed pages contain 0 duplicate post IDs and no unreachable followed posts caused by first-page ranking.
- **SC-002**: Discovery pagination over at least 100 public posts with timestamp ties returns a deterministic complete sequence with 0 duplicates and 0 ordering regressions.
- **SC-003**: When recommendation score and rank are available, at least 95% of recommendation-backed feed placements are ordered consistently with the documented blend rules.
- **SC-004**: When the recommendation provider is unavailable or returns unusable results, users still receive non-error feed responses for at least 99% of valid requests in fallback test runs.
- **SC-005**: Malformed cursors are rejected with a client-correctable error in 100% of cursor validation tests.
- **SC-006**: Operators can distinguish rich recommendation usage, local fallback usage, and provider item filtering in rollout observations for every feed request path.

## Assumptions

- Codohue service container v0.4.0 can provide paginated recommendation items with object identity, score, rank, limit, offset, and total.
- All three Codohue Go SDK modules used by Darkvoid will be upgraded to tag v0.2.0 so they expose the Codohue service v0.4.0 recommendation response shape.
- Public discovery should remain chronological and should not become personalized ranking.
- Personalized feed may blend followed posts, recommendations, trending posts, and discovery fallback, but clients should still receive a single opaque cursor.
- Cursor internals may change because cursors are treated as opaque by clients.
- Existing authentication, visibility, like, follow, and post availability rules continue to define whether a post can be shown.
