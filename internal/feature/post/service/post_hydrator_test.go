package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/post/entity"
)

// --------------------------------------------------------------------------
// enrichBatch tests
// --------------------------------------------------------------------------

func TestHydrate_Batch_EmptyPosts(t *testing.T) {
	h := NewHydrator(HydratorDeps{Media: &mockMediaRepo{}, Likes: &mockLikeRepo{}})
	h.Hydrate(context.Background(), []*entity.Post{}, nil, FieldMedia|FieldLiked)
	// No panic = success
}

func TestHydrate_Batch_MediaOnly(t *testing.T) {
	ctx := context.Background()
	post1 := samplePost(uuid.New())
	post2 := samplePost(uuid.New())

	mr := &mockMediaRepo{
		getByPostsBatch: func(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]*entity.PostMedia, error) {
			return map[uuid.UUID][]*entity.PostMedia{
				post1.ID: {{ID: uuid.New(), PostID: post1.ID, MediaKey: "img1.jpg", MediaType: "image"}},
				post2.ID: {{ID: uuid.New(), PostID: post2.ID, MediaKey: "vid1.mp4", MediaType: "video"}},
			}, nil
		},
	}

	h := NewHydrator(HydratorDeps{Media: mr, Likes: &mockLikeRepo{}})
	h.Hydrate(ctx, []*entity.Post{post1, post2}, nil, FieldMedia|FieldLiked)

	if len(post1.Media) != 1 {
		t.Errorf("expected 1 media for post1, got %d", len(post1.Media))
	}
	if len(post2.Media) != 1 {
		t.Errorf("expected 1 media for post2, got %d", len(post2.Media))
	}
	if post1.Media[0].MediaKey != "img1.jpg" {
		t.Errorf("expected img1.jpg, got %s", post1.Media[0].MediaKey)
	}
}

func TestHydrate_Batch_MediaError_NonFatal(t *testing.T) {
	ctx := context.Background()
	post1 := samplePost(uuid.New())

	mr := &mockMediaRepo{
		getByPostsBatch: func(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]*entity.PostMedia, error) {
			return nil, errors.New("db error")
		},
	}

	h := NewHydrator(HydratorDeps{Media: mr, Likes: &mockLikeRepo{}})
	h.Hydrate(ctx, []*entity.Post{post1}, nil, FieldMedia|FieldLiked)

	// Should not panic, media should be nil/empty
	if post1.Media != nil {
		t.Errorf("expected nil media on error, got %v", post1.Media)
	}
}

func TestHydrate_Batch_IsLiked_NoViewerID(t *testing.T) {
	ctx := context.Background()
	post1 := samplePost(uuid.New())

	lr := &mockLikeRepo{
		getLikedPostIDs: func(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) ([]uuid.UUID, error) {
			t.Error("should not call GetLikedPostIDs when viewerID is nil")
			return nil, nil
		},
	}

	h := NewHydrator(HydratorDeps{Media: &mockMediaRepo{}, Likes: lr})
	h.Hydrate(ctx, []*entity.Post{post1}, nil, FieldMedia|FieldLiked)

	if post1.IsLiked {
		t.Error("expected IsLiked to be false when no viewerID")
	}
}

func TestHydrate_Batch_IsLiked_WithViewerID(t *testing.T) {
	ctx := context.Background()
	viewerID := uuid.New()
	post1 := samplePost(uuid.New())
	post2 := samplePost(uuid.New())
	post3 := samplePost(uuid.New())

	lr := &mockLikeRepo{
		getLikedPostIDs: func(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) ([]uuid.UUID, error) {
			if userID != viewerID {
				t.Errorf("expected viewerID %s, got %s", viewerID, userID)
			}
			// Viewer liked post1 and post3
			return []uuid.UUID{post1.ID, post3.ID}, nil
		},
	}

	h := NewHydrator(HydratorDeps{Media: &mockMediaRepo{}, Likes: lr})
	h.Hydrate(ctx, []*entity.Post{post1, post2, post3}, &viewerID, FieldMedia|FieldLiked)

	if !post1.IsLiked {
		t.Error("expected post1 IsLiked = true")
	}
	if post2.IsLiked {
		t.Error("expected post2 IsLiked = false")
	}
	if !post3.IsLiked {
		t.Error("expected post3 IsLiked = true")
	}
}

func TestHydrate_Batch_IsLikedError_NonFatal(t *testing.T) {
	ctx := context.Background()
	viewerID := uuid.New()
	post1 := samplePost(uuid.New())

	lr := &mockLikeRepo{
		getLikedPostIDs: func(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) ([]uuid.UUID, error) {
			return nil, errors.New("redis error")
		},
	}

	h := NewHydrator(HydratorDeps{Media: &mockMediaRepo{}, Likes: lr})
	h.Hydrate(ctx, []*entity.Post{post1}, &viewerID, FieldMedia|FieldLiked)

	// Should not panic, IsLiked should be false
	if post1.IsLiked {
		t.Error("expected IsLiked = false on error")
	}
}

func TestHydrate_Batch_NoLikeRepo(t *testing.T) {
	ctx := context.Background()
	viewerID := uuid.New()
	post1 := samplePost(uuid.New())

	h := NewHydrator(HydratorDeps{Media: &mockMediaRepo{}, Likes: nil})
	h.Hydrate(ctx, []*entity.Post{post1}, &viewerID, FieldMedia|FieldLiked)

	if post1.IsLiked {
		t.Error("expected IsLiked = false when likeRepo is nil")
	}
}

// --------------------------------------------------------------------------
// enrichAuthors tests
// --------------------------------------------------------------------------

func TestHydrate_Authors_EmptyPosts(t *testing.T) {
	h := NewHydrator(HydratorDeps{})
	h.Hydrate(context.Background(), []*entity.Post{}, nil, FieldAuthor)
	// No panic = success
}

func TestHydrate_Authors_NoUserReader(t *testing.T) {
	post1 := samplePost(uuid.New())
	h := NewHydrator(HydratorDeps{Users: nil})
	h.Hydrate(context.Background(), []*entity.Post{post1}, nil, FieldAuthor)

	if post1.Author != nil {
		t.Error("expected nil author when userReader is nil")
	}
}

func TestHydrate_Authors_SinglePost(t *testing.T) {
	ctx := context.Background()
	authorID := uuid.New()
	post1 := samplePost(authorID)

	mockUR := &mockUserReader{
		getAuthorsByIDs: func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*entity.Author, error) {
			return map[uuid.UUID]*entity.Author{
				authorID: {ID: authorID, Username: "alice", DisplayName: "Alice"},
			}, nil
		},
	}

	h := NewHydrator(HydratorDeps{Users: mockUR})
	h.Hydrate(ctx, []*entity.Post{post1}, nil, FieldAuthor)

	if post1.Author == nil {
		t.Fatal("expected author to be set")
	}
	if post1.Author.Username != "alice" {
		t.Errorf("expected username alice, got %s", post1.Author.Username)
	}
}

func TestHydrate_Authors_MultiplePosts_SameAuthor(t *testing.T) {
	ctx := context.Background()
	authorID := uuid.New()
	post1 := samplePost(authorID)
	post2 := samplePost(authorID)
	post3 := samplePost(authorID)

	mockUR := &mockUserReader{
		getAuthorsByIDs: func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*entity.Author, error) {
			// Should only request unique author IDs
			if len(ids) != 1 {
				t.Errorf("expected 1 unique author ID, got %d", len(ids))
			}
			return map[uuid.UUID]*entity.Author{
				authorID: {ID: authorID, Username: "bob", DisplayName: "Bob"},
			}, nil
		},
	}

	h := NewHydrator(HydratorDeps{Users: mockUR})
	h.Hydrate(ctx, []*entity.Post{post1, post2, post3}, nil, FieldAuthor)

	if post1.Author == nil || post1.Author.Username != "bob" {
		t.Error("expected post1 author to be bob")
	}
	if post2.Author == nil || post2.Author.Username != "bob" {
		t.Error("expected post2 author to be bob")
	}
}

func TestHydrate_Authors_MultiplePosts_DifferentAuthors(t *testing.T) {
	ctx := context.Background()
	author1 := uuid.New()
	author2 := uuid.New()
	post1 := samplePost(author1)
	post2 := samplePost(author2)

	mockUR := &mockUserReader{
		getAuthorsByIDs: func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*entity.Author, error) {
			return map[uuid.UUID]*entity.Author{
				author1: {ID: author1, Username: "alice", DisplayName: "Alice"},
				author2: {ID: author2, Username: "bob", DisplayName: "Bob"},
			}, nil
		},
	}

	h := NewHydrator(HydratorDeps{Users: mockUR})
	h.Hydrate(ctx, []*entity.Post{post1, post2}, nil, FieldAuthor)

	if post1.Author == nil || post1.Author.Username != "alice" {
		t.Error("expected post1 author to be alice")
	}
	if post2.Author == nil || post2.Author.Username != "bob" {
		t.Error("expected post2 author to be bob")
	}
}

func TestHydrate_Authors_Error_NonFatal(t *testing.T) {
	ctx := context.Background()
	post1 := samplePost(uuid.New())

	mockUR := &mockUserReader{
		getAuthorsByIDs: func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*entity.Author, error) {
			return nil, errors.New("user service error")
		},
	}

	h := NewHydrator(HydratorDeps{Users: mockUR})
	h.Hydrate(ctx, []*entity.Post{post1}, nil, FieldAuthor)

	// Should not panic, author should be nil
	if post1.Author != nil {
		t.Error("expected nil author on error")
	}
}

// --------------------------------------------------------------------------
// enrichIsFollowingAuthor tests
// --------------------------------------------------------------------------

func TestHydrate_IsFollowingAuthor_NoFollowChecker(t *testing.T) {
	post1 := samplePost(uuid.New())
	viewerID := uuid.New()

	h := NewHydrator(HydratorDeps{Follows: nil})
	h.Hydrate(context.Background(), []*entity.Post{post1}, &viewerID, FieldFollowingAuthor)

	if post1.IsFollowingAuthor {
		t.Error("expected IsFollowingAuthor = false when followChecker is nil")
	}
}

func TestHydrate_IsFollowingAuthor_NoViewerID(t *testing.T) {
	post1 := samplePost(uuid.New())

	mockFC := &mockFollowChecker{
		getFollowingAmong: func(context.Context, uuid.UUID, []uuid.UUID) ([]uuid.UUID, error) {
			t.Error("should not batch-check following when viewerID is nil")
			return nil, nil
		},
	}

	h := NewHydrator(HydratorDeps{Follows: mockFC})
	h.Hydrate(context.Background(), []*entity.Post{post1}, nil, FieldFollowingAuthor)
}

func TestHydrate_IsFollowingAuthor_EmptyPosts(t *testing.T) {
	viewerID := uuid.New()
	h := NewHydrator(HydratorDeps{Follows: &mockFollowChecker{}})
	h.Hydrate(context.Background(), []*entity.Post{}, &viewerID, FieldFollowingAuthor)
}

func TestHydrate_IsFollowingAuthor_ViewerIsAuthor_Skipped(t *testing.T) {
	ctx := context.Background()
	viewerID := uuid.New()
	post1 := samplePost(viewerID) // Viewer is the author

	mockFC := &mockFollowChecker{
		getFollowingAmong: func(context.Context, uuid.UUID, []uuid.UUID) ([]uuid.UUID, error) {
			t.Error("should not batch-check when every post belongs to the viewer")
			return nil, nil
		},
	}

	h := NewHydrator(HydratorDeps{Follows: mockFC})
	h.Hydrate(ctx, []*entity.Post{post1}, &viewerID, FieldFollowingAuthor)

	if post1.IsFollowingAuthor {
		t.Error("expected IsFollowingAuthor = false for own post")
	}
}

func TestHydrate_IsFollowingAuthor_Success(t *testing.T) {
	ctx := context.Background()
	viewerID := uuid.New()
	author1 := uuid.New()
	author2 := uuid.New()
	post1 := samplePost(author1)
	post2 := samplePost(author2)
	post3 := samplePost(author1) // Same author as post1
	batchCalls := 0

	mockFC := &mockFollowChecker{
		getFollowingAmong: func(_ context.Context, followerID uuid.UUID, followeeIDs []uuid.UUID) ([]uuid.UUID, error) {
			batchCalls++
			if followerID != viewerID {
				t.Errorf("expected followerID %s, got %s", viewerID, followerID)
			}
			if len(followeeIDs) != 2 {
				t.Fatalf("expected 2 unique author IDs, got %v", followeeIDs)
			}
			seen := map[uuid.UUID]bool{followeeIDs[0]: true, followeeIDs[1]: true}
			if !seen[author1] || !seen[author2] {
				t.Fatalf("batch author IDs = %v, want %s and %s", followeeIDs, author1, author2)
			}
			return []uuid.UUID{author1}, nil
		},
	}

	h := NewHydrator(HydratorDeps{Follows: mockFC})
	h.Hydrate(ctx, []*entity.Post{post1, post2, post3}, &viewerID, FieldFollowingAuthor)
	if batchCalls != 1 {
		t.Fatalf("batch follow calls = %d, want 1", batchCalls)
	}

	if !post1.IsFollowingAuthor {
		t.Error("expected post1 IsFollowingAuthor = true")
	}
	if post2.IsFollowingAuthor {
		t.Error("expected post2 IsFollowingAuthor = false")
	}
	if !post3.IsFollowingAuthor {
		t.Error("expected post3 IsFollowingAuthor = true (same author as post1)")
	}
}

func TestHydrate_IsFollowingAuthor_Error_NonFatal(t *testing.T) {
	ctx := context.Background()
	viewerID := uuid.New()
	post1 := samplePost(uuid.New())

	mockFC := &mockFollowChecker{
		getFollowingAmong: func(context.Context, uuid.UUID, []uuid.UUID) ([]uuid.UUID, error) {
			return nil, errors.New("follow service error")
		},
	}

	h := NewHydrator(HydratorDeps{Follows: mockFC})
	h.Hydrate(ctx, []*entity.Post{post1}, &viewerID, FieldFollowingAuthor)

	// Should not panic, IsFollowingAuthor should be false
	if post1.IsFollowingAuthor {
		t.Error("expected IsFollowingAuthor = false on error")
	}
}

// --------------------------------------------------------------------------
// enrichTags tests
// --------------------------------------------------------------------------

func TestHydrate_Tags_EmptyPosts(t *testing.T) {
	h := NewHydrator(HydratorDeps{Tags: &mockHashtagRepo{}})
	h.Hydrate(context.Background(), []*entity.Post{}, nil, FieldTags)
}

func TestHydrate_Tags_NoHashtagRepo(t *testing.T) {
	post1 := samplePost(uuid.New())
	h := NewHydrator(HydratorDeps{Tags: nil})
	h.Hydrate(context.Background(), []*entity.Post{post1}, nil, FieldTags)

	if post1.Tags != nil {
		t.Error("expected nil tags when hashtagRepo is nil")
	}
}

func TestHydrate_Tags_Success(t *testing.T) {
	ctx := context.Background()
	post1 := samplePost(uuid.New())
	post2 := samplePost(uuid.New())

	mockHR := &mockHashtagRepo{
		getNamesByPostIDs: func(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]string, error) {
			return map[uuid.UUID][]string{
				post1.ID: {"golang", "webdev"},
				post2.ID: {"architecture"},
			}, nil
		},
	}

	h := NewHydrator(HydratorDeps{Tags: mockHR})
	h.Hydrate(ctx, []*entity.Post{post1, post2}, nil, FieldTags)

	if len(post1.Tags) != 2 {
		t.Errorf("expected 2 tags for post1, got %d", len(post1.Tags))
	}
	if post1.Tags[0] != "golang" {
		t.Errorf("expected first tag 'golang', got %s", post1.Tags[0])
	}
	if len(post2.Tags) != 1 {
		t.Errorf("expected 1 tag for post2, got %d", len(post2.Tags))
	}
}

func TestHydrate_Tags_Error_NonFatal(t *testing.T) {
	ctx := context.Background()
	post1 := samplePost(uuid.New())

	mockHR := &mockHashtagRepo{
		getNamesByPostIDs: func(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]string, error) {
			return nil, errors.New("db error")
		},
	}

	h := NewHydrator(HydratorDeps{Tags: mockHR})
	h.Hydrate(ctx, []*entity.Post{post1}, nil, FieldTags)

	// Should not panic, tags should be nil/empty
}

// --------------------------------------------------------------------------
// enrichMentions tests
// --------------------------------------------------------------------------

func TestHydrate_Mentions_EmptyPosts(t *testing.T) {
	h := NewHydrator(HydratorDeps{Mentions: &mockMentionRepo{}, Users: &mockUserReader{}})
	h.Hydrate(context.Background(), []*entity.Post{}, nil, FieldMentions)
}

func TestHydrate_Mentions_NoMentionRepo(t *testing.T) {
	post1 := samplePost(uuid.New())
	h := NewHydrator(HydratorDeps{Mentions: nil, Users: &mockUserReader{}})
	h.Hydrate(context.Background(), []*entity.Post{post1}, nil, FieldMentions)

	if post1.Mentions != nil {
		t.Error("expected nil mentions when mentionRepo is nil")
	}
}

func TestHydrate_Mentions_Success(t *testing.T) {
	ctx := context.Background()
	post1 := samplePost(uuid.New())
	post2 := samplePost(uuid.New())
	user1 := uuid.New()
	user2 := uuid.New()
	user3 := uuid.New()

	mockMR := &mockMentionRepo{
		getBatch: func(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
			return map[uuid.UUID][]uuid.UUID{
				post1.ID: {user1, user2},
				post2.ID: {user3},
			}, nil
		},
	}

	mockUR := &mockUserReader{
		getAuthorsByIDs: func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*entity.Author, error) {
			return map[uuid.UUID]*entity.Author{
				user1: {ID: user1, Username: "alice", DisplayName: "Alice"},
				user2: {ID: user2, Username: "bob", DisplayName: "Bob"},
				user3: {ID: user3, Username: "charlie", DisplayName: "Charlie"},
			}, nil
		},
	}

	h := NewHydrator(HydratorDeps{Mentions: mockMR, Users: mockUR})
	h.Hydrate(ctx, []*entity.Post{post1, post2}, nil, FieldMentions)

	if len(post1.Mentions) != 2 {
		t.Errorf("expected 2 mentions for post1, got %d", len(post1.Mentions))
	}
	if len(post2.Mentions) != 1 {
		t.Errorf("expected 1 mention for post2, got %d", len(post2.Mentions))
	}
	if post1.Mentions[0].Username != "alice" {
		t.Errorf("expected first mention 'alice', got %s", post1.Mentions[0].Username)
	}
}

func TestHydrate_Mentions_NoMentionsInPosts(t *testing.T) {
	ctx := context.Background()
	post1 := samplePost(uuid.New())

	mockMR := &mockMentionRepo{
		getBatch: func(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
			return map[uuid.UUID][]uuid.UUID{}, nil
		},
	}

	h := NewHydrator(HydratorDeps{Mentions: mockMR, Users: &mockUserReader{}})
	h.Hydrate(ctx, []*entity.Post{post1}, nil, FieldMentions)

	// Should not call userReader when no mentions
	if len(post1.Mentions) > 0 {
		t.Error("expected no mentions")
	}
}

func TestHydrate_Mentions_GetBatchError_NonFatal(t *testing.T) {
	ctx := context.Background()
	post1 := samplePost(uuid.New())

	mockMR := &mockMentionRepo{
		getBatch: func(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
			return nil, errors.New("db error")
		},
	}

	h := NewHydrator(HydratorDeps{Mentions: mockMR, Users: &mockUserReader{}})
	h.Hydrate(ctx, []*entity.Post{post1}, nil, FieldMentions)

	// Should not panic
}

func TestHydrate_Mentions_GetAuthorsByIDsError_NonFatal(t *testing.T) {
	ctx := context.Background()
	post1 := samplePost(uuid.New())
	user1 := uuid.New()

	mockMR := &mockMentionRepo{
		getBatch: func(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
			return map[uuid.UUID][]uuid.UUID{
				post1.ID: {user1},
			}, nil
		},
	}

	mockUR := &mockUserReader{
		getAuthorsByIDs: func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*entity.Author, error) {
			return nil, errors.New("user service error")
		},
	}

	h := NewHydrator(HydratorDeps{Mentions: mockMR, Users: mockUR})
	h.Hydrate(ctx, []*entity.Post{post1}, nil, FieldMentions)

	// Should not panic, mentions should be empty
}

// Authors and mentions used to be fetched by two separate lookups, so a post
// with mentions cost two round trips to the user directory.
func TestHydrate_AuthorAndMentions_OneDirectoryLookup(t *testing.T) {
	authorID, mentionedID := uuid.New(), uuid.New()
	p := samplePost(authorID)

	var lookups [][]uuid.UUID
	h := NewHydrator(HydratorDeps{
		Mentions: &mockMentionRepo{
			getBatch: func(context.Context, []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
				return map[uuid.UUID][]uuid.UUID{p.ID: {mentionedID}}, nil
			},
		},
		Users: &mockUserReader{
			getAuthorsByIDs: func(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]*entity.Author, error) {
				lookups = append(lookups, ids)
				return map[uuid.UUID]*entity.Author{
					authorID:    {ID: authorID, Username: "author"},
					mentionedID: {ID: mentionedID, Username: "mentioned"},
				}, nil
			},
		},
	})
	h.Hydrate(context.Background(), []*entity.Post{p}, nil, FieldAuthor|FieldMentions)

	if len(lookups) != 1 || len(lookups[0]) != 2 {
		t.Fatalf("expected one lookup of 2 ids, got %v", lookups)
	}
	if p.Author == nil || p.Author.Username != "author" {
		t.Errorf("author not set: %+v", p.Author)
	}
	if len(p.Mentions) != 1 || p.Mentions[0].Username != "mentioned" {
		t.Errorf("mentions not set: %+v", p.Mentions)
	}
}

func TestHydrate_ViewerFieldsSkippedWithoutViewer(t *testing.T) {
	p := samplePost(uuid.New())
	h := NewHydrator(HydratorDeps{
		Likes: &mockLikeRepo{getLikedPostIDs: func(context.Context, uuid.UUID, []uuid.UUID) ([]uuid.UUID, error) {
			t.Error("likes queried without a viewer")
			return nil, nil
		}},
		Follows: &mockFollowChecker{getFollowingAmong: func(context.Context, uuid.UUID, []uuid.UUID) ([]uuid.UUID, error) {
			t.Error("follows queried without a viewer")
			return nil, nil
		}},
	})
	h.Hydrate(context.Background(), []*entity.Post{p}, nil, FieldsAll)
}
