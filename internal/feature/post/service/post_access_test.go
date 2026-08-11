package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	post "github.com/jarviisha/darkvoid/internal/feature/post"
	"github.com/jarviisha/darkvoid/internal/feature/post/entity"
	"github.com/jarviisha/darkvoid/internal/pagination"
)

func TestGetPost_PrivateDeniedToAnonymous(t *testing.T) {
	authorID := uuid.New()
	pr := &mockPostRepo{getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
		p := samplePost(authorID)
		p.Visibility = entity.VisibilityPrivate
		return p, nil
	}}
	svc := newPostService(pr, &mockMediaRepo{}, &mockLikeRepo{})

	_, err := svc.GetPost(context.Background(), uuid.New(), nil)
	if err != post.ErrPostNotFound {
		t.Fatalf("GetPost() error = %v, want ErrPostNotFound", err)
	}
}

func TestGetPost_FollowersRequiresRelationship(t *testing.T) {
	authorID := uuid.New()
	viewerID := uuid.New()
	pr := &mockPostRepo{getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
		p := samplePost(authorID)
		p.Visibility = entity.VisibilityFollowers
		return p, nil
	}}

	tests := []struct {
		name      string
		following bool
		wantErr   error
	}{
		{name: "non-follower denied", following: false, wantErr: post.ErrPostNotFound},
		{name: "follower allowed", following: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newPostService(pr, &mockMediaRepo{}, &mockLikeRepo{})
			svc.followChecker = &mockFollowChecker{isFollowing: func(_ context.Context, followerID, followeeID uuid.UUID) (bool, error) {
				if followerID != viewerID || followeeID != authorID {
					t.Fatalf("IsFollowing(%s, %s), want (%s, %s)", followerID, followeeID, viewerID, authorID)
				}
				return tt.following, nil
			}}

			_, err := svc.GetPost(context.Background(), uuid.New(), &viewerID)
			if err != tt.wantErr {
				t.Fatalf("GetPost() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetUserPosts_AnonymousForcesPublicVisibility(t *testing.T) {
	authorID := uuid.New()
	var gotVisibilities []string
	pr := &mockPostRepo{getByAuthorWithCursor: func(_ context.Context, _ uuid.UUID, _ pgtype.Timestamptz, _ uuid.UUID, visibilities []string, _ int32) ([]*entity.Post, error) {
		gotVisibilities = visibilities
		return nil, nil
	}}
	svc := newPostService(pr, &mockMediaRepo{}, &mockLikeRepo{})

	if _, _, err := svc.GetUserPosts(context.Background(), authorID, nil, nil, "", 20); err != nil {
		t.Fatalf("GetUserPosts() error = %v", err)
	}
	if len(gotVisibilities) != 1 || gotVisibilities[0] != string(entity.VisibilityPublic) {
		t.Fatalf("visibility filters = %v, want [public]", gotVisibilities)
	}
}

func TestCreateComment_PrivatePostDeniedToNonOwner(t *testing.T) {
	authorID := uuid.New()
	viewerID := uuid.New()
	postID := uuid.New()
	pr := &mockPostRepo{getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
		p := samplePost(authorID)
		p.ID = postID
		p.Visibility = entity.VisibilityPrivate
		return p, nil
	}}
	svc := newCommentService(&mockCommentRepo{}, pr)

	_, err := svc.CreateComment(context.Background(), postID, viewerID, nil, "not allowed", nil, nil)
	if err != post.ErrPostNotFound {
		t.Fatalf("CreateComment() error = %v, want ErrPostNotFound", err)
	}
}

func TestGetComments_PrivatePostDeniedToAnonymous(t *testing.T) {
	authorID := uuid.New()
	postID := uuid.New()
	pr := &mockPostRepo{getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
		p := samplePost(authorID)
		p.ID = postID
		p.Visibility = entity.VisibilityPrivate
		return p, nil
	}}
	svc := newCommentService(&mockCommentRepo{}, pr)

	_, _, err := svc.GetComments(context.Background(), postID, nil, pagination.PaginationRequest{Limit: 20})
	if err != post.ErrPostNotFound {
		t.Fatalf("GetComments() error = %v, want ErrPostNotFound", err)
	}
}

func TestGetReplies_PrivatePostDeniedToAnonymous(t *testing.T) {
	authorID := uuid.New()
	postID := uuid.New()
	commentID := uuid.New()
	cr := &mockCommentRepo{getByID: func(_ context.Context, _ uuid.UUID) (*entity.Comment, error) {
		return &entity.Comment{ID: commentID, PostID: postID}, nil
	}}
	pr := &mockPostRepo{getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
		p := samplePost(authorID)
		p.ID = postID
		p.Visibility = entity.VisibilityPrivate
		return p, nil
	}}
	svc := newCommentService(cr, pr)

	_, _, err := svc.GetReplies(context.Background(), commentID, nil, pagination.PaginationRequest{Limit: 20})
	if err != post.ErrPostNotFound {
		t.Fatalf("GetReplies() error = %v, want ErrPostNotFound", err)
	}
}

func TestToggleLike_PrivatePostDeniedToNonOwner(t *testing.T) {
	authorID := uuid.New()
	viewerID := uuid.New()
	pr := &mockPostRepo{getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
		p := samplePost(authorID)
		p.Visibility = entity.VisibilityPrivate
		return p, nil
	}}
	svc := newLikeService(&mockLikeRepo{}, pr)

	_, err := svc.Toggle(context.Background(), viewerID, uuid.New())
	if err != post.ErrPostNotFound {
		t.Fatalf("Toggle() error = %v, want ErrPostNotFound", err)
	}
}

func TestToggleCommentLike_PrivatePostDeniedToNonOwner(t *testing.T) {
	authorID := uuid.New()
	viewerID := uuid.New()
	postID := uuid.New()
	commentID := uuid.New()
	cr := &mockCommentRepo{getByID: func(_ context.Context, _ uuid.UUID) (*entity.Comment, error) {
		return &entity.Comment{ID: commentID, PostID: postID, AuthorID: uuid.New()}, nil
	}}
	pr := &mockPostRepo{getByID: func(_ context.Context, _ uuid.UUID) (*entity.Post, error) {
		p := samplePost(authorID)
		p.ID = postID
		p.Visibility = entity.VisibilityPrivate
		return p, nil
	}}
	svc := newCommentLikeService(&mockCommentLikeRepo{}, cr)
	svc.postRepo = pr

	_, err := svc.Toggle(context.Background(), viewerID, commentID)
	if err != post.ErrPostNotFound {
		t.Fatalf("Toggle() error = %v, want ErrPostNotFound", err)
	}
}
