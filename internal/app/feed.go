package app

import (
	"github.com/jarviisha/darkvoid/internal/feature/feed"
	feedcache "github.com/jarviisha/darkvoid/internal/feature/feed/cache"
	feedhandler "github.com/jarviisha/darkvoid/internal/feature/feed/handler"
	feedservice "github.com/jarviisha/darkvoid/internal/feature/feed/service"
	"github.com/jarviisha/darkvoid/pkg/codohue"
	"github.com/jarviisha/darkvoid/pkg/config"
	pkgredis "github.com/jarviisha/darkvoid/pkg/redis"
	"github.com/jarviisha/darkvoid/pkg/storage"
)

// FeedContext represents the Feed bounded context with all its dependencies.
type FeedContext struct {
	// Services
	feedService *feedservice.FeedService
	dispatcher  *feed.EventDispatcher
	outbox      *feed.PostgresOutbox

	// Handlers
	feedHandler *feedhandler.FeedHandler

	// cache is handed out through Ports; the post services take it as their
	// TrendingInvalidator.
	cache feedcache.FeedCache

	// settings is the one holder every component above reads its tunable knobs
	// from. The settings context writes to it; nothing here ever does.
	settings *feed.Settings
}

type FeedPorts struct {
	Cache      feedcache.FeedCache
	Dispatcher *feed.EventDispatcher
	Settings   *feed.Settings
	Outbox     *feed.PostgresOutbox
}

// SetupFeedContext initializes the Feed context with all required dependencies.
// It accepts only the minimal reader ports the feed context actually needs.
// redisClient is required and must be non-nil; the materialized timeline lives
// in it.
//
// cache and outbox are built by the caller: they need nothing from this context
// (Redis and the pool respectively), and the follow and post services take them
// as dependencies while being constructed first.
//
// codohueClient is built by the caller, not here: the post services need the
// same client and are constructed first.
func SetupFeedContext(
	store storage.Storage,
	postReader feed.PostReader,
	followReader feed.FollowGraphReader,
	likeReader feed.LikeReader,
	redisClient *pkgredis.Client,
	cache feedcache.FeedCache,
	outbox *feed.PostgresOutbox,
	codohueClient *codohue.Client,
	feedFanoutCfg config.FeedFanoutConfig,
) (*FeedContext, error) {
	// One settings holder shared by the read path, the ranker, the timeline
	// store, the background refresher and the dispatcher's write-time score, so
	// all five stay on the same numbers and an operator's edit reaches them
	// together. It is seeded with the defaults; the settings context replaces
	// them with the stored row during wiring, before the server starts serving.
	settings := feed.NewSettings(feed.DefaultRuntimeSettings())
	ranker := feed.NewLocalRanker(settings)
	timelineStore := feedcache.NewRedisTimelineStore(redisClient, settings)
	// One refresher serves both consumers: the read path's refresh-on-miss and
	// the fanout worker's follow-change rebuild.
	refresher := feed.NewPreparedTimelineRefresher(postReader, followReader, timelineStore, ranker, settings)

	// Nil means the integration is off, and the recommender and trending
	// fetcher are simply not wired.
	var opts []feedservice.FeedServiceOption
	if codohueClient != nil {
		opts = append(opts, feedservice.WithRecommender(codohueClient), feedservice.WithTrendingFetcher(codohueClient))
	}
	feedSvc, err := feedservice.NewFeedService(feedservice.FeedDeps{
		Posts:     postReader,
		Follows:   followReader,
		Likes:     likeReader,
		Ranker:    ranker,
		Cache:     cache,
		Timeline:  timelineStore,
		Refresher: refresher,
		Settings:  settings,
	}, opts...)
	if err != nil {
		return nil, err
	}

	fanoutWorker := feed.NewFanoutWorker(followReader, timelineStore, refresher, settings)
	// Workers and queue size stay environment-fed: they allocate a goroutine pool
	// and a channel here, so a stored value could not take effect without
	// rebuilding the dispatcher. See migrations/settings/000001_init.up.sql.
	dispatcher := feed.NewEventDispatcher(settings, feedFanoutCfg.Workers, feedFanoutCfg.QueueSize, fanoutWorker)
	dispatcher.WithOutbox(outbox)

	feedHdlr := feedhandler.NewFeedHandler(feedSvc, store)

	return &FeedContext{
		feedService: feedSvc,
		dispatcher:  dispatcher,
		feedHandler: feedHdlr,
		cache:       cache,
		settings:    settings,
		outbox:      outbox,
	}, nil
}

func (ctx *FeedContext) Ports() FeedPorts {
	return FeedPorts{Cache: ctx.cache, Dispatcher: ctx.dispatcher, Settings: ctx.settings, Outbox: ctx.outbox}
}
