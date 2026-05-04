# Feature Specification: Precomputed Feed Timeline

**Feature Branch**: `004-precomputed-feed`  
**Created**: 2026-05-02  
**Status**: Draft  
**Input**: User description: "Refactor /feed to a precomputed event-driven timeline based on localdocs/feed-refactor.md. Redis timeline is the primary read model, dynamic feed is only transition/warm support, feed session state is removed, and old client cursor compatibility is intentionally not preserved because clients must update to the new contract."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Browse A Fast Personalized Feed (Priority: P1)

As an authenticated user, I want my feed to load from a ready-to-serve personalized timeline so that opening and paging through the feed feels immediate and does not depend on recomputing all candidate sources for every request.

**Why this priority**: This is the core user value of the refactor. The feed must become faster, more predictable, and easier to paginate before secondary behavior matters.

**Independent Test**: Can be fully tested by preparing a user with followed authors who have recent posts, requesting the feed first page and a subsequent page, and verifying that results are ordered, non-duplicated, and returned with a new continuation cursor.

**Acceptance Scenarios**:

1. **Given** a user follows authors with recent visible posts, **When** the user opens the feed, **Then** the response contains the newest eligible timeline items and a continuation cursor when more items are available.
2. **Given** the user received a continuation cursor from the new feed contract, **When** the user requests the next page, **Then** the response continues from the prior page without returning posts already shown in that browsing sequence.
3. **Given** multiple posts from followed authors are available, **When** the feed is requested repeatedly without new posts, **Then** the relative order remains stable across pages.

---

### User Story 2 - See New Followed Posts Quickly (Priority: P2)

As a follower, I want posts from accounts I follow to appear in my feed shortly after they are published so that the feed feels current without requiring manual discovery.

**Why this priority**: Freshness is the main product expectation for a social feed. The new architecture must preserve near real-time delivery while moving away from request-time computation.

**Independent Test**: Can be tested by publishing a post from a followed author, waiting within the freshness target, and confirming that the follower's feed includes the new post without a manual rebuild.

**Acceptance Scenarios**:

1. **Given** user A follows user B, **When** user B publishes a visible post, **Then** user A can see that post in their feed within the freshness target.
2. **Given** a post creation succeeds but feed propagation is temporarily delayed, **When** the follower requests the feed after the system recovers, **Then** the feed can still include the eligible post after timeline refresh behavior runs.
3. **Given** an author has a very large follower set, **When** the author publishes a post, **Then** the system protects feed stability while still making the post available to followers through bounded propagation and refresh behavior.

---

### User Story 3 - Use The New Feed Contract Explicitly (Priority: P3)

As a client developer, I want a clear new feed pagination contract with no legacy cursor compatibility so that clients update deliberately and avoid ambiguous mixed-version behavior.

**Why this priority**: The user explicitly accepts a breaking client change. Removing compatibility reduces backend complexity and prevents maintaining two feed state models.

**Independent Test**: Can be tested by sending requests with the new cursor contract and with old or malformed cursors, then verifying that only the new contract is accepted.

**Acceptance Scenarios**:

1. **Given** a client sends a valid new feed cursor, **When** it requests the next page, **Then** the server returns the next page according to the new contract.
2. **Given** a client sends an old feed cursor or old session-based cursor, **When** it requests the feed, **Then** the server rejects the cursor with a client-facing validation error.
3. **Given** a client starts without a cursor, **When** it requests the feed, **Then** the server starts a new feed browsing sequence using the new contract.
4. **Given** a client receives a cursor from a page containing fallback or supplemental content, **When** it requests the next page, **Then** the same new cursor contract continues all active feed sources without requiring legacy session state.

---

### User Story 4 - Preserve Discovery And Supplemental Sources (Priority: P4)

As a user, I want discovery, trending, and recommendation content to remain available where appropriate so that the feed is not empty or stale when my followed timeline has limited content.

**Why this priority**: Supplemental sources improve feed quality, but they should not make the primary feed path complex or unstable.

**Independent Test**: Can be tested with users who have few or no followed posts and verifying that the feed still returns eligible content without breaking chronological discovery pagination.

**Acceptance Scenarios**:

1. **Given** a user has no ready timeline items, **When** the user opens the feed, **Then** the response can include eligible fallback or supplemental content instead of failing.
2. **Given** supplemental recommendation or trending content is available, **When** the user's feed page is assembled, **Then** a bounded amount of supplemental content may be included without duplicating primary timeline items.
3. **Given** a user requests the discovery endpoint, **When** they paginate discovery results, **Then** discovery continues to use its own stable chronological pagination behavior.
4. **Given** a user's ready timeline is missing, expired, or incomplete, **When** the user opens the feed, **Then** the request triggers a timeline refresh while still returning a valid page or empty-state response.

### Edge Cases

- A user follows no one and has no personalized timeline items.
- A followed author deletes, hides, or changes visibility for a post already present in a user's ready timeline.
- A user unfollows an author whose posts were previously eligible for the user's feed.
- A client sends an old session-based cursor after the breaking change is released.
- A client sends a malformed, expired, or tampered cursor.
- A feed timeline has fewer items than the requested page size.
- Supplemental content returns items that are already present in the primary timeline.
- Feed propagation is delayed, interrupted, or partially completed for an author with many followers.
- A user's ready timeline expires, is evicted, or has not been built yet.
- A user requests additional pages after the first page was assembled from fallback or supplemental sources.
- The system is in rollout and the new feed path must be disabled quickly because freshness, latency, or error-rate targets are missed.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST serve authenticated feed requests from a ready personalized timeline when eligible timeline items are available.
- **FR-002**: System MUST return feed items in a stable newest-first order for primary followed-author timeline content.
- **FR-003**: System MUST provide a new opaque feed continuation cursor for paginating the feed across primary timeline, fallback, recommendation, and trending sources.
- **FR-004**: System MUST reject legacy session-based feed cursors and unsupported cursor versions with a clear client validation error.
- **FR-005**: System MUST NOT preserve compatibility behavior for the previous feed cursor/session contract.
- **FR-006**: System MUST avoid returning duplicate posts across consecutive pages requested with the new cursor contract.
- **FR-007**: System MUST make newly published visible posts eligible for followers' feeds after the publishing action succeeds.
- **FR-008**: System MUST ensure feed propagation failures do not cause the original post publishing action to fail when the post itself was saved successfully.
- **FR-009**: System MUST trigger a refresh of a user's ready timeline when the user requests the feed and the timeline is missing, expired, incomplete, or otherwise unavailable.
- **FR-010**: System MUST provide bounded fallback feed results while a missing or incomplete ready timeline is being refreshed.
- **FR-011**: System MUST keep each user's ready timeline bounded to no more than 1,000 primary followed-author items and no more than 7 days of retained prepared timeline content.
- **FR-012**: System MUST enforce post deletion and visibility rules at response time so users never receive posts they are no longer allowed to see, even if stale prepared entries still exist until lazy or best-effort cleanup.
- **FR-013**: System MUST enforce current follow state at response time so posts from unfollowed authors no longer appear as primary followed-author feed items after the unfollow is reflected, even if stale prepared entries still exist until lazy or best-effort cleanup.
- **FR-014**: System MUST include a bounded amount of recommendation or trending content only when it improves feed completeness and does not duplicate primary timeline content.
- **FR-015**: System MUST keep discovery pagination independent from the personalized feed timeline refactor.
- **FR-016**: System MUST expose enough operational signals to measure feed freshness, propagation delay, fallback usage, cursor rejection rate, and timeline refresh success.
- **FR-017**: System MUST document the new feed request and response contract for client teams, including cursor format expectations, rejection behavior for old cursors, and pagination rules.
- **FR-018**: System MUST provide a controlled rollout path with explicit stages: prepare and warm timelines without serving them, serve the new feed to a limited audience, make the new feed the default after freshness and latency targets pass, then remove old session state after a stable observation period.
- **FR-019**: System MUST remove the old feed session state model from the normal feed browsing path before the feature is considered complete.
- **FR-020**: System MUST ensure feed responses remain valid when supplemental providers are unavailable, slow, or return partial results.
- **FR-021**: System MUST support disabling the new feed path during rollout without blocking feed access for users.
- **FR-022**: System MAY perform background refresh for active users, but lazy refresh on feed request is required and sufficient for the initial feature.

### Key Entities *(include if feature involves data)*

- **Feed Timeline**: A user's ready-to-serve ordered list of eligible followed-author posts. Key attributes include owner user, post identity, ordering position, eligibility status, freshness, and bounded retention.
- **Feed Cursor**: An opaque client token that represents the next position in the new feed contract across primary timeline and supplemental sources. It does not carry legacy session state and is valid only for the new feed contract.
- **Feed Propagation Event**: A record of a feed-impacting action such as post publication or follow state change. It determines which user timelines should become eligible for refresh.
- **Timeline Refresh**: A recovery or warm-up process that repopulates a user's ready timeline when it is missing, incomplete, or stale.
- **Stale Prepared Entry**: A prepared timeline entry that remains in storage after a post visibility, deletion, or follow-state change. Stale prepared entries are acceptable only if response-time eligibility checks prevent them from being returned to users.
- **Supplemental Feed Candidate**: Recommendation, trending, or discovery fallback content that may be merged into a feed page in bounded amounts.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 95% of authenticated feed first-page requests for users with ready timeline items complete in 200 ms or less under normal operating conditions.
- **SC-002**: 95% of authenticated feed next-page requests using the new cursor complete in 100 ms or less under normal operating conditions.
- **SC-003**: 99% of newly published visible posts from followed authors become available to eligible followers within 30 seconds under normal operating conditions.
- **SC-004**: Consecutive feed pages requested with the new cursor have a duplicate post rate below 0.1%.
- **SC-005**: 100% of old session-based feed cursors are rejected with a client validation error after the breaking contract is enabled.
- **SC-006**: At least 99% of successful post publishing actions remain successful even when feed propagation is delayed or partially unavailable.
- **SC-007**: Users with no ready timeline items receive a valid feed response or empty-state response in 300 ms or less for 95% of requests.
- **SC-008**: Discovery pagination behavior remains unchanged, verified by existing discovery pagination tests and contract checks.

## Assumptions

- Clients will be updated in coordination with this release and do not require backward compatibility with the previous feed cursor/session contract.
- The personalized feed remains available only to authenticated users.
- The primary feed should prioritize posts from followed authors; recommendations, trending content, and discovery fallback are supplemental.
- The system may temporarily serve fallback results while a user's ready timeline is being refreshed.
- Timeline refresh is triggered lazily by feed requests when prepared timeline content is absent or incomplete; background refresh for active users is optional.
- The rollout may keep temporary fallback and measurement paths until the new feed path is stable, but legacy session-based browsing is not part of the final behavior.
- Physical cleanup of stale prepared entries is lazy or best-effort; user-visible correctness is enforced when a feed response is assembled.
- Post visibility, deletion, and follow state rules remain authoritative over any previously prepared timeline entry.
- Existing discovery behavior is intentionally out of scope except where feed fallback uses discovery-like content.
