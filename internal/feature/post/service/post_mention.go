package service

import (
	"context"

	"github.com/google/uuid"
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

// emitMentions fires a mention notification per recipient. Called AFTER the
// transaction commits; errors are logged, not returned.
func (s *PostService) emitMentions(ctx context.Context, postID, actorID uuid.UUID, mentionIDs []uuid.UUID) {
	if s.notifEmitter == nil {
		return
	}
	for _, uid := range mentionIDs {
		if err := s.notifEmitter.EmitMention(ctx, actorID, uid, postID); err != nil {
			logger.LogError(ctx, err, "failed to emit mention notification", "post_id", postID, "recipient_id", uid)
		}
	}
}
