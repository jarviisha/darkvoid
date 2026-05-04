package feed

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestFeedCursorV2_RoundTrip(t *testing.T) {
	now := time.Now().UTC()
	postID := uuid.New()
	fallbackPostID := uuid.New()
	encoded := (&FeedCursor{
		Version:              FeedCursorVersion,
		Timeline:             &TimelinePosition{Score: TimelineScoreFromTime(now), PostID: postID.String()},
		RecommendationOffset: 20,
		TrendingCursor:       "trend:123",
		FallbackCursor:       &DiscoverCursor{CreatedAt: now, PostID: fallbackPostID.String()},
		IssuedAt:             now,
	}).Encode()
	decoded, err := DecodeFeedCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeFeedCursor: %v", err)
	}
	if decoded.Version != FeedCursorVersion ||
		decoded.Timeline == nil ||
		decoded.Timeline.PostID != postID.String() ||
		decoded.RecommendationOffset != 20 ||
		decoded.TrendingCursor != "trend:123" ||
		decoded.FallbackCursor == nil ||
		decoded.FallbackCursor.PostID != fallbackPostID.String() {
		t.Fatalf("decoded cursor mismatch: %+v", decoded)
	}
}

func TestDecodeFeedCursor_RejectsMalformedAndLegacyPayloads(t *testing.T) {
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

	legacyPayload := map[string]any{
		"version":       1,
		"session_id":    uuid.NewString(),
		"mode":          "mixed",
		"pending_items": []map[string]string{{"post_id": uuid.NewString()}},
		"seen_post_ids": []string{uuid.NewString()},
		"created_at":    time.Now().UTC(),
		"expires_at":    time.Now().UTC().Add(15 * time.Minute),
	}
	raw, err := json.Marshal(legacyPayload)
	if err != nil {
		t.Fatalf("marshal legacy payload: %v", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	if _, err := DecodeFeedCursor(encoded); err == nil {
		t.Fatal("expected legacy v1 cursor rejection")
	}
}

func TestFeedCursorV2_RejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name   string
		cursor *FeedCursor
	}{
		{name: "unsupported version", cursor: &FeedCursor{Version: FeedCursorVersion + 1}},
		{name: "negative timeline score", cursor: &FeedCursor{Version: FeedCursorVersion, Timeline: &TimelinePosition{Score: -1, PostID: uuid.New().String()}}},
		{name: "invalid timeline post ID", cursor: &FeedCursor{Version: FeedCursorVersion, Timeline: &TimelinePosition{Score: 1, PostID: "not-a-uuid"}}},
		{name: "negative recommendation offset", cursor: &FeedCursor{Version: FeedCursorVersion, RecommendationOffset: -1}},
		{name: "invalid fallback cursor", cursor: &FeedCursor{Version: FeedCursorVersion, FallbackCursor: &DiscoverCursor{CreatedAt: time.Now(), PostID: "not-a-uuid"}}},
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
