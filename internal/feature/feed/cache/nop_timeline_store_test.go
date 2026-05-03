package cache

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/feed"
)

func TestNopTimelineStore_MissAndNonFatalOperations(t *testing.T) {
	store := NewNopTimelineStore()
	ctx := context.Background()
	userID := uuid.New()
	postID := uuid.New()

	if err := store.AddPost(ctx, userID, feed.TimelineEntry{PostID: postID, Score: 1}); err != nil {
		t.Fatalf("AddPost: %v", err)
	}
	if err := store.AddPostsBatch(ctx, userID, []feed.TimelineEntry{{PostID: postID, Score: 1}}); err != nil {
		t.Fatalf("AddPostsBatch: %v", err)
	}
	page, err := store.ReadPage(ctx, userID, nil, 20)
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if page == nil || len(page.Entries) != 0 || page.Last != nil {
		t.Fatalf("unexpected page: %+v", page)
	}
	if err := store.Trim(ctx, userID); err != nil {
		t.Fatalf("Trim: %v", err)
	}
	if err := store.RemovePostBestEffort(ctx, userID, postID); err != nil {
		t.Fatalf("RemovePostBestEffort: %v", err)
	}
}
