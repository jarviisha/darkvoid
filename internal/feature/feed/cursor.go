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

// scorePrecision is the bitSize used for float64 formatting/parsing.
const scorePrecision = 64

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
type FeedCursor struct {
	TimelineScore        *int64   `json:"tl_score,omitempty"`
	TimelinePostID       string   `json:"tl_post_id,omitempty"`
	TimelineUser         string   `json:"tl_user,omitempty"`
	RecommendationOffset int      `json:"rec_offset,omitempty"`
	TrendingScore        *float64 `json:"trend_score,omitempty"`
	TrendingPostID       string   `json:"trend_post_id,omitempty"`
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

// HasContinuation reports whether any source has remaining cursor state.
func (c *FeedCursor) HasContinuation() bool {
	return c != nil && (c.TimelineScore != nil || c.RecommendationOffset > 0 || c.TrendingScore != nil)
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

// FeedMode indicates which phase of the feed the cursor belongs to.
type FeedMode string

const (
	// ModeFollowing means the cursor points into the following timeline.
	ModeFollowing FeedMode = "f"
	// ModeDiscover means the cursor points into the discover (public) timeline.
	ModeDiscover FeedMode = "d"
)

// FollowingCursor is a DB cursor for the feed endpoint.
// Mode distinguishes between the following phase and the discover-fallback phase.
// Encoded as base64("mode,unix_nano,post_id").
type FollowingCursor struct {
	Mode      FeedMode
	CreatedAt time.Time
	PostID    string
}

// Encode returns the base64-encoded string representation of the following cursor.
func (c *FollowingCursor) Encode() string {
	mode := c.Mode
	if mode == "" {
		mode = ModeFollowing
	}
	raw := fmt.Sprintf("%s,%d,%s", mode, c.CreatedAt.UnixNano(), c.PostID)
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

// DecodeFollowingCursor parses a base64-encoded following cursor string.
// Returns nil if the string is empty.
// Accepts both legacy 2-field format ("unix_nano,post_id") and current
// 3-field format ("mode,unix_nano,post_id").
func DecodeFollowingCursor(s string) (*FollowingCursor, error) {
	if s == "" {
		return nil, nil //nolint:nilnil // empty string means no cursor
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor encoding: %w", err)
	}
	parts := strings.SplitN(string(b), ",", 3)
	switch len(parts) {
	case 3: // current format: mode, unix_nano, post_id
		ns, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor timestamp: %w", err)
		}
		return &FollowingCursor{Mode: FeedMode(parts[0]), CreatedAt: time.Unix(0, ns).UTC(), PostID: parts[2]}, nil
	case 2: // legacy format: unix_nano, post_id (mode defaults to following)
		ns, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor format")
		}
		return &FollowingCursor{Mode: ModeFollowing, CreatedAt: time.Unix(0, ns).UTC(), PostID: parts[1]}, nil
	default:
		return nil, fmt.Errorf("invalid cursor format")
	}
}

// PgParams returns the cursor fields as pgx-compatible types for DB queries.
func (c *FollowingCursor) PgParams() (pgtype.Timestamptz, uuid.UUID, error) {
	ts := pgtype.Timestamptz{Time: c.CreatedAt, Valid: true}
	id, err := uuid.Parse(c.PostID)
	return ts, id, err
}

// Cursor is a composite pagination cursor (score, created_at, post_id).
// Encoded as base64("score,unix_nano,post_id") to keep the API opaque.
type Cursor struct {
	Score     float64
	CreatedAt time.Time
	PostID    string
}

// Encode returns the base64-encoded string representation of the cursor.
func (c *Cursor) Encode() string {
	raw := strconv.FormatFloat(c.Score, 'f', -1, scorePrecision) +
		"," + strconv.FormatInt(c.CreatedAt.UnixNano(), 10) +
		"," + c.PostID
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor parses a base64-encoded cursor string.
// Returns nil if the string is empty.
// Accepts both the legacy 2-field format ("score,post_id") and the current
// 3-field format ("score,unix_nano,post_id") for backward compatibility.
func DecodeCursor(s string) (*Cursor, error) {
	if s == "" {
		return nil, nil //nolint:nilnil // empty string means no cursor
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor encoding: %w", err)
	}
	parts := strings.SplitN(string(b), ",", 3)
	switch len(parts) {
	case 3: // current format: score, unix_nano, post_id
		score, err := strconv.ParseFloat(parts[0], scorePrecision)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor score: %w", err)
		}
		ns, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor timestamp: %w", err)
		}
		return &Cursor{Score: score, CreatedAt: time.Unix(0, ns).UTC(), PostID: parts[2]}, nil
	case 2: // legacy format: score, post_id (CreatedAt defaults to zero)
		score, err := strconv.ParseFloat(parts[0], scorePrecision)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor score: %w", err)
		}
		return &Cursor{Score: score, PostID: parts[1]}, nil
	default:
		return nil, fmt.Errorf("invalid cursor format")
	}
}
