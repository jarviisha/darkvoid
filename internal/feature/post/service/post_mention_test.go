package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/post/entity"
)

// --------------------------------------------------------------------------
// persistMentions tests
// --------------------------------------------------------------------------

func TestPersistMentions_NilRepo(t *testing.T) {
	svc := &PostService{}
	postID := uuid.New()
	mentionIDs := []uuid.UUID{uuid.New(), uuid.New()}

	ids, err := svc.persistMentions(context.Background(), nil, postID, mentionIDs)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if ids != nil {
		t.Errorf("expected nil ids, got %v", ids)
	}
}

func TestPersistMentions_EmptyMentionIDs(t *testing.T) {
	svc := &PostService{}
	mockRepo := &mockMentionRepo{}
	postID := uuid.New()

	ids, err := svc.persistMentions(context.Background(), mockRepo, postID, []uuid.UUID{})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if ids != nil {
		t.Errorf("expected nil ids, got %v", ids)
	}
}

func TestPersistMentions_Success(t *testing.T) {
	ctx := context.Background()
	postID := uuid.New()
	user1 := uuid.New()
	user2 := uuid.New()
	user3 := uuid.New()

	var insertedPairs [][2]uuid.UUID
	mockRepo := &mockMentionRepo{
		insert: func(ctx context.Context, pID, uID uuid.UUID) error {
			insertedPairs = append(insertedPairs, [2]uuid.UUID{pID, uID})
			return nil
		},
	}

	svc := &PostService{}
	ids, err := svc.persistMentions(ctx, mockRepo, postID, []uuid.UUID{user1, user2, user3})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 3 {
		t.Errorf("expected 3 IDs, got %d", len(ids))
	}
	if len(insertedPairs) != 3 {
		t.Errorf("expected 3 insert calls, got %d", len(insertedPairs))
	}

	// Verify all mentions were inserted
	for i, pair := range insertedPairs {
		if pair[0] != postID {
			t.Errorf("insert %d: expected postID %s, got %s", i, postID, pair[0])
		}
	}
}

func TestPersistMentions_Deduplication(t *testing.T) {
	ctx := context.Background()
	postID := uuid.New()
	user1 := uuid.New()
	user2 := uuid.New()

	insertCount := 0
	mockRepo := &mockMentionRepo{
		insert: func(ctx context.Context, pID, uID uuid.UUID) error {
			insertCount++
			return nil
		},
	}

	svc := &PostService{}
	// Pass duplicates: user1 appears 3 times, user2 appears 2 times
	ids, err := svc.persistMentions(ctx, mockRepo, postID, []uuid.UUID{user1, user2, user1, user2, user1})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("expected 2 unique IDs, got %d", len(ids))
	}
	if insertCount != 2 {
		t.Errorf("expected 2 insert calls (deduplicated), got %d", insertCount)
	}
}

func TestPersistMentions_InsertError_RollsBack(t *testing.T) {
	ctx := context.Background()
	postID := uuid.New()

	user1 := uuid.New()
	user2 := uuid.New()

	expectedErr := errors.New("unique constraint violation")
	mockRepo := &mockMentionRepo{
		insert: func(ctx context.Context, pID, uID uuid.UUID) error {
			if uID == user2 {
				return expectedErr
			}
			return nil
		},
	}

	svc := &PostService{}
	ids, err := svc.persistMentions(ctx, mockRepo, postID, []uuid.UUID{user1, user2})

	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if ids != nil {
		t.Errorf("expected nil ids on error, got %v", ids)
	}
}

func TestPersistMentions_OrderPreserved(t *testing.T) {
	ctx := context.Background()
	postID := uuid.New()

	user1 := uuid.New()
	user2 := uuid.New()
	user3 := uuid.New()

	var insertOrder []uuid.UUID
	mockRepo := &mockMentionRepo{
		insert: func(ctx context.Context, pID, uID uuid.UUID) error {
			insertOrder = append(insertOrder, uID)
			return nil
		},
	}

	svc := &PostService{}
	ids, err := svc.persistMentions(ctx, mockRepo, postID, []uuid.UUID{user1, user2, user3})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify order is preserved (first occurrence)
	if ids[0] != user1 || ids[1] != user2 || ids[2] != user3 {
		t.Errorf("expected order [%s, %s, %s], got %v", user1, user2, user3, ids)
	}
	if insertOrder[0] != user1 || insertOrder[1] != user2 || insertOrder[2] != user3 {
		t.Errorf("expected insert order [%s, %s, %s], got %v", user1, user2, user3, insertOrder)
	}
}

// --------------------------------------------------------------------------
// enrichMentionsAfterCommit tests
// --------------------------------------------------------------------------

func TestEnrichMentionsAfterCommit_NoUserReader(t *testing.T) {
	svc := &PostService{userReader: nil}
	postID := uuid.New()
	actorID := uuid.New()
	mentionIDs := []uuid.UUID{uuid.New()}

	mentions := svc.enrichMentionsAfterCommit(context.Background(), postID, actorID, mentionIDs)

	if mentions != nil {
		t.Errorf("expected nil mentions when userReader is nil, got %v", mentions)
	}
}

func TestEnrichMentionsAfterCommit_EmptyMentionIDs(t *testing.T) {
	svc := &PostService{userReader: &mockUserReader{}}
	postID := uuid.New()
	actorID := uuid.New()

	mentions := svc.enrichMentionsAfterCommit(context.Background(), postID, actorID, []uuid.UUID{})

	if mentions != nil {
		t.Errorf("expected nil mentions for empty IDs, got %v", mentions)
	}
}

func TestEnrichMentionsAfterCommit_Success_NoNotifications(t *testing.T) {
	ctx := context.Background()
	postID := uuid.New()
	actorID := uuid.New()
	user1 := uuid.New()
	user2 := uuid.New()

	mockUR := &mockUserReader{
		getAuthorsByIDs: func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*entity.Author, error) {
			return map[uuid.UUID]*entity.Author{
				user1: {ID: user1, Username: "alice", DisplayName: "Alice"},
				user2: {ID: user2, Username: "bob", DisplayName: "Bob"},
			}, nil
		},
	}

	svc := &PostService{userReader: mockUR, notifEmitter: nil}
	mentions := svc.enrichMentionsAfterCommit(ctx, postID, actorID, []uuid.UUID{user1, user2})

	if len(mentions) != 2 {
		t.Errorf("expected 2 mentions, got %d", len(mentions))
	}
	if mentions[0].Username != "alice" {
		t.Errorf("expected first mention 'alice', got %s", mentions[0].Username)
	}
	if mentions[1].Username != "bob" {
		t.Errorf("expected second mention 'bob', got %s", mentions[1].Username)
	}
}

func TestEnrichMentionsAfterCommit_Success_WithNotifications(t *testing.T) {
	ctx := context.Background()
	postID := uuid.New()
	actorID := uuid.New()
	user1 := uuid.New()
	user2 := uuid.New()

	mockUR := &mockUserReader{
		getAuthorsByIDs: func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*entity.Author, error) {
			return map[uuid.UUID]*entity.Author{
				user1: {ID: user1, Username: "alice", DisplayName: "Alice"},
				user2: {ID: user2, Username: "bob", DisplayName: "Bob"},
			}, nil
		},
	}

	var emittedNotifs [][3]uuid.UUID // [actorID, recipientID, postID]
	mockNotif := &mockNotificationEmitter{
		emitMention: func(ctx context.Context, aID, rID, pID uuid.UUID) error {
			emittedNotifs = append(emittedNotifs, [3]uuid.UUID{aID, rID, pID})
			return nil
		},
	}

	svc := &PostService{userReader: mockUR, notifEmitter: mockNotif}
	mentions := svc.enrichMentionsAfterCommit(ctx, postID, actorID, []uuid.UUID{user1, user2})

	if len(mentions) != 2 {
		t.Errorf("expected 2 mentions, got %d", len(mentions))
	}

	if len(emittedNotifs) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(emittedNotifs))
	}

	// Verify notification 1
	if emittedNotifs[0][0] != actorID {
		t.Errorf("notif 1: expected actorID %s, got %s", actorID, emittedNotifs[0][0])
	}
	if emittedNotifs[0][1] != user1 {
		t.Errorf("notif 1: expected recipientID %s, got %s", user1, emittedNotifs[0][1])
	}
	if emittedNotifs[0][2] != postID {
		t.Errorf("notif 1: expected postID %s, got %s", postID, emittedNotifs[0][2])
	}

	// Verify notification 2
	if emittedNotifs[1][1] != user2 {
		t.Errorf("notif 2: expected recipientID %s, got %s", user2, emittedNotifs[1][1])
	}
}

func TestEnrichMentionsAfterCommit_GetAuthorsByIDsError_NonFatal(t *testing.T) {
	ctx := context.Background()
	postID := uuid.New()
	actorID := uuid.New()
	user1 := uuid.New()

	mockUR := &mockUserReader{
		getAuthorsByIDs: func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*entity.Author, error) {
			return nil, errors.New("user service unavailable")
		},
	}

	var notifCalled bool
	mockNotif := &mockNotificationEmitter{
		emitMention: func(ctx context.Context, aID, rID, pID uuid.UUID) error {
			notifCalled = true
			return nil
		},
	}

	svc := &PostService{userReader: mockUR, notifEmitter: mockNotif}
	mentions := svc.enrichMentionsAfterCommit(ctx, postID, actorID, []uuid.UUID{user1})

	// Should not panic, but mentions should be empty because no author data
	if len(mentions) != 0 {
		t.Errorf("expected 0 mentions when author fetch fails, got %d", len(mentions))
	}

	// Notification should still be emitted even if author fetch fails
	if !notifCalled {
		t.Error("expected notification to be emitted even on author fetch error")
	}
}

func TestEnrichMentionsAfterCommit_NotificationError_NonFatal(t *testing.T) {
	ctx := context.Background()
	postID := uuid.New()
	actorID := uuid.New()
	user1 := uuid.New()

	mockUR := &mockUserReader{
		getAuthorsByIDs: func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*entity.Author, error) {
			return map[uuid.UUID]*entity.Author{
				user1: {ID: user1, Username: "alice", DisplayName: "Alice"},
			}, nil
		},
	}

	mockNotif := &mockNotificationEmitter{
		emitMention: func(ctx context.Context, aID, rID, pID uuid.UUID) error {
			return errors.New("notification service error")
		},
	}

	svc := &PostService{userReader: mockUR, notifEmitter: mockNotif}
	mentions := svc.enrichMentionsAfterCommit(ctx, postID, actorID, []uuid.UUID{user1})

	// Should not panic, mentions should still be returned
	if len(mentions) != 1 {
		t.Errorf("expected 1 mention despite notification error, got %d", len(mentions))
	}
	if mentions[0].Username != "alice" {
		t.Errorf("expected mention 'alice', got %s", mentions[0].Username)
	}
}

func TestEnrichMentionsAfterCommit_PartialAuthorData(t *testing.T) {
	ctx := context.Background()
	postID := uuid.New()
	actorID := uuid.New()
	user1 := uuid.New()
	user2 := uuid.New()
	user3 := uuid.New()

	mockUR := &mockUserReader{
		getAuthorsByIDs: func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*entity.Author, error) {
			// Only return data for user1 and user3, user2 is missing
			return map[uuid.UUID]*entity.Author{
				user1: {ID: user1, Username: "alice", DisplayName: "Alice"},
				user3: {ID: user3, Username: "charlie", DisplayName: "Charlie"},
			}, nil
		},
	}

	var emittedNotifs []uuid.UUID
	mockNotif := &mockNotificationEmitter{
		emitMention: func(ctx context.Context, aID, rID, pID uuid.UUID) error {
			emittedNotifs = append(emittedNotifs, rID)
			return nil
		},
	}

	svc := &PostService{userReader: mockUR, notifEmitter: mockNotif}
	mentions := svc.enrichMentionsAfterCommit(ctx, postID, actorID, []uuid.UUID{user1, user2, user3})

	// Should only include mentions where author data is available
	if len(mentions) != 2 {
		t.Errorf("expected 2 mentions (user2 missing author data), got %d", len(mentions))
	}

	// But notifications should be sent for all 3 users
	if len(emittedNotifs) != 3 {
		t.Errorf("expected 3 notifications (all users), got %d", len(emittedNotifs))
	}

	// Verify mentions
	if mentions[0].Username != "alice" && mentions[0].Username != "charlie" {
		t.Errorf("unexpected mention username: %s", mentions[0].Username)
	}
	if mentions[1].Username != "alice" && mentions[1].Username != "charlie" {
		t.Errorf("unexpected mention username: %s", mentions[1].Username)
	}
}

func TestEnrichMentionsAfterCommit_EmptyAuthorMap_StillNotifies(t *testing.T) {
	ctx := context.Background()
	postID := uuid.New()
	actorID := uuid.New()
	user1 := uuid.New()

	mockUR := &mockUserReader{
		getAuthorsByIDs: func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*entity.Author, error) {
			return map[uuid.UUID]*entity.Author{}, nil // Empty map
		},
	}

	var notifCalled bool
	mockNotif := &mockNotificationEmitter{
		emitMention: func(ctx context.Context, aID, rID, pID uuid.UUID) error {
			notifCalled = true
			return nil
		},
	}

	svc := &PostService{userReader: mockUR, notifEmitter: mockNotif}
	mentions := svc.enrichMentionsAfterCommit(ctx, postID, actorID, []uuid.UUID{user1})

	if len(mentions) != 0 {
		t.Errorf("expected 0 mentions when no author data, got %d", len(mentions))
	}

	// Notification should still fire
	if !notifCalled {
		t.Error("expected notification even when author data is empty")
	}
}
