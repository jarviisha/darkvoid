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
	// FeedCursorVersion is the current versioned mixed-feed cursor schema.
	FeedCursorVersion = 1
	// MaxFeedSeenIDs caps the seen set encoded or stored for one feed browsing sequence.
	MaxFeedSeenIDs = 240
	// MaxFeedPendingItems caps unreturned candidates carried between feed pages.
	MaxFeedPendingItems = 120
	// FeedSessionTTL is the maximum lifetime for one feed browsing sequence.
	FeedSessionTTL = 15 * time.Minute
)

// FeedPhase identifies the active phase of a feed browsing sequence.
type FeedPhase string

const (
	// PhaseMixed means the feed is still mixing followed, recommended, and trending candidates.
	PhaseMixed FeedPhase = "mixed"
	// PhaseDiscoverFallback means the feed is continuing through public discovery fallback.
	PhaseDiscoverFallback FeedPhase = "discover"
)

// FeedCandidate is a serialized candidate fetched for feed ranking but not necessarily returned yet.
type FeedCandidate struct {
	PostID        string    `json:"post_id"`
	Source        string    `json:"source"`
	SourceRank    int       `json:"source_rank,omitempty"`
	ProviderScore *float64  `json:"provider_score,omitempty"`
	ProviderRank  *int      `json:"provider_rank,omitempty"`
	CreatedAt     time.Time `json:"created_at,omitempty"`
}

// FeedPageState is the opaque continuation state for GET /feed.
type FeedPageState struct {
	Version              int              `json:"version"`
	SessionID            string           `json:"session_id,omitempty"`
	Mode                 FeedPhase        `json:"mode"`
	FollowingCursor      *FollowingCursor `json:"following_cursor,omitempty"`
	RecommendationOffset int              `json:"recommendation_offset,omitempty"`
	RecommendationTotal  int              `json:"recommendation_total,omitempty"`
	TrendingOffset       int              `json:"trending_offset,omitempty"`
	TrendingTotal        int              `json:"trending_total,omitempty"`
	DiscoveryCursor      *DiscoverCursor  `json:"discovery_cursor,omitempty"`
	PendingItems         []FeedCandidate  `json:"pending_items,omitempty"`
	SeenPostIDs          []string         `json:"seen_post_ids,omitempty"`
	CreatedAt            time.Time        `json:"created_at"`
	ExpiresAt            time.Time        `json:"expires_at"`
}

// NewFeedPageState returns a fresh mixed-feed state.
func NewFeedPageState(now time.Time) *FeedPageState {
	return &FeedPageState{
		Version:   FeedCursorVersion,
		Mode:      PhaseMixed,
		CreatedAt: now.UTC(),
		ExpiresAt: now.Add(FeedSessionTTL).UTC(),
	}
}

// Validate verifies that state is usable and bounded.
func (s *FeedPageState) Validate(now time.Time) error {
	if s == nil {
		return nil
	}
	if s.Version != FeedCursorVersion {
		return fmt.Errorf("unsupported feed cursor version")
	}
	switch s.Mode {
	case PhaseMixed, PhaseDiscoverFallback:
	default:
		return fmt.Errorf("invalid feed cursor mode")
	}
	if now.After(s.ExpiresAt) {
		return fmt.Errorf("feed cursor expired")
	}
	if s.RecommendationOffset < 0 || s.TrendingOffset < 0 {
		return fmt.Errorf("invalid feed cursor offset")
	}
	if len(s.PendingItems) > MaxFeedPendingItems {
		return fmt.Errorf("feed cursor has too many pending items")
	}
	if len(s.SeenPostIDs) > MaxFeedSeenIDs {
		return fmt.Errorf("feed cursor has too many seen items")
	}
	if s.FollowingCursor != nil {
		if _, _, err := s.FollowingCursor.PgParams(); err != nil {
			return fmt.Errorf("invalid following cursor: %w", err)
		}
	}
	if s.DiscoveryCursor != nil {
		if _, _, err := s.DiscoveryCursor.PgParams(); err != nil {
			return fmt.Errorf("invalid discover cursor: %w", err)
		}
	}
	for _, item := range s.PendingItems {
		if _, err := uuid.Parse(item.PostID); err != nil {
			return fmt.Errorf("invalid pending post_id")
		}
	}
	for _, id := range s.SeenPostIDs {
		if _, err := uuid.Parse(id); err != nil {
			return fmt.Errorf("invalid seen post_id")
		}
	}
	return nil
}

// AddSeen records post IDs as returned and keeps only the newest bounded suffix.
func (s *FeedPageState) AddSeen(ids ...string) {
	if s == nil {
		return
	}
	seen := make(map[string]bool, len(s.SeenPostIDs)+len(ids))
	out := make([]string, 0, len(s.SeenPostIDs)+len(ids))
	for _, id := range s.SeenPostIDs {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	if len(out) > MaxFeedSeenIDs {
		out = out[len(out)-MaxFeedSeenIDs:]
	}
	s.SeenPostIDs = out
}

// TrimPending caps the pending item list.
func (s *FeedPageState) TrimPending() {
	if s != nil && len(s.PendingItems) > MaxFeedPendingItems {
		s.PendingItems = s.PendingItems[:MaxFeedPendingItems]
	}
}

// SeenSet returns a lookup set for seen post IDs.
func (s *FeedPageState) SeenSet() map[string]bool {
	seen := make(map[string]bool)
	if s == nil {
		return seen
	}
	for _, id := range s.SeenPostIDs {
		seen[id] = true
	}
	return seen
}

// FeedCursor wraps feed page state in a single opaque token.
type FeedCursor struct {
	State *FeedPageState
}

// Encode returns the base64 JSON representation of the feed cursor.
func (c *FeedCursor) Encode() string {
	if c == nil || c.State == nil {
		return ""
	}
	raw, err := json.Marshal(c.State)
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
	var state FeedPageState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("invalid feed cursor format: %w", err)
	}
	if err := state.Validate(time.Now().UTC()); err != nil {
		return nil, err
	}
	return &FeedCursor{State: &state}, nil
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
