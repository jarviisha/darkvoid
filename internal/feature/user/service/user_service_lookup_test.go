package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	"github.com/jarviisha/darkvoid/pkg/errors"
)

// --------------------------------------------------------------------------
// GetUserByID tests
// --------------------------------------------------------------------------

func TestGetUserByID_Success(t *testing.T) {
	id := uuid.New()
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return activeUser(id), nil
		},
	}
	svc := newUserService(repo)

	user, err := svc.GetUserByID(context.Background(), id)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if user.ID != id {
		t.Errorf("expected id %v, got %v", id, user.ID)
	}
}

func TestGetUserByID_NotFound(t *testing.T) {
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return nil, errors.ErrNotFound
		},
	}
	svc := newUserService(repo)

	_, err := svc.GetUserByID(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "USER_NOT_FOUND")
}

func TestGetUserByID_InactiveUser(t *testing.T) {
	id := uuid.New()
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			u := activeUser(id)
			u.IsActive = false
			return u, nil
		},
	}
	svc := newUserService(repo)

	_, err := svc.GetUserByID(context.Background(), id)
	if err == nil {
		t.Fatal("expected error for inactive user, got nil")
	}
	assertServiceErrorCode(t, err, "ACCOUNT_DISABLED")
}

func TestGetUserByID_GenericRepoError(t *testing.T) {
	repoErr := fmt.Errorf("connection lost")
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return nil, repoErr
		},
	}
	_, err := newUserService(repo).GetUserByID(context.Background(), uuid.New())
	if err != repoErr {
		t.Fatalf("expected repo error to pass through, got: %v", err)
	}
}

// --------------------------------------------------------------------------
// GetUserByUsername tests
// --------------------------------------------------------------------------

func TestGetUserByUsername_Success(t *testing.T) {
	id := uuid.New()
	repo := &mockUserRepo{
		getUserByUsername: func(_ context.Context, _ string) (*entity.User, error) {
			return activeUser(id), nil
		},
	}
	svc := newUserService(repo)

	user, err := svc.GetUserByUsername(context.Background(), "johndoe")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if user.ID != id {
		t.Errorf("expected id %v, got %v", id, user.ID)
	}
}

func TestGetUserByUsername_EmptyUsername(t *testing.T) {
	svc := newUserService(&mockUserRepo{})

	_, err := svc.GetUserByUsername(context.Background(), "")
	if err == nil {
		t.Fatal("expected validation error for empty username, got nil")
	}
}

func TestGetUserByUsername_NotFound(t *testing.T) {
	repo := &mockUserRepo{
		getUserByUsername: func(_ context.Context, _ string) (*entity.User, error) {
			return nil, errors.ErrNotFound
		},
	}
	svc := newUserService(repo)

	_, err := svc.GetUserByUsername(context.Background(), "nobody")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "USER_NOT_FOUND")
}

func TestGetUserByUsername_InactiveUser(t *testing.T) {
	id := uuid.New()
	repo := &mockUserRepo{
		getUserByUsername: func(_ context.Context, _ string) (*entity.User, error) {
			u := activeUser(id)
			u.IsActive = false
			return u, nil
		},
	}
	_, err := newUserService(repo).GetUserByUsername(context.Background(), "johndoe")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "ACCOUNT_DISABLED")
}

func TestGetUserByUsername_GenericRepoError(t *testing.T) {
	repoErr := fmt.Errorf("db timeout")
	repo := &mockUserRepo{
		getUserByUsername: func(_ context.Context, _ string) (*entity.User, error) {
			return nil, repoErr
		},
	}
	_, err := newUserService(repo).GetUserByUsername(context.Background(), "johndoe")
	if err != repoErr {
		t.Fatalf("expected repo error to pass through, got: %v", err)
	}
}

// --------------------------------------------------------------------------
// GetUserByEmail tests
// --------------------------------------------------------------------------

func TestGetUserByEmail_Success(t *testing.T) {
	id := uuid.New()
	repo := &mockUserRepo{
		getUserByEmail: func(_ context.Context, _ string) (*entity.User, error) {
			return activeUser(id), nil
		},
	}
	u, err := newUserService(repo).GetUserByEmail(context.Background(), "john@example.com")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if u.ID != id {
		t.Errorf("expected id %v, got %v", id, u.ID)
	}
}

func TestGetUserByEmail_EmptyEmail(t *testing.T) {
	svc := newUserService(&mockUserRepo{})

	_, err := svc.GetUserByEmail(context.Background(), "")
	if err == nil {
		t.Fatal("expected validation error for empty email, got nil")
	}
}

func TestGetUserByEmail_NotFound(t *testing.T) {
	repo := &mockUserRepo{
		getUserByEmail: func(_ context.Context, _ string) (*entity.User, error) {
			return nil, errors.ErrNotFound
		},
	}
	svc := newUserService(repo)

	_, err := svc.GetUserByEmail(context.Background(), "nobody@example.com")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "USER_NOT_FOUND")
}

func TestGetUserByEmail_InactiveUser(t *testing.T) {
	id := uuid.New()
	repo := &mockUserRepo{
		getUserByEmail: func(_ context.Context, _ string) (*entity.User, error) {
			u := activeUser(id)
			u.IsActive = false
			return u, nil
		},
	}
	_, err := newUserService(repo).GetUserByEmail(context.Background(), "john@example.com")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "ACCOUNT_DISABLED")
}

func TestGetUserByEmail_GenericRepoError(t *testing.T) {
	repoErr := fmt.Errorf("db timeout")
	repo := &mockUserRepo{
		getUserByEmail: func(_ context.Context, _ string) (*entity.User, error) {
			return nil, repoErr
		},
	}
	_, err := newUserService(repo).GetUserByEmail(context.Background(), "john@example.com")
	if err != repoErr {
		t.Fatalf("expected repo error to pass through, got: %v", err)
	}
}

func TestGetUserByEmail_EmailNormalized(t *testing.T) {
	var queriedEmail string
	repo := &mockUserRepo{
		getUserByEmail: func(_ context.Context, email string) (*entity.User, error) {
			queriedEmail = email
			return activeUser(uuid.New()), nil
		},
	}
	_, err := newUserService(repo).GetUserByEmail(context.Background(), "  JOHN@Example.COM  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if queriedEmail != "john@example.com" {
		t.Errorf("expected repo to receive normalized email, got %q", queriedEmail)
	}
}
