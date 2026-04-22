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

func TestEnrichBatch_EmptyPosts(t *testing.T) {
	svc := newPostService(&mockPostRepo{}, &mockMediaRepo{}, &mockLikeRepo{})
	svc.enrichBatch(context.Background(), []*entity.Post{}, nil)
	// No panic = success
}

func TestEnrichBatch_MediaOnly(t *testing.T) {
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

	svc := newPostService(&mockPostRepo{}, mr, &mockLikeRepo{})
	svc.enrichBatch(ctx, []*entity.Post{post1, post2}, nil)

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

func TestEnrichBatch_MediaError_NonFatal(t *testing.T) {
	ctx := context.Background()
	post1 := samplePost(uuid.New())

	mr := &mockMediaRepo{
		getByPostsBatch: func(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]*entity.PostMedia, error) {
			return nil, errors.New("db error")
		},
	}

	svc := newPostService(&mockPostRepo{}, mr, &mockLikeRepo{})
	svc.enrichBatch(ctx, []*entity.Post{post1}, nil)

	// Should not panic, media should be nil/empty
	if post1.Media != nil {
		t.Errorf("expected nil media on error, got %v", post1.Media)
	}
}

func TestEnrichBatch_IsLiked_NoViewerID(t *testing.T) {
	ctx := context.Background()
	post1 := samplePost(uuid.New())

	lr := &mockLikeRepo{
		getLikedPostIDs: func(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) ([]uuid.UUID, error) {
			t.Error("should not call GetLikedPostIDs when viewerID is nil")
			return nil, nil
		},
	}

	svc := newPostService(&mockPostRepo{}, &mockMediaRepo{}, lr)
	svc.enrichBatch(ctx, []*entity.Post{post1}, nil)

	if post1.IsLiked {
		t.Error("expected IsLiked to be false when no viewerID")
	}
}

func TestEnrichBatch_IsLiked_WithViewerID(t *testing.T) {
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

	svc := newPostService(&mockPostRepo{}, &mockMediaRepo{}, lr)
	svc.enrichBatch(ctx, []*entity.Post{post1, post2, post3}, &viewerID)

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

func TestEnrichBatch_IsLikedError_NonFatal(t *testing.T) {
	ctx := context.Background()
	viewerID := uuid.New()
	post1 := samplePost(uuid.New())

	lr := &mockLikeRepo{
		getLikedPostIDs: func(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) ([]uuid.UUID, error) {
			return nil, errors.New("redis error")
		},
	}

	svc := newPostService(&mockPostRepo{}, &mockMediaRepo{}, lr)
	svc.enrichBatch(ctx, []*entity.Post{post1}, &viewerID)

	// Should not panic, IsLiked should be false
	if post1.IsLiked {
		t.Error("expected IsLiked = false on error")
	}
}

func TestEnrichBatch_NoLikeRepo(t *testing.T) {
	ctx := context.Background()
	viewerID := uuid.New()
	post1 := samplePost(uuid.New())

	svc := newPostService(&mockPostRepo{}, &mockMediaRepo{}, nil)
	svc.enrichBatch(ctx, []*entity.Post{post1}, &viewerID)

	if post1.IsLiked {
		t.Error("expected IsLiked = false when likeRepo is nil")
	}
}

// --------------------------------------------------------------------------
// enrichAuthors tests
// --------------------------------------------------------------------------

func TestEnrichAuthors_EmptyPosts(t *testing.T) {
	svc := &PostService{}
	svc.enrichAuthors(context.Background(), []*entity.Post{})
	// No panic = success
}

func TestEnrichAuthors_NoUserReader(t *testing.T) {
	post1 := samplePost(uuid.New())
	svc := &PostService{userReader: nil}
	svc.enrichAuthors(context.Background(), []*entity.Post{post1})

	if post1.Author != nil {
		t.Error("expected nil author when userReader is nil")
	}
}

func TestEnrichAuthors_SinglePost(t *testing.T) {
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

	svc := &PostService{userReader: mockUR}
	svc.enrichAuthors(ctx, []*entity.Post{post1})

	if post1.Author == nil {
		t.Fatal("expected author to be set")
	}
	if post1.Author.Username != "alice" {
		t.Errorf("expected username alice, got %s", post1.Author.Username)
	}
}

func TestEnrichAuthors_MultiplePosts_SameAuthor(t *testing.T) {
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

	svc := &PostService{userReader: mockUR}
	svc.enrichAuthors(ctx, []*entity.Post{post1, post2, post3})

	if post1.Author == nil || post1.Author.Username != "bob" {
		t.Error("expected post1 author to be bob")
	}
	if post2.Author == nil || post2.Author.Username != "bob" {
		t.Error("expected post2 author to be bob")
	}
}

func TestEnrichAuthors_MultiplePosts_DifferentAuthors(t *testing.T) {
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

	svc := &PostService{userReader: mockUR}
	svc.enrichAuthors(ctx, []*entity.Post{post1, post2})

	if post1.Author == nil || post1.Author.Username != "alice" {
		t.Error("expected post1 author to be alice")
	}
	if post2.Author == nil || post2.Author.Username != "bob" {
		t.Error("expected post2 author to be bob")
	}
}

func TestEnrichAuthors_Error_NonFatal(t *testing.T) {
	ctx := context.Background()
	post1 := samplePost(uuid.New())

	mockUR := &mockUserReader{
		getAuthorsByIDs: func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*entity.Author, error) {
			return nil, errors.New("user service error")
		},
	}

	svc := &PostService{userReader: mockUR}
	svc.enrichAuthors(ctx, []*entity.Post{post1})

	// Should not panic, author should be nil
	if post1.Author != nil {
		t.Error("expected nil author on error")
	}
}

// --------------------------------------------------------------------------
// enrichIsFollowingAuthor tests
// --------------------------------------------------------------------------

func TestEnrichIsFollowingAuthor_NoFollowChecker(t *testing.T) {
	post1 := samplePost(uuid.New())
	viewerID := uuid.New()

	svc := &PostService{followChecker: nil}
	svc.enrichIsFollowingAuthor(context.Background(), []*entity.Post{post1}, &viewerID)

	if post1.IsFollowingAuthor {
		t.Error("expected IsFollowingAuthor = false when followChecker is nil")
	}
}

func TestEnrichIsFollowingAuthor_NoViewerID(t *testing.T) {
	post1 := samplePost(uuid.New())

	mockFC := &mockFollowChecker{
		isFollowing: func(ctx context.Context, followerID, followeeID uuid.UUID) (bool, error) {
			t.Error("should not call IsFollowing when viewerID is nil")
			return false, nil
		},
	}

	svc := &PostService{followChecker: mockFC}
	svc.enrichIsFollowingAuthor(context.Background(), []*entity.Post{post1}, nil)
}

func TestEnrichIsFollowingAuthor_EmptyPosts(t *testing.T) {
	viewerID := uuid.New()
	svc := &PostService{followChecker: &mockFollowChecker{}}
	svc.enrichIsFollowingAuthor(context.Background(), []*entity.Post{}, &viewerID)
}

func TestEnrichIsFollowingAuthor_ViewerIsAuthor_Skipped(t *testing.T) {
	ctx := context.Background()
	viewerID := uuid.New()
	post1 := samplePost(viewerID) // Viewer is the author

	mockFC := &mockFollowChecker{
		isFollowing: func(ctx context.Context, followerID, followeeID uuid.UUID) (bool, error) {
			t.Error("should not check if viewer is following themselves")
			return false, nil
		},
	}

	svc := &PostService{followChecker: mockFC}
	svc.enrichIsFollowingAuthor(ctx, []*entity.Post{post1}, &viewerID)

	if post1.IsFollowingAuthor {
		t.Error("expected IsFollowingAuthor = false for own post")
	}
}

func TestEnrichIsFollowingAuthor_Success(t *testing.T) {
	ctx := context.Background()
	viewerID := uuid.New()
	author1 := uuid.New()
	author2 := uuid.New()
	post1 := samplePost(author1)
	post2 := samplePost(author2)
	post3 := samplePost(author1) // Same author as post1

	mockFC := &mockFollowChecker{
		isFollowing: func(ctx context.Context, followerID, followeeID uuid.UUID) (bool, error) {
			if followerID != viewerID {
				t.Errorf("expected followerID %s, got %s", viewerID, followerID)
			}
			// Viewer follows author1 but not author2
			return followeeID == author1, nil
		},
	}

	svc := &PostService{followChecker: mockFC}
	svc.enrichIsFollowingAuthor(ctx, []*entity.Post{post1, post2, post3}, &viewerID)

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

func TestEnrichIsFollowingAuthor_Error_NonFatal(t *testing.T) {
	ctx := context.Background()
	viewerID := uuid.New()
	post1 := samplePost(uuid.New())

	mockFC := &mockFollowChecker{
		isFollowing: func(ctx context.Context, followerID, followeeID uuid.UUID) (bool, error) {
			return false, errors.New("follow service error")
		},
	}

	svc := &PostService{followChecker: mockFC}
	svc.enrichIsFollowingAuthor(ctx, []*entity.Post{post1}, &viewerID)

	// Should not panic, IsFollowingAuthor should be false
	if post1.IsFollowingAuthor {
		t.Error("expected IsFollowingAuthor = false on error")
	}
}

// --------------------------------------------------------------------------
// enrichTags tests
// --------------------------------------------------------------------------

func TestEnrichTags_EmptyPosts(t *testing.T) {
	svc := &PostService{hashtagRepo: &mockHashtagRepo{}}
	svc.enrichTags(context.Background(), []*entity.Post{})
}

func TestEnrichTags_NoHashtagRepo(t *testing.T) {
	post1 := samplePost(uuid.New())
	svc := &PostService{hashtagRepo: nil}
	svc.enrichTags(context.Background(), []*entity.Post{post1})

	if post1.Tags != nil {
		t.Error("expected nil tags when hashtagRepo is nil")
	}
}

func TestEnrichTags_Success(t *testing.T) {
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

	svc := &PostService{hashtagRepo: mockHR}
	svc.enrichTags(ctx, []*entity.Post{post1, post2})

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

func TestEnrichTags_Error_NonFatal(t *testing.T) {
	ctx := context.Background()
	post1 := samplePost(uuid.New())

	mockHR := &mockHashtagRepo{
		getNamesByPostIDs: func(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]string, error) {
			return nil, errors.New("db error")
		},
	}

	svc := &PostService{hashtagRepo: mockHR}
	svc.enrichTags(ctx, []*entity.Post{post1})

	// Should not panic, tags should be nil/empty
}

// --------------------------------------------------------------------------
// enrichMentions tests
// --------------------------------------------------------------------------

func TestEnrichMentions_EmptyPosts(t *testing.T) {
	svc := &PostService{mentionRepo: &mockMentionRepo{}, userReader: &mockUserReader{}}
	svc.enrichMentions(context.Background(), []*entity.Post{})
}

func TestEnrichMentions_NoMentionRepo(t *testing.T) {
	post1 := samplePost(uuid.New())
	svc := &PostService{mentionRepo: nil, userReader: &mockUserReader{}}
	svc.enrichMentions(context.Background(), []*entity.Post{post1})

	if post1.Mentions != nil {
		t.Error("expected nil mentions when mentionRepo is nil")
	}
}

func TestEnrichMentions_Success(t *testing.T) {
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

	svc := &PostService{mentionRepo: mockMR, userReader: mockUR}
	svc.enrichMentions(ctx, []*entity.Post{post1, post2})

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

func TestEnrichMentions_NoMentionsInPosts(t *testing.T) {
	ctx := context.Background()
	post1 := samplePost(uuid.New())

	mockMR := &mockMentionRepo{
		getBatch: func(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
			return map[uuid.UUID][]uuid.UUID{}, nil
		},
	}

	svc := &PostService{mentionRepo: mockMR, userReader: &mockUserReader{}}
	svc.enrichMentions(ctx, []*entity.Post{post1})

	// Should not call userReader when no mentions
	if len(post1.Mentions) > 0 {
		t.Error("expected no mentions")
	}
}

func TestEnrichMentions_GetBatchError_NonFatal(t *testing.T) {
	ctx := context.Background()
	post1 := samplePost(uuid.New())

	mockMR := &mockMentionRepo{
		getBatch: func(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
			return nil, errors.New("db error")
		},
	}

	svc := &PostService{mentionRepo: mockMR, userReader: &mockUserReader{}}
	svc.enrichMentions(ctx, []*entity.Post{post1})

	// Should not panic
}

func TestEnrichMentions_GetAuthorsByIDsError_NonFatal(t *testing.T) {
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

	svc := &PostService{mentionRepo: mockMR, userReader: mockUR}
	svc.enrichMentions(ctx, []*entity.Post{post1})

	// Should not panic, mentions should be empty
}
