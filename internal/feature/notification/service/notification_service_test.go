package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jarviisha/darkvoid/internal/feature/notification/entity"
)

type testNotificationCache struct{ invalidated []uuid.UUID }

func (c *testNotificationCache) GetUnreadCount(context.Context, uuid.UUID) (int64, error) {
	return -1, nil
}
func (c *testNotificationCache) SetUnreadCount(context.Context, uuid.UUID, int64) error { return nil }
func (c *testNotificationCache) InvalidateUnreadCount(_ context.Context, id uuid.UUID) error {
	c.invalidated = append(c.invalidated, id)
	return nil
}

type testNotificationRepo struct{ recipientID uuid.UUID }

func (r *testNotificationRepo) Create(_ context.Context, recipientID, actorID uuid.UUID, typ string, _, _ *uuid.UUID, groupKey string) (*entity.Notification, error) {
	return &entity.Notification{ID: uuid.New(), RecipientID: recipientID, ActorID: actorID, Type: entity.NotificationType(typ), GroupKey: groupKey}, nil
}
func (r *testNotificationRepo) CreateSystemNotification(_ context.Context, recipientID, actorID uuid.UUID, _, groupKey string) (*entity.Notification, error) {
	return &entity.Notification{ID: uuid.New(), RecipientID: recipientID, ActorID: actorID, Type: entity.TypeSystemAnnouncement, GroupKey: groupKey}, nil
}
func (r *testNotificationRepo) GetByRecipientWithCursor(context.Context, uuid.UUID, pgtype.Timestamptz, uuid.UUID, int32) ([]*entity.Notification, error) {
	return nil, nil
}
func (r *testNotificationRepo) MarkAsRead(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (r *testNotificationRepo) MarkAllAsRead(context.Context, uuid.UUID) error         { return nil }
func (r *testNotificationRepo) CountUnread(context.Context, uuid.UUID) (int64, error)  { return 0, nil }
func (r *testNotificationRepo) GetGroupActors(context.Context, uuid.UUID, string, int32) ([]uuid.UUID, int64, error) {
	return nil, 0, nil
}
func (r *testNotificationRepo) DeleteByActorAndGroupKey(context.Context, uuid.UUID, string) (uuid.UUID, error) {
	return r.recipientID, nil
}

type testUserReader struct{}

func (testUserReader) GetAuthorsByIDs(context.Context, []uuid.UUID) (map[uuid.UUID]*entity.Actor, error) {
	return map[uuid.UUID]*entity.Actor{}, nil
}

func TestNotificationMutationsInvalidateUnreadCache(t *testing.T) {
	recipientID, actorID := uuid.New(), uuid.New()
	repo := &testNotificationRepo{recipientID: recipientID}
	cache := &testNotificationCache{}
	svc := NewNotificationService(repo, cache, testUserReader{})

	if err := svc.EmitLike(context.Background(), actorID, recipientID, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteNotification(context.Background(), actorID, "like:key"); err != nil {
		t.Fatal(err)
	}
	if len(cache.invalidated) != 2 || cache.invalidated[0] != recipientID || cache.invalidated[1] != recipientID {
		t.Fatalf("invalidations = %v, want recipient twice", cache.invalidated)
	}
}
