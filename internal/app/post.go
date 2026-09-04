package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	postcache "github.com/jarviisha/darkvoid/internal/feature/post/cache"
	"github.com/jarviisha/darkvoid/internal/feature/post/handler"
	"github.com/jarviisha/darkvoid/internal/feature/post/repository"
	"github.com/jarviisha/darkvoid/internal/feature/post/service"
	"github.com/jarviisha/darkvoid/pkg/codohue"
	"github.com/jarviisha/darkvoid/pkg/deps"
	pkgredis "github.com/jarviisha/darkvoid/pkg/redis"
	"github.com/jarviisha/darkvoid/pkg/storage"
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
	GetFollowingAmong(ctx context.Context, followerID uuid.UUID, followeeIDs []uuid.UUID) ([]uuid.UUID, error)
}

// PostContextDeps carries everything the Post context needs.
//
// A struct rather than positional parameters: several of these are one-method
// interfaces over the same repositories, and two adjacent same-typed parameters
// are exactly the transposition a compiler cannot catch.
type PostContextDeps struct {
	Pool          *pgxpool.Pool
	Storage       storage.Storage
	UserRepo      postUserRepo
	Redis         *pkgredis.Client
	FollowService postFollowService
	Notifications *NotificationContext
	Trending      service.TrendingInvalidator
	FeedOutbox    service.FeedEventOutbox

	// Codohue is nil whenever CODOHUE_ENABLED is unset, and the services below
	// are simply built without it.
	Codohue *codohue.Client
}

// SetupPostContext initializes the Post context with all required dependencies.
func SetupPostContext(d PostContextDeps) (*PostContext, error) {
	if err := deps.Missing(map[string]any{
		"Pool":          d.Pool,
		"Storage":       d.Storage,
		"UserRepo":      d.UserRepo,
		"Redis":         d.Redis,
		"FollowService": d.FollowService,
		"Notifications": d.Notifications,
		"Trending":      d.Trending,
		"FeedOutbox":    d.FeedOutbox,
	}); err != nil {
		return nil, fmt.Errorf("post context: %w", err)
	}

	postRepo := repository.NewPostRepository(d.Pool)
	mediaRepo := repository.NewMediaRepository(d.Pool)
	likeRepo := repository.NewLikeRepository(d.Pool)
	commentRepo := repository.NewCommentRepository(d.Pool)
	commentMediaRepo := repository.NewCommentMediaRepository(d.Pool)
	commentLikeRepo := repository.NewCommentLikeRepository(d.Pool)
	hashtagRepo := repository.NewHashtagRepository(d.Pool)
	searchRepo := repository.NewPostSearchRepository(d.Pool)
	mentionRepo := repository.NewMentionRepository(d.Pool)
	commentMentionRepo := repository.NewCommentMentionRepository(d.Pool)

	ur := &postUserReader{userRepo: d.UserRepo}
	hCache := postcache.NewRedisHashtagCache(d.Redis)
	checker := &postFollowChecker{followService: d.FollowService}
	notif := d.Notifications.notifService

	var postOpts []service.PostServiceOption
	var likeOpts []service.LikeServiceOption
	var commentOpts []service.CommentServiceOption
	if d.Codohue != nil {
		postOpts = append(postOpts,
			service.WithObjectDeleter(d.Codohue),
			service.WithCatalogIngester(d.Codohue),
		)
		likeOpts = append(likeOpts, service.WithLikeBehaviorEventPublisher(d.Codohue))
		commentOpts = append(commentOpts, service.WithCommentBehaviorEventPublisher(d.Codohue))
	}

	postService, err := service.NewPostService(service.PostDeps{
		Pool:                d.Pool,
		Posts:               postRepo,
		Media:               mediaRepo,
		Users:               ur,
		Hashtags:            hashtagRepo,
		Likes:               likeRepo,
		Mentions:            mentionRepo,
		FollowChecker:       checker,
		Notifications:       notif,
		TrendingInvalidator: d.Trending,
		FeedOutbox:          d.FeedOutbox,
	}, postOpts...)
	if err != nil {
		return nil, fmt.Errorf("post service: %w", err)
	}

	likeService, err := service.NewLikeService(service.LikeDeps{
		Likes:         likeRepo,
		Posts:         postRepo,
		FollowChecker: checker,
		Notifications: notif,
	}, likeOpts...)
	if err != nil {
		return nil, fmt.Errorf("like service: %w", err)
	}

	commentService, err := service.NewCommentService(service.CommentDeps{
		Pool:            d.Pool,
		Comments:        commentRepo,
		CommentMedia:    commentMediaRepo,
		Posts:           postRepo,
		Users:           ur,
		CommentLikes:    commentLikeRepo,
		CommentMentions: commentMentionRepo,
		FollowChecker:   checker,
		Notifications:   notif,
	}, commentOpts...)
	if err != nil {
		return nil, fmt.Errorf("comment service: %w", err)
	}

	commentLikeService, err := service.NewCommentLikeService(service.CommentLikeDeps{
		CommentLikes:  commentLikeRepo,
		Comments:      commentRepo,
		Posts:         postRepo,
		FollowChecker: checker,
		Notifications: notif,
	})
	if err != nil {
		return nil, fmt.Errorf("comment like service: %w", err)
	}

	hashtagService := service.NewHashtagService(hashtagRepo, hCache, postRepo, ur)

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
		postHandler:        handler.NewPostHandler(postService, d.Storage),
		likeHandler:        handler.NewLikeHandler(likeService),
		commentHandler:     handler.NewCommentHandler(commentService, d.Storage),
		commentLikeHandler: handler.NewCommentLikeHandler(commentLikeService),
		hashtagHandler:     handler.NewHashtagHandler(hashtagService, d.Storage),
	}, nil
}

// WireFeedEventEmitter attaches the feed event dispatcher to the post service.
//
// It is the only dependency this context still receives after construction: the
// dispatcher's fanout worker reads posts, so the feed context cannot be built
// until this one exists.
func (ctx *PostContext) WireFeedEventEmitter(e service.FeedEventEmitter) error {
	return ctx.postService.WireFeedEventEmitter(e)
}
