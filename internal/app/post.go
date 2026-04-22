package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	postcache "github.com/jarviisha/darkvoid/internal/feature/post/cache"
	"github.com/jarviisha/darkvoid/internal/feature/post/handler"
	"github.com/jarviisha/darkvoid/internal/feature/post/repository"
	"github.com/jarviisha/darkvoid/internal/feature/post/service"
	"github.com/jarviisha/darkvoid/pkg/codohue"
	pkgredis "github.com/jarviisha/darkvoid/pkg/redis"
	"github.com/jarviisha/darkvoid/pkg/storage"
	"github.com/jarviisha/darkvoid/pkg/tfidf"
)

// PostContext represents the Post bounded context with all its dependencies
type PostContext struct {
	// Repositories
	postRepo           *repository.PostRepository
	mediaRepo          *repository.MediaRepository
	likeRepo           *repository.LikeRepository
	commentRepo        *repository.CommentRepository
	commentMediaRepo   *repository.CommentMediaRepository
	hashtagRepo        *repository.HashtagRepository
	searchRepo         *repository.PostSearchRepository
	mentionRepo        *repository.MentionRepository
	commentMentionRepo *repository.CommentMentionRepository

	// Services
	postService        *service.PostService
	likeService        *service.LikeService
	commentService     *service.CommentService
	commentLikeService *service.CommentLikeService
	hashtagService     *service.HashtagService

	// Handlers
	postHandler        *handler.PostHandler
	likeHandler        *handler.LikeHandler
	commentHandler     *handler.CommentHandler
	commentLikeHandler *handler.CommentLikeHandler
	hashtagHandler     *handler.HashtagHandler
}

type PostPorts struct {
	FeedPostRepo      feedPostRepo
	FeedMediaRepo     feedMediaRepo
	FeedLikeRepo      feedLikeRepo
	SearchPostRepo    postSearchRepo
	SearchHashtagRepo hashtagSearchRepo
}

type postUserRepo interface {
	GetUsersByIDsAny(ctx context.Context, ids []uuid.UUID) ([]*postUser, error)
}

func (ctx *PostContext) Ports() PostPorts {
	return PostPorts{
		FeedPostRepo:      ctx.postRepo,
		FeedMediaRepo:     ctx.mediaRepo,
		FeedLikeRepo:      ctx.likeRepo,
		SearchPostRepo:    ctx.searchRepo,
		SearchHashtagRepo: ctx.hashtagRepo,
	}
}

type postUser struct {
	ID          uuid.UUID
	Username    string
	DisplayName string
	AvatarKey   *string
}

type postFollowService interface {
	IsFollowing(ctx context.Context, followerID, followeeID uuid.UUID) (bool, error)
}

// SetupPostContext initializes the Post context with all required dependencies.
func SetupPostContext(pool *pgxpool.Pool, store storage.Storage, userRepo postUserRepo, redis *pkgredis.Client) *PostContext {
	// Repositories
	postRepo := repository.NewPostRepository(pool)
	mediaRepo := repository.NewMediaRepository(pool)
	likeRepo := repository.NewLikeRepository(pool)
	commentRepo := repository.NewCommentRepository(pool)
	commentMediaRepo := repository.NewCommentMediaRepository(pool)
	commentLikeRepo := repository.NewCommentLikeRepository(pool)
	hashtagRepo := repository.NewHashtagRepository(pool)
	searchRepo := repository.NewPostSearchRepository(pool)
	mentionRepo := repository.NewMentionRepository(pool)
	commentMentionRepo := repository.NewCommentMentionRepository(pool)

	ur := &postUserReader{userRepo: userRepo}

	// Hashtag cache — use Redis when available, nop otherwise
	var hCache postcache.HashtagCache
	if redis != nil {
		hCache = postcache.NewRedisHashtagCache(redis)
	} else {
		hCache = postcache.NewNopHashtagCache()
	}

	// Services
	postService := service.NewPostService(pool, postRepo, mediaRepo, ur, hashtagRepo,
		service.WithLikeRepo(likeRepo),
		service.WithMentionRepo(mentionRepo),
	)
	likeService := service.NewLikeService(likeRepo, postRepo)
	commentService := service.NewCommentService(pool, commentRepo, commentMediaRepo, postRepo, ur,
		service.WithCommentLikeRepo(commentLikeRepo),
		service.WithCommentMentionRepo(commentMentionRepo),
	)
	commentLikeService := service.NewCommentLikeService(commentLikeRepo, commentRepo)
	hashtagService := service.NewHashtagService(hashtagRepo, hCache, postRepo, ur)

	// Handlers
	postHandler := handler.NewPostHandler(postService, store)
	likeHandler := handler.NewLikeHandler(likeService)
	commentHandler := handler.NewCommentHandler(commentService, store)
	commentLikeHandler := handler.NewCommentLikeHandler(commentLikeService)
	hashtagHandler := handler.NewHashtagHandler(hashtagService, store)

	return &PostContext{
		postRepo:           postRepo,
		mediaRepo:          mediaRepo,
		likeRepo:           likeRepo,
		commentRepo:        commentRepo,
		commentMediaRepo:   commentMediaRepo,
		hashtagRepo:        hashtagRepo,
		searchRepo:         searchRepo,
		mentionRepo:        mentionRepo,
		commentMentionRepo: commentMentionRepo,
		postService:        postService,
		likeService:        likeService,
		commentService:     commentService,
		commentLikeService: commentLikeService,
		hashtagService:     hashtagService,
		postHandler:        postHandler,
		likeHandler:        likeHandler,
		commentHandler:     commentHandler,
		commentLikeHandler: commentLikeHandler,
		hashtagHandler:     hashtagHandler,
	}
}

func (ctx *PostContext) WireFollowChecker(followService postFollowService) {
	ctx.postService.WithFollowChecker(&postFollowChecker{followService: followService})
}

func (ctx *PostContext) WireFeedCacheInvalidator(inv service.TrendingInvalidator) {
	ctx.likeService.WithTrendingInvalidator(inv)
	ctx.commentService.WithTrendingInvalidator(inv)
}

func (ctx *PostContext) WireNotificationEmitter(notif *NotificationContext) {
	ctx.postService.WithNotificationEmitter(notif.notifService)
	ctx.likeService.WithNotificationEmitter(notif.notifService)
	ctx.commentService.WithNotificationEmitter(notif.notifService)
	ctx.commentLikeService.WithNotificationEmitter(notif.notifService)
}

func (ctx *PostContext) WireCodohue(client *codohue.Client, embeddingDim int) {
	ctx.likeService.WithBehaviorEventPublisher(client)
	ctx.commentService.WithBehaviorEventPublisher(client)
	ctx.postService.WithObjectDeleter(client)
	ctx.postService.WithEmbedding(tfidf.New(embeddingDim), client)
}
