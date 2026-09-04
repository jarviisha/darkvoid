package app

import (
	"github.com/jarviisha/darkvoid/internal/feature/feed"
	feedcache "github.com/jarviisha/darkvoid/internal/feature/feed/cache"
	"github.com/jarviisha/darkvoid/pkg/codohue"
	"github.com/jarviisha/darkvoid/pkg/storage"
)

func (app *Application) setupFeedContext(
	store storage.Storage,
	cache feedcache.FeedCache,
	outbox *feed.PostgresOutbox,
	codohueClient *codohue.Client,
) {
	postPorts := app.Post.Ports()
	userPorts := app.User.Ports()
	postReader, followReader, likeReader := buildFeedReaders(
		postPorts.FeedPostRepo,
		postPorts.FeedMediaRepo,
		postPorts.FeedLikeRepo,
		userPorts.FeedUserRepo,
		userPorts.FeedFollowService,
	)

	app.Feed = SetupFeedContext(
		store,
		postReader, followReader, likeReader,
		app.redis, cache, outbox, codohueClient,
		app.cfg.FeedFanout,
	)
	app.log.Info("feed context initialized",
		"redis_cache", app.redis != nil,
		"codohue_enabled", app.cfg.Codohue.Enabled,
		"codohue_events_redis_dedicated", app.codohueEvents != nil,
	)
}

func (app *Application) wireFeedDependencies() {
	feedPorts := app.Feed.Ports()

	app.User.WireFeedInvalidator(feedPorts.Cache)
	app.Post.WireFeedCacheInvalidator(feedPorts.Cache)
	app.Post.WireFeedEventEmitter(feedPorts.Dispatcher)
	app.Post.WireFeedEventOutbox(&feedEventOutbox{outbox: feedPorts.Outbox})
	app.User.WireFeedEventEmitter(feedPorts.Dispatcher)
	app.User.WireFeedEventOutbox(&feedEventOutbox{outbox: feedPorts.Outbox})
	app.log.Info("feed cache wired into follow and post services")
}

// setupFeedInfra builds the two feed components that need nothing from the feed
// context itself: the cache needs only Redis, the outbox only the pool.
//
// They are built ahead of every bounded context because the follow service and
// the four post services depend on them, and all five are constructed before the
// feed context is. Leaving them inside SetupFeedContext is what forced those
// dependencies to arrive by post-construction mutation.
func (app *Application) setupFeedInfra() (feedcache.FeedCache, *feed.PostgresOutbox) {
	return feedcache.NewRedisFeedCache(app.redis), feed.NewPostgresOutbox(app.pool)
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
