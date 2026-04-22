package app

import (
	"context"

	postentity "github.com/jarviisha/darkvoid/internal/feature/post/entity"
	searchdto "github.com/jarviisha/darkvoid/internal/feature/search/dto"
	searchhandler "github.com/jarviisha/darkvoid/internal/feature/search/handler"
	searchsvc "github.com/jarviisha/darkvoid/internal/feature/search/service"
	userentity "github.com/jarviisha/darkvoid/internal/feature/user/entity"
)

// SearchContext holds all search-related dependencies.
type SearchContext struct {
	handler *searchhandler.SearchHandler
}

type searchUserSearcher interface {
	SearchByQuery(ctx context.Context, query string, limit, offset int32) ([]searchdto.UserResult, error)
}

type searchPostSearcher interface {
	SearchByQuery(ctx context.Context, query string, limit, offset int32) ([]searchdto.PostResult, error)
}

type searchHashtagSearcher interface {
	SearchByPrefix(ctx context.Context, prefix string, limit int32) ([]string, error)
}

// SetupSearchContext wires the unified search bounded context.
func SetupSearchContext(
	users searchUserSearcher,
	posts searchPostSearcher,
	hashtags searchHashtagSearcher,
) *SearchContext {
	svc := searchsvc.NewSearchService(users, posts, hashtags)
	return &SearchContext{handler: searchhandler.NewSearchHandler(svc)}
}

type userSearchRepo interface {
	SearchByQuery(ctx context.Context, query string, limit, offset int32) ([]*userentity.User, error)
}

type postSearchRepo interface {
	SearchByQuery(ctx context.Context, query string, limit, offset int32) ([]*postentity.Post, error)
}

type hashtagSearchRepo interface {
	SearchByPrefix(ctx context.Context, prefix string, limit int32) ([]string, error)
}
