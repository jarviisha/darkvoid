package cache

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	feedentity "github.com/jarviisha/darkvoid/internal/feature/feed/entity"
)

// TestTrendingCache_SerializedSchemaPinned pins the exact field set the
// trending cache serializes. The cache stores whole Post bodies and serves
// them without re-reading the database, so changing the entity's serialized
// shape while old values sit under the same key means up to a TTL's worth of
// silently misdecoded posts after a deploy.
//
// If this test fails, you changed the serialized shape of feedentity.Post (or
// its nested types): bump the version suffix in trendingKey in the same
// change, then update the pinned sets here.
func TestTrendingCache_SerializedSchemaPinned(t *testing.T) {
	avatar := "avatars/a.png"
	post := &feedentity.Post{
		ID:         uuid.New(),
		AuthorID:   uuid.New(),
		Content:    "post",
		Visibility: "public",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
		Media: []feedentity.PostMedia{{
			ID:        uuid.New(),
			PostID:    uuid.New(),
			MediaKey:  "media/key",
			MediaType: "image",
			Position:  1,
			CreatedAt: time.Now().UTC(),
		}},
		LikeCount:         1,
		CommentCount:      2,
		IsLiked:           true,
		IsFollowingAuthor: true,
		Author: &feedentity.Author{
			ID:          uuid.New(),
			Username:    "u",
			DisplayName: "U",
			AvatarKey:   &avatar,
		},
	}

	raw, err := json.Marshal(post)
	if err != nil {
		t.Fatalf("marshal post: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal post: %v", err)
	}

	assertExactFields(t, "Post", decoded, []string{
		"ID", "AuthorID", "Content", "Visibility", "CreatedAt", "UpdatedAt",
		"Media", "LikeCount", "CommentCount", "IsLiked", "IsFollowingAuthor", "Author",
	})

	var media []map[string]json.RawMessage
	if err := json.Unmarshal(decoded["Media"], &media); err != nil || len(media) != 1 {
		t.Fatalf("unmarshal media: %v (%d items)", err, len(media))
	}
	assertExactFields(t, "PostMedia", media[0], []string{
		"ID", "PostID", "MediaKey", "MediaType", "Position", "CreatedAt",
	})

	var author map[string]json.RawMessage
	if err := json.Unmarshal(decoded["Author"], &author); err != nil {
		t.Fatalf("unmarshal author: %v", err)
	}
	assertExactFields(t, "Author", author, []string{
		"ID", "Username", "DisplayName", "AvatarKey",
	})
}

func assertExactFields(t *testing.T, name string, got map[string]json.RawMessage, want []string) {
	t.Helper()
	wantSet := make(map[string]bool, len(want))
	for _, f := range want {
		wantSet[f] = true
		if _, ok := got[f]; !ok {
			t.Errorf("%s: serialized field %q missing — bump trendingKey's version and update this pin", name, f)
		}
	}
	for f := range got {
		if !wantSet[f] {
			t.Errorf("%s: new serialized field %q — bump trendingKey's version and update this pin", name, f)
		}
	}
}
