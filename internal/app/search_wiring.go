package app

import (
	"github.com/jarviisha/darkvoid/pkg/storage"
)

func (app *Application) setupSearchContext(store storage.Storage) {
	userPorts := app.User.Ports()
	postPorts := app.Post.Ports()
	users, posts, hashtags := buildSearchAdapters(userPorts.SearchUserRepo, postPorts.SearchPostRepo, postPorts.SearchHashtagRepo, store)
	app.Search = SetupSearchContext(users, posts, hashtags)
	app.log.Info("search context initialized")
}

func buildSearchAdapters(
	userRepo userSearchRepo,
	postRepo postSearchRepo,
	hashtagRepo hashtagSearchRepo,
	store storage.Storage,
) (searchUserSearcher, searchPostSearcher, searchHashtagSearcher) {
	return &searchUserAdapter{repo: userRepo, store: store},
		&searchPostAdapter{repo: postRepo},
		&searchHashtagAdapter{repo: hashtagRepo}
}
