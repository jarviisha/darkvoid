package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
)

// --------------------------------------------------------------------------
// CreateUser tests
// --------------------------------------------------------------------------

func TestCreateUser_Success(t *testing.T) {
	userID := uuid.New()
	repo := &mockUserRepo{
		createUser: func(_ context.Context, u *entity.User) (*entity.User, error) {
			u.ID = userID
			return u, nil
		},
	}

	svc := newUserService(repo)
	id, err := svc.CreateUser(context.Background(), validCreateReq())

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if id != userID {
		t.Errorf("expected id %v, got %v", userID, id)
	}
}

func TestCreateUser_InvalidUsername_TooShort(t *testing.T) {
	svc := newUserService(&mockUserRepo{})
	req := validCreateReq()
	req.Username = "ab" // less than 3 chars

	_, err := svc.CreateUser(context.Background(), req)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	assertServiceErrorCode(t, err, "VALIDATION_ERROR")
}

func TestCreateUser_InvalidUsername_InvalidChars(t *testing.T) {
	svc := newUserService(&mockUserRepo{})
	req := validCreateReq()
	req.Username = "john doe" // space not allowed

	_, err := svc.CreateUser(context.Background(), req)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	assertServiceErrorCode(t, err, "VALIDATION_ERROR")
}

func TestCreateUser_InvalidEmail(t *testing.T) {
	svc := newUserService(&mockUserRepo{})
	req := validCreateReq()
	req.Email = "not-an-email"

	_, err := svc.CreateUser(context.Background(), req)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	assertServiceErrorCode(t, err, "VALIDATION_ERROR")
}

func TestCreateUser_WeakPassword(t *testing.T) {
	svc := newUserService(&mockUserRepo{})

	for name, tc := range rejectedPasswords {
		t.Run(name, func(t *testing.T) {
			req := validCreateReq()
			req.Password = tc.password

			_, err := svc.CreateUser(context.Background(), req)
			assertServiceErrorCode(t, err, tc.code)
			assertErrorField(t, err, "password")
		})
	}
}

func TestCreateUser_DuplicateUsername(t *testing.T) {
	repo := &mockUserRepo{
		existsUsername: func(_ context.Context, _ string) (bool, error) {
			return true, nil
		},
	}
	svc := newUserService(repo)

	_, err := svc.CreateUser(context.Background(), validCreateReq())
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
	assertServiceErrorCode(t, err, "CONFLICT")
}

func TestCreateUser_DuplicateEmail(t *testing.T) {
	repo := &mockUserRepo{
		existsEmail: func(_ context.Context, _ string) (bool, error) {
			return true, nil
		},
	}
	svc := newUserService(repo)

	_, err := svc.CreateUser(context.Background(), validCreateReq())
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
	assertServiceErrorCode(t, err, "CONFLICT")
}

func TestCreateUser_EmailNormalized(t *testing.T) {
	var savedEmail string
	repo := &mockUserRepo{
		createUser: func(_ context.Context, u *entity.User) (*entity.User, error) {
			savedEmail = u.Email
			u.ID = uuid.New()
			return u, nil
		},
	}
	svc := newUserService(repo)
	req := validCreateReq()
	req.Email = "JOHN@EXAMPLE.COM"

	_, err := svc.CreateUser(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if savedEmail != "john@example.com" {
		t.Errorf("expected email to be lowercased to john@example.com, got %q", savedEmail)
	}
}

func TestCreateUser_DisplayNameSaved(t *testing.T) {
	var savedDisplayName string
	repo := &mockUserRepo{
		createUser: func(_ context.Context, u *entity.User) (*entity.User, error) {
			savedDisplayName = u.DisplayName
			u.ID = uuid.New()
			return u, nil
		},
	}
	svc := newUserService(repo)
	req := validCreateReq()
	req.DisplayName = "John Doe"

	_, err := svc.CreateUser(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if savedDisplayName != "John Doe" {
		t.Errorf("expected display name %q, got %q", "John Doe", savedDisplayName)
	}
}

// --------------------------------------------------------------------------
// CreateUser — repo error paths
// --------------------------------------------------------------------------

func TestCreateUser_ExistsUsernameRepoError(t *testing.T) {
	repo := &mockUserRepo{
		existsUsername: func(_ context.Context, _ string) (bool, error) {
			return false, fmt.Errorf("db down")
		},
	}
	_, err := newUserService(repo).CreateUser(context.Background(), validCreateReq())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

func TestCreateUser_ExistsEmailRepoError(t *testing.T) {
	repo := &mockUserRepo{
		existsEmail: func(_ context.Context, _ string) (bool, error) {
			return false, fmt.Errorf("db down")
		},
	}
	_, err := newUserService(repo).CreateUser(context.Background(), validCreateReq())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

func TestCreateUser_CreateRepoError(t *testing.T) {
	repoErr := fmt.Errorf("insert failed")
	repo := &mockUserRepo{
		createUser: func(_ context.Context, _ *entity.User) (*entity.User, error) {
			return nil, repoErr
		},
	}
	_, err := newUserService(repo).CreateUser(context.Background(), validCreateReq())
	if err != repoErr {
		t.Fatalf("expected repo error to be returned as-is, got: %v", err)
	}
}

// --------------------------------------------------------------------------
// BootstrapRootUser tests
// --------------------------------------------------------------------------

func TestBootstrapRootUser_CreatesWhenEmpty(t *testing.T) {
	created := false
	repo := &mockUserRepo{
		createUser: func(_ context.Context, u *entity.User) (*entity.User, error) {
			created = true
			u.ID = uuid.New()
			return u, nil
		},
	}
	svc := newUserService(repo)

	root, ok, err := svc.BootstrapRootUser(context.Background(), "root@example.com", "RootPass123", "root", "Root User")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected bootstrap to return true")
	}
	if !created {
		t.Error("expected CreateUser to be called")
	}
	if root == nil {
		t.Fatal("expected the created root user to be returned")
	}
}

// The root account holds the admin role, so it gets the rules every other
// password gets; a value bcrypt cannot hash fails with the rule, not a 500.
func TestBootstrapRootUser_WeakPasswordRejected(t *testing.T) {
	for name, tc := range rejectedPasswords {
		t.Run(name, func(t *testing.T) {
			repo := &mockUserRepo{
				createUser: func(_ context.Context, _ *entity.User) (*entity.User, error) {
					t.Error("CreateUser must not be called with a rejected password")
					return nil, fmt.Errorf("unexpected call")
				},
			}

			_, _, err := newUserService(repo).BootstrapRootUser(context.Background(), "root@example.com", tc.password, "root", "Root User")
			assertServiceErrorCode(t, err, tc.code)
			assertErrorField(t, err, "password")
		})
	}
}

func TestBootstrapRootUser_ReturnsExistingUser(t *testing.T) {
	id := uuid.New()
	repo := &mockUserRepo{
		existsUsername: func(_ context.Context, _ string) (bool, error) {
			return true, nil
		},
		getUserByUsername: func(_ context.Context, _ string) (*entity.User, error) {
			return activeUser(id), nil
		},
		createUser: func(_ context.Context, _ *entity.User) (*entity.User, error) {
			t.Error("CreateUser must not be called when the root user exists")
			return nil, fmt.Errorf("unexpected call")
		},
	}
	svc := newUserService(repo)

	// ROOT_PASSWORD only sets the password of an account being created, so a
	// weak one must not fail the boot of an install whose root user exists.
	root, ok, err := svc.BootstrapRootUser(context.Background(), "root@example.com", "admin", "root", "Root User")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected bootstrap to return false (skip) when users exist")
	}
	if root == nil || root.ID != id {
		t.Fatalf("expected existing root user %v to be returned, got %+v", id, root)
	}
}

func TestBootstrapRootUser_ExistingLookupError(t *testing.T) {
	repo := &mockUserRepo{
		existsUsername: func(_ context.Context, _ string) (bool, error) {
			return true, nil
		},
		getUserByUsername: func(_ context.Context, _ string) (*entity.User, error) {
			return nil, fmt.Errorf("db down")
		},
	}

	root, ok, err := newUserService(repo).BootstrapRootUser(context.Background(), "root@example.com", "RootPass123", "root", "Root")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if ok || root != nil {
		t.Error("expected no user and ok=false on error")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

func TestBootstrapRootUser_ExistsUsernameError(t *testing.T) {
	repo := &mockUserRepo{
		existsUsername: func(_ context.Context, _ string) (bool, error) {
			return false, fmt.Errorf("db down")
		},
	}
	_, ok, err := newUserService(repo).BootstrapRootUser(context.Background(), "root@example.com", "RootPass123", "root", "Root")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if ok {
		t.Error("expected ok=false on error")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

func TestBootstrapRootUser_CreateError(t *testing.T) {
	repo := &mockUserRepo{
		createUser: func(_ context.Context, _ *entity.User) (*entity.User, error) {
			return nil, fmt.Errorf("insert failed")
		},
	}
	_, ok, err := newUserService(repo).BootstrapRootUser(context.Background(), "root@example.com", "RootPass123", "root", "Root")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if ok {
		t.Error("expected ok=false on error")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}
