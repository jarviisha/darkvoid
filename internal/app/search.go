package app

import (
	"context"

	postentity "github.com/jarviisha/darkvoid/internal/feature/post/entity"
	searchhandler "github.com/jarviisha/darkvoid/internal/feature/search/handler"
	searchsvc "github.com/jarviisha/darkvoid/internal/feature/search/service"
	userentity "github.com/jarviisha/darkvoid/internal/feature/user/entity"
)

// SearchContext holds all search-related dependencies.
type SearchContext struct {
	handler *searchhandler.SearchHandler
}

// SetupSearchContext wires the unified search bounded context. The parameters are
// the concrete adapters rather than interfaces: searchsvc declares the ports it
// consumes, and these satisfy them structurally, so restating them here would be
// the composition root declaring an interface against itself.
func SetupSearchContext(
	users *searchUserAdapter,
	posts *searchPostAdapter,
	hashtags hashtagSearchRepo,
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
