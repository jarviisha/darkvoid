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

	// Handlers
	feedHandler *feedhandler.FeedHandler

	// Cache is exported so app.go can wire WithTrendingInvalidator into post services.
	cache        feedcache.FeedCache
	sessionCache feedcache.FeedSessionCache
}

type FeedPorts struct {
	Cache feedcache.FeedCache
}

// SetupFeedContext initializes the Feed context with all required dependencies.
// It accepts only the minimal reader ports the feed context actually needs.
// redisClient may be nil — in that case a no-op cache is used.
// Returns the FeedContext and a *codohue.Client (nil when Codohue is disabled) so
// the caller (app.go) can wire Codohue into other contexts.
func SetupFeedContext(
	store storage.Storage,
	postReader feed.PostReader,
	followReader feed.FollowReader,
	likeReader feed.LikeReader,
	redisClient *pkgredis.Client,
	cohodueCfg config.CodohueConfig,
) (*FeedContext, *codohue.Client) {
	// Build cache: Redis when available, no-op otherwise.
	var fc feedcache.FeedCache
	var sessionCache feedcache.FeedSessionCache
	if redisClient != nil {
		fc = feedcache.NewRedisFeedCache(redisClient)
		sessionCache = feedcache.NewRedisFeedSessionCache(redisClient)
	} else {
		fc = feedcache.NewNopFeedCache()
		sessionCache = feedcache.NewNopFeedSessionCache()
	}

	feedSvc := feedservice.NewFeedService(postReader, followReader, likeReader, feed.NewLocalRanker(feed.DefaultScorerConfig()), fc, sessionCache)

	// Wire Codohue recommender and trending fetcher into the feed service when enabled.
	// Wiring Codohue into other contexts (post services) is the caller's responsibility.
	var codohueClient *codohue.Client
	if cohodueCfg.Enabled {
		codohueClient = codohue.NewClient(cohodueCfg.BaseURL, cohodueCfg.NamespaceKey, cohodueCfg.Namespace, redisClient)
		feedSvc.WithRecommender(codohueClient)
		feedSvc.WithTrendingFetcher(codohueClient)
	}

	feedHdlr := feedhandler.NewFeedHandler(feedSvc, store)

	return &FeedContext{
		feedService:  feedSvc,
		feedHandler:  feedHdlr,
		cache:        fc,
		sessionCache: sessionCache,
	}, codohueClient
}

func (ctx *FeedContext) Ports() FeedPorts {
	return FeedPorts{Cache: ctx.cache}
}
