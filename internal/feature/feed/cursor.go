package feed

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

var obsoleteFeedCursorFields = map[string]struct{}{
	"v":               {},
	"version":         {},
	"timeline":        {},
	"fallback_cursor": {},
	"session_id":      {},
	"mode":            {},
	"pending_items":   {},
	"seen_post_ids":   {},
	"created_at":      {},
	"expires_at":      {},
	"issued_at":       {},
}

// TrendPosition identifies a continuation point in the trending source.
type TrendPosition struct {
	Score  float64
	PostID string
}

// FeedCursor is the opaque continuation token for GET /feed.
//
// Following and Discover carry (created_at unix-nano, post_id) DB positions.
// Following advances the mixed path through followed authors' posts; Discover
// marks that the feed has handed off to the public discover stream and where
// to resume it. The two are deliberately shaped alike: when the following
// source runs dry mid-scroll, its position becomes the discover position, so
// the feed continues chronologically below the last following post served
// instead of restarting from the top.
type FeedCursor struct {
	TimelineScore        *int64   `json:"tl_score,omitempty"`
	TimelinePostID       string   `json:"tl_post_id,omitempty"`
	TimelineUser         string   `json:"tl_user,omitempty"`
	RecommendationOffset int      `json:"rec_offset,omitempty"`
	RecommendationSeen   []int    `json:"rec_seen,omitempty"`
	TrendingScore        *float64 `json:"trend_score,omitempty"`
	TrendingPostID       string   `json:"trend_post_id,omitempty"`
	TrendingSeen         []string `json:"trend_seen,omitempty"`
	FollowingCreatedAt   *int64   `json:"fl_ts,omitempty"`
	FollowingPostID      string   `json:"fl_post_id,omitempty"`
	DiscoverCreatedAt    *int64   `json:"disc_ts,omitempty"`
	DiscoverPostID       string   `json:"disc_post_id,omitempty"`
	DiscoverSeen         []string `json:"disc_seen,omitempty"`
}

// Encode returns the base64 JSON representation of the feed cursor.
func (c *FeedCursor) Encode() string {
	if c == nil {
		return ""
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeFeedCursor parses the no-version opaque feed cursor.
func DecodeFeedCursor(s string) (*FeedCursor, error) {
	if s == "" {
		return nil, nil //nolint:nilnil // empty string means no cursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid feed cursor encoding: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("invalid feed cursor format: %w", err)
	}
	for field := range fields {
		if _, obsolete := obsoleteFeedCursorFields[field]; obsolete {
			return nil, fmt.Errorf("obsolete feed cursor field %q", field)
		}
	}
	var cursor FeedCursor
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return nil, fmt.Errorf("invalid feed cursor format: %w", err)
	}
	if err := cursor.Validate(); err != nil {
		return nil, err
	}
	return &cursor, nil
}

// Validate verifies the no-version feed cursor is usable.
func (c *FeedCursor) Validate() error {
	if c == nil {
		return nil
	}
	if c.TimelineScore != nil {
		if *c.TimelineScore < 0 {
			return fmt.Errorf("invalid timeline cursor score")
		}
		if _, err := uuid.Parse(c.TimelinePostID); err != nil {
			return fmt.Errorf("invalid timeline cursor post_id")
		}
	} else if c.TimelinePostID != "" {
		return fmt.Errorf("timeline post_id without timeline score")
	}
	if c.RecommendationOffset < 0 {
		return fmt.Errorf("invalid recommendation cursor offset")
	}
	for _, offset := range c.RecommendationSeen {
		if offset < c.RecommendationOffset {
			return fmt.Errorf("invalid seen recommendation offset")
		}
	}
	if c.TrendingScore != nil {
		if *c.TrendingScore < 0 {
			return fmt.Errorf("invalid trending cursor score")
		}
		if _, err := uuid.Parse(c.TrendingPostID); err != nil {
			return fmt.Errorf("invalid trending cursor post_id")
		}
	} else if c.TrendingPostID != "" {
		return fmt.Errorf("trending post_id without trending score")
	}
	if len(c.TrendingSeen) > 0 && c.TrendingScore == nil {
		return fmt.Errorf("seen trending post_ids without trending score")
	}
	for _, id := range c.TrendingSeen {
		if _, err := uuid.Parse(id); err != nil {
			return fmt.Errorf("invalid seen trending post_id")
		}
	}
	if c.FollowingCreatedAt != nil {
		if _, err := uuid.Parse(c.FollowingPostID); err != nil {
			return fmt.Errorf("invalid following cursor post_id")
		}
	} else if c.FollowingPostID != "" {
		return fmt.Errorf("following post_id without following timestamp")
	}
	if c.DiscoverCreatedAt != nil {
		if _, err := uuid.Parse(c.DiscoverPostID); err != nil {
			return fmt.Errorf("invalid discover cursor post_id")
		}
	} else if c.DiscoverPostID != "" {
		return fmt.Errorf("discover post_id without discover timestamp")
	}
	for _, id := range c.DiscoverSeen {
		if _, err := uuid.Parse(id); err != nil {
			return fmt.Errorf("invalid seen discover post_id")
		}
	}
	if c.TimelineUser != "" {
		if _, err := uuid.Parse(c.TimelineUser); err != nil {
			return fmt.Errorf("invalid timeline cursor user")
		}
	}
	return nil
}

// ValidateForUser verifies cursor ownership against the authenticated feed owner.
func (c *FeedCursor) ValidateForUser(userID uuid.UUID) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c == nil || c.TimelineUser == "" {
		return nil
	}
	if c.TimelineUser != userID.String() {
		return fmt.Errorf("cursor user mismatch")
	}
	return nil
}

// TimelinePosition returns the prepared timeline continuation point.
func (c *FeedCursor) TimelinePosition() *TimelinePosition {
	if c == nil || c.TimelineScore == nil {
		return nil
	}
	return &TimelinePosition{Score: *c.TimelineScore, PostID: c.TimelinePostID}
}

// TrendingPosition returns the trending source continuation point.
func (c *FeedCursor) TrendingPosition() *TrendPosition {
	if c == nil || c.TrendingScore == nil {
		return nil
	}
	return &TrendPosition{Score: *c.TrendingScore, PostID: c.TrendingPostID}
}

// TrendingSeenSet returns the trending posts already served below the trending
// position. The position alone cannot express them: the trending list is ordered
// by score, so a single boundary only ever names a suffix, and a post served out
// of that order — because it also arrived from following or recommendations, and
// collapsed to that source — has to be recorded by id or it is either re-served
// or skipped along with everything above it.
func (c *FeedCursor) TrendingSeenSet() map[uuid.UUID]bool {
	seen := make(map[uuid.UUID]bool, len(c.trendingSeen()))
	for _, raw := range c.trendingSeen() {
		id, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		seen[id] = true
	}
	return seen
}

func (c *FeedCursor) trendingSeen() []string {
	if c == nil {
		return nil
	}
	return c.TrendingSeen
}

// DiscoverSeen is capped because it travels in a URL. Past the cap the oldest
// entries are dropped and those posts can be served a second time, which is the
// milder of the two failures available — the alternative, anchoring discover
// below everything served, skips every unserved post between that anchor and the
// following boundary, and the trending source reaches back 24h.
//
// ponytail: FIFO cap, switch to storing positions server-side if scrolls get
// long enough for the drop to show.
// MaxDiscoverSeen and MaxTrendingSeen bound those lists.
//
// The discover cap bites hardest on an account with no following posts: the
// handoff boundary is then absent, every served post is reachable by discover, so
// four pages of the 100-post trending list overflow the cap and roughly forty
// posts can come back a second time.
const (
	MaxDiscoverSeen = 60
	MaxTrendingSeen = pageWindow
)

// pageWindow is the trending window size, which bounds the ragged edge whenever
// the window was not cut short.
const pageWindow = 20

// DiscoverSeenIDs is the nil-safe reader for the raw list: GetFeed reaches for
// it on the first page, where the cursor itself is nil.
func (c *FeedCursor) DiscoverSeenIDs() []string {
	if c == nil {
		return nil
	}
	return c.DiscoverSeen
}

// DiscoverSeenSet returns the posts already served that the discover stream
// could otherwise hand back. The discover position cannot express them: it
// bounds the stream chronologically, while these were served out of that order
// by the trending and recommendation sources, which rank rather than paginate.
func (c *FeedCursor) DiscoverSeenSet() map[uuid.UUID]bool {
	if c == nil {
		return map[uuid.UUID]bool{}
	}
	seen := make(map[uuid.UUID]bool, len(c.DiscoverSeen))
	for _, raw := range c.DiscoverSeenIDs() {
		id, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		seen[id] = true
	}
	return seen
}

// FollowingPosition returns the following source continuation point.
func (c *FeedCursor) FollowingPosition() *FollowingCursor {
	if c == nil || c.FollowingCreatedAt == nil {
		return nil
	}
	return &FollowingCursor{CreatedAt: time.Unix(0, *c.FollowingCreatedAt).UTC(), PostID: c.FollowingPostID}
}

// DiscoverPosition returns the discover stream continuation point.
func (c *FeedCursor) DiscoverPosition() *DiscoverCursor {
	if c == nil || c.DiscoverCreatedAt == nil {
		return nil
	}
	return &DiscoverCursor{CreatedAt: time.Unix(0, *c.DiscoverCreatedAt).UTC(), PostID: c.DiscoverPostID}
}

// HasContinuation reports whether any source has remaining cursor state.
func (c *FeedCursor) HasContinuation() bool {
	return c != nil && (c.TimelineScore != nil || c.RecommendationOffset > 0 || c.TrendingScore != nil ||
		c.FollowingCreatedAt != nil || c.DiscoverCreatedAt != nil)
}

// DiscoverCursor is a composite pagination cursor (created_at, post_id) for the discover feed.
// Encoded as base64("unix_nano,post_id").
type DiscoverCursor struct {
	CreatedAt time.Time
	PostID    string
}

// Encode returns the base64-encoded string representation of the discover cursor.
func (c *DiscoverCursor) Encode() string {
	raw := fmt.Sprintf("%d,%s", c.CreatedAt.UnixNano(), c.PostID)
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

// DecodeDiscoverCursor parses a base64-encoded discover cursor string.
// Returns nil if the string is empty.
func DecodeDiscoverCursor(s string) (*DiscoverCursor, error) {
	if s == "" {
		return nil, nil //nolint:nilnil // empty string means no cursor
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor encoding: %w", err)
	}
	parts := strings.SplitN(string(b), ",", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid cursor format")
	}
	ns, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor timestamp: %w", err)
	}
	return &DiscoverCursor{CreatedAt: time.Unix(0, ns).UTC(), PostID: parts[1]}, nil
}

// PgParams returns the cursor fields as pgx-compatible types for DB queries.
// If the cursor is nil, returns sentinel values (MaxDiscoverTime, uuid.Max) to include all posts.
func (c *DiscoverCursor) PgParams() (pgtype.Timestamptz, uuid.UUID, error) {
	ts := pgtype.Timestamptz{Time: c.CreatedAt, Valid: true}
	id, err := uuid.Parse(c.PostID)
	return ts, id, err
}

// DefaultPgParams returns sentinel values used when no discover cursor is present.
func DefaultDiscoverPgParams() (pgtype.Timestamptz, uuid.UUID) {
	return pgtype.Timestamptz{Time: MaxDiscoverTime, Valid: true}, uuid.Max
}

// FollowingCursor is the following source's position, handed to the DB query.
// It never travels to a client on its own: FeedCursor is the wire format, and
// FollowingPosition reconstructs this from it.
type FollowingCursor struct {
	CreatedAt time.Time
	PostID    string
}

// PgParams returns the cursor fields as pgx-compatible types for DB queries.
func (c *FollowingCursor) PgParams() (pgtype.Timestamptz, uuid.UUID, error) {
	ts := pgtype.Timestamptz{Time: c.CreatedAt, Valid: true}
	id, err := uuid.Parse(c.PostID)
	return ts, id, err
}
