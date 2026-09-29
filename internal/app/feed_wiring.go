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
) error {
	postPorts := app.Post.Ports()
	userPorts := app.User.Ports()
	posts := &postReader{
		postRepo:   postPorts.FeedPostRepo,
		mediaRepo:  postPorts.FeedMediaRepo,
		likeRepo:   postPorts.FeedLikeRepo,
		userReader: &userReader{userRepo: userPorts.FeedUserRepo},
	}

	// The follow service and the like repository already have the method sets
	// the feed declares, so they go in as they are.
	feedCtx, err := SetupFeedContext(
		store,
		posts, userPorts.FeedFollowService, postPorts.FeedLikeRepo,
		app.redis, cache, outbox, codohueClient,
		app.cfg.FeedFanout,
	)
	if err != nil {
		return err
	}
	app.Feed = feedCtx
	app.log.Info("feed context initialized",
		"redis_cache", app.redis != nil,
		"codohue_enabled", app.cfg.Codohue.Enabled,
		"codohue_events_redis_dedicated", app.codohueEvents != nil,
	)
	return nil
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
