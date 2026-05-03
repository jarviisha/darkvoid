package service

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	post "github.com/jarviisha/darkvoid/internal/feature/post"
	"github.com/jarviisha/darkvoid/internal/feature/post/entity"
	"github.com/jarviisha/darkvoid/internal/feature/post/repository"
	"github.com/jarviisha/darkvoid/pkg/errors"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

// PostServiceOption is a functional option for configuring optional PostService dependencies.
type PostServiceOption func(*PostService)

// WithLikeRepo attaches a like repository for like-count and is-liked enrichment.
func WithLikeRepo(r likeRepo) PostServiceOption {
	return func(s *PostService) { s.likeRepo = r }
}

// WithMentionRepo attaches the mention repository.
func WithMentionRepo(r *repository.MentionRepository) PostServiceOption {
	return func(s *PostService) { s.mentionRepo = &mentionRepoTxable{r} }
}

// WithFollowChecker attaches a follow checker for is_following_author enrichment.
// Called by the app layer once the user context is ready.
func (s *PostService) WithFollowChecker(fc followChecker) {
	s.followChecker = fc
}

// WithNotificationEmitter wires a cross-context notification emitter after construction.
// Called by the app layer once the notification context is ready.
func (s *PostService) WithNotificationEmitter(e notificationEmitter) {
	s.notifEmitter = e
}

// WithFeedEventEmitter wires feed-impacting post events after construction.
func (s *PostService) WithFeedEventEmitter(e FeedEventEmitter) {
	s.feedEmitter = e
}

// WithObjectDeleter attaches a Codohue object deleter. Called at wire-up time.
// When set, DeletePost will remove the post from the recommendation index after successful deletion.
func (s *PostService) WithObjectDeleter(d ObjectDeleter) {
	s.objectDeleter = d
}

// WithEmbedding attaches an embedding provider and object embedder. Called at wire-up time.
// When both are set, CreatePost and UpdatePost push BYOE vectors to Codohue asynchronously.
// Both must be non-nil to activate embedding; providing only one has no effect.
func (s *PostService) WithEmbedding(provider EmbeddingProvider, embedder ObjectEmbedder) {
	s.embeddingProvider = provider
	s.objectEmbedder = embedder
}

// PostService handles post business logic
type PostService struct {
	pool          txBeginner
	postRepo      postRepo
	mediaRepo     mediaRepo
	likeRepo      likeRepo      // optional: nil → like count/isLiked skipped
	followChecker followChecker // optional: nil → is_following_author skipped
	userReader    userReader
	hashtagRepo   hashtagRepo
	mentionRepo   mentionRepo // optional: nil → mentions skipped

	notifEmitter      notificationEmitter // optional: nil → notifications skipped
	feedEmitter       FeedEventEmitter    // optional: nil → feed propagation skipped
	objectDeleter     ObjectDeleter       // optional: nil → no recommendation index cleanup on delete
	embeddingProvider EmbeddingProvider   // optional: nil → no BYOE embeddings
	objectEmbedder    ObjectEmbedder      // optional: nil → no BYOE embeddings
}

// NewPostService creates a new PostService. Required dependencies are passed as positional
// arguments; optional ones are injected via PostServiceOption functions.
func NewPostService(
	pool *pgxpool.Pool,
	postRepo *repository.PostRepository,
	mediaRepo *repository.MediaRepository,
	userReader userReader,
	hashtagRepo *repository.HashtagRepository,
	opts ...PostServiceOption,
) *PostService {
	s := &PostService{
		pool:        pool,
		postRepo:    &postRepoTxable{postRepo},
		mediaRepo:   &mediaRepoTxable{mediaRepo},
		userReader:  userReader,
		hashtagRepo: &hashtagRepoTxable{hashtagRepo},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// CreatePost creates a new post
func (s *PostService) CreatePost(ctx context.Context, authorID uuid.UUID, content string, visibility entity.Visibility, mediaKeys []string, mentionUserIDs []uuid.UUID, tags []string) (*entity.Post, error) {
	if strings.TrimSpace(content) == "" && len(mediaKeys) == 0 {
		return nil, post.ErrEmptyContent
	}
	if !isValidVisibility(visibility) {
		return nil, post.ErrInvalidVisibility
	}

	validTags, err := validateTags(tags)
	if err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	txPost := s.postRepo.WithTx(tx)
	txMedia := s.mediaRepo.WithTx(tx)

	p, err := txPost.Create(ctx, authorID, strings.TrimSpace(content), visibility)
	if err != nil {
		logger.LogError(ctx, err, "failed to create post", "author_id", authorID)
		return nil, errors.NewInternalError(err)
	}

	for i, key := range mediaKeys {
		media, err := txMedia.Add(ctx, p.ID, key, inferMediaType(key), int32(i))
		if err != nil {
			logger.LogError(ctx, err, "failed to attach media", "post_id", p.ID)
			return nil, errors.NewInternalError(err)
		}
		p.Media = append(p.Media, media)
	}

	if len(validTags) > 0 && s.hashtagRepo != nil {
		if err := s.hashtagRepo.WithTx(tx).UpsertAndLink(ctx, p.ID, validTags); err != nil {
			logger.LogError(ctx, err, "failed to persist hashtags", "post_id", p.ID)
			return nil, errors.NewInternalError(err)
		}
		p.Tags = validTags
	}

	// Persist mentions within transaction
	var persistedMentionIDs []uuid.UUID
	if s.mentionRepo != nil && len(mentionUserIDs) > 0 {
		ids, err := s.persistMentions(ctx, s.mentionRepo.WithTx(tx), p.ID, mentionUserIDs)
		if err != nil {
			logger.LogError(ctx, err, "failed to persist mentions", "post_id", p.ID)
			return nil, errors.NewInternalError(err)
		}
		persistedMentionIDs = ids
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, errors.NewInternalError(err)
	}

	// Enrich mentions and fire notifications AFTER commit (non-fatal)
	p.Mentions = s.enrichMentionsAfterCommit(ctx, p.ID, authorID, persistedMentionIDs)

	s.pushEmbeddingAsync(p.ID.String(), p.Content, p.Tags)
	s.emitPostCreatedFeedEvent(ctx, p)

	logger.Info(ctx, "post created", "post_id", p.ID, "author_id", authorID)
	return p, nil
}

func (s *PostService) emitPostCreatedFeedEvent(ctx context.Context, p *entity.Post) {
	if s.feedEmitter == nil || p == nil {
		return
	}
	if err := s.feedEmitter.EmitPostCreated(ctx, p.ID, p.AuthorID, string(p.Visibility), p.CreatedAt); err != nil {
		logger.LogError(ctx, err, "failed to emit post-created feed event", "post_id", p.ID)
	}
}

// GetPost retrieves a single post by ID, enriched with like count and optional isLiked flag
func (s *PostService) GetPost(ctx context.Context, postID uuid.UUID, viewerID *uuid.UUID) (*entity.Post, error) {
	p, err := s.postRepo.GetByID(ctx, postID)
	if err != nil {
		if errors.Is(err, errors.ErrNotFound) {
			return nil, post.ErrPostNotFound
		}
		return nil, err
	}

	s.enrichBatch(ctx, []*entity.Post{p}, viewerID)
	s.enrichAuthors(ctx, []*entity.Post{p})
	s.enrichTags(ctx, []*entity.Post{p})
	s.enrichMentions(ctx, []*entity.Post{p})
	s.enrichIsFollowingAuthor(ctx, []*entity.Post{p}, viewerID)
	return p, nil
}

// GetUserPosts returns cursor-paginated posts for a user, optionally filtered by visibility.
// cursor nil means start from the latest post. visibility "" means no filter.
func (s *PostService) GetUserPosts(ctx context.Context, authorID uuid.UUID, viewerID *uuid.UUID, cursor *post.UserPostCursor, visibility string, limit int32) ([]*entity.Post, *post.UserPostCursor, error) {
	if limit <= 0 {
		limit = 20
	}

	cursorTS := pgtype.Timestamptz{Time: post.MaxUserPostTime, Valid: true}
	cursorID := uuid.Max
	if cursor != nil {
		cursorTS = pgtype.Timestamptz{Time: cursor.CreatedAt, Valid: true}
		var err error
		cursorID, err = uuid.Parse(cursor.PostID)
		if err != nil {
			return nil, nil, errors.NewBadRequestError("invalid cursor post_id")
		}
	}

	// Fetch one extra to detect if there's a next page
	posts, err := s.postRepo.GetByAuthorWithCursor(ctx, authorID, cursorTS, cursorID, visibility, limit+1)
	if err != nil {
		logger.LogError(ctx, err, "failed to get user posts", "author_id", authorID)
		return nil, nil, errors.NewInternalError(err)
	}

	var nextCursor *post.UserPostCursor
	if len(posts) > int(limit) {
		last := posts[limit-1]
		nextCursor = &post.UserPostCursor{
			CreatedAt: last.CreatedAt,
			PostID:    last.ID.String(),
		}
		posts = posts[:limit]
	}

	s.enrichBatch(ctx, posts, viewerID)
	s.enrichAuthors(ctx, posts)
	s.enrichTags(ctx, posts)
	s.enrichMentions(ctx, posts)
	s.enrichIsFollowingAuthor(ctx, posts, viewerID)

	return posts, nextCursor, nil
}

// UpdatePost updates content/visibility of a post (only by owner)
func (s *PostService) UpdatePost(ctx context.Context, postID, userID uuid.UUID, content string, visibility entity.Visibility, mentionUserIDs []uuid.UUID, tags []string) (*entity.Post, error) {
	existing, err := s.postRepo.GetByID(ctx, postID)
	if err != nil {
		if errors.Is(err, errors.ErrNotFound) {
			return nil, post.ErrPostNotFound
		}
		return nil, err
	}
	if existing.AuthorID != userID {
		return nil, post.ErrForbidden
	}
	if !isValidVisibility(visibility) {
		return nil, post.ErrInvalidVisibility
	}

	validTags, err := validateTags(tags)
	if err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	txPost := s.postRepo.WithTx(tx)

	updated, err := txPost.Update(ctx, postID, strings.TrimSpace(content), visibility)
	if err != nil {
		logger.LogError(ctx, err, "failed to update post", "post_id", postID)
		return nil, errors.NewInternalError(err)
	}

	if s.hashtagRepo != nil {
		if err := s.hashtagRepo.WithTx(tx).ReplaceForPost(ctx, postID, validTags); err != nil {
			logger.LogError(ctx, err, "failed to replace hashtags", "post_id", postID)
			return nil, errors.NewInternalError(err)
		}
	}

	// Replace mentions within transaction
	var persistedMentionIDs []uuid.UUID
	if s.mentionRepo != nil {
		// Delete old mentions
		if err := s.mentionRepo.WithTx(tx).DeleteByPost(ctx, postID); err != nil {
			logger.LogError(ctx, err, "failed to clear old mentions", "post_id", postID)
			return nil, errors.NewInternalError(err)
		}
		// Insert new mentions
		if len(mentionUserIDs) > 0 {
			ids, err := s.persistMentions(ctx, s.mentionRepo.WithTx(tx), postID, mentionUserIDs)
			if err != nil {
				logger.LogError(ctx, err, "failed to persist mentions", "post_id", postID)
				return nil, errors.NewInternalError(err)
			}
			persistedMentionIDs = ids
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, errors.NewInternalError(err)
	}

	// Enrich mentions and fire notifications AFTER commit (non-fatal)
	updated.Mentions = s.enrichMentionsAfterCommit(ctx, postID, userID, persistedMentionIDs)

	s.enrichBatch(ctx, []*entity.Post{updated}, &userID)
	s.enrichAuthors(ctx, []*entity.Post{updated})
	s.enrichTags(ctx, []*entity.Post{updated})
	s.enrichMentions(ctx, []*entity.Post{updated})

	s.pushEmbeddingAsync(postID.String(), updated.Content, updated.Tags)

	logger.Info(ctx, "post updated", "post_id", postID)
	return updated, nil
}

// DeletePost soft-deletes a post (only by owner)
func (s *PostService) DeletePost(ctx context.Context, postID, userID uuid.UUID) error {
	existing, err := s.postRepo.GetByID(ctx, postID)
	if err != nil {
		if errors.Is(err, errors.ErrNotFound) {
			return post.ErrPostNotFound
		}
		return err
	}
	if existing.AuthorID != userID {
		return post.ErrForbidden
	}
	if err := s.postRepo.Delete(ctx, postID); err != nil {
		logger.LogError(ctx, err, "failed to delete post", "post_id", postID)
		return errors.NewInternalError(err)
	}

	// Remove the post from the recommendation index so it no longer appears in suggestions.
	// Fire-and-forget — a failure here does not roll back the deletion.
	if s.objectDeleter != nil {
		if err := s.objectDeleter.DeleteObject(ctx, postID.String()); err != nil {
			logger.LogError(ctx, err, "codohue: failed to delete object from index", "post_id", postID)
		}
	}

	logger.Info(ctx, "post deleted", "post_id", postID)
	return nil
}

// pushEmbeddingAsync computes a TF-IDF vector for the post and uploads it to Codohue
// in a background goroutine. Uses a detached context so it outlives the HTTP request.
// Both embeddingProvider and objectEmbedder must be non-nil for this to run.
func (s *PostService) pushEmbeddingAsync(postID, content string, tags []string) {
	if s.embeddingProvider == nil || s.objectEmbedder == nil {
		return
	}

	// Combine content and hashtags — tags add vocabulary signal with no extra cost.
	text := content
	if len(tags) > 0 {
		text += " " + strings.Join(tags, " ")
	}

	go func() {
		ctx := context.Background()
		vec, err := s.embeddingProvider.Embed(ctx, text)
		if err != nil {
			logger.LogError(ctx, err, "tfidf: failed to vectorize post", "post_id", postID)
			return
		}
		if err := s.objectEmbedder.UpsertObjectEmbedding(ctx, postID, vec); err != nil {
			logger.LogError(ctx, err, "codohue: failed to push object embedding", "post_id", postID)
		}
	}()
}
