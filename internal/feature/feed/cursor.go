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

const (
	// FeedCursorVersion is the current versioned feed cursor schema.
	FeedCursorVersion = 2
)

// FeedCursor is the opaque continuation token for GET /feed.
type FeedCursor struct {
	Version              int               `json:"v"`
	Timeline             *TimelinePosition `json:"timeline,omitempty"`
	RecommendationOffset int               `json:"rec_offset,omitempty"`
	TrendingCursor       string            `json:"trend_cursor,omitempty"`
	FallbackCursor       *DiscoverCursor   `json:"fallback_cursor,omitempty"`
	IssuedAt             time.Time         `json:"issued_at"`
}

// Encode returns the base64 JSON representation of the feed cursor.
func (c *FeedCursor) Encode() string {
	if c == nil {
		return ""
	}
	wire := *c
	if wire.Version == 0 {
		wire.Version = FeedCursorVersion
	}
	if wire.IssuedAt.IsZero() {
		wire.IssuedAt = time.Now().UTC()
	}
	raw, err := json.Marshal(wire)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeFeedCursor parses a versioned opaque feed cursor.
func DecodeFeedCursor(s string) (*FeedCursor, error) {
	if s == "" {
		return nil, nil //nolint:nilnil // empty string means no cursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid feed cursor encoding: %w", err)
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

// Validate verifies the v2 feed cursor is usable.
func (c *FeedCursor) Validate() error {
	if c == nil {
		return nil
	}
	if c.Version != FeedCursorVersion {
		return fmt.Errorf("unsupported feed cursor version")
	}
	if c.Timeline != nil {
		if c.Timeline.Score < 0 {
			return fmt.Errorf("invalid timeline cursor score")
		}
		if _, err := uuid.Parse(c.Timeline.PostID); err != nil {
			return fmt.Errorf("invalid timeline cursor post_id")
		}
	}
	if c.RecommendationOffset < 0 {
		return fmt.Errorf("invalid recommendation cursor offset")
	}
	if c.FallbackCursor != nil {
		if _, _, err := c.FallbackCursor.PgParams(); err != nil {
			return fmt.Errorf("invalid fallback cursor: %w", err)
		}
	}
	return nil
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
