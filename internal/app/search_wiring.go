package app

import (
	"github.com/jarviisha/darkvoid/pkg/storage"
)

func (app *Application) setupSearchContext(store storage.Storage) {
	userPorts := app.User.Ports()
	postPorts := app.Post.Ports()
	app.Search = SetupSearchContext(
		&searchUserAdapter{repo: userPorts.SearchUserRepo, store: store},
		&searchPostAdapter{repo: postPorts.SearchPostRepo},
		postPorts.SearchHashtagRepo,
	)
	app.log.Info("search context initialized")
}
