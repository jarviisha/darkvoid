package service

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/user/db"
	"github.com/jarviisha/darkvoid/internal/feature/user/dto"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	"github.com/jarviisha/darkvoid/pkg/errors"
)

// --------------------------------------------------------------------------
// Mock: userRepo
// --------------------------------------------------------------------------

type mockUserRepo struct {
	existsUsername       func(ctx context.Context, username string) (bool, error)
	existsEmail          func(ctx context.Context, email string) (bool, error)
	existsEmailExcluding func(ctx context.Context, email string, userID uuid.UUID) (bool, error)
	createUser           func(ctx context.Context, user *entity.User) (*entity.User, error)
	getUserByID          func(ctx context.Context, id uuid.UUID) (*entity.User, error)
	getUserByUsername    func(ctx context.Context, username string) (*entity.User, error)
	getUserByEmail       func(ctx context.Context, email string) (*entity.User, error)
	updateUser           func(ctx context.Context, id uuid.UUID, email *string, updatedBy *uuid.UUID) (*entity.User, error)
	updateUserProfile    func(ctx context.Context, id uuid.UUID, params db.UpdateUserProfileParams) (*entity.User, error)
	updateUserPassword   func(ctx context.Context, id uuid.UUID, passwordHash string, updatedBy *uuid.UUID) error
	deactivateUser       func(ctx context.Context, id uuid.UUID, updatedBy *uuid.UUID) error
}

func (m *mockUserRepo) ExistsUsername(ctx context.Context, username string) (bool, error) {
	if m.existsUsername != nil {
		return m.existsUsername(ctx, username)
	}
	return false, nil
}
func (m *mockUserRepo) ExistsEmail(ctx context.Context, email string) (bool, error) {
	if m.existsEmail != nil {
		return m.existsEmail(ctx, email)
	}
	return false, nil
}
func (m *mockUserRepo) ExistsEmailExcludingUser(ctx context.Context, email string, userID uuid.UUID) (bool, error) {
	if m.existsEmailExcluding != nil {
		return m.existsEmailExcluding(ctx, email, userID)
	}
	return false, nil
}
func (m *mockUserRepo) CreateUser(ctx context.Context, user *entity.User) (*entity.User, error) {
	if m.createUser != nil {
		return m.createUser(ctx, user)
	}
	user.ID = uuid.New()
	user.CreatedAt = time.Now()
	return user, nil
}
func (m *mockUserRepo) GetUserByID(ctx context.Context, id uuid.UUID) (*entity.User, error) {
	if m.getUserByID != nil {
		return m.getUserByID(ctx, id)
	}
	return nil, errors.ErrNotFound
}
func (m *mockUserRepo) GetUserByUsername(ctx context.Context, username string) (*entity.User, error) {
	if m.getUserByUsername != nil {
		return m.getUserByUsername(ctx, username)
	}
	return nil, errors.ErrNotFound
}
func (m *mockUserRepo) GetUserByEmail(ctx context.Context, email string) (*entity.User, error) {
	if m.getUserByEmail != nil {
		return m.getUserByEmail(ctx, email)
	}
	return nil, errors.ErrNotFound
}
func (m *mockUserRepo) UpdateUser(ctx context.Context, id uuid.UUID, email *string, updatedBy *uuid.UUID) (*entity.User, error) {
	if m.updateUser != nil {
		return m.updateUser(ctx, id, email, updatedBy)
	}
	return nil, nil //nolint:nilnil // mock returns zero values
}
func (m *mockUserRepo) UpdateUserProfile(ctx context.Context, id uuid.UUID, params db.UpdateUserProfileParams) (*entity.User, error) {
	if m.updateUserProfile != nil {
		return m.updateUserProfile(ctx, id, params)
	}
	return activeUser(id), nil
}
func (m *mockUserRepo) UpdateUserPassword(ctx context.Context, id uuid.UUID, passwordHash string, updatedBy *uuid.UUID) error {
	if m.updateUserPassword != nil {
		return m.updateUserPassword(ctx, id, passwordHash, updatedBy)
	}
	return nil
}
func (m *mockUserRepo) DeactivateUser(ctx context.Context, id uuid.UUID, updatedBy *uuid.UUID) error {
	if m.deactivateUser != nil {
		return m.deactivateUser(ctx, id, updatedBy)
	}
	return nil
}

// --------------------------------------------------------------------------
// Mock: storage.Storage (for Upload* tests)
// --------------------------------------------------------------------------

type storagePutCall struct {
	key         string
	contentType string
}

type mockStorage struct {
	mu          sync.Mutex
	putCalls    []storagePutCall
	deleteCalls []string
	putErr      error
	deleteErr   error
	deleteWG    *sync.WaitGroup // optional, Done() called on every Delete
}

func (m *mockStorage) Put(_ context.Context, key string, _ io.Reader, _ int64, contentType string) error {
	m.mu.Lock()
	m.putCalls = append(m.putCalls, storagePutCall{key: key, contentType: contentType})
	putErr := m.putErr
	m.mu.Unlock()
	return putErr
}

func (m *mockStorage) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	m.deleteCalls = append(m.deleteCalls, key)
	deleteErr := m.deleteErr
	wg := m.deleteWG
	m.mu.Unlock()
	if wg != nil {
		wg.Done()
	}
	return deleteErr
}

func (m *mockStorage) URL(key string) string { return "https://cdn.test/" + key }

// --------------------------------------------------------------------------
// Shared helpers
// --------------------------------------------------------------------------

func newUserService(repo userRepo) *UserService {
	return &UserService{userRepo: repo, storage: nil}
}

func validCreateReq() *dto.CreateUserRequest {
	return &dto.CreateUserRequest{
		Username:    "johndoe",
		Email:       "john@example.com",
		DisplayName: "John Doe",
		Password:    "SecurePass123",
	}
}

func activeUser(id uuid.UUID) *entity.User {
	return &entity.User{
		ID:          id,
		Username:    "johndoe",
		Email:       "john@example.com",
		IsActive:    true,
		DisplayName: "John Doe",
		CreatedAt:   time.Now(),
	}
}

func assertServiceErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	appErr := errors.GetAppError(err)
	if appErr == nil {
		t.Fatalf("expected AppError with code %q, got non-AppError: %v", code, err)
	}
	if appErr.Code != code {
		t.Errorf("expected error code %q, got %q", code, appErr.Code)
	}
}
