package service

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/user/db"
	"github.com/jarviisha/darkvoid/internal/feature/user/dto"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	"github.com/jarviisha/darkvoid/pkg/errors"
)

func uploadReader() io.Reader { return strings.NewReader("fake-image-bytes") }

// --------------------------------------------------------------------------
// GetMyProfile tests
// --------------------------------------------------------------------------

func TestGetMyProfile_Success(t *testing.T) {
	id := uuid.New()
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return activeUser(id), nil
		},
	}
	u, err := newUserService(repo).GetMyProfile(context.Background(), id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.ID != id {
		t.Errorf("expected id %v, got %v", id, u.ID)
	}
}

func TestGetMyProfile_NotFound(t *testing.T) {
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return nil, errors.ErrNotFound
		},
	}
	_, err := newUserService(repo).GetMyProfile(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "USER_NOT_FOUND")
}

func TestGetMyProfile_GenericRepoError(t *testing.T) {
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return nil, fmt.Errorf("db down")
		},
	}
	_, err := newUserService(repo).GetMyProfile(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

// GetMyProfile intentionally does not gate on IsActive. A deactivated user
// must still be able to read their own account state (e.g. to understand why
// their account was deactivated). This differs from GetUserByID, which rejects
// inactive accounts for third-party lookups.
func TestGetMyProfile_InactiveUserStillReturnsProfile(t *testing.T) {
	id := uuid.New()
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			u := activeUser(id)
			u.IsActive = false
			return u, nil
		},
	}
	u, err := newUserService(repo).GetMyProfile(context.Background(), id)
	if err != nil {
		t.Fatalf("expected no error for own profile even when inactive, got %v", err)
	}
	if u.IsActive {
		t.Error("expected returned user to be inactive")
	}
}

// --------------------------------------------------------------------------
// GetProfileByUserID tests
// --------------------------------------------------------------------------

func TestGetProfileByUserID_Success(t *testing.T) {
	id := uuid.New()
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return activeUser(id), nil
		},
	}
	u, err := newUserService(repo).GetProfileByUserID(context.Background(), id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.ID != id {
		t.Errorf("expected id %v, got %v", id, u.ID)
	}
}

func TestGetProfileByUserID_NotFound(t *testing.T) {
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return nil, errors.ErrNotFound
		},
	}
	_, err := newUserService(repo).GetProfileByUserID(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "USER_NOT_FOUND")
}

func TestGetProfileByUserID_GenericRepoError(t *testing.T) {
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return nil, fmt.Errorf("db down")
		},
	}
	_, err := newUserService(repo).GetProfileByUserID(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

// --------------------------------------------------------------------------
// UpdateMyProfile tests
// --------------------------------------------------------------------------

func TestUpdateMyProfile_Success(t *testing.T) {
	id := uuid.New()
	bio := "Software engineer"
	updateCalled := false
	repo := &mockUserRepo{
		updateUserProfile: func(_ context.Context, _ uuid.UUID, params db.UpdateUserProfileParams) (*entity.User, error) {
			updateCalled = true
			u := activeUser(id)
			u.Bio = params.Bio
			return u, nil
		},
	}
	svc := newUserService(repo)

	u, err := svc.UpdateMyProfile(context.Background(), id, &dto.UpdateProfileRequest{Bio: &bio})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !updateCalled {
		t.Error("expected UpdateUserProfile to be called")
	}
	if u.Bio == nil || *u.Bio != bio {
		t.Errorf("expected bio %q, got %v", bio, u.Bio)
	}
}

func TestUpdateMyProfile_NotFound(t *testing.T) {
	repo := &mockUserRepo{
		updateUserProfile: func(_ context.Context, _ uuid.UUID, _ db.UpdateUserProfileParams) (*entity.User, error) {
			return nil, errors.ErrNotFound
		},
	}
	svc := newUserService(repo)

	_, err := svc.UpdateMyProfile(context.Background(), uuid.New(), &dto.UpdateProfileRequest{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "USER_NOT_FOUND")
}

func TestUpdateMyProfile_GenericRepoError(t *testing.T) {
	repo := &mockUserRepo{
		updateUserProfile: func(_ context.Context, _ uuid.UUID, _ db.UpdateUserProfileParams) (*entity.User, error) {
			return nil, fmt.Errorf("db down")
		},
	}
	_, err := newUserService(repo).UpdateMyProfile(context.Background(), uuid.New(), &dto.UpdateProfileRequest{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

// --------------------------------------------------------------------------
// UploadAvatar tests
// --------------------------------------------------------------------------

func TestUploadAvatar_Success_NoOldKey(t *testing.T) {
	id := uuid.New()
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return activeUser(id), nil // AvatarKey nil
		},
		updateUserProfile: func(_ context.Context, _ uuid.UUID, params db.UpdateUserProfileParams) (*entity.User, error) {
			u := activeUser(id)
			u.AvatarKey = params.AvatarKey
			return u, nil
		},
	}
	store := &mockStorage{}
	svc := &UserService{userRepo: repo, storage: store}

	u, err := svc.UploadAvatar(context.Background(), id, uploadReader(), 100, "image/jpeg", ".jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.putCalls) != 1 {
		t.Fatalf("expected 1 Put call, got %d", len(store.putCalls))
	}
	if !strings.HasPrefix(store.putCalls[0].key, "avatars/") || !strings.HasSuffix(store.putCalls[0].key, ".jpg") {
		t.Errorf("unexpected Put key: %q", store.putCalls[0].key)
	}
	if len(store.deleteCalls) != 0 {
		t.Errorf("expected 0 Delete calls (no old key), got %d", len(store.deleteCalls))
	}
	if u.AvatarKey == nil || *u.AvatarKey != store.putCalls[0].key {
		t.Errorf("expected user.AvatarKey=%q, got %v", store.putCalls[0].key, u.AvatarKey)
	}
}

func TestUploadAvatar_Success_WithOldKey_DeletesOldAsync(t *testing.T) {
	id := uuid.New()
	oldKey := "avatars/old-key.jpg"
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			u := activeUser(id)
			u.AvatarKey = &oldKey
			return u, nil
		},
		updateUserProfile: func(_ context.Context, _ uuid.UUID, params db.UpdateUserProfileParams) (*entity.User, error) {
			u := activeUser(id)
			u.AvatarKey = params.AvatarKey
			return u, nil
		},
	}
	var wg sync.WaitGroup
	wg.Add(1)
	store := &mockStorage{deleteWG: &wg}
	svc := &UserService{userRepo: repo, storage: store}

	_, err := svc.UploadAvatar(context.Background(), id, uploadReader(), 100, "image/jpeg", ".jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	waitForGoroutines(t, &wg)

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.deleteCalls) != 1 || store.deleteCalls[0] != oldKey {
		t.Errorf("expected 1 Delete call for old key %q, got %v", oldKey, store.deleteCalls)
	}
}

func TestUploadAvatar_UserNotFound(t *testing.T) {
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return nil, errors.ErrNotFound
		},
	}
	store := &mockStorage{}
	svc := &UserService{userRepo: repo, storage: store}

	_, err := svc.UploadAvatar(context.Background(), uuid.New(), uploadReader(), 100, "image/jpeg", ".jpg")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "USER_NOT_FOUND")
	if len(store.putCalls) != 0 {
		t.Errorf("Put should not be called when user not found, got %d", len(store.putCalls))
	}
}

func TestUploadAvatar_GetUserGenericError(t *testing.T) {
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return nil, fmt.Errorf("db down")
		},
	}
	store := &mockStorage{}
	svc := &UserService{userRepo: repo, storage: store}

	_, err := svc.UploadAvatar(context.Background(), uuid.New(), uploadReader(), 100, "image/jpeg", ".jpg")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
	if len(store.putCalls) != 0 {
		t.Errorf("Put should not be called, got %d", len(store.putCalls))
	}
}

func TestUploadAvatar_StoragePutFails(t *testing.T) {
	id := uuid.New()
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return activeUser(id), nil
		},
	}
	store := &mockStorage{putErr: fmt.Errorf("s3 unavailable")}
	svc := &UserService{userRepo: repo, storage: store}

	_, err := svc.UploadAvatar(context.Background(), id, uploadReader(), 100, "image/jpeg", ".jpg")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

func TestUploadAvatar_UpdateProfileFails_DeletesNewUpload(t *testing.T) {
	id := uuid.New()
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return activeUser(id), nil
		},
		updateUserProfile: func(_ context.Context, _ uuid.UUID, _ db.UpdateUserProfileParams) (*entity.User, error) {
			return nil, fmt.Errorf("db down")
		},
	}
	store := &mockStorage{}
	svc := &UserService{userRepo: repo, storage: store}

	_, err := svc.UploadAvatar(context.Background(), id, uploadReader(), 100, "image/jpeg", ".jpg")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.putCalls) != 1 {
		t.Fatalf("expected 1 Put, got %d", len(store.putCalls))
	}
	if len(store.deleteCalls) != 1 || store.deleteCalls[0] != store.putCalls[0].key {
		t.Errorf("expected orphaned upload %q to be deleted, got deleteCalls=%v", store.putCalls[0].key, store.deleteCalls)
	}
}

// --------------------------------------------------------------------------
// UploadCover tests
// --------------------------------------------------------------------------

func TestUploadCover_Success_NoOldKey(t *testing.T) {
	id := uuid.New()
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return activeUser(id), nil
		},
		updateUserProfile: func(_ context.Context, _ uuid.UUID, params db.UpdateUserProfileParams) (*entity.User, error) {
			u := activeUser(id)
			u.CoverKey = params.CoverKey
			return u, nil
		},
	}
	store := &mockStorage{}
	svc := &UserService{userRepo: repo, storage: store}

	u, err := svc.UploadCover(context.Background(), id, uploadReader(), 100, "image/png", ".png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.putCalls) != 1 {
		t.Fatalf("expected 1 Put call, got %d", len(store.putCalls))
	}
	if !strings.HasPrefix(store.putCalls[0].key, "covers/") || !strings.HasSuffix(store.putCalls[0].key, ".png") {
		t.Errorf("unexpected Put key: %q", store.putCalls[0].key)
	}
	if len(store.deleteCalls) != 0 {
		t.Errorf("expected 0 Delete calls (no old key), got %d", len(store.deleteCalls))
	}
	if u.CoverKey == nil || *u.CoverKey != store.putCalls[0].key {
		t.Errorf("expected user.CoverKey=%q, got %v", store.putCalls[0].key, u.CoverKey)
	}
}

func TestUploadCover_Success_WithOldKey_DeletesOldAsync(t *testing.T) {
	id := uuid.New()
	oldKey := "covers/old-cover.png"
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			u := activeUser(id)
			u.CoverKey = &oldKey
			return u, nil
		},
		updateUserProfile: func(_ context.Context, _ uuid.UUID, params db.UpdateUserProfileParams) (*entity.User, error) {
			u := activeUser(id)
			u.CoverKey = params.CoverKey
			return u, nil
		},
	}
	var wg sync.WaitGroup
	wg.Add(1)
	store := &mockStorage{deleteWG: &wg}
	svc := &UserService{userRepo: repo, storage: store}

	_, err := svc.UploadCover(context.Background(), id, uploadReader(), 100, "image/png", ".png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	waitForGoroutines(t, &wg)

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.deleteCalls) != 1 || store.deleteCalls[0] != oldKey {
		t.Errorf("expected 1 Delete call for old key %q, got %v", oldKey, store.deleteCalls)
	}
}

func TestUploadCover_UserNotFound(t *testing.T) {
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return nil, errors.ErrNotFound
		},
	}
	store := &mockStorage{}
	svc := &UserService{userRepo: repo, storage: store}

	_, err := svc.UploadCover(context.Background(), uuid.New(), uploadReader(), 100, "image/png", ".png")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "USER_NOT_FOUND")
	if len(store.putCalls) != 0 {
		t.Errorf("Put should not be called when user not found, got %d", len(store.putCalls))
	}
}

func TestUploadCover_GetUserGenericError(t *testing.T) {
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return nil, fmt.Errorf("db down")
		},
	}
	store := &mockStorage{}
	svc := &UserService{userRepo: repo, storage: store}

	_, err := svc.UploadCover(context.Background(), uuid.New(), uploadReader(), 100, "image/png", ".png")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
	if len(store.putCalls) != 0 {
		t.Errorf("Put should not be called, got %d", len(store.putCalls))
	}
}

func TestUploadCover_StoragePutFails(t *testing.T) {
	id := uuid.New()
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return activeUser(id), nil
		},
	}
	store := &mockStorage{putErr: fmt.Errorf("s3 unavailable")}
	svc := &UserService{userRepo: repo, storage: store}

	_, err := svc.UploadCover(context.Background(), id, uploadReader(), 100, "image/png", ".png")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")
}

func TestUploadCover_UpdateProfileFails_DeletesNewUpload(t *testing.T) {
	id := uuid.New()
	repo := &mockUserRepo{
		getUserByID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return activeUser(id), nil
		},
		updateUserProfile: func(_ context.Context, _ uuid.UUID, _ db.UpdateUserProfileParams) (*entity.User, error) {
			return nil, fmt.Errorf("db down")
		},
	}
	store := &mockStorage{}
	svc := &UserService{userRepo: repo, storage: store}

	_, err := svc.UploadCover(context.Background(), id, uploadReader(), 100, "image/png", ".png")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertServiceErrorCode(t, err, "INTERNAL_ERROR")

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.putCalls) != 1 {
		t.Fatalf("expected 1 Put, got %d", len(store.putCalls))
	}
	if len(store.deleteCalls) != 1 || store.deleteCalls[0] != store.putCalls[0].key {
		t.Errorf("expected orphaned upload %q to be deleted, got deleteCalls=%v", store.putCalls[0].key, store.deleteCalls)
	}
}
