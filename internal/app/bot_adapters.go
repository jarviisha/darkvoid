package app

import (
	"context"

	"github.com/google/uuid"
)

// botPostReader implements the bot service's postReader using the post repository.
// The bot activity log stores a post id but no content, so this is how a log row
// becomes readable without the bot context importing the post context.
type botPostReader struct {
	postRepo feedPostRepo
}

// GetPostPreviews returns the content of the given posts, keyed by id. Ids with no
// surviving post are simply absent: a bot's post can be deleted, and the log entry
// that recorded it has to stay readable. Trimming to a preview length is the
// service's job — this only fetches.
func (r *botPostReader) GetPostPreviews(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	posts, err := r.postRepo.GetPostsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	previews := make(map[uuid.UUID]string, len(posts))
	for _, p := range posts {
		previews[p.ID] = p.Content
	}
	return previews, nil
}
