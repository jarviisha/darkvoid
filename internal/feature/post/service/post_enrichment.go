package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/post/entity"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

// enrichBatch loads media and isLiked for a slice of posts in bulk.
// LikeCount is already populated from the denormalized DB counter on each post row.
// Like-related fields are skipped when likeRepo is not configured.
func (s *PostService) enrichBatch(ctx context.Context, posts []*entity.Post, viewerID *uuid.UUID) {
	if len(posts) == 0 {
		return
	}

	ids := make([]uuid.UUID, len(posts))
	for i, p := range posts {
		ids[i] = p.ID
	}

	// Batch fetch media
	mediaMap, err := s.mediaRepo.GetByPostsBatch(ctx, ids)
	if err != nil {
		logger.LogError(ctx, err, "failed to batch fetch post media")
	} else {
		for _, p := range posts {
			if media, ok := mediaMap[p.ID]; ok {
				p.Media = media
			}
		}
	}

	// Batch fetch isLiked status
	if s.likeRepo != nil && viewerID != nil {
		likedIDs, err := s.likeRepo.GetLikedPostIDs(ctx, *viewerID, ids)
		if err != nil {
			logger.LogError(ctx, err, "failed to batch fetch liked post IDs")
		} else {
			likedSet := make(map[uuid.UUID]bool, len(likedIDs))
			for _, id := range likedIDs {
				likedSet[id] = true
			}
			for _, p := range posts {
				p.IsLiked = likedSet[p.ID]
			}
		}
	}
}

// enrichAuthors batch-fetches author info for a slice of posts.
func (s *PostService) enrichAuthors(ctx context.Context, posts []*entity.Post) {
	if len(posts) == 0 || s.userReader == nil {
		return
	}

	seen := make(map[uuid.UUID]bool, len(posts))
	ids := make([]uuid.UUID, 0, len(posts))
	for _, p := range posts {
		if !seen[p.AuthorID] {
			seen[p.AuthorID] = true
			ids = append(ids, p.AuthorID)
		}
	}

	authors, err := s.userReader.GetAuthorsByIDs(ctx, ids)
	if err != nil {
		logger.LogError(ctx, err, "failed to enrich post authors")
		return
	}

	for _, p := range posts {
		if a, ok := authors[p.AuthorID]; ok {
			p.Author = a
		}
	}
}

// enrichIsFollowingAuthor sets IsFollowingAuthor on each post for the given viewer.
// Best-effort: on error, posts are returned as-is.
func (s *PostService) enrichIsFollowingAuthor(ctx context.Context, posts []*entity.Post, viewerID *uuid.UUID) {
	if s.followChecker == nil || viewerID == nil || len(posts) == 0 {
		return
	}

	// Collect unique author IDs and batch-check by single calls (typically few unique authors).
	seen := make(map[uuid.UUID]bool)
	for _, p := range posts {
		seen[p.AuthorID] = false
	}

	for authorID := range seen {
		if authorID == *viewerID {
			continue
		}
		ok, err := s.followChecker.IsFollowing(ctx, *viewerID, authorID)
		if err != nil {
			logger.LogError(ctx, err, "failed to check following for post enrichment", "author_id", authorID)
			continue
		}
		seen[authorID] = ok
	}

	for _, p := range posts {
		p.IsFollowingAuthor = seen[p.AuthorID]
	}
}

// enrichTags batch-fetches hashtag names for a slice of posts.
func (s *PostService) enrichTags(ctx context.Context, posts []*entity.Post) {
	if len(posts) == 0 || s.hashtagRepo == nil {
		return
	}

	ids := make([]uuid.UUID, len(posts))
	for i, p := range posts {
		ids[i] = p.ID
	}

	tagsMap, err := s.hashtagRepo.GetNamesByPostIDs(ctx, ids)
	if err != nil {
		logger.LogError(ctx, err, "failed to enrich post tags")
		return
	}

	for _, p := range posts {
		if names, ok := tagsMap[p.ID]; ok {
			p.Tags = names
		}
	}
}

// enrichMentions batch-loads mention user info for a slice of posts.
func (s *PostService) enrichMentions(ctx context.Context, posts []*entity.Post) {
	if s.mentionRepo == nil || len(posts) == 0 {
		return
	}

	ids := make([]uuid.UUID, len(posts))
	for i, p := range posts {
		ids[i] = p.ID
	}

	mentionMap, err := s.mentionRepo.GetBatch(ctx, ids)
	if err != nil {
		logger.LogError(ctx, err, "failed to batch fetch post mentions")
		return
	}

	// Collect all unique user IDs across all posts
	seen := make(map[uuid.UUID]bool)
	for _, userIDs := range mentionMap {
		for _, uid := range userIDs {
			seen[uid] = true
		}
	}
	if len(seen) == 0 {
		return
	}

	allIDs := make([]uuid.UUID, 0, len(seen))
	for uid := range seen {
		allIDs = append(allIDs, uid)
	}

	authors, err := s.userReader.GetAuthorsByIDs(ctx, allIDs)
	if err != nil {
		logger.LogError(ctx, err, "failed to enrich post mention authors")
		return
	}

	for _, p := range posts {
		userIDs, ok := mentionMap[p.ID]
		if !ok {
			continue
		}
		mentions := make([]*entity.MentionedUser, 0, len(userIDs))
		for _, uid := range userIDs {
			if a, ok := authors[uid]; ok {
				mentions = append(mentions, &entity.MentionedUser{
					ID:          a.ID,
					Username:    a.Username,
					DisplayName: a.DisplayName,
				})
			}
		}
		p.Mentions = mentions
	}
}
