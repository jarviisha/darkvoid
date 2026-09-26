package feed

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestFeedCursor_RoundTripNoVersion(t *testing.T) {
	tlScore := int64(1234567890123)
	trendScore := 987.5
	userID := uuid.New()
	timelinePostID := uuid.New()
	trendingPostID := uuid.New()

	encoded := (&FeedCursor{
		TimelineScore:        &tlScore,
		TimelinePostID:       timelinePostID.String(),
		TimelineUser:         userID.String(),
		RecommendationOffset: 20,
		TrendingScore:        &trendScore,
		TrendingPostID:       trendingPostID.String(),
	}).Encode()

	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode raw cursor: %v", err)
	}
	var fields map[string]any
	if unmarshalErr := json.Unmarshal(raw, &fields); unmarshalErr != nil {
		t.Fatalf("unmarshal raw cursor: %v", unmarshalErr)
	}
	if _, ok := fields["v"]; ok {
		t.Fatalf("cursor contains version field: %s", raw)
	}
	if _, ok := fields["timeline"]; ok {
		t.Fatalf("cursor contains nested timeline field: %s", raw)
	}

	decoded, err := DecodeFeedCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeFeedCursor: %v", err)
	}
	if decoded.TimelineScore == nil || *decoded.TimelineScore != tlScore {
		t.Fatalf("timeline score mismatch: %+v", decoded)
	}
	if decoded.TimelinePostID != timelinePostID.String() ||
		decoded.TimelineUser != userID.String() ||
		decoded.RecommendationOffset != 20 ||
		decoded.TrendingScore == nil ||
		*decoded.TrendingScore != trendScore ||
		decoded.TrendingPostID != trendingPostID.String() {
		t.Fatalf("decoded cursor mismatch: %+v", decoded)
	}
}

func TestDecodeFeedCursor_EmptyMalformedAndOptionalFields(t *testing.T) {
	if cursor, err := DecodeFeedCursor(""); err != nil || cursor != nil {
		t.Fatalf("empty cursor = %v/%v, want nil/nil", cursor, err)
	}
	if _, err := DecodeFeedCursor("not-valid!!!"); err == nil {
		t.Fatal("expected malformed base64 error")
	}
	malformedJSON := base64.RawURLEncoding.EncodeToString([]byte("{"))
	if _, err := DecodeFeedCursor(malformedJSON); err == nil {
		t.Fatal("expected malformed JSON error")
	}

	encoded := (&FeedCursor{TimelineUser: uuid.NewString()}).Encode()
	decoded, err := DecodeFeedCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeFeedCursor optional-only: %v", err)
	}
	if decoded.TimelineUser == "" || decoded.HasContinuation() {
		t.Fatalf("optional-only cursor mismatch: %+v", decoded)
	}
}

func TestFeedCursor_Positions(t *testing.T) {
	tlScore := int64(42)
	trendScore := 99.5
	timelinePostID := uuid.NewString()
	trendingPostID := uuid.NewString()
	cursor := &FeedCursor{
		TimelineScore:  &tlScore,
		TimelinePostID: timelinePostID,
		TrendingScore:  &trendScore,
		TrendingPostID: trendingPostID,
	}

	timeline := cursor.TimelinePosition()
	if timeline == nil || timeline.Score != tlScore || timeline.PostID != timelinePostID {
		t.Fatalf("timeline position = %+v", timeline)
	}
	trending := cursor.TrendingPosition()
	if trending == nil || trending.Score != trendScore || trending.PostID != trendingPostID {
		t.Fatalf("trending position = %+v", trending)
	}
}

func TestFeedCursor_FollowingAndDiscoverRoundTrip(t *testing.T) {
	flTS := time.Now().UTC().Truncate(time.Microsecond)
	discTS := flTS.Add(-time.Hour)
	flNano := flTS.UnixNano()
	discNano := discTS.UnixNano()
	flPostID := uuid.NewString()
	discPostID := uuid.NewString()

	encoded := (&FeedCursor{
		FollowingCreatedAt: &flNano,
		FollowingPostID:    flPostID,
		DiscoverCreatedAt:  &discNano,
		DiscoverPostID:     discPostID,
	}).Encode()
	decoded, err := DecodeFeedCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeFeedCursor: %v", err)
	}
	if !decoded.HasContinuation() {
		t.Fatalf("expected continuation state: %+v", decoded)
	}

	following := decoded.FollowingPosition()
	if following == nil || !following.CreatedAt.Equal(flTS) || following.PostID != flPostID {
		t.Fatalf("following position = %+v, want (%s, %s)", following, flTS, flPostID)
	}
	discover := decoded.DiscoverPosition()
	if discover == nil || !discover.CreatedAt.Equal(discTS) || discover.PostID != discPostID {
		t.Fatalf("discover position = %+v, want (%s, %s)", discover, discTS, discPostID)
	}
}

func TestDecodeFeedCursor_RejectsObsoletePayloads(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]any
	}{
		{name: "version field", payload: map[string]any{"v": 2}},
		{name: "legacy version field", payload: map[string]any{"version": 1}},
		{name: "nested timeline", payload: map[string]any{"timeline": map[string]any{"Score": 1, "PostID": uuid.NewString()}}},
		{name: "fallback cursor", payload: map[string]any{"fallback_cursor": map[string]any{"PostID": uuid.NewString()}}},
		{name: "session id", payload: map[string]any{"session_id": uuid.NewString()}},
		{name: "pending items", payload: map[string]any{"pending_items": []map[string]string{{"post_id": uuid.NewString()}}}},
		{name: "seen ids", payload: map[string]any{"seen_post_ids": []string{uuid.NewString()}}},
		{name: "issued at", payload: map[string]any{"issued_at": time.Now().UTC()}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := encodePayload(t, tt.payload)
			if _, err := DecodeFeedCursor(encoded); err == nil {
				t.Fatal("expected obsolete cursor rejection")
			}
		})
	}
}

func TestFeedCursor_RejectsInvalidFields(t *testing.T) {
	negativeTimeline := int64(-1)
	validTimeline := int64(1)
	negativeTrend := -1.0
	validTrend := 1.0
	tests := []struct {
		name   string
		cursor *FeedCursor
	}{
		{name: "negative timeline score", cursor: &FeedCursor{TimelineScore: &negativeTimeline, TimelinePostID: uuid.NewString()}},
		{name: "timeline score missing post ID", cursor: &FeedCursor{TimelineScore: &validTimeline}},
		{name: "timeline post ID without score", cursor: &FeedCursor{TimelinePostID: uuid.NewString()}},
		{name: "invalid timeline post ID", cursor: &FeedCursor{TimelineScore: &validTimeline, TimelinePostID: "not-a-uuid"}},
		{name: "negative recommendation offset", cursor: &FeedCursor{RecommendationOffset: -1}},
		{name: "negative trending score", cursor: &FeedCursor{TrendingScore: &negativeTrend, TrendingPostID: uuid.NewString()}},
		{name: "trending score missing post ID", cursor: &FeedCursor{TrendingScore: &validTrend}},
		{name: "trending post ID without score", cursor: &FeedCursor{TrendingPostID: uuid.NewString()}},
		{name: "invalid trending post ID", cursor: &FeedCursor{TrendingScore: &validTrend, TrendingPostID: "not-a-uuid"}},
		{name: "seen trending post IDs without score", cursor: &FeedCursor{TrendingSeen: []string{uuid.NewString()}}},
		{name: "invalid seen trending post ID", cursor: &FeedCursor{TrendingScore: &validTrend, TrendingPostID: uuid.NewString(), TrendingSeen: []string{"not-a-uuid"}}},
		{name: "invalid timeline user", cursor: &FeedCursor{TimelineUser: "not-a-uuid"}},
		{name: "following timestamp missing post ID", cursor: &FeedCursor{FollowingCreatedAt: &validTimeline}},
		{name: "following post ID without timestamp", cursor: &FeedCursor{FollowingPostID: uuid.NewString()}},
		{name: "invalid following post ID", cursor: &FeedCursor{FollowingCreatedAt: &validTimeline, FollowingPostID: "not-a-uuid"}},
		{name: "discover timestamp missing post ID", cursor: &FeedCursor{DiscoverCreatedAt: &validTimeline}},
		{name: "discover post ID without timestamp", cursor: &FeedCursor{DiscoverPostID: uuid.NewString()}},
		{name: "invalid discover post ID", cursor: &FeedCursor{DiscoverCreatedAt: &validTimeline, DiscoverPostID: "not-a-uuid"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := tt.cursor.Encode()
			if _, err := DecodeFeedCursor(encoded); err == nil {
				t.Fatal("expected decode error")
			}
		})
	}
}

func TestFeedCursor_ValidateForUser(t *testing.T) {
	userID := uuid.New()
	if err := (&FeedCursor{TimelineUser: userID.String()}).ValidateForUser(userID); err != nil {
		t.Fatalf("ValidateForUser matching user: %v", err)
	}
	if err := (&FeedCursor{TimelineUser: uuid.NewString()}).ValidateForUser(userID); err == nil {
		t.Fatal("expected mismatched cursor user error")
	}
}

func TestFeedCursor_FieldNames(t *testing.T) {
	tlScore := int64(1)
	trendScore := 2.0
	flTS := int64(4)
	discTS := int64(5)
	raw, err := base64.RawURLEncoding.DecodeString((&FeedCursor{
		TimelineScore:        &tlScore,
		TimelinePostID:       uuid.NewString(),
		TimelineUser:         uuid.NewString(),
		RecommendationOffset: 3,
		TrendingScore:        &trendScore,
		TrendingPostID:       uuid.NewString(),
		FollowingCreatedAt:   &flTS,
		FollowingPostID:      uuid.NewString(),
		DiscoverCreatedAt:    &discTS,
		DiscoverPostID:       uuid.NewString(),
	}).Encode())
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	for _, field := range []string{"tl_score", "tl_post_id", "tl_user", "rec_offset", "trend_score", "trend_post_id", "fl_ts", "fl_post_id", "disc_ts", "disc_post_id"} {
		if !json.Valid(raw) {
			t.Fatalf("cursor is not JSON: %s", raw)
		}
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err != nil {
			t.Fatalf("unmarshal cursor: %v", err)
		}
		if _, ok := obj[field]; !ok {
			t.Fatalf("missing cursor field %q in %s", field, raw)
		}
	}
}

func encodePayload(t *testing.T, payload map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func TestFeedCursor_TrendingSeenRoundTrip(t *testing.T) {
	score := 4.0
	first, second := uuid.New(), uuid.New()
	cursor := &FeedCursor{
		TrendingScore:  &score,
		TrendingPostID: uuid.NewString(),
		TrendingSeen:   []string{first.String(), second.String()},
	}
	decoded, err := DecodeFeedCursor(cursor.Encode())
	if err != nil {
		t.Fatalf("DecodeFeedCursor: %v", err)
	}
	seen := decoded.TrendingSeenSet()
	if len(seen) != 2 || !seen[first] || !seen[second] {
		t.Fatalf("TrendingSeenSet() = %v, want both ids", seen)
	}
	if !decoded.HasContinuation() {
		t.Fatal("HasContinuation() = false for a cursor carrying seen trending ids")
	}
	// A nil cursor must not panic: GetFeed calls these on the first page.
	var absent *FeedCursor
	if got := absent.TrendingSeenSet(); len(got) != 0 {
		t.Fatalf("nil cursor TrendingSeenSet() = %v, want empty", got)
	}
}

func TestFeedCursor_DiscoverSeenRoundTrip(t *testing.T) {
	timestamp := time.Now().UnixNano()
	served := uuid.New()
	cursor := &FeedCursor{
		DiscoverCreatedAt: &timestamp,
		DiscoverPostID:    uuid.NewString(),
		DiscoverSeen:      []string{served.String()},
	}
	decoded, err := DecodeFeedCursor(cursor.Encode())
	if err != nil {
		t.Fatalf("DecodeFeedCursor: %v", err)
	}
	if got := decoded.DiscoverSeenSet(); len(got) != 1 || !got[served] {
		t.Fatalf("DiscoverSeenSet() = %v, want the served id", got)
	}

	bad := &FeedCursor{DiscoverCreatedAt: &timestamp, DiscoverPostID: uuid.NewString(), DiscoverSeen: []string{"not-a-uuid"}}
	if _, err := DecodeFeedCursor(bad.Encode()); err == nil {
		t.Fatal("expected decode error for an invalid seen discover post_id")
	}

	var absent *FeedCursor
	if got := absent.DiscoverSeenIDs(); got != nil {
		t.Fatalf("nil cursor DiscoverSeenIDs() = %v, want nil", got)
	}
}
