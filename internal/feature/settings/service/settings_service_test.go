package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/settings/dto"
	"github.com/jarviisha/darkvoid/internal/feature/settings/entity"
)

type stubRepo struct {
	settings   entity.FeedSettings
	getErr     error
	updateErr  error
	lastUpdate entity.FeedSettingsUpdate
	updates    int
	gets       int
}

func (r *stubRepo) GetFeedSettings(context.Context) (*entity.FeedSettings, error) {
	r.gets++
	if r.getErr != nil {
		return nil, r.getErr
	}
	s := r.settings
	return &s, nil
}

func (r *stubRepo) UpdateFeedSettings(_ context.Context, update entity.FeedSettingsUpdate) (*entity.FeedSettings, error) {
	r.updates++
	r.lastUpdate = update
	if r.updateErr != nil {
		return nil, r.updateErr
	}
	s := r.settings
	return &s, nil
}

type recordingSink struct {
	applied []entity.FeedSettings
}

func (s *recordingSink) ApplyFeedSettings(settings entity.FeedSettings) {
	s.applied = append(s.applied, settings)
}

func newTestService(repo *stubRepo) (*SettingsService, *recordingSink) {
	sink := &recordingSink{}
	svc := NewSettingsService(repo)
	svc.WithFeedSettingsSink(sink)
	return svc, sink
}

func storedSettings() entity.FeedSettings {
	s := entity.DefaultFeedSettings()
	s.TimelineEnabled = true
	s.TimelineRolloutPercent = 25
	s.UpdatedAt = time.Date(2026, 7, 27, 10, 30, 0, 0, time.UTC)
	return s
}

func TestGetFeedSettings_Success(t *testing.T) {
	repo := &stubRepo{settings: storedSettings()}
	svc, _ := newTestService(repo)

	resp, err := svc.GetFeedSettings(context.Background())
	if err != nil {
		t.Fatalf("GetFeedSettings: %v", err)
	}
	if !resp.TimelineEnabled || resp.TimelineRolloutPercent != 25 {
		t.Fatalf("response = %+v, want the stored values", resp)
	}
	if resp.TimelineTTLSeconds != 604800 {
		t.Fatalf("ttl seconds = %d, want 604800", resp.TimelineTTLSeconds)
	}
	if resp.UpdatedAt != "2026-07-27T10:30:00Z" {
		t.Fatalf("updated_at = %q", resp.UpdatedAt)
	}
}

// GetFeedSettings reads through rather than returning the last published
// snapshot: the endpoint shows an operator what they are about to edit, and a
// local snapshot can be a refresh interval behind an edit made on another
// instance.
func TestGetFeedSettings_ReadsThroughToRepository(t *testing.T) {
	repo := &stubRepo{settings: storedSettings()}
	svc, _ := newTestService(repo)

	for range 3 {
		if _, err := svc.GetFeedSettings(context.Background()); err != nil {
			t.Fatalf("GetFeedSettings: %v", err)
		}
	}
	if repo.gets != 3 {
		t.Fatalf("repository reads = %d, want one per call", repo.gets)
	}
}

func TestGetFeedSettings_RepositoryError(t *testing.T) {
	repo := &stubRepo{getErr: errors.New("db down")}
	svc, _ := newTestService(repo)

	if _, err := svc.GetFeedSettings(context.Background()); err == nil {
		t.Fatal("expected the repository error to propagate")
	}
}

func TestUpdateFeedSettings_Success(t *testing.T) {
	repo := &stubRepo{settings: storedSettings()}
	svc, sink := newTestService(repo)
	admin := uuid.New()
	percent := int32(50)

	resp, err := svc.UpdateFeedSettings(context.Background(), &dto.UpdateFeedSettingsRequest{TimelineRolloutPercent: &percent}, admin)
	if err != nil {
		t.Fatalf("UpdateFeedSettings: %v", err)
	}
	if resp == nil {
		t.Fatal("expected a response")
	}
	if repo.lastUpdate.UpdatedBy == nil || *repo.lastUpdate.UpdatedBy != admin {
		t.Fatalf("updated_by = %v, want the acting admin", repo.lastUpdate.UpdatedBy)
	}
	if repo.lastUpdate.TimelineRolloutPercent == nil || *repo.lastUpdate.TimelineRolloutPercent != 50 {
		t.Fatalf("rollout percent = %v, want 50", repo.lastUpdate.TimelineRolloutPercent)
	}
	// Fields the request did not name must stay nil, or the COALESCE'd query would
	// write the caller's zero values over settings it never meant to touch.
	if repo.lastUpdate.TimelineEnabled != nil || repo.lastUpdate.DecayExponent != nil {
		t.Fatalf("unnamed fields must stay nil, got %+v", repo.lastUpdate)
	}
	if len(sink.applied) != 1 {
		t.Fatalf("sink applications = %d, want 1", len(sink.applied))
	}
}

// What reaches the sink is the row the database returned, not the request. The
// update names some fields and inherits the rest, so only the round trip knows
// the whole new state — publishing the request would leave the feed running on
// the named fields plus a set of zero values.
func TestUpdateFeedSettings_PublishesTheStoredRowNotTheRequest(t *testing.T) {
	stored := storedSettings()
	stored.DecayExponent = 2.5
	repo := &stubRepo{settings: stored}
	svc, sink := newTestService(repo)
	percent := int32(50)

	if _, err := svc.UpdateFeedSettings(context.Background(), &dto.UpdateFeedSettingsRequest{TimelineRolloutPercent: &percent}, uuid.New()); err != nil {
		t.Fatalf("UpdateFeedSettings: %v", err)
	}
	if len(sink.applied) != 1 {
		t.Fatalf("sink applications = %d, want 1", len(sink.applied))
	}
	if got := sink.applied[0].DecayExponent; got != 2.5 {
		t.Fatalf("published decay_exponent = %v, want the stored 2.5 — the request never named it", got)
	}
}

func TestUpdateFeedSettings_ValidationError(t *testing.T) {
	repo := &stubRepo{settings: storedSettings()}
	svc, sink := newTestService(repo)
	percent := int32(150)

	if _, err := svc.UpdateFeedSettings(context.Background(), &dto.UpdateFeedSettingsRequest{TimelineRolloutPercent: &percent}, uuid.New()); err == nil {
		t.Fatal("expected rollout percent 150 to be rejected")
	}
	if repo.updates != 0 {
		t.Fatal("a rejected update must not reach the repository")
	}
	if len(sink.applied) != 0 {
		t.Fatal("a rejected update must not be published")
	}
}

// An empty body is a 400 rather than a write. It would otherwise bump updated_at
// and record an operator against a change that altered nothing.
func TestUpdateFeedSettings_EmptyRequestRejected(t *testing.T) {
	repo := &stubRepo{settings: storedSettings()}
	svc, _ := newTestService(repo)

	if _, err := svc.UpdateFeedSettings(context.Background(), &dto.UpdateFeedSettingsRequest{}, uuid.New()); err == nil {
		t.Fatal("expected an update naming no field to be rejected")
	}
	if repo.updates != 0 {
		t.Fatal("an empty update must not reach the repository")
	}
}

// Turning a kill switch off is the edit this endpoint exists for, so false must
// survive the whole path rather than reading as "omitted".
func TestUpdateFeedSettings_FalseIsAValueNotAnOmission(t *testing.T) {
	repo := &stubRepo{settings: storedSettings()}
	svc, _ := newTestService(repo)
	off := false

	if _, err := svc.UpdateFeedSettings(context.Background(), &dto.UpdateFeedSettingsRequest{FanoutEnabled: &off}, uuid.New()); err != nil {
		t.Fatalf("UpdateFeedSettings: %v", err)
	}
	if repo.lastUpdate.FanoutEnabled == nil {
		t.Fatal("fanout_enabled=false must reach the repository, not read as unchanged")
	}
	if *repo.lastUpdate.FanoutEnabled {
		t.Fatal("fanout_enabled reached the repository as true")
	}
}

func TestUpdateFeedSettings_RepositoryError(t *testing.T) {
	repo := &stubRepo{settings: storedSettings(), updateErr: errors.New("db down")}
	svc, sink := newTestService(repo)
	percent := int32(10)

	if _, err := svc.UpdateFeedSettings(context.Background(), &dto.UpdateFeedSettingsRequest{TimelineRolloutPercent: &percent}, uuid.New()); err == nil {
		t.Fatal("expected the repository error to propagate")
	}
	if len(sink.applied) != 0 {
		t.Fatal("a failed write must not be published — the feed would run on settings the database does not hold")
	}
}

func TestRefresh_PublishesStoredSettings(t *testing.T) {
	repo := &stubRepo{settings: storedSettings()}
	svc, sink := newTestService(repo)

	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(sink.applied) != 1 {
		t.Fatalf("sink applications = %d, want 1", len(sink.applied))
	}
	if !sink.applied[0].TimelineEnabled || sink.applied[0].TimelineRolloutPercent != 25 {
		t.Fatalf("published = %+v, want the stored values", sink.applied[0])
	}
}

func TestRefresh_RepositoryError(t *testing.T) {
	repo := &stubRepo{getErr: errors.New("db down")}
	svc, sink := newTestService(repo)

	if err := svc.Refresh(context.Background()); err == nil {
		t.Fatal("expected the repository error to propagate so the caller can log it")
	}
	if len(sink.applied) != 0 {
		t.Fatal("a failed read must not publish; the last known values stand")
	}
}

// Nothing is wired in a unit test, and in production the sink is attached before
// the first Refresh. Either way this must not panic.
func TestPublish_NoSinkIsNotAPanic(t *testing.T) {
	repo := &stubRepo{settings: storedSettings()}
	svc := NewSettingsService(repo)

	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh with no sink: %v", err)
	}
}

func TestToFeedSettingsUpdate_NilRequest(t *testing.T) {
	if !toFeedSettingsUpdate(nil).IsEmpty() {
		t.Fatal("a nil request must produce an empty update, not a zero-valued one")
	}
}
