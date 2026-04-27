package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jarviisha/darkvoid/internal/feature/user/db"
	apperrors "github.com/jarviisha/darkvoid/pkg/errors"
)

// --------------------------------------------------------------------------
// rowsToFollows mapping
// --------------------------------------------------------------------------

func TestRowsToFollows_Empty(t *testing.T) {
	result := rowsToFollows([]db.UsrFollow{})
	if len(result) != 0 {
		t.Errorf("expected empty slice, got %d", len(result))
	}
}

func TestRowsToFollows_PreservesFields(t *testing.T) {
	followerID := uuid.New()
	followeeID := uuid.New()
	ts := time.Now().UTC().Truncate(time.Microsecond)

	rows := []db.UsrFollow{
		{FollowerID: followerID, FolloweeID: followeeID, CreatedAt: pgtype.Timestamptz{Time: ts, Valid: true}},
	}

	result := rowsToFollows(rows)

	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	f := result[0]
	if f.FollowerID != followerID {
		t.Errorf("FollowerID mismatch: want %v got %v", followerID, f.FollowerID)
	}
	if f.FolloweeID != followeeID {
		t.Errorf("FolloweeID mismatch: want %v got %v", followeeID, f.FolloweeID)
	}
	if !f.CreatedAt.Equal(ts) {
		t.Errorf("CreatedAt mismatch: want %v got %v", ts, f.CreatedAt)
	}
}

func TestRowsToFollows_MultipleRows(t *testing.T) {
	rows := []db.UsrFollow{
		{FollowerID: uuid.New(), FolloweeID: uuid.New()},
		{FollowerID: uuid.New(), FolloweeID: uuid.New()},
		{FollowerID: uuid.New(), FolloweeID: uuid.New()},
	}

	result := rowsToFollows(rows)

	if len(result) != 3 {
		t.Errorf("expected 3 results, got %d", len(result))
	}
	for i, r := range result {
		if r.FollowerID != rows[i].FollowerID {
			t.Errorf("[%d] FollowerID mismatch", i)
		}
	}
}

// --------------------------------------------------------------------------
// FollowRepository — error propagation
// --------------------------------------------------------------------------

func TestFollowRepository_Follow_Success(t *testing.T) {
	followerID := uuid.New()
	followeeID := uuid.New()

	var gotParams db.FollowParams
	q := &mockQuerier{
		follow: func(_ context.Context, p db.FollowParams) error {
			gotParams = p
			return nil
		},
	}

	repo := &FollowRepository{queries: q}
	err := repo.Follow(context.Background(), followerID, followeeID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotParams.FollowerID != followerID || gotParams.FolloweeID != followeeID {
		t.Errorf("params mismatch: want (%v,%v) got (%v,%v)", followerID, followeeID, gotParams.FollowerID, gotParams.FolloweeID)
	}
}

func TestFollowRepository_Follow_DBError(t *testing.T) {
	sentinel := errors.New("db unavailable")
	q := &mockQuerier{
		follow: func(_ context.Context, _ db.FollowParams) error {
			return sentinel
		},
	}

	repo := &FollowRepository{queries: q}
	err := repo.Follow(context.Background(), uuid.New(), uuid.New())

	appErr := apperrors.GetAppError(err)
	if appErr == nil {
		t.Fatalf("expected AppError, got %v", err)
	}
	if appErr.Code != "INTERNAL_ERROR" {
		t.Errorf("expected INTERNAL_ERROR, got %s", appErr.Code)
	}
}

func TestFollowRepository_IsFollowing_True(t *testing.T) {
	q := &mockQuerier{
		isFollowing: func(_ context.Context, _ db.IsFollowingParams) (bool, error) {
			return true, nil
		},
	}

	repo := &FollowRepository{queries: q}
	ok, err := repo.IsFollowing(context.Background(), uuid.New(), uuid.New())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Errorf("expected true")
	}
}

func TestFollowRepository_Unfollow_DBError(t *testing.T) {
	sentinel := errors.New("connection lost")
	q := &mockQuerier{
		unfollow: func(_ context.Context, _ db.UnfollowParams) error {
			return sentinel
		},
	}

	repo := &FollowRepository{queries: q}
	err := repo.Unfollow(context.Background(), uuid.New(), uuid.New())

	appErr := apperrors.GetAppError(err)
	if appErr == nil {
		t.Fatalf("expected AppError, got %v", err)
	}
	if appErr.Code != "INTERNAL_ERROR" {
		t.Errorf("expected INTERNAL_ERROR, got %s", appErr.Code)
	}
}
