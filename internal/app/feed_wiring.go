package app

import (
	"github.com/jarviisha/darkvoid/internal/feature/feed"
	"github.com/jarviisha/darkvoid/pkg/codohue"
	"github.com/jarviisha/darkvoid/pkg/storage"
)

func (app *Application) setupFeedContext(store storage.Storage) *codohue.Client {
	postPorts := app.Post.Ports()
	userPorts := app.User.Ports()
	postReader, followReader, likeReader := buildFeedReaders(
		postPorts.FeedPostRepo,
		postPorts.FeedMediaRepo,
		postPorts.FeedLikeRepo,
		userPorts.FeedUserRepo,
		userPorts.FeedFollowService,
	)

	var codohueClient *codohue.Client
	app.Feed, codohueClient = SetupFeedContext(
		store,
		postReader, followReader, likeReader,
		app.redis, app.cfg.FeedTimeline, app.cfg.Codohue,
	)
	app.log.Info("feed context initialized",
		"redis_cache", app.redis != nil,
		"codohue_enabled", app.cfg.Codohue.Enabled,
	)
	return codohueClient
}

func (app *Application) wireFeedDependencies() {
	feedPorts := app.Feed.Ports()

	app.User.WireFeedInvalidator(feedPorts.Cache)
	app.Post.WireFeedCacheInvalidator(feedPorts.Cache)
	app.Post.WireFeedEventEmitter(feedPorts.Dispatcher)
	app.User.WireFeedEventEmitter(feedPorts.Dispatcher)
	app.log.Info("feed cache wired into follow and post services")
}

func buildFeedReaders(
	postRepo feedPostRepo,
	mediaRepo feedMediaRepo,
	likeRepo feedLikeRepo,
	userRepo feedUserRepo,
	followService feedFollowService,
) (feed.PostReader, feed.FollowGraphReader, feed.LikeReader) {
	ur := &userReader{userRepo: userRepo}

	return &postReader{
		postRepo:   postRepo,
		mediaRepo:  mediaRepo,
		likeRepo:   likeRepo,
		userReader: ur,
	}, &followReader{followService: followService}, &likeReader{likeRepo: likeRepo}
}
