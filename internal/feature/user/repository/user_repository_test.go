package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jarviisha/darkvoid/internal/feature/user/db"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	apperrors "github.com/jarviisha/darkvoid/pkg/errors"
)

// --------------------------------------------------------------------------
// dbUserToEntity mapping
// --------------------------------------------------------------------------

func TestDbUserToEntity_NoOptionalFields(t *testing.T) {
	id := uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)

	dbUser := db.UsrUser{
		ID:           id,
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: "hash",
		IsActive:     true,
		DisplayName:  "Alice",
		CreatedAt:    pgtype.Timestamp{Time: now, Valid: true},
	}

	u := dbUserToEntity(dbUser)

	if u.ID != id {
		t.Errorf("ID mismatch: want %v got %v", id, u.ID)
	}
	if u.Username != "alice" {
		t.Errorf("Username mismatch")
	}
	if u.UpdatedAt != nil {
		t.Errorf("UpdatedAt should be nil when DB field invalid")
	}
	if u.CreatedBy != nil {
		t.Errorf("CreatedBy should be nil when DB field invalid")
	}
	if u.UpdatedBy != nil {
		t.Errorf("UpdatedBy should be nil when DB field invalid")
	}
	if !u.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt mismatch: want %v got %v", now, u.CreatedAt)
	}
}

func TestDbUserToEntity_UpdatedAtSet(t *testing.T) {
	ts := time.Now().UTC().Truncate(time.Microsecond)
	dbUser := db.UsrUser{
		UpdatedAt: pgtype.Timestamp{Time: ts, Valid: true},
	}

	u := dbUserToEntity(dbUser)

	if u.UpdatedAt == nil {
		t.Fatal("UpdatedAt should not be nil")
	}
	if !u.UpdatedAt.Equal(ts) {
		t.Errorf("UpdatedAt mismatch: want %v got %v", ts, *u.UpdatedAt)
	}
}

func TestDbUserToEntity_CreatedBySet(t *testing.T) {
	adminID := uuid.New()
	dbUser := db.UsrUser{
		CreatedBy: pgtype.UUID{Bytes: adminID, Valid: true},
	}

	u := dbUserToEntity(dbUser)

	if u.CreatedBy == nil {
		t.Fatal("CreatedBy should not be nil")
	}
	if *u.CreatedBy != adminID {
		t.Errorf("CreatedBy mismatch: want %v got %v", adminID, *u.CreatedBy)
	}
}

func TestDbUserToEntity_UpdatedBySet(t *testing.T) {
	adminID := uuid.New()
	dbUser := db.UsrUser{
		UpdatedBy: pgtype.UUID{Bytes: adminID, Valid: true},
	}

	u := dbUserToEntity(dbUser)

	if u.UpdatedBy == nil {
		t.Fatal("UpdatedBy should not be nil")
	}
	if *u.UpdatedBy != adminID {
		t.Errorf("UpdatedBy mismatch: want %v got %v", adminID, *u.UpdatedBy)
	}
}

func TestDbUserToEntity_ProfileFields(t *testing.T) {
	bio := "hello world"
	avatar := "avatars/a.png"
	dbUser := db.UsrUser{
		Bio:            &bio,
		AvatarKey:      &avatar,
		FollowerCount:  42,
		FollowingCount: 7,
	}

	u := dbUserToEntity(dbUser)

	if u.Bio == nil || *u.Bio != bio {
		t.Errorf("Bio mismatch")
	}
	if u.AvatarKey == nil || *u.AvatarKey != avatar {
		t.Errorf("AvatarKey mismatch")
	}
	if u.FollowerCount != 42 {
		t.Errorf("FollowerCount: want 42 got %d", u.FollowerCount)
	}
	if u.FollowingCount != 7 {
		t.Errorf("FollowingCount: want 7 got %d", u.FollowingCount)
	}
}

// --------------------------------------------------------------------------
// UserRepository — error propagation
// --------------------------------------------------------------------------

func TestUserRepository_GetUserByID_Success(t *testing.T) {
	id := uuid.New()
	now := time.Now()

	q := &mockQuerier{
		getUserByID: func(_ context.Context, got uuid.UUID) (db.UsrUser, error) {
			if got != id {
				t.Errorf("wrong id: want %v got %v", id, got)
			}
			return db.UsrUser{ID: id, Username: "bob", CreatedAt: pgtype.Timestamp{Time: now, Valid: true}}, nil
		},
	}

	repo := &UserRepository{queries: q}
	user, err := repo.GetUserByID(context.Background(), id)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.ID != id {
		t.Errorf("ID mismatch: want %v got %v", id, user.ID)
	}
}

func TestUserRepository_GetUserByID_NotFound(t *testing.T) {
	q := &mockQuerier{
		getUserByID: func(_ context.Context, _ uuid.UUID) (db.UsrUser, error) {
			return db.UsrUser{}, pgx.ErrNoRows
		},
	}

	repo := &UserRepository{queries: q}
	_, err := repo.GetUserByID(context.Background(), uuid.New())

	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUserRepository_CreateUser_Conflict(t *testing.T) {
	q := &mockQuerier{
		createUser: func(_ context.Context, _ db.CreateUserParams) (db.UsrUser, error) {
			return db.UsrUser{}, &pgconn.PgError{Code: "23505", ConstraintName: "users_email_key"}
		},
	}

	repo := &UserRepository{queries: q}
	_, err := repo.CreateUser(context.Background(), minimalUser())

	appErr := apperrors.GetAppError(err)
	if appErr == nil {
		t.Fatalf("expected AppError, got %v", err)
	}
	if appErr.Code != "CONFLICT" {
		t.Errorf("expected CONFLICT, got %s", appErr.Code)
	}
}

func TestUserRepository_ExistsEmail_DBError(t *testing.T) {
	sentinel := errors.New("connection reset")
	q := &mockQuerier{
		existsEmail: func(_ context.Context, _ string) (bool, error) {
			return false, sentinel
		},
	}

	repo := &UserRepository{queries: q}
	_, err := repo.ExistsEmail(context.Background(), "x@x.com")

	appErr := apperrors.GetAppError(err)
	if appErr == nil {
		t.Fatalf("expected AppError, got %v", err)
	}
	if appErr.Code != "INTERNAL_ERROR" {
		t.Errorf("expected INTERNAL_ERROR, got %s", appErr.Code)
	}
}

func TestUserRepository_GetUsersByIDs_EmptySlice(t *testing.T) {
	q := &mockQuerier{
		getUsersByIDs: func(_ context.Context, ids []uuid.UUID) ([]db.UsrUser, error) {
			return []db.UsrUser{}, nil
		},
	}

	repo := &UserRepository{queries: q}
	users, err := repo.GetUsersByIDs(context.Background(), []uuid.UUID{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("expected empty slice, got %d users", len(users))
	}
}

// --------------------------------------------------------------------------
// helpers
// --------------------------------------------------------------------------

func minimalUser() *entity.User {
	return &entity.User{
		Username:     "testuser",
		Email:        "test@example.com",
		PasswordHash: "hash",
		DisplayName:  "Test User",
	}
}
