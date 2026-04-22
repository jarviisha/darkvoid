package app

import (
	"context"

	postentity "github.com/jarviisha/darkvoid/internal/feature/post/entity"
	searchdto "github.com/jarviisha/darkvoid/internal/feature/search/dto"
	userentity "github.com/jarviisha/darkvoid/internal/feature/user/entity"
	"github.com/jarviisha/darkvoid/pkg/storage"
)

// searchUserAdapter implements searchsvc.userSearcher using the user repository.
type searchUserAdapter struct {
	repo  userSearchRepo
	store storage.Storage
}

func (a *searchUserAdapter) SearchByQuery(ctx context.Context, query string, limit, offset int32) ([]searchdto.UserResult, error) {
	users, err := a.repo.SearchByQuery(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	return usersToSearchResults(users, a.store), nil
}

// searchPostAdapter implements searchsvc.postSearcher using the post search repository.
type searchPostAdapter struct {
	repo postSearchRepo
}

func (a *searchPostAdapter) SearchByQuery(ctx context.Context, query string, limit, offset int32) ([]searchdto.PostResult, error) {
	posts, err := a.repo.SearchByQuery(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	return postsToSearchResults(posts), nil
}

// searchHashtagAdapter implements searchsvc.hashtagSearcher using the hashtag repository.
type searchHashtagAdapter struct {
	repo hashtagSearchRepo
}

func (a *searchHashtagAdapter) SearchByPrefix(ctx context.Context, prefix string, limit int32) ([]string, error) {
	return a.repo.SearchByPrefix(ctx, prefix, limit)
}

func usersToSearchResults(users []*userentity.User, store storage.Storage) []searchdto.UserResult {
	results := make([]searchdto.UserResult, 0, len(users))
	for _, u := range users {
		r := searchdto.UserResult{
			ID:            u.ID.String(),
			Username:      u.Username,
			DisplayName:   u.DisplayName,
			FollowerCount: u.FollowerCount,
		}
		if u.AvatarKey != nil {
			url := store.URL(*u.AvatarKey)
			r.AvatarURL = &url
		}
		results = append(results, r)
	}
	return results
}

func postsToSearchResults(posts []*postentity.Post) []searchdto.PostResult {
	results := make([]searchdto.PostResult, 0, len(posts))
	for _, p := range posts {
		results = append(results, searchdto.PostResult{
			ID:           p.ID.String(),
			AuthorID:     p.AuthorID.String(),
			Content:      p.Content,
			LikeCount:    p.LikeCount,
			CommentCount: p.CommentCount,
			CreatedAt:    p.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	return results
}
