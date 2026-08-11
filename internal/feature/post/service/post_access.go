package service

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	post "github.com/jarviisha/darkvoid/internal/feature/post"
	"github.com/jarviisha/darkvoid/internal/feature/post/entity"
	"github.com/jarviisha/darkvoid/pkg/errors"
)

func getVisiblePost(ctx context.Context, repo postRepo, checker followChecker, postID uuid.UUID, viewerID *uuid.UUID) (*entity.Post, error) {
	p, err := repo.GetByID(ctx, postID)
	if err != nil {
		if errors.Is(err, errors.ErrNotFound) {
			return nil, post.ErrPostNotFound
		}
		return nil, err
	}
	if err := requirePostVisibility(ctx, checker, p, viewerID); err != nil {
		return nil, err
	}
	return p, nil
}

func requirePostVisibility(ctx context.Context, checker followChecker, p *entity.Post, viewerID *uuid.UUID) error {
	if p == nil {
		return post.ErrPostNotFound
	}
	if viewerID != nil && *viewerID == p.AuthorID {
		return nil
	}

	switch p.Visibility {
	case entity.VisibilityPublic:
		return nil
	case entity.VisibilityPrivate:
		return post.ErrPostNotFound
	case entity.VisibilityFollowers:
		if viewerID == nil || checker == nil {
			return post.ErrPostNotFound
		}
		following, err := checker.IsFollowing(ctx, *viewerID, p.AuthorID)
		if err != nil {
			return errors.NewInternalError(fmt.Errorf("check post visibility: %w", err))
		}
		if !following {
			return post.ErrPostNotFound
		}
		return nil
	default:
		return post.ErrPostNotFound
	}
}

func allowedPostVisibilities(ctx context.Context, checker followChecker, authorID uuid.UUID, viewerID *uuid.UUID) ([]string, error) {
	public := string(entity.VisibilityPublic)
	if viewerID == nil {
		return []string{public}, nil
	}
	if *viewerID == authorID {
		return []string{
			public,
			string(entity.VisibilityFollowers),
			string(entity.VisibilityPrivate),
		}, nil
	}
	if checker == nil {
		return []string{public}, nil
	}

	following, err := checker.IsFollowing(ctx, *viewerID, authorID)
	if err != nil {
		return nil, errors.NewInternalError(fmt.Errorf("check user post visibility: %w", err))
	}
	if following {
		return []string{public, string(entity.VisibilityFollowers)}, nil
	}
	return []string{public}, nil
}

func requestedPostVisibilities(requested string, allowed []string) ([]string, error) {
	if requested == "" {
		return allowed, nil
	}
	if !isValidVisibility(entity.Visibility(requested)) {
		return nil, post.ErrInvalidVisibility
	}
	if !slices.Contains(allowed, requested) {
		return nil, nil
	}
	return []string{requested}, nil
}
