package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/user/dto"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	httputil "github.com/jarviisha/darkvoid/internal/http"
	"github.com/jarviisha/darkvoid/pkg/errors"
)

// --------------------------------------------------------------------------
// Mock: profileService
// --------------------------------------------------------------------------

type mockProfileService struct {
	getMyProfile       func(ctx context.Context, userID uuid.UUID) (*entity.User, error)
	getProfileByUserID func(ctx context.Context, userID uuid.UUID) (*entity.User, error)
	updateMyProfile    func(ctx context.Context, userID uuid.UUID, req *dto.UpdateProfileRequest) (*entity.User, error)
	uploadAvatar       func(ctx context.Context, userID uuid.UUID, r io.Reader, size int64, contentType string, ext string) (*entity.User, error)
	uploadCover        func(ctx context.Context, userID uuid.UUID, r io.Reader, size int64, contentType string, ext string) (*entity.User, error)
}

func (m *mockProfileService) GetMyProfile(ctx context.Context, userID uuid.UUID) (*entity.User, error) {
	if m.getMyProfile != nil {
		return m.getMyProfile(ctx, userID)
	}
	return nil, errors.ErrInternal
}
func (m *mockProfileService) GetProfileByUserID(ctx context.Context, userID uuid.UUID) (*entity.User, error) {
	if m.getProfileByUserID != nil {
		return m.getProfileByUserID(ctx, userID)
	}
	return nil, errors.ErrInternal
}
func (m *mockProfileService) UpdateMyProfile(ctx context.Context, userID uuid.UUID, req *dto.UpdateProfileRequest) (*entity.User, error) {
	if m.updateMyProfile != nil {
		return m.updateMyProfile(ctx, userID, req)
	}
	return nil, errors.ErrInternal
}
func (m *mockProfileService) UploadAvatar(ctx context.Context, userID uuid.UUID, r io.Reader, size int64, contentType string, ext string) (*entity.User, error) {
	if m.uploadAvatar != nil {
		return m.uploadAvatar(ctx, userID, r, size, contentType, ext)
	}
	return nil, errors.ErrInternal
}
func (m *mockProfileService) UploadCover(ctx context.Context, userID uuid.UUID, r io.Reader, size int64, contentType string, ext string) (*entity.User, error) {
	if m.uploadCover != nil {
		return m.uploadCover(ctx, userID, r, size, contentType, ext)
	}
	return nil, errors.ErrInternal
}

// --------------------------------------------------------------------------
// Mock: followChecker
// --------------------------------------------------------------------------

type mockFollowChecker struct {
	isFollowing func(ctx context.Context, followerID, followeeID uuid.UUID) (bool, error)
}

func (m *mockFollowChecker) IsFollowing(ctx context.Context, followerID, followeeID uuid.UUID) (bool, error) {
	if m.isFollowing != nil {
		return m.isFollowing(ctx, followerID, followeeID)
	}
	return false, nil
}

// --------------------------------------------------------------------------
// Test helpers
// --------------------------------------------------------------------------

func newProfileHandler(svc profileService) *ProfileHandler {
	return &ProfileHandler{profileService: svc, resolver: nopResolver, storage: nil}
}

func newProfileHandlerWithChecker(svc profileService, checker followChecker) *ProfileHandler {
	return &ProfileHandler{profileService: svc, resolver: nopResolver, followChecker: checker, storage: nil}
}

func multipartRequest(t *testing.T, rawURL, filename, content string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	_, _ = fw.Write([]byte(content))
	if err = mw.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPut, rawURL, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func sampleUserFull(id uuid.UUID) *entity.User {
	bio := "Software engineer"
	return &entity.User{
		ID:          id,
		Username:    "johndoe",
		Email:       "john@example.com",
		IsActive:    true,
		DisplayName: "John Doe",
		Bio:         &bio,
		CreatedAt:   time.Now(),
	}
}

// --------------------------------------------------------------------------
// GetMyProfile tests
// --------------------------------------------------------------------------

func TestGetMyProfile_Success(t *testing.T) {
	userID := uuid.New()
	svc := &mockProfileService{
		getMyProfile: func(_ context.Context, id uuid.UUID) (*entity.User, error) {
			return sampleUserFull(id), nil
		},
	}
	h := newProfileHandler(svc)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/users/me", nil)
	req = req.WithContext(httputil.WithUserID(req.Context(), userID))
	w := httptest.NewRecorder()
	h.GetMyProfile(w, req)

	assertStatus(t, w, http.StatusOK)

	var resp dto.UserResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Username != "johndoe" {
		t.Errorf("expected username johndoe, got %q", resp.Username)
	}
}

func TestGetMyProfile_NotAuthenticated(t *testing.T) {
	h := newProfileHandler(&mockProfileService{})

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/users/me", nil)
	w := httptest.NewRecorder()
	h.GetMyProfile(w, req)

	assertStatus(t, w, http.StatusUnauthorized)
	assertErrorCode(t, w, "UNAUTHORIZED")
}

func TestGetMyProfile_NotFound(t *testing.T) {
	userID := uuid.New()
	svc := &mockProfileService{
		getMyProfile: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return nil, errors.NewNotFoundError("user not found")
		},
	}
	h := newProfileHandler(svc)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/users/me", nil)
	req = req.WithContext(httputil.WithUserID(req.Context(), userID))
	w := httptest.NewRecorder()
	h.GetMyProfile(w, req)

	assertStatus(t, w, http.StatusNotFound)
}

// --------------------------------------------------------------------------
// UpdateMyProfile tests
// --------------------------------------------------------------------------

func TestUpdateMyProfile_Success(t *testing.T) {
	userID := uuid.New()
	bio := "Updated bio"
	svc := &mockProfileService{
		updateMyProfile: func(_ context.Context, id uuid.UUID, req *dto.UpdateProfileRequest) (*entity.User, error) {
			u := sampleUserFull(id)
			u.Bio = req.Bio
			return u, nil
		},
	}
	h := newProfileHandler(svc)

	b, _ := json.Marshal(dto.UpdateProfileRequest{Bio: &bio})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPut, "/users/me", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(httputil.WithUserID(req.Context(), userID))
	w := httptest.NewRecorder()
	h.UpdateMyProfile(w, req)

	assertStatus(t, w, http.StatusOK)
}

func TestUpdateMyProfile_NotAuthenticated(t *testing.T) {
	h := newProfileHandler(&mockProfileService{})

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPut, "/users/me", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.UpdateMyProfile(w, req)

	assertStatus(t, w, http.StatusUnauthorized)
	assertErrorCode(t, w, "UNAUTHORIZED")
}

func TestUpdateMyProfile_InvalidJSON(t *testing.T) {
	userID := uuid.New()
	h := newProfileHandler(&mockProfileService{})

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPut, "/users/me", bytes.NewBufferString("bad json"))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(httputil.WithUserID(req.Context(), userID))
	w := httptest.NewRecorder()
	h.UpdateMyProfile(w, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestUpdateMyProfile_ServiceError(t *testing.T) {
	userID := uuid.New()
	svc := &mockProfileService{
		updateMyProfile: func(_ context.Context, _ uuid.UUID, _ *dto.UpdateProfileRequest) (*entity.User, error) {
			return nil, errors.NewNotFoundError("user not found")
		},
	}
	h := newProfileHandler(svc)

	b, _ := json.Marshal(dto.UpdateProfileRequest{})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPut, "/users/me", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(httputil.WithUserID(req.Context(), userID))
	w := httptest.NewRecorder()
	h.UpdateMyProfile(w, req)

	assertStatus(t, w, http.StatusNotFound)
}

// --------------------------------------------------------------------------
// GetUserProfile tests
// --------------------------------------------------------------------------

func TestGetUserProfile_Success(t *testing.T) {
	userID := uuid.New()
	svc := &mockProfileService{
		getProfileByUserID: func(_ context.Context, id uuid.UUID) (*entity.User, error) {
			return sampleUserFull(id), nil
		},
	}
	h := newProfileHandler(svc)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, fmt.Sprintf("/users/%s/profile", userID), nil)
	req = withChiParam(req, "userKey", userID.String())
	w := httptest.NewRecorder()
	h.GetUserProfile(w, req)

	assertStatus(t, w, http.StatusOK)
}

func TestGetUserProfile_InvalidUUID(t *testing.T) {
	h := newProfileHandler(&mockProfileService{})

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/users/not-a-uuid/profile", nil)
	req = withChiParam(req, "userKey", "not-a-uuid")
	w := httptest.NewRecorder()
	h.GetUserProfile(w, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestGetUserProfile_NotFound(t *testing.T) {
	svc := &mockProfileService{
		getProfileByUserID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			return nil, errors.NewNotFoundError("user not found")
		},
	}
	h := newProfileHandler(svc)

	userID := uuid.New()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, fmt.Sprintf("/users/%s/profile", userID), nil)
	req = withChiParam(req, "userKey", userID.String())
	w := httptest.NewRecorder()
	h.GetUserProfile(w, req)

	assertStatus(t, w, http.StatusNotFound)
}

func TestGetUserProfile_ByUsername(t *testing.T) {
	userID := uuid.New()
	profileByIDCalled := false
	svc := &mockProfileService{
		getProfileByUserID: func(_ context.Context, _ uuid.UUID) (*entity.User, error) {
			profileByIDCalled = true
			return nil, errors.ErrInternal
		},
	}
	resolver := &mockUserResolver{
		getUserByUsername: func(_ context.Context, username string) (*entity.User, error) {
			if username != "johndoe" {
				return nil, errors.NewNotFoundError("user not found")
			}
			return sampleUserFull(userID), nil
		},
	}
	h := &ProfileHandler{profileService: svc, resolver: resolver, storage: nil}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/users/johndoe/profile?by=username", nil)
	req.URL, _ = url.Parse("/users/johndoe/profile?by=username")
	req = withChiParam(req, "userKey", "johndoe")
	w := httptest.NewRecorder()
	h.GetUserProfile(w, req)

	assertStatus(t, w, http.StatusOK)
	if profileByIDCalled {
		t.Error("expected GetProfileByUserID NOT to be called when resolved via username")
	}
}

func TestGetUserProfile_ByUsernameNotFound(t *testing.T) {
	resolver := &mockUserResolver{
		getUserByUsername: func(_ context.Context, _ string) (*entity.User, error) {
			return nil, errors.NewNotFoundError("user not found")
		},
	}
	h := &ProfileHandler{profileService: &mockProfileService{}, resolver: resolver, storage: nil}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/users/unknown/profile?by=username", nil)
	req.URL, _ = url.Parse("/users/unknown/profile?by=username")
	req = withChiParam(req, "userKey", "unknown")
	w := httptest.NewRecorder()
	h.GetUserProfile(w, req)

	assertStatus(t, w, http.StatusNotFound)
}

func TestGetUserProfile_IsFollowingEnriched(t *testing.T) {
	viewerID := uuid.New()
	targetID := uuid.New()

	svc := &mockProfileService{
		getProfileByUserID: func(_ context.Context, id uuid.UUID) (*entity.User, error) {
			return sampleUserFull(id), nil
		},
	}
	checker := &mockFollowChecker{
		isFollowing: func(_ context.Context, follower, followee uuid.UUID) (bool, error) {
			if follower != viewerID || followee != targetID {
				return false, nil
			}
			return true, nil
		},
	}
	h := newProfileHandlerWithChecker(svc, checker)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, fmt.Sprintf("/users/%s/profile", targetID), nil)
	req = withChiParam(req, "userKey", targetID.String())
	req = req.WithContext(httputil.WithUserID(req.Context(), viewerID))
	w := httptest.NewRecorder()
	h.GetUserProfile(w, req)

	assertStatus(t, w, http.StatusOK)

	var resp dto.ProfileResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.IsFollowing == nil {
		t.Fatal("expected is_following to be set in response")
	}
	if !*resp.IsFollowing {
		t.Error("expected is_following to be true")
	}
}

// --------------------------------------------------------------------------
// UploadAvatar tests
// --------------------------------------------------------------------------

func TestUploadAvatar_Success(t *testing.T) {
	userID := uuid.New()
	svc := &mockProfileService{
		uploadAvatar: func(_ context.Context, id uuid.UUID, _ io.Reader, _ int64, _, _ string) (*entity.User, error) {
			return sampleUserFull(id), nil
		},
	}
	h := newProfileHandler(svc)

	req := multipartRequest(t, "/me/avatar", "avatar.jpg", "fake-image-data")
	req = req.WithContext(httputil.WithUserID(req.Context(), userID))
	w := httptest.NewRecorder()
	h.UploadAvatar(w, req)

	assertStatus(t, w, http.StatusOK)
}

func TestUploadAvatar_NotAuthenticated(t *testing.T) {
	h := newProfileHandler(&mockProfileService{})

	req := multipartRequest(t, "/me/avatar", "avatar.jpg", "data")
	w := httptest.NewRecorder()
	h.UploadAvatar(w, req)

	assertStatus(t, w, http.StatusUnauthorized)
	assertErrorCode(t, w, "UNAUTHORIZED")
}

func TestUploadAvatar_MissingFile(t *testing.T) {
	userID := uuid.New()
	h := newProfileHandler(&mockProfileService{})

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.Close()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPut, "/me/avatar", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req = req.WithContext(httputil.WithUserID(req.Context(), userID))
	w := httptest.NewRecorder()
	h.UploadAvatar(w, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestUploadAvatar_ServiceError(t *testing.T) {
	userID := uuid.New()
	svc := &mockProfileService{
		uploadAvatar: func(_ context.Context, _ uuid.UUID, _ io.Reader, _ int64, _, _ string) (*entity.User, error) {
			return nil, errors.NewBadRequestError("unsupported file type")
		},
	}
	h := newProfileHandler(svc)

	req := multipartRequest(t, "/me/avatar", "avatar.gif", "data")
	req = req.WithContext(httputil.WithUserID(req.Context(), userID))
	w := httptest.NewRecorder()
	h.UploadAvatar(w, req)

	assertStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "BAD_REQUEST")
}

// --------------------------------------------------------------------------
// UploadCover tests
// --------------------------------------------------------------------------

func TestUploadCover_Success(t *testing.T) {
	userID := uuid.New()
	svc := &mockProfileService{
		uploadCover: func(_ context.Context, id uuid.UUID, _ io.Reader, _ int64, _, _ string) (*entity.User, error) {
			return sampleUserFull(id), nil
		},
	}
	h := newProfileHandler(svc)

	req := multipartRequest(t, "/me/cover", "cover.jpg", "fake-image-data")
	req = req.WithContext(httputil.WithUserID(req.Context(), userID))
	w := httptest.NewRecorder()
	h.UploadCover(w, req)

	assertStatus(t, w, http.StatusOK)
}

func TestUploadCover_NotAuthenticated(t *testing.T) {
	h := newProfileHandler(&mockProfileService{})

	req := multipartRequest(t, "/me/cover", "cover.jpg", "data")
	w := httptest.NewRecorder()
	h.UploadCover(w, req)

	assertStatus(t, w, http.StatusUnauthorized)
	assertErrorCode(t, w, "UNAUTHORIZED")
}

func TestUploadCover_MissingFile(t *testing.T) {
	userID := uuid.New()
	h := newProfileHandler(&mockProfileService{})

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.Close()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPut, "/me/cover", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req = req.WithContext(httputil.WithUserID(req.Context(), userID))
	w := httptest.NewRecorder()
	h.UploadCover(w, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestUploadCover_ServiceError(t *testing.T) {
	userID := uuid.New()
	svc := &mockProfileService{
		uploadCover: func(_ context.Context, _ uuid.UUID, _ io.Reader, _ int64, _, _ string) (*entity.User, error) {
			return nil, errors.NewBadRequestError("unsupported file type")
		},
	}
	h := newProfileHandler(svc)

	req := multipartRequest(t, "/me/cover", "cover.gif", "data")
	req = req.WithContext(httputil.WithUserID(req.Context(), userID))
	w := httptest.NewRecorder()
	h.UploadCover(w, req)

	assertStatus(t, w, http.StatusBadRequest)
	assertErrorCode(t, w, "BAD_REQUEST")
}
