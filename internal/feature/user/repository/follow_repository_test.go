package repository

import (
	"context"
	"errors"
	"os"
	"strings"
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

func TestFollowRepository_GetFollowingAmong_UsesOneBoundedQuery(t *testing.T) {
	followerID := uuid.New()
	followeeIDs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	want := []uuid.UUID{followeeIDs[0], followeeIDs[2]}
	queryCalls := 0
	q := &mockQuerier{
		getFollowingAmong: func(_ context.Context, arg db.GetFollowingAmongParams) ([]uuid.UUID, error) {
			queryCalls++
			if arg.FollowerID != followerID {
				t.Fatalf("follower ID = %s, want %s", arg.FollowerID, followerID)
			}
			if len(arg.FolloweeIds) != len(followeeIDs) {
				t.Fatalf("followee IDs = %v, want %v", arg.FolloweeIds, followeeIDs)
			}
			return want, nil
		},
	}

	got, err := (&FollowRepository{queries: q}).GetFollowingAmong(context.Background(), followerID, followeeIDs)
	if err != nil {
		t.Fatalf("GetFollowingAmong: %v", err)
	}
	if queryCalls != 1 || len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("result/calls = %v/%d, want %v/1", got, queryCalls, want)
	}
}

func TestFollowRepository_GetFollowingAmong_EmptyInputSkipsQuery(t *testing.T) {
	repo := &FollowRepository{queries: nil}
	got, err := repo.GetFollowingAmong(context.Background(), uuid.New(), nil)
	if err != nil || got != nil {
		t.Fatalf("empty batch result = %v, %v; want nil, nil", got, err)
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

func TestFollowSQL_UsesDeterministicOrdersAndBatchLookup(t *testing.T) {
	raw, err := os.ReadFile("../sql/follow_queries.sql")
	if err != nil {
		t.Fatalf("read follow query source: %v", err)
	}
	query := string(raw)
	required := []string{
		"ORDER BY created_at DESC, follower_id DESC",
		"ORDER BY created_at DESC, followee_id DESC",
		"followee_id = ANY(sqlc.arg('followee_ids')::uuid[])",
	}
	for _, fragment := range required {
		if !strings.Contains(query, fragment) {
			t.Fatalf("follow queries missing %q", fragment)
		}
	}
}
