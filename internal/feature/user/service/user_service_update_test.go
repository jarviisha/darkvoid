package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/user/dto"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	"github.com/jarviisha/darkvoid/pkg/errors"
)

// --------------------------------------------------------------------------
// UpdateUser tests
// --------------------------------------------------------------------------

func TestUpdateUser_Success(t *testing.T) {
	id := uuid.New()
	newEmail := "new@example.com"
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return activeUser(id), nil
		},
		updateUser: func(_ context.Context, _ uuid.UUID, email *string, _ *uuid.UUID) (*entity.User, error) {
			u := activeUser(id)
			u.Email = *email
			return u, nil
		},
	}
	svc := newUserService(repo)

	user, err := svc.UpdateUser(context.Background(), id, &dto.UpdateUserRequest{Email: &newEmail}, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if user.Email != newEmail {
		t.Errorf("expected email %q, got %q", newEmail, user.Email)
	}
}

func TestUpdateUser_InvalidEmail(t *testing.T) {
	svc := newUserService(&mockUserRepo{})
	bad := "not-an-email"

	_, err := svc.UpdateUser(context.Background(), uuid.New(), &dto.UpdateUserRequest{Email: &bad}, nil)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	assertServiceErrorCode(t, err, "VALIDATION_ERROR")
}

func TestUpdateUser_EmptyEmail(t *testing.T) {
	svc := newUserService(&mockUserRepo{})
	empty := ""

	_, err := svc.UpdateUser(context.Background(), uuid.New(), &dto.UpdateUserRequest{Email: &empty}, nil)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	assertServiceErrorCode(t, err, "VALIDATION_ERROR")
}

func TestUpdateUser_UserNotFound(t *testing.T) {
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return nil, errors.ErrNotFound
		},
	}
	svc := newUserService(repo)
	email := "new@example.com"

	_, err := svc.UpdateUser(context.Background(), uuid.New(), &dto.UpdateUserRequest{Email: &email}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "USER_NOT_FOUND")
}

func TestUpdateUser_DuplicateEmail(t *testing.T) {
	id := uuid.New()
	email := "taken@example.com"
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return activeUser(id), nil
		},
		existsEmailExcluding: func(_ context.Context, _ string, _ uuid.UUID) (bool, error) {
			return true, nil
		},
	}
	svc := newUserService(repo)

	_, err := svc.UpdateUser(context.Background(), id, &dto.UpdateUserRequest{Email: &email}, nil)
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
	assertServiceErrorCode(t, err, "CONFLICT")
}

func TestUpdateUser_SameEmail_NoUniquenessCheck(t *testing.T) {
	id := uuid.New()
	sameEmail := "john@example.com"
	uniquenessCheckCalled := false
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return activeUser(id), nil // activeUser has email john@example.com
		},
		existsEmailExcluding: func(_ context.Context, _ string, _ uuid.UUID) (bool, error) {
			uniquenessCheckCalled = true
			return false, nil
		},
		updateUser: func(_ context.Context, _ uuid.UUID, _ *string, _ *uuid.UUID) (*entity.User, error) {
			return activeUser(id), nil
		},
	}
	svc := newUserService(repo)

	_, err := svc.UpdateUser(context.Background(), id, &dto.UpdateUserRequest{Email: &sameEmail}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uniquenessCheckCalled {
		t.Error("uniqueness check should not be called when email is unchanged")
	}
}

func TestUpdateUser_InactiveUser(t *testing.T) {
	id := uuid.New()
	email := "new@example.com"
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			u := activeUser(id)
			u.IsActive = false
			return u, nil
		},
	}
	_, err := newUserService(repo).UpdateUser(context.Background(), id, &dto.UpdateUserRequest{Email: &email}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "ACCOUNT_DISABLED")
}

func TestUpdateUser_ExistsEmailRepoError(t *testing.T) {
	id := uuid.New()
	email := "new@example.com"
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return activeUser(id), nil
		},
		existsEmailExcluding: func(_ context.Context, _ string, _ uuid.UUID) (bool, error) {
			return false, fmt.Errorf("db error")
		},
	}
	_, err := newUserService(repo).UpdateUser(context.Background(), id, &dto.UpdateUserRequest{Email: &email}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

func TestUpdateUser_UpdateRepoError(t *testing.T) {
	id := uuid.New()
	email := "new@example.com"
	repoErr := fmt.Errorf("update failed")
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return activeUser(id), nil
		},
		updateUser: func(_ context.Context, _ uuid.UUID, _ *string, _ *uuid.UUID) (*entity.User, error) {
			return nil, repoErr
		},
	}
	_, err := newUserService(repo).UpdateUser(context.Background(), id, &dto.UpdateUserRequest{Email: &email}, nil)
	if err != repoErr {
		t.Fatalf("expected repo error to pass through, got: %v", err)
	}
}

func TestUpdateUser_GetUserGenericError(t *testing.T) {
	repoErr := fmt.Errorf("db down")
	email := "new@example.com"
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return nil, repoErr
		},
	}
	_, err := newUserService(repo).UpdateUser(context.Background(), uuid.New(), &dto.UpdateUserRequest{Email: &email}, nil)
	if err != repoErr {
		t.Fatalf("expected repo error to pass through, got: %v", err)
	}
}

func TestUpdateUser_NilEmail_SkipsUniquenessCheck(t *testing.T) {
	id := uuid.New()
	uniquenessCheckCalled := false
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return activeUser(id), nil
		},
		existsEmailExcluding: func(_ context.Context, _ string, _ uuid.UUID) (bool, error) {
			uniquenessCheckCalled = true
			return false, nil
		},
		updateUser: func(_ context.Context, _ uuid.UUID, _ *string, _ *uuid.UUID) (*entity.User, error) {
			return activeUser(id), nil
		},
	}
	_, err := newUserService(repo).UpdateUser(context.Background(), id, &dto.UpdateUserRequest{Email: nil}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uniquenessCheckCalled {
		t.Error("uniqueness check must be skipped when Email is nil")
	}
}

// --------------------------------------------------------------------------
// DeactivateUser tests
// --------------------------------------------------------------------------

func TestDeactivateUser_Success(t *testing.T) {
	id := uuid.New()
	deactivateCalled := false
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return activeUser(id), nil
		},
		deactivateUser: func(_ context.Context, _ uuid.UUID, _ *uuid.UUID) error {
			deactivateCalled = true
			return nil
		},
	}
	svc := newUserService(repo)

	err := svc.DeactivateUser(context.Background(), id, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !deactivateCalled {
		t.Error("expected DeactivateUser to be called on repository")
	}
}

func TestDeactivateUser_NotFound(t *testing.T) {
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return nil, errors.ErrNotFound
		},
	}
	svc := newUserService(repo)

	err := svc.DeactivateUser(context.Background(), uuid.New(), nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "USER_NOT_FOUND")
}

func TestDeactivateUser_AlreadyInactive(t *testing.T) {
	id := uuid.New()
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			u := activeUser(id)
			u.IsActive = false
			return u, nil
		},
	}
	svc := newUserService(repo)

	err := svc.DeactivateUser(context.Background(), id, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "ACCOUNT_DISABLED")
}

func TestDeactivateUser_GetUserGenericError(t *testing.T) {
	repoErr := fmt.Errorf("db down")
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return nil, repoErr
		},
	}
	err := newUserService(repo).DeactivateUser(context.Background(), uuid.New(), nil)
	if err != repoErr {
		t.Fatalf("expected repo error to pass through, got: %v", err)
	}
}

func TestDeactivateUser_DeactivateRepoError(t *testing.T) {
	id := uuid.New()
	repoErr := fmt.Errorf("delete failed")
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return activeUser(id), nil
		},
		deactivateUser: func(_ context.Context, _ uuid.UUID, _ *uuid.UUID) error {
			return repoErr
		},
	}
	err := newUserService(repo).DeactivateUser(context.Background(), id, nil)
	if err != repoErr {
		t.Fatalf("expected repo error to pass through, got: %v", err)
	}
}
