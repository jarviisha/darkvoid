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
}

type FeedPorts struct {
	Cache      feedcache.FeedCache
	Dispatcher *feed.EventDispatcher
}

// SetupFeedContext initializes the Feed context with all required dependencies.
// It accepts only the minimal reader ports the feed context actually needs.
// redisClient may be nil — in that case a no-op cache is used.
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
	feedTimelineCfg config.FeedTimelineConfig,
	cohodueCfg config.CodohueConfig,
) (*FeedContext, *codohue.Client) {
	// Build cache: Redis when available, no-op otherwise.
	var fc feedcache.FeedCache
	if redisClient != nil {
		fc = feedcache.NewRedisFeedCache(redisClient)
	} else {
		fc = feedcache.NewNopFeedCache()
	}

	// One ScorerConfig and one LocalRanker shared by the read path, the
	// background refresher, and the dispatcher's write-time score, so all
	// three stay on the same formula.
	scorerCfg := feed.DefaultScorerConfig()
	ranker := feed.NewLocalRanker(scorerCfg)
	feedSvc := feedservice.NewFeedService(postReader, followReader, likeReader, ranker, fc)
	var timelineStore feed.TimelineStore
	if redisClient != nil {
		timelineStore = feedcache.NewRedisTimelineStore(redisClient, feedTimelineCfg.TimelineMaxItems, feedTimelineCfg.TimelineTTL)
	} else {
		timelineStore = feedcache.NewNopTimelineStore()
	}
	feedSvc.WithTimelineStore(timelineStore)
	feedSvc.WithTimelineOptions(feedTimelineCfg.TimelineEnabled, feedTimelineCfg.TimelineRolloutPercent, feedTimelineCfg.RefreshOnMiss)
	feedSvc.WithTimelineRefresher(feed.NewPreparedTimelineRefresher(postReader, followReader, timelineStore, ranker, feedTimelineCfg.TimelineMaxItems))
	fanoutWorker := feed.NewFanoutWorker(followReader, timelineStore, feedTimelineCfg.FanoutMaxFollowers)
	dispatcher := feed.NewEventDispatcher(feedTimelineCfg.FanoutEnabled, feedTimelineCfg.FanoutWorkers, feedTimelineCfg.FanoutQueueSize, fanoutWorker, scorerCfg)

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
	}, codohueClient
}

func (ctx *FeedContext) Ports() FeedPorts {
	return FeedPorts{Cache: ctx.cache, Dispatcher: ctx.dispatcher}
}
