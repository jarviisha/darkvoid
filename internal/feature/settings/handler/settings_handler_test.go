package handler

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/settings/dto"
	httputil "github.com/jarviisha/darkvoid/internal/http"
	"github.com/jarviisha/darkvoid/pkg/errors"
)

type stubService struct {
	resp       *dto.FeedSettingsResponse
	err        error
	lastReq    *dto.UpdateFeedSettingsRequest
	lastAdmin  uuid.UUID
	updateCall int
}

func (s *stubService) GetFeedSettings(context.Context) (*dto.FeedSettingsResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

func (s *stubService) UpdateFeedSettings(_ context.Context, req *dto.UpdateFeedSettingsRequest, adminID uuid.UUID) (*dto.FeedSettingsResponse, error) {
	s.updateCall++
	s.lastReq = req
	s.lastAdmin = adminID
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

func okResponse() *dto.FeedSettingsResponse {
	return &dto.FeedSettingsResponse{
		TimelineEnabled:        true,
		TimelineRolloutPercent: 25,
		TimelineMaxItems:       1000,
		TimelineTTLSeconds:     604800,
		FanoutEnabled:          true,
		FanoutMaxFollowers:     10000,
		RelationshipBonus:      10,
		RecencyScale:           20,
		DecayExponent:          1.5,
		UpdatedAt:              "2026-07-27T10:30:00Z",
	}
}

func authed(r *http.Request, adminID uuid.UUID) *http.Request {
	return r.WithContext(httputil.WithUserID(r.Context(), adminID))
}

func TestGetFeedSettings_Success(t *testing.T) {
	svc := &stubService{resp: okResponse()}
	h := NewSettingsHandler(svc)

	w := httptest.NewRecorder()
	h.GetFeedSettings(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/admin/settings/feed", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body dto.FeedSettingsResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.TimelineRolloutPercent != 25 || body.DecayExponent != 1.5 {
		t.Fatalf("body = %+v", body)
	}
}

func TestGetFeedSettings_ServiceError(t *testing.T) {
	svc := &stubService{err: errors.NewInternalError(stderrors.New("db down"))}
	h := NewSettingsHandler(svc)

	w := httptest.NewRecorder()
	h.GetFeedSettings(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/admin/settings/feed", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestUpdateFeedSettings_Success(t *testing.T) {
	svc := &stubService{resp: okResponse()}
	h := NewSettingsHandler(svc)
	admin := uuid.New()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/admin/settings/feed", strings.NewReader(`{"timeline_rollout_percent":25}`))
	w := httptest.NewRecorder()
	h.UpdateFeedSettings(w, authed(req, admin))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if svc.lastAdmin != admin {
		t.Fatalf("admin id = %s, want %s", svc.lastAdmin, admin)
	}
	if svc.lastReq.TimelineRolloutPercent == nil || *svc.lastReq.TimelineRolloutPercent != 25 {
		t.Fatalf("decoded request = %+v", svc.lastReq)
	}
}

// An unauthenticated request must not reach the service. The route sits behind
// the admin group's auth, so this only fires if that nesting is ever broken —
// which is exactly when it matters.
func TestUpdateFeedSettings_Unauthenticated(t *testing.T) {
	svc := &stubService{resp: okResponse()}
	h := NewSettingsHandler(svc)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/admin/settings/feed", strings.NewReader(`{"timeline_rollout_percent":25}`))
	w := httptest.NewRecorder()
	h.UpdateFeedSettings(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if svc.updateCall != 0 {
		t.Fatal("an unauthenticated request reached the service")
	}
}

func TestUpdateFeedSettings_InvalidBody(t *testing.T) {
	svc := &stubService{resp: okResponse()}
	h := NewSettingsHandler(svc)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/admin/settings/feed", strings.NewReader(`{"timeline_rollout_percent":`))
	w := httptest.NewRecorder()
	h.UpdateFeedSettings(w, authed(req, uuid.New()))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if svc.updateCall != 0 {
		t.Fatal("an undecodable body reached the service")
	}
}

// A JSON false must arrive as a non-nil pointer. If it decoded as "absent", the
// one edit these kill switches exist for could not be expressed over the wire.
func TestUpdateFeedSettings_FalseDecodesAsAValue(t *testing.T) {
	svc := &stubService{resp: okResponse()}
	h := NewSettingsHandler(svc)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/admin/settings/feed", strings.NewReader(`{"fanout_enabled":false}`))
	w := httptest.NewRecorder()
	h.UpdateFeedSettings(w, authed(req, uuid.New()))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if svc.lastReq.FanoutEnabled == nil {
		t.Fatal("fanout_enabled:false decoded as absent")
	}
	if *svc.lastReq.FanoutEnabled {
		t.Fatal("fanout_enabled decoded as true")
	}
}

func TestUpdateFeedSettings_ServiceValidationError(t *testing.T) {
	svc := &stubService{err: errors.NewBadRequestError("timeline_rollout_percent must be between 0 and 100")}
	h := NewSettingsHandler(svc)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/admin/settings/feed", strings.NewReader(`{"timeline_rollout_percent":150}`))
	w := httptest.NewRecorder()
	h.UpdateFeedSettings(w, authed(req, uuid.New()))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
