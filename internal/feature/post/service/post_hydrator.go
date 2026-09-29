package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/post/entity"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

// Fields selects what Hydrator.Hydrate fills on a post.
type Fields uint8

const (
	FieldMedia Fields = 1 << iota
	FieldAuthor
	FieldTags
	FieldMentions
	// FieldLiked and FieldFollowingAuthor depend on the viewer; both are
	// skipped when the viewer is nil.
	FieldLiked
	FieldFollowingAuthor

	// FieldsShared is everything that reads the same for every viewer, and so
	// may be cached across viewers.
	FieldsShared = FieldMedia | FieldAuthor | FieldTags | FieldMentions
	// FieldsViewer is everything that must be filled per request.
	FieldsViewer = FieldLiked | FieldFollowingAuthor
	FieldsAll    = FieldsShared | FieldsViewer
)

type hydratorMediaRepo interface {
	GetByPostsBatch(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]*entity.PostMedia, error)
}

type hydratorLikeRepo interface {
	GetLikedPostIDs(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) ([]uuid.UUID, error)
}

type hydratorTagRepo interface {
	GetNamesByPostIDs(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]string, error)
}

type hydratorMentionRepo interface {
	GetBatch(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error)
}

type hydratorFollowChecker interface {
	GetFollowingAmong(ctx context.Context, followerID uuid.UUID, followeeIDs []uuid.UUID) ([]uuid.UUID, error)
}

// HydratorDeps carries the batch readers Hydrator fills posts from.
type HydratorDeps struct {
	Media    hydratorMediaRepo
	Likes    hydratorLikeRepo
	Users    userReader
	Tags     hydratorTagRepo
	Mentions hydratorMentionRepo
	Follows  hydratorFollowChecker
}

// Hydrator fills the non-stored fields of posts in bulk: one query per field
// for the whole slice, and one author lookup covering both post authors and
// mentioned users. Every path that returns posts goes through it, so which
// fields a post carries is decided by the caller's Fields, not by which code
// path served it.
//
// Hydration is best-effort: a failed lookup is logged and leaves that field
// unset rather than failing the request. A nil dependency skips its fields.
type Hydrator struct {
	d HydratorDeps
}

// NewHydrator creates a Hydrator.
func NewHydrator(d HydratorDeps) *Hydrator {
	return &Hydrator{d: d}
}

// Hydrate fills fields on posts, in place, as seen by viewerID.
func (h *Hydrator) Hydrate(ctx context.Context, posts []*entity.Post, viewerID *uuid.UUID, fields Fields) {
	if len(posts) == 0 {
		return
	}
	ids := make([]uuid.UUID, len(posts))
	for i, p := range posts {
		ids[i] = p.ID
	}

	if fields&FieldMedia != 0 && h.d.Media != nil {
		h.media(ctx, posts, ids)
	}
	if fields&FieldTags != 0 && h.d.Tags != nil {
		h.tags(ctx, posts, ids)
	}
	if fields&(FieldAuthor|FieldMentions) != 0 && h.d.Users != nil {
		h.people(ctx, posts, ids, fields)
	}
	if viewerID == nil {
		return
	}
	if fields&FieldLiked != 0 && h.d.Likes != nil {
		h.liked(ctx, posts, ids, *viewerID)
	}
	if fields&FieldFollowingAuthor != 0 && h.d.Follows != nil {
		h.followingAuthor(ctx, posts, *viewerID)
	}
}

func (h *Hydrator) media(ctx context.Context, posts []*entity.Post, ids []uuid.UUID) {
	mediaMap, err := h.d.Media.GetByPostsBatch(ctx, ids)
	if err != nil {
		logger.LogError(ctx, err, "failed to batch fetch post media")
		return
	}
	for _, p := range posts {
		if m, ok := mediaMap[p.ID]; ok {
			p.Media = m
		}
	}
}

func (h *Hydrator) tags(ctx context.Context, posts []*entity.Post, ids []uuid.UUID) {
	tagsMap, err := h.d.Tags.GetNamesByPostIDs(ctx, ids)
	if err != nil {
		logger.LogError(ctx, err, "failed to batch fetch post tags")
		return
	}
	for _, p := range posts {
		if names, ok := tagsMap[p.ID]; ok {
			p.Tags = names
		}
	}
}

// people fills authors and mentions from a single directory lookup.
func (h *Hydrator) people(ctx context.Context, posts []*entity.Post, ids []uuid.UUID, fields Fields) {
	var mentionMap map[uuid.UUID][]uuid.UUID
	if fields&FieldMentions != 0 && h.d.Mentions != nil {
		var err error
		mentionMap, err = h.d.Mentions.GetBatch(ctx, ids)
		if err != nil {
			logger.LogError(ctx, err, "failed to batch fetch post mentions")
		}
	}

	seen := make(map[uuid.UUID]bool)
	var userIDs []uuid.UUID
	add := func(id uuid.UUID) {
		if !seen[id] {
			seen[id] = true
			userIDs = append(userIDs, id)
		}
	}
	if fields&FieldAuthor != 0 {
		for _, p := range posts {
			add(p.AuthorID)
		}
	}
	for _, mentioned := range mentionMap {
		for _, id := range mentioned {
			add(id)
		}
	}
	if len(userIDs) == 0 {
		return
	}

	authors, err := h.d.Users.GetAuthorsByIDs(ctx, userIDs)
	if err != nil {
		logger.LogError(ctx, err, "failed to fetch post authors and mentions")
		return
	}
	for _, p := range posts {
		if fields&FieldAuthor != 0 {
			if a, ok := authors[p.AuthorID]; ok {
				p.Author = a
			}
		}
		if mentioned, ok := mentionMap[p.ID]; ok {
			p.Mentions = mentionedUsers(authors, mentioned)
		}
	}
}

// Mentioned resolves user ids the caller already holds, keeping their order.
// A post that was just written knows its mentions in the order the author typed
// them; reading them back cannot recover that order.
func (h *Hydrator) Mentioned(ctx context.Context, ids []uuid.UUID) []*entity.MentionedUser {
	if len(ids) == 0 || h.d.Users == nil {
		return nil
	}
	authors, err := h.d.Users.GetAuthorsByIDs(ctx, ids)
	if err != nil {
		logger.LogError(ctx, err, "failed to fetch mentioned users")
		return nil
	}
	return mentionedUsers(authors, ids)
}

// mentionedUsers projects ids through authors in order, dropping unknown ids.
func mentionedUsers(authors map[uuid.UUID]*entity.Author, ids []uuid.UUID) []*entity.MentionedUser {
	out := make([]*entity.MentionedUser, 0, len(ids))
	for _, id := range ids {
		if a, ok := authors[id]; ok {
			out = append(out, &entity.MentionedUser{ID: a.ID, Username: a.Username, DisplayName: a.DisplayName})
		}
	}
	return out
}

func (h *Hydrator) liked(ctx context.Context, posts []*entity.Post, ids []uuid.UUID, viewerID uuid.UUID) {
	likedIDs, err := h.d.Likes.GetLikedPostIDs(ctx, viewerID, ids)
	if err != nil {
		logger.LogError(ctx, err, "failed to batch fetch liked post IDs")
		return
	}
	liked := make(map[uuid.UUID]bool, len(likedIDs))
	for _, id := range likedIDs {
		liked[id] = true
	}
	for _, p := range posts {
		p.IsLiked = liked[p.ID]
	}
}

// followingAuthor never asks about the viewer's own posts: nobody follows
// themselves, so those stay false without a lookup.
func (h *Hydrator) followingAuthor(ctx context.Context, posts []*entity.Post, viewerID uuid.UUID) {
	seen := make(map[uuid.UUID]bool, len(posts))
	authorIDs := make([]uuid.UUID, 0, len(posts))
	for _, p := range posts {
		if p.AuthorID == viewerID || seen[p.AuthorID] {
			continue
		}
		seen[p.AuthorID] = true
		authorIDs = append(authorIDs, p.AuthorID)
	}
	if len(authorIDs) == 0 {
		return
	}
	followingIDs, err := h.d.Follows.GetFollowingAmong(ctx, viewerID, authorIDs)
	if err != nil {
		logger.LogError(ctx, err, "failed to batch-check following for post hydration", "author_count", len(authorIDs))
		return
	}
	following := make(map[uuid.UUID]bool, len(followingIDs))
	for _, id := range followingIDs {
		following[id] = true
	}
	for _, p := range posts {
		p.IsFollowingAuthor = following[p.AuthorID]
	}
}
