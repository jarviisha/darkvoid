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

	// Handlers
	feedHandler *feedhandler.FeedHandler

	// Cache is exported so app.go can wire WithTrendingInvalidator into post services.
	cache feedcache.FeedCache

	// settings is the one holder every component above reads its tunable knobs
	// from. The settings context writes to it; nothing here ever does.
	settings *feed.Settings
}

type FeedPorts struct {
	Cache      feedcache.FeedCache
	Dispatcher *feed.EventDispatcher
	Settings   *feed.Settings
}

// SetupFeedContext initializes the Feed context with all required dependencies.
// It accepts only the minimal reader ports the feed context actually needs.
// redisClient is required and must be non-nil; the feed cache and the
// materialized timeline both live in it.
//
// eventsRedisClient is the Redis carrying the codohue:events stream, which is not
// always redisClient: Codohue's consumer reads that stream from whichever instance
// Codohue owns. Keeping it a separate parameter is what lets the cache, the
// timeline store, and the notification pub/sub stay on our own Redis while only
// the event producer reaches into Codohue's. May be nil, which disables event
// publishing but leaves the rest of the integration working.
//
// Returns the FeedContext and a *codohue.Client (nil when Codohue is disabled) so
// the caller (app.go) can wire Codohue into other contexts.
func SetupFeedContext(
	store storage.Storage,
	postReader feed.PostReader,
	followReader feed.FollowGraphReader,
	likeReader feed.LikeReader,
	redisClient *pkgredis.Client,
	eventsRedisClient *pkgredis.Client,
	feedFanoutCfg config.FeedFanoutConfig,
	cohodueCfg config.CodohueConfig,
) (*FeedContext, *codohue.Client) {
	fc := feedcache.NewRedisFeedCache(redisClient)

	// One settings holder shared by the read path, the ranker, the timeline
	// store, the background refresher and the dispatcher's write-time score, so
	// all five stay on the same numbers and an operator's edit reaches them
	// together. It is seeded with the defaults; the settings context replaces
	// them with the stored row during wiring, before the server starts serving.
	settings := feed.NewSettings(feed.DefaultRuntimeSettings())
	ranker := feed.NewLocalRanker(settings)
	feedSvc := feedservice.NewFeedService(postReader, followReader, likeReader, ranker, fc)
	timelineStore := feedcache.NewRedisTimelineStore(redisClient, settings)
	feedSvc.WithTimelineStore(timelineStore)
	feedSvc.WithSettings(settings)
	// One refresher serves both consumers: the read path's refresh-on-miss and
	// the fanout worker's follow-change rebuild.
	refresher := feed.NewPreparedTimelineRefresher(postReader, followReader, timelineStore, ranker, settings)
	feedSvc.WithTimelineRefresher(refresher)
	fanoutWorker := feed.NewFanoutWorker(followReader, timelineStore, refresher, settings)
	// Workers and queue size stay environment-fed: they allocate a goroutine pool
	// and a channel here, so a stored value could not take effect without
	// rebuilding the dispatcher. See migrations/settings/000002.
	dispatcher := feed.NewEventDispatcher(settings, feedFanoutCfg.Workers, feedFanoutCfg.QueueSize, fanoutWorker)

	// Wire Codohue recommender and trending fetcher into the feed service when enabled.
	// Wiring Codohue into other contexts (post services) is the caller's responsibility.
	var codohueClient *codohue.Client
	if cohodueCfg.Enabled {
		codohueClient = codohue.NewClient(cohodueCfg.BaseURL, cohodueCfg.NamespaceKey, cohodueCfg.Namespace, eventsRedisClient)
		feedSvc.WithRecommender(codohueClient)
		feedSvc.WithTrendingFetcher(codohueClient)
	}

	feedHdlr := feedhandler.NewFeedHandler(feedSvc, store)

	return &FeedContext{
		feedService: feedSvc,
		dispatcher:  dispatcher,
		feedHandler: feedHdlr,
		cache:       fc,
		settings:    settings,
	}, codohueClient
}

func (ctx *FeedContext) Ports() FeedPorts {
	return FeedPorts{Cache: ctx.cache, Dispatcher: ctx.dispatcher, Settings: ctx.settings}
}
