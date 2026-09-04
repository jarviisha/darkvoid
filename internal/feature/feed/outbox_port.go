package feed

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// OutboxPort adapts PostgresOutbox to the enqueue interfaces the post and follow
// services declare on their own side.
//
// It lives here rather than in the composition root because every method is a
// feed.Event literal and nothing else: the translation is owned by the feed
// context, and keeping one copy of it is what lets the API and the seeder agree
// on the events a mutation produces.
type OutboxPort struct{ outbox *PostgresOutbox }

func NewOutboxPort(outbox *PostgresOutbox) *OutboxPort {
	return &OutboxPort{outbox: outbox}
}

func (o *OutboxPort) EnqueuePostCreated(ctx context.Context, tx pgx.Tx, postID, authorID uuid.UUID, visibility string, createdAt time.Time) error {
	return o.outbox.Enqueue(ctx, tx, Event{Type: EventPostCreated, PostID: postID, AuthorID: authorID, Visibility: visibility, CreatedAt: createdAt})
}

func (o *OutboxPort) EnqueuePostDeleted(ctx context.Context, tx pgx.Tx, postID, authorID uuid.UUID) error {
	return o.outbox.Enqueue(ctx, tx, Event{Type: EventPostDeleted, PostID: postID, AuthorID: authorID})
}

func (o *OutboxPort) EnqueuePostVisibilityChanged(ctx context.Context, tx pgx.Tx, postID, authorID uuid.UUID, visibility string, createdAt time.Time) error {
	return o.outbox.Enqueue(ctx, tx, Event{Type: EventVisibilityChanged, PostID: postID, AuthorID: authorID, Visibility: visibility, CreatedAt: createdAt})
}

func (o *OutboxPort) EnqueueFollowCreated(ctx context.Context, tx pgx.Tx, followerID, followeeID uuid.UUID) error {
	return o.outbox.Enqueue(ctx, tx, Event{Type: EventFollowCreated, ActorID: followerID, FolloweeID: followeeID})
}

func (o *OutboxPort) EnqueueFollowDeleted(ctx context.Context, tx pgx.Tx, followerID, followeeID uuid.UUID) error {
	return o.outbox.Enqueue(ctx, tx, Event{Type: EventFollowDeleted, ActorID: followerID, FolloweeID: followeeID})
}
