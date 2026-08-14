package service

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jarviisha/darkvoid/internal/feature/notification"
	"github.com/jarviisha/darkvoid/internal/feature/notification/entity"
	pkgerrors "github.com/jarviisha/darkvoid/pkg/errors"
)

type testNotificationCache struct {
	getCount      int64
	getErr        error
	setErr        error
	invalidateErr error
	setCalls      int
	setCount      int64
	invalidated   []uuid.UUID
}

func (c *testNotificationCache) GetUnreadCount(context.Context, uuid.UUID) (int64, error) {
	return c.getCount, c.getErr
}
func (c *testNotificationCache) SetUnreadCount(_ context.Context, _ uuid.UUID, count int64) error {
	c.setCalls++
	c.setCount = count
	return c.setErr
}
func (c *testNotificationCache) InvalidateUnreadCount(_ context.Context, id uuid.UUID) error {
	c.invalidated = append(c.invalidated, id)
	return c.invalidateErr
}

type groupResult struct {
	actorIDs []uuid.UUID
	total    int64
	err      error
}

type testNotificationRepo struct {
	recipientID     uuid.UUID
	createResult    *entity.Notification
	createErr       error
	systemResult    *entity.Notification
	systemErr       error
	items           []*entity.Notification
	getErr          error
	markErr         error
	markAllErr      error
	count           int64
	countErr        error
	deleteErr       error
	groups          map[string]groupResult
	createCalls     int
	countCalls      int
	lastCursorLimit int32
}

func (r *testNotificationRepo) Create(_ context.Context, recipientID, actorID uuid.UUID, typ string, _, _ *uuid.UUID, groupKey string) (*entity.Notification, error) {
	r.createCalls++
	if r.createResult != nil || r.createErr != nil {
		return r.createResult, r.createErr
	}
	return &entity.Notification{ID: uuid.New(), RecipientID: recipientID, ActorID: actorID, Type: entity.NotificationType(typ), GroupKey: groupKey, CreatedAt: time.Now()}, nil
}
func (r *testNotificationRepo) CreateSystemNotification(_ context.Context, recipientID, actorID uuid.UUID, message, groupKey string) (*entity.Notification, error) {
	if r.systemResult != nil || r.systemErr != nil {
		return r.systemResult, r.systemErr
	}
	return &entity.Notification{ID: uuid.New(), RecipientID: recipientID, ActorID: actorID, Type: entity.TypeSystemAnnouncement, Payload: entity.SystemPayload{Message: message}, GroupKey: groupKey, CreatedAt: time.Now()}, nil
}
func (r *testNotificationRepo) GetByRecipientWithCursor(_ context.Context, _ uuid.UUID, _ pgtype.Timestamptz, _ uuid.UUID, limit int32) ([]*entity.Notification, error) {
	r.lastCursorLimit = limit
	return r.items, r.getErr
}
func (r *testNotificationRepo) MarkAsRead(context.Context, uuid.UUID, uuid.UUID) error {
	return r.markErr
}
func (r *testNotificationRepo) MarkAllAsRead(context.Context, uuid.UUID) error {
	return r.markAllErr
}
func (r *testNotificationRepo) CountUnread(context.Context, uuid.UUID) (int64, error) {
	r.countCalls++
	return r.count, r.countErr
}
func (r *testNotificationRepo) GetGroupActors(_ context.Context, _ uuid.UUID, groupKey string, _ int32) ([]uuid.UUID, int64, error) {
	result := r.groups[groupKey]
	return result.actorIDs, result.total, result.err
}
func (r *testNotificationRepo) DeleteByActorAndGroupKey(context.Context, uuid.UUID, string) (uuid.UUID, error) {
	return r.recipientID, r.deleteErr
}

type testUserReader struct {
	authors map[uuid.UUID]*entity.Actor
	err     error
	calls   int
}

func (r *testUserReader) GetAuthorsByIDs(context.Context, []uuid.UUID) (map[uuid.UUID]*entity.Actor, error) {
	r.calls++
	return r.authors, r.err
}

func TestNotificationMutationsInvalidateUnreadCache(t *testing.T) {
	t.Parallel()
	recipientID, actorID := uuid.New(), uuid.New()
	repo := &testNotificationRepo{recipientID: recipientID}
	cache := &testNotificationCache{}
	svc := NewNotificationService(repo, cache, &testUserReader{})

	operations := []struct {
		name string
		run  func() error
	}{
		{name: "like", run: func() error { return svc.EmitLike(context.Background(), actorID, recipientID, uuid.New()) }},
		{name: "comment like", run: func() error { return svc.EmitCommentLike(context.Background(), actorID, recipientID, uuid.New()) }},
		{name: "comment", run: func() error {
			return svc.EmitComment(context.Background(), actorID, recipientID, uuid.New(), uuid.New())
		}},
		{name: "reply", run: func() error { return svc.EmitReply(context.Background(), actorID, recipientID, uuid.New(), uuid.New()) }},
		{name: "follow", run: func() error { return svc.EmitFollow(context.Background(), actorID, recipientID) }},
		{name: "mention", run: func() error { return svc.EmitMention(context.Background(), actorID, recipientID, uuid.New()) }},
		{name: "system", run: func() error {
			return svc.EmitSystemAnnouncement(context.Background(), actorID, recipientID, "maintenance", "system:1")
		}},
		{name: "delete", run: func() error { return svc.DeleteNotification(context.Background(), actorID, "like:key") }},
		{name: "mark one", run: func() error { return svc.MarkAsRead(context.Background(), uuid.New(), recipientID) }},
		{name: "mark all", run: func() error { return svc.MarkAllAsRead(context.Background(), recipientID) }},
	}
	for _, operation := range operations {
		if err := operation.run(); err != nil {
			t.Fatalf("%s: %v", operation.name, err)
		}
	}
	if len(cache.invalidated) != len(operations) {
		t.Fatalf("invalidations = %d, want %d", len(cache.invalidated), len(operations))
	}
	for _, got := range cache.invalidated {
		if got != recipientID {
			t.Fatalf("invalidated user = %s, want %s", got, recipientID)
		}
	}
}

func TestEmit_SkipsSelfAndReturnsRepositoryFailure(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	repo := &testNotificationRepo{}
	cache := &testNotificationCache{}
	svc := NewNotificationService(repo, cache, &testUserReader{})
	if err := svc.EmitLike(context.Background(), userID, userID, uuid.New()); err != nil {
		t.Fatalf("self EmitLike() error = %v", err)
	}
	if repo.createCalls != 0 || len(cache.invalidated) != 0 {
		t.Fatalf("self notification performed work: create=%d invalidations=%d", repo.createCalls, len(cache.invalidated))
	}

	sentinel := stderrors.New("database unavailable")
	repo.createErr = sentinel
	if err := svc.EmitLike(context.Background(), uuid.New(), userID, uuid.New()); !stderrors.Is(err, sentinel) {
		t.Fatalf("EmitLike() error = %v, want sentinel", err)
	}
}

func TestGetUnreadCount_CacheAndDatabaseFallback(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	tests := []struct {
		name          string
		cache         *testNotificationCache
		repo          *testNotificationRepo
		want          int64
		wantErr       bool
		wantRepoCalls int
		wantSetCalls  int
	}{
		{name: "cache hit", cache: &testNotificationCache{getCount: 8}, repo: &testNotificationRepo{count: 99}, want: 8},
		{name: "cache miss", cache: &testNotificationCache{getCount: -1}, repo: &testNotificationRepo{count: 12}, want: 12, wantRepoCalls: 1, wantSetCalls: 1},
		{name: "cache read failure", cache: &testNotificationCache{getCount: -1, getErr: stderrors.New("redis down")}, repo: &testNotificationRepo{count: 4}, want: 4, wantRepoCalls: 1, wantSetCalls: 1},
		{name: "cache write failure is best effort", cache: &testNotificationCache{getCount: -1, setErr: stderrors.New("redis down")}, repo: &testNotificationRepo{count: 5}, want: 5, wantRepoCalls: 1, wantSetCalls: 1},
		{name: "database failure", cache: &testNotificationCache{getCount: -1}, repo: &testNotificationRepo{countErr: stderrors.New("database down")}, wantErr: true, wantRepoCalls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			count, err := NewNotificationService(tt.repo, tt.cache, &testUserReader{}).GetUnreadCount(context.Background(), userID)
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetUnreadCount() error = %v, wantErr %v", err, tt.wantErr)
			}
			if count != tt.want {
				t.Fatalf("GetUnreadCount() = %d, want %d", count, tt.want)
			}
			if tt.repo.countCalls != tt.wantRepoCalls || tt.cache.setCalls != tt.wantSetCalls {
				t.Fatalf("calls = repo %d cache set %d, want %d/%d", tt.repo.countCalls, tt.cache.setCalls, tt.wantRepoCalls, tt.wantSetCalls)
			}
		})
	}
}

func TestGetNotifications_PaginatesAndEnrichesActorsAndGroups(t *testing.T) {
	t.Parallel()
	userID, actorID, groupActorID := uuid.New(), uuid.New(), uuid.New()
	createdAt := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	items := make([]*entity.Notification, pageSize+1)
	for i := range items {
		items[i] = &entity.Notification{
			ID: uuid.New(), ActorID: actorID, RecipientID: userID,
			Type: entity.TypeLike, GroupKey: "like:post", CreatedAt: createdAt.Add(-time.Duration(i) * time.Minute),
		}
	}
	repo := &testNotificationRepo{
		items: items,
		groups: map[string]groupResult{
			"like:post": {actorIDs: []uuid.UUID{actorID, groupActorID}, total: 23},
		},
	}
	reader := &testUserReader{authors: map[uuid.UUID]*entity.Actor{
		actorID:      {ID: actorID, Username: "actor"},
		groupActorID: {ID: groupActorID, Username: "group-actor"},
	}}

	got, next, err := NewNotificationService(repo, &testNotificationCache{}, reader).GetNotifications(context.Background(), userID, nil)
	if err != nil {
		t.Fatalf("GetNotifications() error = %v", err)
	}
	if len(got) != pageSize || next == nil || next.ID != got[pageSize-1].ID.String() {
		t.Fatalf("page length/cursor = %d/%+v", len(got), next)
	}
	if repo.lastCursorLimit != pageSize+1 {
		t.Fatalf("repository limit = %d, want %d", repo.lastCursorLimit, pageSize+1)
	}
	if got[0].Actor == nil || got[0].GroupCount != 23 || len(got[0].GroupActors) != 2 {
		t.Fatalf("enriched notification = %#v", got[0])
	}
	if reader.calls != 2 {
		t.Fatalf("user reader calls = %d, want actor and group batches", reader.calls)
	}
}

func TestGetNotifications_RejectsInvalidCursorAndWrapsRepositoryFailure(t *testing.T) {
	t.Parallel()
	svc := NewNotificationService(&testNotificationRepo{}, &testNotificationCache{}, &testUserReader{})
	_, _, err := svc.GetNotifications(context.Background(), uuid.New(), &notification.NotificationCursor{CreatedAt: time.Now(), ID: "invalid"})
	if appErr := pkgerrors.GetAppError(err); appErr == nil || appErr.HTTPStatus != 400 {
		t.Fatalf("invalid cursor error = %v, want bad request", err)
	}

	sentinel := stderrors.New("database unavailable")
	svc = NewNotificationService(&testNotificationRepo{getErr: sentinel}, &testNotificationCache{}, &testUserReader{})
	_, _, err = svc.GetNotifications(context.Background(), uuid.New(), nil)
	if appErr := pkgerrors.GetAppError(err); appErr == nil || !stderrors.Is(appErr, sentinel) {
		t.Fatalf("repository error = %v, want wrapped sentinel", err)
	}
}

func TestMutationFailuresDoNotInvalidateCache(t *testing.T) {
	t.Parallel()
	sentinel := stderrors.New("database unavailable")
	tests := []struct {
		name string
		repo *testNotificationRepo
		run  func(*NotificationService) error
	}{
		{name: "system", repo: &testNotificationRepo{systemErr: sentinel}, run: func(s *NotificationService) error {
			return s.EmitSystemAnnouncement(context.Background(), uuid.New(), uuid.New(), "message", "group")
		}},
		{name: "delete", repo: &testNotificationRepo{deleteErr: sentinel}, run: func(s *NotificationService) error {
			return s.DeleteNotification(context.Background(), uuid.New(), "group")
		}},
		{name: "mark one", repo: &testNotificationRepo{markErr: sentinel}, run: func(s *NotificationService) error { return s.MarkAsRead(context.Background(), uuid.New(), uuid.New()) }},
		{name: "mark all", repo: &testNotificationRepo{markAllErr: sentinel}, run: func(s *NotificationService) error { return s.MarkAllAsRead(context.Background(), uuid.New()) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cache := &testNotificationCache{}
			err := tt.run(NewNotificationService(tt.repo, cache, &testUserReader{}))
			if err == nil {
				t.Fatal("mutation error = nil")
			}
			if len(cache.invalidated) != 0 {
				t.Fatalf("invalidations = %v, want none", cache.invalidated)
			}
		})
	}
}
