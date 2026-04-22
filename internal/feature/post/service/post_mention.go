package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/post/entity"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

// persistMentions inserts mention rows within a transaction for the given user IDs.
// Returns the mention user IDs for later enrichment.
// Errors are returned to caller (not logged) so transaction can be rolled back.
func (s *PostService) persistMentions(
	ctx context.Context,
	txMention mentionRepo,
	postID uuid.UUID,
	mentionIDs []uuid.UUID,
) ([]uuid.UUID, error) {
	if txMention == nil || len(mentionIDs) == 0 {
		return nil, nil
	}

	// Deduplicate
	seen := make(map[uuid.UUID]struct{}, len(mentionIDs))
	ids := make([]uuid.UUID, 0, len(mentionIDs))
	for _, uid := range mentionIDs {
		if _, ok := seen[uid]; !ok {
			seen[uid] = struct{}{}
			ids = append(ids, uid)
		}
	}

	// Insert mentions within transaction
	for _, uid := range ids {
		if err := txMention.Insert(ctx, postID, uid); err != nil {
			return nil, err
		}
	}

	return ids, nil
}

// enrichMentionsAfterCommit enriches mention user info and fires notifications.
// This is called AFTER transaction commit. Errors are logged but not fatal.
func (s *PostService) enrichMentionsAfterCommit(
	ctx context.Context,
	postID, actorID uuid.UUID,
	mentionIDs []uuid.UUID,
) []*entity.MentionedUser {
	if s.userReader == nil || len(mentionIDs) == 0 {
		return nil
	}

	authors, err := s.userReader.GetAuthorsByIDs(ctx, mentionIDs)
	if err != nil {
		logger.LogError(ctx, err, "failed to enrich mention authors", "post_id", postID)
		authors = make(map[uuid.UUID]*entity.Author)
	}

	mentioned := make([]*entity.MentionedUser, 0, len(mentionIDs))
	for _, uid := range mentionIDs {
		if a, ok := authors[uid]; ok {
			mentioned = append(mentioned, &entity.MentionedUser{
				ID:          a.ID,
				Username:    a.Username,
				DisplayName: a.DisplayName,
			})
		}
		if s.notifEmitter != nil {
			if err := s.notifEmitter.EmitMention(ctx, actorID, uid, postID); err != nil {
				logger.LogError(ctx, err, "failed to emit mention notification", "post_id", postID, "recipient_id", uid)
			}
		}
	}
	return mentioned
}
