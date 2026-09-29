package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
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
// emitMentions tests
// --------------------------------------------------------------------------

func TestEmitMentions_NoEmitter(t *testing.T) {
	svc := &PostService{}
	svc.emitMentions(context.Background(), uuid.New(), uuid.New(), []uuid.UUID{uuid.New()})
	// No panic = success
}

func TestEmitMentions_OnePerRecipient(t *testing.T) {
	postID, actorID := uuid.New(), uuid.New()
	user1, user2 := uuid.New(), uuid.New()

	var emitted [][3]uuid.UUID // [actorID, recipientID, postID]
	svc := &PostService{notifEmitter: &mockNotificationEmitter{
		emitMention: func(_ context.Context, aID, rID, pID uuid.UUID) error {
			emitted = append(emitted, [3]uuid.UUID{aID, rID, pID})
			return nil
		},
	}}
	svc.emitMentions(context.Background(), postID, actorID, []uuid.UUID{user1, user2})

	want := [][3]uuid.UUID{{actorID, user1, postID}, {actorID, user2, postID}}
	if len(emitted) != len(want) || emitted[0] != want[0] || emitted[1] != want[1] {
		t.Errorf("emitted %v, want %v", emitted, want)
	}
}

func TestEmitMentions_ErrorDoesNotStopTheRest(t *testing.T) {
	calls := 0
	svc := &PostService{notifEmitter: &mockNotificationEmitter{
		emitMention: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
			calls++
			return errors.New("notification service error")
		},
	}}
	svc.emitMentions(context.Background(), uuid.New(), uuid.New(), []uuid.UUID{uuid.New(), uuid.New()})

	if calls != 2 {
		t.Errorf("expected 2 emit attempts, got %d", calls)
	}
}
