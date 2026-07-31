package service

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/bot/dto"
	"github.com/jarviisha/darkvoid/internal/feature/bot/entity"
	"github.com/jarviisha/darkvoid/internal/pagination"
	"github.com/jarviisha/darkvoid/pkg/errors"
)

// ─── Fakes ───────────────────────────────────────────────────────────────────

// fakeStore is an in-memory botStore. Fields prefixed with "got" record what the
// service asked for, so tests can assert on the call and not just the result.
type fakeStore struct {
	bots   []*entity.Bot
	config *entity.Config
	topics []*entity.Topic
	runs   []*entity.Run
	stats  []*entity.RunStats

	getBotErr     error
	createBotErr  error
	updateBotErr  error
	configErr     error
	clearErr      error
	requestRunErr error
	pruneErr      error

	gotStatsSince   time.Time
	gotPruneBefore  *time.Time
	gotEnabledLimit int32
	gotConfigUpdate *entity.ConfigUpdate
	gotCreatedRun   *entity.NewRun
	gotCleared      []uuid.UUID
	gotLinked       map[uuid.UUID]uuid.UUID
	gotRunFilter    struct {
		botID  *uuid.UUID
		limit  int32
		offset int32
	}
}

func (f *fakeStore) ListBots(context.Context) ([]*entity.Bot, error) { return f.bots, nil }

func (f *fakeStore) GetBot(_ context.Context, id uuid.UUID) (*entity.Bot, error) {
	if f.getBotErr != nil {
		return nil, f.getBotErr
	}
	for _, b := range f.bots {
		if b.ID == id {
			return b, nil
		}
	}
	return nil, errors.ErrNotFound
}

func (f *fakeStore) GetBotByUsername(_ context.Context, username string) (*entity.Bot, error) {
	for _, b := range f.bots {
		if b.Username == username {
			return b, nil
		}
	}
	return nil, errors.ErrNotFound
}

// ListEnabledBots mirrors the query's ordering — pending run requests first, then by
// username — because that ordering is what makes run-now reach the plan for a
// persona sorting past the cap.
func (f *fakeStore) ListEnabledBots(_ context.Context, limit int32) ([]*entity.Bot, error) {
	f.gotEnabledLimit = limit

	enabled := make([]*entity.Bot, 0, len(f.bots))
	for _, b := range f.bots {
		if b.Enabled {
			enabled = append(enabled, b)
		}
	}
	slices.SortStableFunc(enabled, func(a, b *entity.Bot) int {
		if a.RunRequested() != b.RunRequested() {
			if a.RunRequested() {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Username, b.Username)
	})

	if int(limit) < len(enabled) {
		enabled = enabled[:limit]
	}
	return enabled, nil
}

func (f *fakeStore) CreateBot(_ context.Context, username, displayName, style string) (*entity.Bot, error) {
	if f.createBotErr != nil {
		return nil, f.createBotErr
	}
	b := &entity.Bot{ID: uuid.New(), Username: username, DisplayName: displayName, Style: style, Enabled: true}
	f.bots = append(f.bots, b)
	return b, nil
}

func (f *fakeStore) UpdateBot(_ context.Context, id uuid.UUID, displayName, style *string, enabled *bool) (*entity.Bot, error) {
	if f.updateBotErr != nil {
		return nil, f.updateBotErr
	}
	for _, b := range f.bots {
		if b.ID != id {
			continue
		}
		if displayName != nil {
			b.DisplayName = *displayName
		}
		if style != nil {
			b.Style = *style
		}
		if enabled != nil {
			b.Enabled = *enabled
		}
		// Mirrors the CASE in the query: disabling retires any pending run-now,
		// which can no longer fire once the persona is out of the plan.
		if !b.Enabled {
			b.RunRequestedAt = nil
		}
		return b, nil
	}
	return nil, errors.ErrNotFound
}

func (f *fakeStore) DeleteBot(_ context.Context, id uuid.UUID) error {
	for i, b := range f.bots {
		if b.ID == id {
			f.bots = append(f.bots[:i], f.bots[i+1:]...)
			return nil
		}
	}
	return errors.ErrNotFound
}

func (f *fakeStore) LinkBotUser(_ context.Context, id, userID uuid.UUID) error {
	if f.gotLinked == nil {
		f.gotLinked = map[uuid.UUID]uuid.UUID{}
	}
	f.gotLinked[id] = userID
	return nil
}

func (f *fakeStore) RequestBotRun(_ context.Context, id uuid.UUID) (*entity.Bot, error) {
	if f.requestRunErr != nil {
		return nil, f.requestRunErr
	}
	for _, b := range f.bots {
		// Mirrors the write's `AND enabled` predicate: a disabled persona matches
		// no rows, exactly like a deleted one.
		if b.ID == id && b.Enabled {
			now := time.Now()
			b.RunRequestedAt = &now
			return b, nil
		}
	}
	return nil, errors.ErrNotFound
}

func (f *fakeStore) ClearBotRunRequest(_ context.Context, id uuid.UUID) error {
	if f.clearErr != nil {
		return f.clearErr
	}
	f.gotCleared = append(f.gotCleared, id)
	return nil
}

func (f *fakeStore) GetConfig(context.Context) (*entity.Config, error) {
	if f.configErr != nil {
		return nil, f.configErr
	}
	return f.config, nil
}

func (f *fakeStore) UpdateConfig(_ context.Context, update entity.ConfigUpdate) (*entity.Config, error) {
	f.gotConfigUpdate = &update
	if f.configErr != nil {
		return nil, f.configErr
	}
	if update.PostInterval != nil {
		f.config.PostInterval = *update.PostInterval
	}
	if update.Accounts != nil {
		f.config.Accounts = *update.Accounts
	}
	switch {
	case update.Models == nil:
		// pgx sends NULL for a nil slice, so COALESCE keeps the stored chain.
	case len(update.Models) == 0:
		// pgx encodes a non-nil empty slice as '{}', which COALESCE applies and
		// CHECK (cardinality(models) > 0) rejects. The fake must fail here like
		// the real store does, so the service guard is what keeps the empty-models
		// test green — not the fake quietly re-implementing the guard.
		return nil, errors.NewBadRequestError("violates check constraint on config.models")
	default:
		f.config.Models = update.Models
	}
	if update.Paused != nil {
		f.config.Paused = *update.Paused
	}
	// PromptTemplate is applied on a non-nil pointer rather than a non-empty string:
	// '' is the documented way back to the bot's built-in prompt, so the fake has to
	// distinguish "omitted" from "cleared" exactly as the COALESCE does.
	if update.PromptTemplate != nil {
		f.config.PromptTemplate = *update.PromptTemplate
	}
	if update.Temperature != nil {
		f.config.Temperature = *update.Temperature
	}
	if update.MaxTagsPerPost != nil {
		f.config.MaxTagsPerPost = *update.MaxTagsPerPost
	}
	if update.RecentMemory != nil {
		f.config.RecentMemory = *update.RecentMemory
	}
	if update.APITimeout != nil {
		f.config.APITimeout = *update.APITimeout
	}
	if update.GeminiTimeout != nil {
		f.config.GeminiTimeout = *update.GeminiTimeout
	}
	return f.config, nil
}

func (f *fakeStore) ListTopics(context.Context) ([]*entity.Topic, error) { return f.topics, nil }

func (f *fakeStore) ListEnabledTopics(context.Context) ([]*entity.Topic, error) {
	out := make([]*entity.Topic, 0, len(f.topics))
	for _, t := range f.topics {
		if t.Enabled {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeStore) CreateTopic(_ context.Context, content string) (*entity.Topic, error) {
	t := &entity.Topic{ID: uuid.New(), Content: content, Enabled: true}
	f.topics = append(f.topics, t)
	return t, nil
}

func (f *fakeStore) SetTopicEnabled(_ context.Context, id uuid.UUID, enabled bool) (*entity.Topic, error) {
	for _, t := range f.topics {
		if t.ID == id {
			t.Enabled = enabled
			return t, nil
		}
	}
	return nil, errors.ErrNotFound
}

func (f *fakeStore) DeleteTopic(_ context.Context, id uuid.UUID) error {
	for i, tp := range f.topics {
		if tp.ID == id {
			f.topics = append(f.topics[:i], f.topics[i+1:]...)
			return nil
		}
	}
	// Mirrors the :execrows contract: a zero-row delete surfaces as not-found.
	return errors.ErrNotFound
}

func (f *fakeStore) CreateRun(_ context.Context, run entity.NewRun) (*entity.Run, error) {
	f.gotCreatedRun = &run
	created := &entity.Run{
		ID: uuid.New(), BotID: run.BotID, PostID: run.PostID,
		ModelUsed: run.ModelUsed, Status: run.Status, Error: run.Error,
	}
	f.runs = append(f.runs, created)
	return created, nil
}

func (f *fakeStore) ListRuns(_ context.Context, botID *uuid.UUID, limit, offset int32) ([]*entity.Run, error) {
	f.gotRunFilter.botID = botID
	f.gotRunFilter.limit = limit
	f.gotRunFilter.offset = offset
	return f.runs, nil
}

func (f *fakeStore) CountRuns(context.Context, *uuid.UUID) (int64, error) {
	return int64(len(f.runs)), nil
}

func (f *fakeStore) ListRunStats(_ context.Context, since time.Time) ([]*entity.RunStats, error) {
	f.gotStatsSince = since
	return f.stats, nil
}

func (f *fakeStore) PruneRuns(_ context.Context, before time.Time) (int64, error) {
	f.gotPruneBefore = &before
	if f.pruneErr != nil {
		return 0, f.pruneErr
	}
	kept := f.runs[:0]
	var deleted int64
	for _, r := range f.runs {
		if r.CreatedAt.Before(before) {
			deleted++
			continue
		}
		kept = append(kept, r)
	}
	f.runs = kept
	return deleted, nil
}

// fakePostReader returns canned previews, or an error to prove the log degrades.
type fakePostReader struct {
	previews map[uuid.UUID]string
	err      error
	gotIDs   []uuid.UUID
}

func (f *fakePostReader) GetPostPreviews(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	f.gotIDs = ids
	if f.err != nil {
		return nil, f.err
	}
	return f.previews, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func newTestStore() *fakeStore {
	return &fakeStore{
		config: &entity.Config{
			PostInterval:   30 * time.Second,
			Accounts:       3,
			Models:         []string{"gemini-2.5-flash"},
			Temperature:    1.0,
			MaxTagsPerPost: 3,
			RecentMemory:   5,
			APITimeout:     15 * time.Second,
			GeminiTimeout:  60 * time.Second,
		},
	}
}

// ptr is for the partial-update DTOs, whose optional fields are pointers so that an
// omitted field and a deliberate zero stay distinguishable.
func ptr[T any](v T) *T { return &v }

func bot(username string, enabled bool) *entity.Bot {
	return &entity.Bot{ID: uuid.New(), Username: username, DisplayName: username, Style: "s", Enabled: enabled}
}

func topic(content string, enabled bool) *entity.Topic {
	return &entity.Topic{ID: uuid.New(), Content: content, Enabled: enabled}
}

func statusOf(t *testing.T, err error) int {
	t.Helper()
	appErr := errors.GetAppError(err)
	if appErr == nil {
		t.Fatalf("expected an AppError, got %v", err)
	}
	return appErr.HTTPStatus
}

// ─── GetPlan ─────────────────────────────────────────────────────────────────

func TestGetPlan_CapsPersonasAtConfiguredAccounts(t *testing.T) {
	store := newTestStore()
	store.config.Accounts = 2
	store.bots = []*entity.Bot{bot("bot_a", true), bot("bot_b", true), bot("bot_c", true)}
	store.topics = []*entity.Topic{topic("cà phê", true)}

	plan, err := NewBotService(store).GetPlan(context.Background())
	if err != nil {
		t.Fatalf("GetPlan() error = %v", err)
	}

	if store.gotEnabledLimit != 2 {
		t.Errorf("limit passed to the store = %d, want 2", store.gotEnabledLimit)
	}
	if len(plan.Bots) != 2 {
		t.Fatalf("plan has %d personas, want 2 — the bot applies no cap of its own", len(plan.Bots))
	}
}

// The generation knobs travel on the plan, not on a separate endpoint. If any of
// them is dropped here, the bot silently falls back to its built-in default and an
// operator's edit does nothing — with the admin UI still showing it as saved.
func TestGetPlan_CarriesTheGenerationKnobs(t *testing.T) {
	store := newTestStore()
	store.config.PromptTemplate = "viết như {{.DisplayName}}"
	store.config.Temperature = 0.3
	store.config.MaxTagsPerPost = 2
	store.config.RecentMemory = 7
	store.config.APITimeout = 11 * time.Second
	store.config.GeminiTimeout = 45 * time.Second
	store.bots = []*entity.Bot{bot("bot_a", true)}
	store.topics = []*entity.Topic{topic("cà phê", true)}

	plan, err := NewBotService(store).GetPlan(context.Background())
	if err != nil {
		t.Fatalf("GetPlan() error = %v", err)
	}

	if plan.PromptTemplate != "viết như {{.DisplayName}}" {
		t.Errorf("PromptTemplate = %q", plan.PromptTemplate)
	}
	if plan.Temperature != 0.3 {
		t.Errorf("Temperature = %v, want 0.3", plan.Temperature)
	}
	if plan.MaxTagsPerPost != 2 || plan.RecentMemory != 7 {
		t.Errorf("MaxTagsPerPost/RecentMemory = %d/%d, want 2/7", plan.MaxTagsPerPost, plan.RecentMemory)
	}
	if plan.APITimeoutSeconds != 11 || plan.GeminiTimeoutSeconds != 45 {
		t.Errorf("timeouts = %d/%d, want 11/45", plan.APITimeoutSeconds, plan.GeminiTimeoutSeconds)
	}
}

func TestGetPlan_ExcludesDisabledPersonasAndTopics(t *testing.T) {
	store := newTestStore()
	store.bots = []*entity.Bot{bot("bot_on", true), bot("bot_off", false)}
	store.topics = []*entity.Topic{topic("kept", true), topic("retired", false)}

	plan, err := NewBotService(store).GetPlan(context.Background())
	if err != nil {
		t.Fatalf("GetPlan() error = %v", err)
	}

	if len(plan.Bots) != 1 || plan.Bots[0].Username != "bot_on" {
		t.Errorf("plan personas = %+v, want only bot_on", plan.Bots)
	}
	if len(plan.Topics) != 1 || plan.Topics[0] != "kept" {
		t.Errorf("plan topics = %v, want only the enabled one", plan.Topics)
	}
}

func TestGetPlan_ConvertsIntervalToSecondsAndPassesPaused(t *testing.T) {
	store := newTestStore()
	store.config.PostInterval = 2 * time.Minute
	store.config.Paused = true
	store.bots = []*entity.Bot{bot("bot_a", true)}
	store.topics = []*entity.Topic{topic("cà phê", true)}

	plan, err := NewBotService(store).GetPlan(context.Background())
	if err != nil {
		t.Fatalf("GetPlan() error = %v", err)
	}

	if plan.PostIntervalSeconds != 120 {
		t.Errorf("PostIntervalSeconds = %d, want 120", plan.PostIntervalSeconds)
	}
	// A paused bot still needs the interval so it keeps polling for the resume.
	if !plan.Paused {
		t.Error("Paused = false, want true")
	}
}

func TestGetPlan_SurfacesPendingRunRequest(t *testing.T) {
	store := newTestStore()
	requested := time.Now()
	b := bot("bot_a", true)
	b.RunRequestedAt = &requested
	store.bots = []*entity.Bot{b, bot("bot_b", true)}
	store.topics = []*entity.Topic{topic("cà phê", true)}

	plan, err := NewBotService(store).GetPlan(context.Background())
	if err != nil {
		t.Fatalf("GetPlan() error = %v", err)
	}

	if !plan.Bots[0].RunRequested {
		t.Error("bot_a should report a pending run request")
	}
	if plan.Bots[1].RunRequested {
		t.Error("bot_b has no request pending")
	}
}

// An operator can request a run for any persona, but the plan only carries
// `accounts` of them. A persona sorting past the cap has to make it in anyway, or its
// pending request sits in the table forever and run-now silently does nothing.
func TestGetPlan_IncludesAPendingRequestBeyondTheAccountCap(t *testing.T) {
	store := newTestStore()
	store.config.Accounts = 3
	store.topics = []*entity.Topic{topic("cà phê", true)}

	requested := time.Now()
	last := bot("bot_sky", true) // sorts fifth, outside a cap of three
	last.RunRequestedAt = &requested
	store.bots = []*entity.Bot{
		bot("bot_codek", true), bot("bot_dulich", true), bot("bot_hafood", true),
		bot("bot_mien", true), last,
	}

	plan, err := NewBotService(store).GetPlan(context.Background())
	if err != nil {
		t.Fatalf("GetPlan() error = %v", err)
	}

	if len(plan.Bots) != 3 {
		t.Fatalf("plan has %d personas, want the cap of 3", len(plan.Bots))
	}
	if plan.Bots[0].Username != "bot_sky" || !plan.Bots[0].RunRequested {
		t.Errorf("plan leads with %+v, want bot_sky carrying its pending request", plan.Bots[0])
	}
}

// An empty pool is a state to report, not an error: the bot has no useful recovery
// and erroring would make every tick fail.
func TestGetPlan_EmptyPoolsAreNotAnError(t *testing.T) {
	plan, err := NewBotService(newTestStore()).GetPlan(context.Background())
	if err != nil {
		t.Fatalf("GetPlan() error = %v, want the empty plan", err)
	}
	if len(plan.Bots) != 0 || len(plan.Topics) != 0 {
		t.Errorf("plan = %+v, want empty pools", plan)
	}
}

// ─── ReportRun ───────────────────────────────────────────────────────────────

func TestReportRun_Success(t *testing.T) {
	store := newTestStore()
	b := bot("bot_a", true)
	store.bots = []*entity.Bot{b}
	postID := uuid.New().String()
	model := "gemini-2.5-flash"

	err := NewBotService(store).ReportRun(context.Background(), &dto.ReportRunRequest{
		BotID: b.ID.String(), Status: "success", PostID: &postID, ModelUsed: &model,
	})
	if err != nil {
		t.Fatalf("ReportRun() error = %v", err)
	}

	if store.gotCreatedRun == nil {
		t.Fatal("no run was recorded")
	}
	if store.gotCreatedRun.Status != entity.RunSuccess {
		t.Errorf("recorded status = %q, want success", store.gotCreatedRun.Status)
	}
	if store.gotCreatedRun.ModelUsed == nil || *store.gotCreatedRun.ModelUsed != model {
		t.Error("the model that answered should be recorded, so quota rotation is visible")
	}
}

func TestReportRun_ValidationErrors(t *testing.T) {
	store := newTestStore()
	b := bot("bot_a", true)
	store.bots = []*entity.Bot{b}
	postID := uuid.New().String()
	reason := "quota exhausted"
	notAUUID := "nope"

	tests := []struct {
		name string
		req  dto.ReportRunRequest
	}{
		{"bot id is not a uuid", dto.ReportRunRequest{BotID: notAUUID, Status: "success", PostID: &postID}},
		{"unknown status", dto.ReportRunRequest{BotID: b.ID.String(), Status: "pending", PostID: &postID}},
		{"post id is not a uuid", dto.ReportRunRequest{BotID: b.ID.String(), Status: "success", PostID: &notAUUID}},
		{"success without a post id", dto.ReportRunRequest{BotID: b.ID.String(), Status: "success"}},
		{"success carrying an error", dto.ReportRunRequest{BotID: b.ID.String(), Status: "success", PostID: &postID, Error: &reason}},
		{"error without a reason", dto.ReportRunRequest{BotID: b.ID.String(), Status: "error"}},
		{"error carrying a post id", dto.ReportRunRequest{BotID: b.ID.String(), Status: "error", PostID: &postID, Error: &reason}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store.gotCreatedRun = nil
			err := NewBotService(store).ReportRun(context.Background(), &tt.req)
			if err == nil {
				t.Fatal("expected a rejection")
			}
			if got := statusOf(t, err); got != 400 {
				t.Errorf("status = %d, want 400", got)
			}
			if store.gotCreatedRun != nil {
				t.Error("nothing should be recorded for a rejected report")
			}
		})
	}
}

func TestReportRun_UnknownBotIsNotFound(t *testing.T) {
	store := newTestStore()
	postID := uuid.New().String()

	err := NewBotService(store).ReportRun(context.Background(), &dto.ReportRunRequest{
		BotID: uuid.New().String(), Status: "success", PostID: &postID,
	})
	if err == nil {
		t.Fatal("expected a rejection")
	}
	if got := statusOf(t, err); got != 404 {
		t.Errorf("status = %d, want 404", got)
	}
}

// The pending flag must survive an ordinary interval post, or a request that
// arrived after the bot last fetched its plan would be silently dropped.
func TestReportRun_ClearsRunRequestOnlyWhenHonored(t *testing.T) {
	postID := uuid.New().String()

	for _, tt := range []struct {
		name        string
		honored     bool
		wantCleared int
	}{
		{"ordinary interval post leaves the flag alone", false, 0},
		{"honoured request clears the flag", true, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore()
			b := bot("bot_a", true)
			store.bots = []*entity.Bot{b}

			err := NewBotService(store).ReportRun(context.Background(), &dto.ReportRunRequest{
				BotID: b.ID.String(), Status: "success", PostID: &postID, HonoredRunRequest: tt.honored,
			})
			if err != nil {
				t.Fatalf("ReportRun() error = %v", err)
			}
			if len(store.gotCleared) != tt.wantCleared {
				t.Errorf("cleared %d requests, want %d", len(store.gotCleared), tt.wantCleared)
			}
		})
	}
}

// The run is already recorded by then, so failing the call would make the bot
// repeat a post it already made.
func TestReportRun_ClearFailureDoesNotFailTheReport(t *testing.T) {
	store := newTestStore()
	store.clearErr = errors.ErrInternal
	b := bot("bot_a", true)
	store.bots = []*entity.Bot{b}
	postID := uuid.New().String()

	err := NewBotService(store).ReportRun(context.Background(), &dto.ReportRunRequest{
		BotID: b.ID.String(), Status: "success", PostID: &postID, HonoredRunRequest: true,
	})
	if err != nil {
		t.Errorf("ReportRun() error = %v, want nil — the run was already recorded", err)
	}
}

// ─── Retention ───────────────────────────────────────────────────────────────

// withClock pins the service's clock so the retention window is testable without
// waiting a month.
func withClock(svc *BotService, at time.Time) *BotService {
	svc.now = func() time.Time { return at }
	return svc
}

// Without a bound the log grows for as long as the bot runs, and the per-persona
// summary on the admin list has to aggregate all of it on every page load.
func TestReportRun_PrunesPastTheRetentionWindow(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	store := newTestStore()
	b := bot("bot_a", true)
	store.bots = []*entity.Bot{b}
	postID := uuid.New().String()

	err := withClock(NewBotService(store), now).ReportRun(context.Background(), &dto.ReportRunRequest{
		BotID: b.ID.String(), Status: "success", PostID: &postID,
	})
	if err != nil {
		t.Fatalf("ReportRun() error = %v", err)
	}

	if store.gotPruneBefore == nil {
		t.Fatal("recording a run should also prune the log")
	}
	if want := now.Add(-entity.RunRetention); !store.gotPruneBefore.Equal(want) {
		t.Errorf("pruned before %v, want %v", store.gotPruneBefore, want)
	}
}

// The run is already recorded by the time pruning happens, so housekeeping must not
// turn a published post into a reported error.
func TestReportRun_PruneFailureDoesNotFailTheReport(t *testing.T) {
	store := newTestStore()
	store.pruneErr = errors.ErrInternal
	b := bot("bot_a", true)
	store.bots = []*entity.Bot{b}
	postID := uuid.New().String()

	err := NewBotService(store).ReportRun(context.Background(), &dto.ReportRunRequest{
		BotID: b.ID.String(), Status: "success", PostID: &postID,
	})
	if err != nil {
		t.Errorf("ReportRun() error = %v, want nil — the run was already recorded", err)
	}
	if store.gotCreatedRun == nil {
		t.Error("the run should still have been recorded")
	}
}

// The summary must not describe a period the log no longer covers, so it reads from
// the same cutoff the prune deletes at.
func TestListBots_SummarisesOnlyTheRetainedWindow(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	store := newTestStore()
	store.bots = []*entity.Bot{bot("bot_a", true)}

	if _, err := withClock(NewBotService(store), now).ListBots(context.Background()); err != nil {
		t.Fatalf("ListBots() error = %v", err)
	}

	if want := now.Add(-entity.RunRetention); !store.gotStatsSince.Equal(want) {
		t.Errorf("stats read from %v, want the retention cutoff %v", store.gotStatsSince, want)
	}
}

// ─── UpdateConfig ────────────────────────────────────────────────────────────

func TestUpdateConfig_AppliesPartialUpdate(t *testing.T) {
	store := newTestStore()
	adminID := uuid.New()
	interval := int32(120)

	resp, err := NewBotService(store).UpdateConfig(context.Background(),
		&dto.UpdateConfigRequest{PostIntervalSeconds: &interval}, adminID)
	if err != nil {
		t.Fatalf("UpdateConfig() error = %v", err)
	}

	if resp.PostIntervalSeconds != 120 {
		t.Errorf("PostIntervalSeconds = %d, want 120", resp.PostIntervalSeconds)
	}
	if resp.Accounts != 3 {
		t.Errorf("Accounts = %d, want the stored 3 — an omitted field must not be cleared", resp.Accounts)
	}
	if store.gotConfigUpdate.UpdatedBy == nil || *store.gotConfigUpdate.UpdatedBy != adminID {
		t.Error("the editing admin should be recorded")
	}
}

func TestUpdateConfig_RejectsOutOfRangeValues(t *testing.T) {
	zero := int32(0)
	tooMany := int32(entity.MaxAccounts + 1)
	empty := ""

	tests := []struct {
		name string
		req  dto.UpdateConfigRequest
	}{
		{"interval below the floor", dto.UpdateConfigRequest{PostIntervalSeconds: &zero}},
		{"no accounts", dto.UpdateConfigRequest{Accounts: &zero}},
		{"more accounts than the column holds", dto.UpdateConfigRequest{Accounts: &tooMany}},
		{"empty model id", dto.UpdateConfigRequest{Models: []string{"gemini-2.5-flash", empty}}},
		{"temperature below the floor", dto.UpdateConfigRequest{Temperature: ptr(-0.1)}},
		{"temperature above the ceiling", dto.UpdateConfigRequest{Temperature: ptr(2.1)}},
		{"negative tag cap", dto.UpdateConfigRequest{MaxTagsPerPost: ptr(int32(-1))}},
		// Above the post service's own tag limit, so every post would be rejected by
		// CreatePost and the setting would surface as a run of failures instead.
		{"tag cap above what CreatePost accepts", dto.UpdateConfigRequest{MaxTagsPerPost: ptr(int32(entity.MaxTagsCeiling + 1))}},
		{"negative repetition guard", dto.UpdateConfigRequest{RecentMemory: ptr(int32(-1))}},
		{"repetition guard above the ceiling", dto.UpdateConfigRequest{RecentMemory: ptr(int32(entity.MaxRecentMemory + 1))}},
		{"api timeout of zero", dto.UpdateConfigRequest{APITimeoutSeconds: &zero}},
		{"gemini timeout above the ceiling", dto.UpdateConfigRequest{GeminiTimeoutSeconds: ptr(int32(entity.MaxTimeout/time.Second) + 1)}},
		// Validation renders the template rather than only parsing it, so a reference
		// to a field that does not exist is caught here rather than on the bot host.
		{"unparseable prompt template", dto.UpdateConfigRequest{PromptTemplate: ptr("{{.DisplayName")}},
		{"prompt template naming a field that does not exist", dto.UpdateConfigRequest{PromptTemplate: ptr("viết như {{.NoSuchField}}")}},
		{"prompt template that renders to nothing", dto.UpdateConfigRequest{PromptTemplate: ptr("{{/* rỗng */}}  ")}},
		{"prompt template past the length limit", dto.UpdateConfigRequest{PromptTemplate: ptr(strings.Repeat("x", entity.MaxPromptTemplateLen+1))}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore()
			_, err := NewBotService(store).UpdateConfig(context.Background(), &tt.req, uuid.New())
			if err == nil {
				t.Fatal("expected a rejection")
			}
			if got := statusOf(t, err); got != 400 {
				t.Errorf("status = %d, want 400", got)
			}
			if store.gotConfigUpdate != nil {
				t.Error("nothing should reach the store for a rejected update")
			}
		})
	}
}

// An empty array is indistinguishable from an omitted field on the wire, and an
// empty fallback chain would stall the bot.
func TestUpdateConfig_EmptyModelsLeavesTheChainAlone(t *testing.T) {
	store := newTestStore()

	resp, err := NewBotService(store).UpdateConfig(context.Background(),
		&dto.UpdateConfigRequest{Models: []string{}}, uuid.New())
	if err != nil {
		t.Fatalf("UpdateConfig() error = %v", err)
	}
	if len(resp.Models) != 1 || resp.Models[0] != "gemini-2.5-flash" {
		t.Errorf("Models = %v, want the stored chain", resp.Models)
	}
}

// The generation knobs have to survive the round trip, since the plan is what the
// bot reads them from — a value that validates but never reaches the store would
// look saved in the admin UI and change nothing about what gets posted.
func TestUpdateConfig_AppliesTheGenerationKnobs(t *testing.T) {
	store := newTestStore()

	resp, err := NewBotService(store).UpdateConfig(context.Background(), &dto.UpdateConfigRequest{
		PromptTemplate:       ptr("viết như {{.DisplayName}} về {{.Topic}}"),
		Temperature:          ptr(0.4),
		MaxTagsPerPost:       ptr(int32(1)),
		RecentMemory:         ptr(int32(0)),
		APITimeoutSeconds:    ptr(int32(5)),
		GeminiTimeoutSeconds: ptr(int32(90)),
	}, uuid.New())
	if err != nil {
		t.Fatalf("UpdateConfig() error = %v", err)
	}

	if resp.PromptTemplate != "viết như {{.DisplayName}} về {{.Topic}}" {
		t.Errorf("PromptTemplate = %q", resp.PromptTemplate)
	}
	if resp.Temperature != 0.4 {
		t.Errorf("Temperature = %v, want 0.4", resp.Temperature)
	}
	if resp.MaxTagsPerPost != 1 {
		t.Errorf("MaxTagsPerPost = %d, want 1", resp.MaxTagsPerPost)
	}
	// 0 is a legal value here — it turns the repetition guard off — so it must be
	// written rather than treated as an omitted field.
	if resp.RecentMemory != 0 {
		t.Errorf("RecentMemory = %d, want the configured 0", resp.RecentMemory)
	}
	if resp.APITimeoutSeconds != 5 || resp.GeminiTimeoutSeconds != 90 {
		t.Errorf("timeouts = %d/%d, want 5/90", resp.APITimeoutSeconds, resp.GeminiTimeoutSeconds)
	}
}

// Clearing the template is the only way back to the bot's built-in prompt, so an
// empty string has to reach the store instead of being read as "no change".
func TestUpdateConfig_EmptyPromptTemplateRevertsToTheBuiltIn(t *testing.T) {
	store := newTestStore()
	store.config.PromptTemplate = "một template cũ"

	resp, err := NewBotService(store).UpdateConfig(context.Background(),
		&dto.UpdateConfigRequest{PromptTemplate: ptr("")}, uuid.New())
	if err != nil {
		t.Fatalf("UpdateConfig() error = %v", err)
	}
	if resp.PromptTemplate != "" {
		t.Errorf("PromptTemplate = %q, want it cleared", resp.PromptTemplate)
	}
	if store.gotConfigUpdate.PromptTemplate == nil {
		t.Error("an empty template must reach the store, not be dropped as an omitted field")
	}
}

// Omitting the template must leave the stored one alone, which is what separates it
// from clearing it.
func TestUpdateConfig_OmittedPromptTemplateIsLeftAlone(t *testing.T) {
	store := newTestStore()
	store.config.PromptTemplate = "một template cũ"

	resp, err := NewBotService(store).UpdateConfig(context.Background(),
		&dto.UpdateConfigRequest{Temperature: ptr(0.5)}, uuid.New())
	if err != nil {
		t.Fatalf("UpdateConfig() error = %v", err)
	}
	if resp.PromptTemplate != "một template cũ" {
		t.Errorf("PromptTemplate = %q, want the stored one", resp.PromptTemplate)
	}
	if store.gotConfigUpdate.PromptTemplate != nil {
		t.Error("an omitted template must not reach the store")
	}
}

func TestSetPaused_LeavesPersonaFlagsAlone(t *testing.T) {
	store := newTestStore()
	store.bots = []*entity.Bot{bot("bot_a", true), bot("bot_b", false)}

	resp, err := NewBotService(store).SetPaused(context.Background(), true, uuid.New())
	if err != nil {
		t.Fatalf("SetPaused() error = %v", err)
	}
	if !resp.Paused {
		t.Error("Paused = false, want true")
	}
	if !store.bots[0].Enabled || store.bots[1].Enabled {
		t.Error("pausing must not touch per-persona Enabled, or resuming would not restore the selection")
	}
}

// ─── Personas ────────────────────────────────────────────────────────────────

func TestCreateBot_Success(t *testing.T) {
	store := newTestStore()

	resp, err := NewBotService(store).CreateBot(context.Background(), &dto.CreateBotRequest{
		Username: "bot_new", DisplayName: "Bot Mới", Style: "giọng trẻ trung",
	})
	if err != nil {
		t.Fatalf("CreateBot() error = %v", err)
	}
	if resp.Username != "bot_new" || !resp.Enabled {
		t.Errorf("response = %+v, want an enabled bot_new", resp)
	}
}

func TestCreateBot_ValidationError(t *testing.T) {
	tests := []struct {
		name string
		req  dto.CreateBotRequest
	}{
		{"username too short", dto.CreateBotRequest{Username: "ab", DisplayName: "d", Style: "s"}},
		{"username with a space", dto.CreateBotRequest{Username: "bot sky", DisplayName: "d", Style: "s"}},
		{"no display name", dto.CreateBotRequest{Username: "bot_new", Style: "s"}},
		{"display name past the column width", dto.CreateBotRequest{
			Username: "bot_new", DisplayName: strings.Repeat("â", entity.MaxDisplayNameLen+1), Style: "s"}},
		{"no style", dto.CreateBotRequest{Username: "bot_new", DisplayName: "d"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewBotService(newTestStore()).CreateBot(context.Background(), &tt.req)
			if err == nil {
				t.Fatal("expected a rejection")
			}
			if got := statusOf(t, err); got != 400 {
				t.Errorf("status = %d, want 400", got)
			}
		})
	}
}

func TestCreateBot_DuplicateUsernameIsConflict(t *testing.T) {
	store := newTestStore()
	store.bots = []*entity.Bot{bot("bot_sky", true)}

	_, err := NewBotService(store).CreateBot(context.Background(), &dto.CreateBotRequest{
		Username: "bot_sky", DisplayName: "d", Style: "s",
	})
	if err == nil {
		t.Fatal("expected a rejection")
	}
	if got := statusOf(t, err); got != 409 {
		t.Errorf("status = %d, want 409", got)
	}
}

func TestUpdateBot_EmptyRequestRejected(t *testing.T) {
	store := newTestStore()
	b := bot("bot_a", true)
	store.bots = []*entity.Bot{b}

	_, err := NewBotService(store).UpdateBot(context.Background(), b.ID, &dto.UpdateBotRequest{})
	if err == nil {
		t.Fatal("expected a rejection — an empty update would only touch updated_at")
	}
	if got := statusOf(t, err); got != 400 {
		t.Errorf("status = %d, want 400", got)
	}
}

// The column is VARCHAR(100); the service mirrors it so an oversized name is a 400
// with a readable message, not a 22001 from the driver surfacing as a 500. The
// bound counts runes because Postgres counts characters, not bytes.
func TestUpdateBot_DisplayNamePastTheColumnWidthRejected(t *testing.T) {
	store := newTestStore()
	b := bot("bot_a", true)
	store.bots = []*entity.Bot{b}

	tooLong := strings.Repeat("â", entity.MaxDisplayNameLen+1)
	_, err := NewBotService(store).UpdateBot(context.Background(), b.ID,
		&dto.UpdateBotRequest{DisplayName: &tooLong})
	if err == nil {
		t.Fatal("expected a rejection")
	}
	if got := statusOf(t, err); got != 400 {
		t.Errorf("status = %d, want 400", got)
	}
}

func TestUpdateBot_UnknownBotIsNotFound(t *testing.T) {
	store := newTestStore()
	store.updateBotErr = errors.ErrNotFound
	enabled := false

	_, err := NewBotService(store).UpdateBot(context.Background(), uuid.New(),
		&dto.UpdateBotRequest{Enabled: &enabled})
	if err == nil {
		t.Fatal("expected a rejection")
	}
	if got := statusOf(t, err); got != 404 {
		t.Errorf("status = %d, want 404", got)
	}
}

func TestRequestRun_RecordsThePendingFlag(t *testing.T) {
	store := newTestStore()
	b := bot("bot_a", true)
	store.bots = []*entity.Bot{b}

	resp, err := NewBotService(store).RequestRun(context.Background(), b.ID)
	if err != nil {
		t.Fatalf("RequestRun() error = %v", err)
	}
	if resp.RunRequestedAt == nil {
		t.Error("the response should report when the run was requested")
	}
}

// ─── ListBots ────────────────────────────────────────────────────────────────

func TestListBots_MergesRunStats(t *testing.T) {
	store := newTestStore()
	active, quiet := bot("bot_active", true), bot("bot_quiet", true)
	store.bots = []*entity.Bot{active, quiet}
	lastSuccess := time.Date(2026, 7, 27, 10, 28, 0, 0, time.UTC)
	store.stats = []*entity.RunStats{{
		BotID: active.ID, LastSuccessAt: &lastSuccess, SuccessesLast24h: 41, ErrorsLast24h: 2,
	}}

	resp, err := NewBotService(store).ListBots(context.Background())
	if err != nil {
		t.Fatalf("ListBots() error = %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("got %d personas, want 2", len(resp.Data))
	}

	if resp.Data[0].SuccessesLast24h != 41 || resp.Data[0].ErrorsLast24h != 2 {
		t.Errorf("stats not merged: %+v", resp.Data[0])
	}
	if resp.Data[0].LastSuccessAt == nil || *resp.Data[0].LastSuccessAt != "2026-07-27T10:28:00Z" {
		t.Errorf("LastSuccessAt = %v", resp.Data[0].LastSuccessAt)
	}

	// A persona that has never run is absent from the stats query, not zeroed by it.
	if resp.Data[1].LastSuccessAt != nil || resp.Data[1].SuccessesLast24h != 0 {
		t.Errorf("a persona with no runs should carry no timestamps and zero counts: %+v", resp.Data[1])
	}
}

// ─── ListRuns ────────────────────────────────────────────────────────────────

func TestListRuns_AttachesPostPreviews(t *testing.T) {
	store := newTestStore()
	postID := uuid.New()
	store.runs = []*entity.Run{{ID: uuid.New(), BotID: uuid.New(), PostID: &postID, Status: entity.RunSuccess}}

	reader := &fakePostReader{previews: map[uuid.UUID]string{postID: "Sáng nay cà phê ở góc phố quen"}}
	svc := NewBotService(store)
	svc.WithPostReader(reader)

	resp, err := svc.ListRuns(context.Background(), RunFilter{PaginationRequest: pagination.PaginationRequest{Limit: 20}})
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}
	if resp.Data[0].PostPreview == nil || *resp.Data[0].PostPreview != "Sáng nay cà phê ở góc phố quen" {
		t.Errorf("PostPreview = %v", resp.Data[0].PostPreview)
	}
	if len(reader.gotIDs) != 1 {
		t.Errorf("the reader should be called once with all ids, got %d", len(reader.gotIDs))
	}
}

// The status and error columns are the part an operator is looking for, so a
// preview lookup failure must not take the log down with it.
func TestListRuns_PreviewFailureDegradesGracefully(t *testing.T) {
	store := newTestStore()
	postID := uuid.New()
	store.runs = []*entity.Run{{ID: uuid.New(), BotID: uuid.New(), PostID: &postID, Status: entity.RunSuccess}}

	svc := NewBotService(store)
	svc.WithPostReader(&fakePostReader{err: errors.ErrInternal})

	resp, err := svc.ListRuns(context.Background(), RunFilter{PaginationRequest: pagination.PaginationRequest{Limit: 20}})
	if err != nil {
		t.Fatalf("ListRuns() error = %v, want the log without previews", err)
	}
	if resp.Data[0].PostPreview != nil {
		t.Error("PostPreview should be absent when the lookup failed")
	}
}

func TestListRuns_NoReaderWiredOmitsPreviews(t *testing.T) {
	store := newTestStore()
	postID := uuid.New()
	store.runs = []*entity.Run{{ID: uuid.New(), BotID: uuid.New(), PostID: &postID, Status: entity.RunSuccess}}

	resp, err := NewBotService(store).ListRuns(context.Background(),
		RunFilter{PaginationRequest: pagination.PaginationRequest{Limit: 20}})
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}
	if resp.Data[0].PostPreview != nil {
		t.Error("PostPreview should be absent with no post reader wired")
	}
}

func TestListRuns_FiltersByBotAndReportsTotal(t *testing.T) {
	store := newTestStore()
	botID := uuid.New()
	store.runs = []*entity.Run{
		{ID: uuid.New(), BotID: botID, Status: entity.RunError, Error: strPtr("boom")},
	}

	resp, err := NewBotService(store).ListRuns(context.Background(), RunFilter{
		PaginationRequest: pagination.PaginationRequest{Limit: 10, Offset: 20},
		BotID:             &botID,
	})
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}

	if store.gotRunFilter.botID == nil || *store.gotRunFilter.botID != botID {
		t.Error("the persona filter should reach the store")
	}
	if store.gotRunFilter.limit != 10 || store.gotRunFilter.offset != 20 {
		t.Errorf("pagination = limit %d offset %d, want 10/20", store.gotRunFilter.limit, store.gotRunFilter.offset)
	}
	if resp.Total != 1 || resp.Limit != 10 || resp.Offset != 20 {
		t.Errorf("pagination response = %+v", resp.PaginationResponse)
	}
}

// ─── LinkIdentity ────────────────────────────────────────────────────────────

func TestLinkIdentity_RecordsTheAccount(t *testing.T) {
	store := newTestStore()
	b := bot("bot_a", true)
	store.bots = []*entity.Bot{b}
	userID := uuid.New()

	err := NewBotService(store).LinkIdentity(context.Background(), b.ID, &dto.LinkIdentityRequest{
		UserID: userID.String(),
	})
	if err != nil {
		t.Fatalf("LinkIdentity() error = %v", err)
	}
	if store.gotLinked[b.ID] != userID {
		t.Errorf("linked %v, want %v", store.gotLinked[b.ID], userID)
	}
}

// The id is otherwise taken on trust, so the two cases that are certainly mistakes
// rather than claims have to be caught: an unparseable value, and the zero UUID,
// which is an uninitialised variable that would render as a link to nothing.
func TestLinkIdentity_RejectsBadUserID(t *testing.T) {
	for _, tt := range []struct {
		name   string
		userID string
	}{
		{"not a uuid", "nope"},
		{"empty", ""},
		{"zero uuid", uuid.Nil.String()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore()
			b := bot("bot_a", true)
			store.bots = []*entity.Bot{b}

			err := NewBotService(store).LinkIdentity(context.Background(), b.ID,
				&dto.LinkIdentityRequest{UserID: tt.userID})
			if err == nil {
				t.Fatal("expected a rejection")
			}
			if got := statusOf(t, err); got != 400 {
				t.Errorf("status = %d, want 400", got)
			}
			if len(store.gotLinked) != 0 {
				t.Error("nothing should have been written")
			}
		})
	}
}

// ─── Topics ──────────────────────────────────────────────────────────────────

func TestCreateTopic_EmptyContentRejected(t *testing.T) {
	_, err := NewBotService(newTestStore()).CreateTopic(context.Background(), &dto.CreateTopicRequest{})
	if err == nil {
		t.Fatal("expected a rejection")
	}
	if got := statusOf(t, err); got != 400 {
		t.Errorf("status = %d, want 400", got)
	}
}

func TestSetTopicEnabled_RetiresWithoutDeleting(t *testing.T) {
	store := newTestStore()
	tp := topic("cà phê", true)
	store.topics = []*entity.Topic{tp}

	resp, err := NewBotService(store).SetTopicEnabled(context.Background(), tp.ID, false)
	if err != nil {
		t.Fatalf("SetTopicEnabled() error = %v", err)
	}
	if resp.Enabled {
		t.Error("Enabled = true, want false")
	}
	if len(store.topics) != 1 {
		t.Error("retiring a topic must keep the row the activity log refers to")
	}
}

func TestCreateTopic_Success(t *testing.T) {
	store := newTestStore()

	resp, err := NewBotService(store).CreateTopic(context.Background(),
		&dto.CreateTopicRequest{Content: "một quán mì mới mở"})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	if resp.Content != "một quán mì mới mở" || !resp.Enabled {
		t.Errorf("response = %+v", resp)
	}
}

func TestListTopics_IncludesRetiredOnes(t *testing.T) {
	store := newTestStore()
	store.topics = []*entity.Topic{topic("kept", true), topic("retired", false)}

	resp, err := NewBotService(store).ListTopics(context.Background())
	if err != nil {
		t.Fatalf("ListTopics() error = %v", err)
	}
	// Unlike the plan, the admin list has to show retired subjects so they can be
	// restored.
	if len(resp.Data) != 2 {
		t.Errorf("got %d topics, want both the kept and the retired one", len(resp.Data))
	}
}

func TestSetTopicEnabled_UnknownIsNotFound(t *testing.T) {
	_, err := NewBotService(newTestStore()).SetTopicEnabled(context.Background(), uuid.New(), false)
	if err == nil {
		t.Fatal("expected a rejection")
	}
	if got := statusOf(t, err); got != 404 {
		t.Errorf("status = %d, want 404", got)
	}
}

func TestDeleteTopic_Success(t *testing.T) {
	store := newTestStore()
	tp := topic("cà phê", true)
	store.topics = []*entity.Topic{tp}

	if err := NewBotService(store).DeleteTopic(context.Background(), tp.ID); err != nil {
		t.Fatalf("DeleteTopic() error = %v", err)
	}
}

// DeleteBot and SetTopicEnabled both answer 404 for an unknown id; a delete that
// removed nothing must not read as success for a double-click or a typo'd id.
func TestDeleteTopic_UnknownIsNotFound(t *testing.T) {
	err := NewBotService(newTestStore()).DeleteTopic(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected a rejection")
	}
	if got := statusOf(t, err); got != 404 {
		t.Errorf("status = %d, want 404", got)
	}
}

// ─── Remaining persona and config paths ──────────────────────────────────────

func TestDeleteBot_Success(t *testing.T) {
	store := newTestStore()
	b := bot("bot_a", true)
	store.bots = []*entity.Bot{b}

	if err := NewBotService(store).DeleteBot(context.Background(), b.ID); err != nil {
		t.Fatalf("DeleteBot() error = %v", err)
	}
	if len(store.bots) != 0 {
		t.Error("the persona should be gone")
	}
}

func TestDeleteBot_UnknownIsNotFound(t *testing.T) {
	err := NewBotService(newTestStore()).DeleteBot(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected a rejection")
	}
	if got := statusOf(t, err); got != 404 {
		t.Errorf("status = %d, want 404", got)
	}
}

// The plan carries only enabled personas, so a request for a disabled one could
// never fire. Recording it and answering 200 would report a post as scheduled that
// is never going to happen — the same silent no-op the pending-first ordering exists
// to prevent for personas past the account cap.
func TestRequestRun_DisabledPersonaIsRejected(t *testing.T) {
	store := newTestStore()
	disabled := bot("bot_sky", false)
	store.bots = []*entity.Bot{disabled}

	_, err := NewBotService(store).RequestRun(context.Background(), disabled.ID)
	if err == nil {
		t.Fatal("expected a rejection")
	}
	if got := statusOf(t, err); got != 409 {
		t.Errorf("status = %d, want 409", got)
	}
	if disabled.RunRequested() {
		t.Error("nothing should have been recorded for a persona that cannot act on it")
	}
}

// A disable can land between the service's pre-check and the write; the UPDATE's
// `AND enabled` predicate turns that into no rows. The answer must be the same 409
// the pre-check gives, not a 404 for a persona that still exists — and nothing may
// be recorded, or the request would fire the moment the persona is re-enabled.
func TestRequestRun_DisableInFlightIsRejected(t *testing.T) {
	store := newTestStore()
	target := bot("bot_sky", true)
	store.bots = []*entity.Bot{target}
	// The pre-check sees an enabled persona; the write then matches no rows, as it
	// would when the disable commits in between.
	store.requestRunErr = errors.ErrNotFound

	_, err := NewBotService(store).RequestRun(context.Background(), target.ID)
	if err == nil {
		t.Fatal("expected a rejection")
	}
	if got := statusOf(t, err); got != 409 {
		t.Errorf("status = %d, want 409", got)
	}
	if target.RunRequested() {
		t.Error("nothing should have been recorded for a request that cannot fire")
	}
}

// Disabling has to retire a pending request, or re-enabling the persona weeks later
// makes it post immediately for a request nobody remembers making.
func TestUpdateBot_DisablingClearsAPendingRunRequest(t *testing.T) {
	store := newTestStore()
	requested := time.Now()
	target := bot("bot_sky", true)
	target.RunRequestedAt = &requested
	store.bots = []*entity.Bot{target}

	disabled := false
	resp, err := NewBotService(store).UpdateBot(context.Background(), target.ID,
		&dto.UpdateBotRequest{Enabled: &disabled})
	if err != nil {
		t.Fatalf("UpdateBot() error = %v", err)
	}

	if resp.Enabled {
		t.Error("Enabled = true, want false")
	}
	if resp.RunRequestedAt != nil {
		t.Errorf("run_requested_at = %v, want it retired alongside the persona", *resp.RunRequestedAt)
	}
}

// Re-enabling must not resurrect anything, and an unrelated edit must leave a
// genuine pending request alone.
func TestUpdateBot_UnrelatedEditKeepsAPendingRunRequest(t *testing.T) {
	store := newTestStore()
	requested := time.Now()
	target := bot("bot_sky", true)
	target.RunRequestedAt = &requested
	store.bots = []*entity.Bot{target}

	style := "giọng mới"
	resp, err := NewBotService(store).UpdateBot(context.Background(), target.ID,
		&dto.UpdateBotRequest{Style: &style})
	if err != nil {
		t.Fatalf("UpdateBot() error = %v", err)
	}
	if resp.RunRequestedAt == nil {
		t.Error("editing the voice should not discard a pending run request")
	}
}

func TestRequestRun_UnknownIsNotFound(t *testing.T) {
	_, err := NewBotService(newTestStore()).RequestRun(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected a rejection")
	}
	if got := statusOf(t, err); got != 404 {
		t.Errorf("status = %d, want 404", got)
	}
}

func TestLinkIdentity_UnknownBotIsNotFound(t *testing.T) {
	err := NewBotService(newTestStore()).LinkIdentity(context.Background(), uuid.New(),
		&dto.LinkIdentityRequest{UserID: uuid.New().String()})
	if err == nil {
		t.Fatal("expected a rejection")
	}
	if got := statusOf(t, err); got != 404 {
		t.Errorf("status = %d, want 404", got)
	}
}

func TestGetConfig_ReturnsStoredValues(t *testing.T) {
	store := newTestStore()
	store.config.PostInterval = 90 * time.Second
	store.config.Paused = true

	resp, err := NewBotService(store).GetConfig(context.Background())
	if err != nil {
		t.Fatalf("GetConfig() error = %v", err)
	}
	if resp.PostIntervalSeconds != 90 || !resp.Paused || resp.Accounts != 3 {
		t.Errorf("response = %+v", resp)
	}
}

// ─── Store failures propagate ────────────────────────────────────────────────

func TestStoreFailuresPropagate(t *testing.T) {
	t.Run("config read", func(t *testing.T) {
		store := newTestStore()
		store.configErr = errors.ErrInternal
		if _, err := NewBotService(store).GetConfig(context.Background()); err == nil {
			t.Error("a config read failure should surface")
		}
	})

	t.Run("plan config read", func(t *testing.T) {
		store := newTestStore()
		store.configErr = errors.ErrInternal
		if _, err := NewBotService(store).GetPlan(context.Background()); err == nil {
			t.Error("a config read failure should fail the plan — the bot has no defaults to fall back on")
		}
	})

	t.Run("create persona", func(t *testing.T) {
		store := newTestStore()
		store.createBotErr = errors.ErrInternal
		_, err := NewBotService(store).CreateBot(context.Background(), &dto.CreateBotRequest{
			Username: "bot_new", DisplayName: "d", Style: "s",
		})
		if err == nil {
			t.Error("a write failure should surface")
		}
	})
}

// ─── Helpers under test ──────────────────────────────────────────────────────

// Post content is routinely non-ASCII, so a byte-based cut would split a rune.
func TestTruncate_IsRuneAware(t *testing.T) {
	if got := truncate("cà phê", 20); got != "cà phê" {
		t.Errorf("truncate() = %q, want the input unchanged", got)
	}
	got := truncate("cà phê sáng ở góc phố", 6)
	if got != "cà phê…" {
		t.Errorf("truncate() = %q, want %q", got, "cà phê…")
	}
}

func strPtr(s string) *string { return &s }
