package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/bot/dto"
	"github.com/jarviisha/darkvoid/internal/feature/bot/service"
	httputil "github.com/jarviisha/darkvoid/internal/http"
	"github.com/jarviisha/darkvoid/pkg/errors"
)

// ─── Mock ────────────────────────────────────────────────────────────────────

// mockBotService records what the handler asked for and returns whatever the test
// set. Unset hooks return an empty success, so each test wires only what it asserts.
type mockBotService struct {
	listBots        func(ctx context.Context) (*dto.ListBotsResponse, error)
	createBot       func(ctx context.Context, req *dto.CreateBotRequest) (*dto.BotResponse, error)
	updateBot       func(ctx context.Context, id uuid.UUID, req *dto.UpdateBotRequest) (*dto.BotResponse, error)
	deleteBot       func(ctx context.Context, id uuid.UUID) error
	requestRun      func(ctx context.Context, id uuid.UUID) (*dto.BotResponse, error)
	getConfig       func(ctx context.Context) (*dto.ConfigResponse, error)
	updateConfig    func(ctx context.Context, req *dto.UpdateConfigRequest, adminID uuid.UUID) (*dto.ConfigResponse, error)
	setPaused       func(ctx context.Context, paused bool, adminID uuid.UUID) (*dto.ConfigResponse, error)
	listTopics      func(ctx context.Context) (*dto.ListTopicsResponse, error)
	createTopic     func(ctx context.Context, req *dto.CreateTopicRequest) (*dto.TopicResponse, error)
	setTopicEnabled func(ctx context.Context, id uuid.UUID, enabled bool) (*dto.TopicResponse, error)
	deleteTopic     func(ctx context.Context, id uuid.UUID) error
	listRuns        func(ctx context.Context, filter service.RunFilter) (*dto.ListRunsResponse, error)
	getPlan         func(ctx context.Context) (*dto.PlanResponse, error)
	reportRun       func(ctx context.Context, req *dto.ReportRunRequest) error
	linkIdentity    func(ctx context.Context, botID uuid.UUID, req *dto.LinkIdentityRequest) error
}

func (m *mockBotService) ListBots(ctx context.Context) (*dto.ListBotsResponse, error) {
	if m.listBots != nil {
		return m.listBots(ctx)
	}
	return &dto.ListBotsResponse{}, nil
}

func (m *mockBotService) CreateBot(ctx context.Context, req *dto.CreateBotRequest) (*dto.BotResponse, error) {
	if m.createBot != nil {
		return m.createBot(ctx, req)
	}
	return &dto.BotResponse{}, nil
}

func (m *mockBotService) UpdateBot(ctx context.Context, id uuid.UUID, req *dto.UpdateBotRequest) (*dto.BotResponse, error) {
	if m.updateBot != nil {
		return m.updateBot(ctx, id, req)
	}
	return &dto.BotResponse{}, nil
}

func (m *mockBotService) DeleteBot(ctx context.Context, id uuid.UUID) error {
	if m.deleteBot != nil {
		return m.deleteBot(ctx, id)
	}
	return nil
}

func (m *mockBotService) RequestRun(ctx context.Context, id uuid.UUID) (*dto.BotResponse, error) {
	if m.requestRun != nil {
		return m.requestRun(ctx, id)
	}
	return &dto.BotResponse{}, nil
}

func (m *mockBotService) GetConfig(ctx context.Context) (*dto.ConfigResponse, error) {
	if m.getConfig != nil {
		return m.getConfig(ctx)
	}
	return &dto.ConfigResponse{}, nil
}

func (m *mockBotService) UpdateConfig(ctx context.Context, req *dto.UpdateConfigRequest, adminID uuid.UUID) (*dto.ConfigResponse, error) {
	if m.updateConfig != nil {
		return m.updateConfig(ctx, req, adminID)
	}
	return &dto.ConfigResponse{}, nil
}

func (m *mockBotService) SetPaused(ctx context.Context, paused bool, adminID uuid.UUID) (*dto.ConfigResponse, error) {
	if m.setPaused != nil {
		return m.setPaused(ctx, paused, adminID)
	}
	return &dto.ConfigResponse{}, nil
}

func (m *mockBotService) ListTopics(ctx context.Context) (*dto.ListTopicsResponse, error) {
	if m.listTopics != nil {
		return m.listTopics(ctx)
	}
	return &dto.ListTopicsResponse{}, nil
}

func (m *mockBotService) CreateTopic(ctx context.Context, req *dto.CreateTopicRequest) (*dto.TopicResponse, error) {
	if m.createTopic != nil {
		return m.createTopic(ctx, req)
	}
	return &dto.TopicResponse{}, nil
}

func (m *mockBotService) SetTopicEnabled(ctx context.Context, id uuid.UUID, enabled bool) (*dto.TopicResponse, error) {
	if m.setTopicEnabled != nil {
		return m.setTopicEnabled(ctx, id, enabled)
	}
	return &dto.TopicResponse{}, nil
}

func (m *mockBotService) DeleteTopic(ctx context.Context, id uuid.UUID) error {
	if m.deleteTopic != nil {
		return m.deleteTopic(ctx, id)
	}
	return nil
}

func (m *mockBotService) ListRuns(ctx context.Context, filter service.RunFilter) (*dto.ListRunsResponse, error) {
	if m.listRuns != nil {
		return m.listRuns(ctx, filter)
	}
	return &dto.ListRunsResponse{}, nil
}

func (m *mockBotService) GetPlan(ctx context.Context) (*dto.PlanResponse, error) {
	if m.getPlan != nil {
		return m.getPlan(ctx)
	}
	return &dto.PlanResponse{}, nil
}

func (m *mockBotService) ReportRun(ctx context.Context, req *dto.ReportRunRequest) error {
	if m.reportRun != nil {
		return m.reportRun(ctx, req)
	}
	return nil
}

func (m *mockBotService) LinkIdentity(ctx context.Context, botID uuid.UUID, req *dto.LinkIdentityRequest) error {
	if m.linkIdentity != nil {
		return m.linkIdentity(ctx, botID, req)
	}
	return nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func newRequest(t *testing.T, method, target, body string) *http.Request {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	r, err := http.NewRequestWithContext(context.Background(), method, target, reader)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	return r
}

// withAuth puts an authenticated user in the context, as the auth middleware would.
func withAuth(r *http.Request, userID uuid.UUID) *http.Request {
	return r.WithContext(httputil.WithUserID(r.Context(), userID))
}

// withID injects the {id} path parameter, which is the only one the bot routes use.
// It builds on the existing context, so it composes with withAuth in either order.
func withID(r *http.Request, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func assertStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Errorf("status = %d, want %d (body: %s)", w.Code, want, w.Body.String())
	}
}

// ─── Error passthrough ───────────────────────────────────────────────────────

// erroringService fails every operation with err, so one table can prove that no
// handler swallows a service error or rewrites its status.
func erroringService(err error) *mockBotService {
	return &mockBotService{
		listBots:   func(context.Context) (*dto.ListBotsResponse, error) { return nil, err },
		createBot:  func(context.Context, *dto.CreateBotRequest) (*dto.BotResponse, error) { return nil, err },
		updateBot:  func(context.Context, uuid.UUID, *dto.UpdateBotRequest) (*dto.BotResponse, error) { return nil, err },
		deleteBot:  func(context.Context, uuid.UUID) error { return err },
		requestRun: func(context.Context, uuid.UUID) (*dto.BotResponse, error) { return nil, err },
		getConfig:  func(context.Context) (*dto.ConfigResponse, error) { return nil, err },
		updateConfig: func(context.Context, *dto.UpdateConfigRequest, uuid.UUID) (*dto.ConfigResponse, error) {
			return nil, err
		},
		setPaused:       func(context.Context, bool, uuid.UUID) (*dto.ConfigResponse, error) { return nil, err },
		listTopics:      func(context.Context) (*dto.ListTopicsResponse, error) { return nil, err },
		createTopic:     func(context.Context, *dto.CreateTopicRequest) (*dto.TopicResponse, error) { return nil, err },
		setTopicEnabled: func(context.Context, uuid.UUID, bool) (*dto.TopicResponse, error) { return nil, err },
		deleteTopic:     func(context.Context, uuid.UUID) error { return err },
		listRuns:        func(context.Context, service.RunFilter) (*dto.ListRunsResponse, error) { return nil, err },
		getPlan:         func(context.Context) (*dto.PlanResponse, error) { return nil, err },
		reportRun:       func(context.Context, *dto.ReportRunRequest) error { return err },
		linkIdentity:    func(context.Context, uuid.UUID, *dto.LinkIdentityRequest) error { return err },
	}
}

func TestAllHandlers_PropagateTheServiceStatus(t *testing.T) {
	id := uuid.New().String()

	// Every route, with whatever context that handler needs to reach its service
	// call: an authenticated user for the config routes, an {id} where one is read.
	routes := []struct {
		name   string
		call   func(h *BotHandler, w http.ResponseWriter, r *http.Request)
		method string
		body   string
		auth   bool
		withID bool
	}{
		{"ListBots", (*BotHandler).ListBots, http.MethodGet, "", false, false},
		{"CreateBot", (*BotHandler).CreateBot, http.MethodPost, `{"username":"bot_x"}`, false, false},
		{"UpdateBot", (*BotHandler).UpdateBot, http.MethodPatch, `{"enabled":false}`, false, true},
		{"DeleteBot", (*BotHandler).DeleteBot, http.MethodDelete, "", false, true},
		{"RequestRun", (*BotHandler).RequestRun, http.MethodPost, "", false, true},
		{"GetConfig", (*BotHandler).GetConfig, http.MethodGet, "", false, false},
		{"UpdateConfig", (*BotHandler).UpdateConfig, http.MethodPut, `{}`, true, false},
		{"Pause", (*BotHandler).Pause, http.MethodPost, "", true, false},
		{"Resume", (*BotHandler).Resume, http.MethodPost, "", true, false},
		{"ListTopics", (*BotHandler).ListTopics, http.MethodGet, "", false, false},
		{"CreateTopic", (*BotHandler).CreateTopic, http.MethodPost, `{"content":"x"}`, false, false},
		{"SetTopicEnabled", (*BotHandler).SetTopicEnabled, http.MethodPatch, `{"enabled":false}`, false, true},
		{"DeleteTopic", (*BotHandler).DeleteTopic, http.MethodDelete, "", false, true},
		{"ListRuns", (*BotHandler).ListRuns, http.MethodGet, "", false, false},
		{"GetPlan", (*BotHandler).GetPlan, http.MethodGet, "", false, false},
		{"ReportRun", (*BotHandler).ReportRun, http.MethodPost, `{"status":"success"}`, false, false},
		{"LinkIdentity", (*BotHandler).LinkIdentity, http.MethodPost, `{"user_id":"` + id + `"}`, false, true},
	}

	// One case per status a service method can produce, so a handler cannot be
	// passing the error through while flattening it to 500.
	for _, sErr := range []struct {
		name string
		err  error
		want int
	}{
		{"not found", errors.NewNotFoundError("bot"), http.StatusNotFound},
		{"conflict", errors.NewConflictError("bot"), http.StatusConflict},
		{"validation", errors.NewValidationError("field", "bad"), http.StatusBadRequest},
		{"internal", errors.ErrInternal, http.StatusInternalServerError},
	} {
		t.Run(sErr.name, func(t *testing.T) {
			for _, rt := range routes {
				r := newRequest(t, rt.method, "/", rt.body)
				if rt.withID {
					r = withID(r, id)
				}
				if rt.auth {
					r = withAuth(r, uuid.New())
				}
				w := httptest.NewRecorder()
				rt.call(NewBotHandler(erroringService(sErr.err)), w, r)

				if w.Code != sErr.want {
					t.Errorf("%s: status = %d, want %d", rt.name, w.Code, sErr.want)
				}
			}
		})
	}
}

// ─── Personas ────────────────────────────────────────────────────────────────

func TestListBots_Success(t *testing.T) {
	svc := &mockBotService{
		listBots: func(context.Context) (*dto.ListBotsResponse, error) {
			return &dto.ListBotsResponse{Data: []dto.BotResponse{{Username: "bot_sky"}}}, nil
		},
	}
	w := httptest.NewRecorder()
	NewBotHandler(svc).ListBots(w, newRequest(t, http.MethodGet, "/admin/bots", ""))

	assertStatus(t, w, http.StatusOK)
	var body dto.ListBotsResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if len(body.Data) != 1 || body.Data[0].Username != "bot_sky" {
		t.Errorf("body = %+v", body)
	}
}

func TestCreateBot_Created(t *testing.T) {
	var got *dto.CreateBotRequest
	svc := &mockBotService{
		createBot: func(_ context.Context, req *dto.CreateBotRequest) (*dto.BotResponse, error) {
			got = req
			return &dto.BotResponse{Username: req.Username}, nil
		},
	}
	w := httptest.NewRecorder()
	body := `{"username":"bot_new","display_name":"Bot Mới","style":"giọng trẻ trung"}`
	NewBotHandler(svc).CreateBot(w, newRequest(t, http.MethodPost, "/admin/bots", body))

	assertStatus(t, w, http.StatusCreated)
	if got == nil || got.Username != "bot_new" || got.DisplayName != "Bot Mới" {
		t.Errorf("decoded request = %+v", got)
	}
}

func TestCreateBot_InvalidBody(t *testing.T) {
	w := httptest.NewRecorder()
	NewBotHandler(&mockBotService{}).CreateBot(w, newRequest(t, http.MethodPost, "/admin/bots", "{not json"))
	assertStatus(t, w, http.StatusBadRequest)
}

func TestCreateBot_ServiceErrorPropagatesStatus(t *testing.T) {
	svc := &mockBotService{
		createBot: func(context.Context, *dto.CreateBotRequest) (*dto.BotResponse, error) {
			return nil, errors.NewConflictError(`bot "bot_sky"`)
		},
	}
	w := httptest.NewRecorder()
	NewBotHandler(svc).CreateBot(w, newRequest(t, http.MethodPost, "/admin/bots", `{"username":"bot_sky"}`))
	assertStatus(t, w, http.StatusConflict)
}

func TestUpdateBot_PassesIDAndFields(t *testing.T) {
	botID := uuid.New()
	var gotID uuid.UUID
	var gotReq *dto.UpdateBotRequest
	svc := &mockBotService{
		updateBot: func(_ context.Context, id uuid.UUID, req *dto.UpdateBotRequest) (*dto.BotResponse, error) {
			gotID, gotReq = id, req
			return &dto.BotResponse{}, nil
		},
	}

	r := withID(newRequest(t, http.MethodPatch, "/admin/bots/"+botID.String(), `{"enabled":false}`), botID.String())
	w := httptest.NewRecorder()
	NewBotHandler(svc).UpdateBot(w, r)

	assertStatus(t, w, http.StatusOK)
	if gotID != botID {
		t.Errorf("id = %v, want %v", gotID, botID)
	}
	if gotReq.Enabled == nil || *gotReq.Enabled {
		t.Errorf("Enabled = %v, want a pointer to false", gotReq.Enabled)
	}
	// An omitted field must stay nil, or the service cannot tell it apart from a clear.
	if gotReq.Style != nil || gotReq.DisplayName != nil {
		t.Error("omitted fields should decode to nil")
	}
}

func TestUpdateBot_InvalidIDRejectedBeforeTheService(t *testing.T) {
	called := false
	svc := &mockBotService{
		updateBot: func(context.Context, uuid.UUID, *dto.UpdateBotRequest) (*dto.BotResponse, error) {
			called = true
			return &dto.BotResponse{}, nil
		},
	}
	r := withID(newRequest(t, http.MethodPatch, "/admin/bots/nope", `{"enabled":false}`), "nope")
	w := httptest.NewRecorder()
	NewBotHandler(svc).UpdateBot(w, r)

	assertStatus(t, w, http.StatusBadRequest)
	if called {
		t.Error("the service should not be reached with an unparseable id")
	}
}

func TestDeleteBot_NoContent(t *testing.T) {
	botID := uuid.New()
	r := withID(newRequest(t, http.MethodDelete, "/admin/bots/"+botID.String(), ""), botID.String())
	w := httptest.NewRecorder()
	NewBotHandler(&mockBotService{}).DeleteBot(w, r)
	assertStatus(t, w, http.StatusNoContent)
}

func TestRequestRun_Success(t *testing.T) {
	botID := uuid.New()
	r := withID(newRequest(t, http.MethodPost, "/admin/bots/"+botID.String()+"/run-now", ""), botID.String())
	w := httptest.NewRecorder()
	NewBotHandler(&mockBotService{}).RequestRun(w, r)
	assertStatus(t, w, http.StatusOK)
}

// ─── Config ──────────────────────────────────────────────────────────────────

func TestUpdateConfig_RecordsTheEditingAdmin(t *testing.T) {
	adminID := uuid.New()
	var gotAdmin uuid.UUID
	svc := &mockBotService{
		updateConfig: func(_ context.Context, _ *dto.UpdateConfigRequest, id uuid.UUID) (*dto.ConfigResponse, error) {
			gotAdmin = id
			return &dto.ConfigResponse{}, nil
		},
	}

	r := withAuth(newRequest(t, http.MethodPut, "/admin/bots/config", `{"accounts":5}`), adminID)
	w := httptest.NewRecorder()
	NewBotHandler(svc).UpdateConfig(w, r)

	assertStatus(t, w, http.StatusOK)
	if gotAdmin != adminID {
		t.Errorf("admin id = %v, want %v", gotAdmin, adminID)
	}
}

func TestUpdateConfig_UnauthenticatedRejected(t *testing.T) {
	w := httptest.NewRecorder()
	NewBotHandler(&mockBotService{}).UpdateConfig(w, newRequest(t, http.MethodPut, "/admin/bots/config", `{}`))
	assertStatus(t, w, http.StatusUnauthorized)
}

func TestPauseAndResume_PassTheIntendedState(t *testing.T) {
	for _, tt := range []struct {
		name  string
		call  func(h *BotHandler, w http.ResponseWriter, r *http.Request)
		want  bool
		label string
	}{
		{"pause", (*BotHandler).Pause, true, "/admin/bots/pause"},
		{"resume", (*BotHandler).Resume, false, "/admin/bots/resume"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var got bool
			svc := &mockBotService{
				setPaused: func(_ context.Context, paused bool, _ uuid.UUID) (*dto.ConfigResponse, error) {
					got = paused
					return &dto.ConfigResponse{Paused: paused}, nil
				},
			}
			r := withAuth(newRequest(t, http.MethodPost, tt.label, ""), uuid.New())
			w := httptest.NewRecorder()
			tt.call(NewBotHandler(svc), w, r)

			assertStatus(t, w, http.StatusOK)
			if got != tt.want {
				t.Errorf("paused = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPause_UnauthenticatedRejected(t *testing.T) {
	w := httptest.NewRecorder()
	NewBotHandler(&mockBotService{}).Pause(w, newRequest(t, http.MethodPost, "/admin/bots/pause", ""))
	assertStatus(t, w, http.StatusUnauthorized)
}

// ─── Activity log ────────────────────────────────────────────────────────────

func TestListRuns_ParsesPaginationAndBotFilter(t *testing.T) {
	botID := uuid.New()
	var got service.RunFilter
	svc := &mockBotService{
		listRuns: func(_ context.Context, filter service.RunFilter) (*dto.ListRunsResponse, error) {
			got = filter
			return &dto.ListRunsResponse{}, nil
		},
	}

	target := "/admin/bots/runs?limit=5&offset=10&bot_id=" + botID.String()
	w := httptest.NewRecorder()
	NewBotHandler(svc).ListRuns(w, newRequest(t, http.MethodGet, target, ""))

	assertStatus(t, w, http.StatusOK)
	if got.Limit != 5 || got.Offset != 10 {
		t.Errorf("pagination = limit %d offset %d, want 5/10", got.Limit, got.Offset)
	}
	if got.BotID == nil || *got.BotID != botID {
		t.Errorf("BotID = %v, want %v", got.BotID, botID)
	}
}

func TestListRuns_NoFilterMeansEveryPersona(t *testing.T) {
	var got service.RunFilter
	svc := &mockBotService{
		listRuns: func(_ context.Context, filter service.RunFilter) (*dto.ListRunsResponse, error) {
			got = filter
			return &dto.ListRunsResponse{}, nil
		},
	}
	w := httptest.NewRecorder()
	NewBotHandler(svc).ListRuns(w, newRequest(t, http.MethodGet, "/admin/bots/runs", ""))

	assertStatus(t, w, http.StatusOK)
	if got.BotID != nil {
		t.Errorf("BotID = %v, want nil", got.BotID)
	}
}

// Ignoring an unparseable bot_id would return the unfiltered log, which reads as
// "this persona did all of that".
func TestListRuns_InvalidBotFilterRejected(t *testing.T) {
	called := false
	svc := &mockBotService{
		listRuns: func(context.Context, service.RunFilter) (*dto.ListRunsResponse, error) {
			called = true
			return &dto.ListRunsResponse{}, nil
		},
	}
	w := httptest.NewRecorder()
	NewBotHandler(svc).ListRuns(w, newRequest(t, http.MethodGet, "/admin/bots/runs?bot_id=nope", ""))

	assertStatus(t, w, http.StatusBadRequest)
	if called {
		t.Error("the service should not be reached with an unparseable bot_id")
	}
}

// ─── Topics ──────────────────────────────────────────────────────────────────

func TestCreateTopic_Created(t *testing.T) {
	w := httptest.NewRecorder()
	NewBotHandler(&mockBotService{}).CreateTopic(w,
		newRequest(t, http.MethodPost, "/admin/bots/topics", `{"content":"cà phê sáng"}`))
	assertStatus(t, w, http.StatusCreated)
}

func TestSetTopicEnabled_PassesTheState(t *testing.T) {
	topicID := uuid.New()
	var gotID uuid.UUID
	var gotEnabled bool
	svc := &mockBotService{
		setTopicEnabled: func(_ context.Context, id uuid.UUID, enabled bool) (*dto.TopicResponse, error) {
			gotID, gotEnabled = id, enabled
			return &dto.TopicResponse{}, nil
		},
	}

	r := withID(newRequest(t, http.MethodPatch, "/admin/bots/topics/"+topicID.String(), `{"enabled":false}`), topicID.String())
	w := httptest.NewRecorder()
	NewBotHandler(svc).SetTopicEnabled(w, r)

	assertStatus(t, w, http.StatusOK)
	if gotID != topicID || gotEnabled {
		t.Errorf("id = %v enabled = %v", gotID, gotEnabled)
	}
}

func TestDeleteTopic_NoContent(t *testing.T) {
	topicID := uuid.New()
	r := withID(newRequest(t, http.MethodDelete, "/admin/bots/topics/"+topicID.String(), ""), topicID.String())
	w := httptest.NewRecorder()
	NewBotHandler(&mockBotService{}).DeleteTopic(w, r)
	assertStatus(t, w, http.StatusNoContent)
}

func TestListTopics_Success(t *testing.T) {
	w := httptest.NewRecorder()
	NewBotHandler(&mockBotService{}).ListTopics(w, newRequest(t, http.MethodGet, "/admin/bots/topics", ""))
	assertStatus(t, w, http.StatusOK)
}

func TestGetConfig_Success(t *testing.T) {
	w := httptest.NewRecorder()
	NewBotHandler(&mockBotService{}).GetConfig(w, newRequest(t, http.MethodGet, "/admin/bots/config", ""))
	assertStatus(t, w, http.StatusOK)
}

// ─── Agent plane ─────────────────────────────────────────────────────────────

func TestGetPlan_Success(t *testing.T) {
	svc := &mockBotService{
		getPlan: func(context.Context) (*dto.PlanResponse, error) {
			return &dto.PlanResponse{
				PostIntervalSeconds: 120,
				Models:              []string{"gemini-2.5-flash"},
				Bots:                []dto.PlanBot{{Username: "bot_sky", RunRequested: true}},
				Topics:              []string{"cà phê sáng"},
			}, nil
		},
	}
	w := httptest.NewRecorder()
	NewBotHandler(svc).GetPlan(w, newRequest(t, http.MethodGet, "/bot/plan", ""))

	assertStatus(t, w, http.StatusOK)
	var plan dto.PlanResponse
	if err := json.NewDecoder(w.Body).Decode(&plan); err != nil {
		t.Fatalf("failed to decode plan: %v", err)
	}
	if plan.PostIntervalSeconds != 120 || len(plan.Bots) != 1 || !plan.Bots[0].RunRequested {
		t.Errorf("plan = %+v", plan)
	}
}

func TestReportRun_NoContent(t *testing.T) {
	var got *dto.ReportRunRequest
	svc := &mockBotService{
		reportRun: func(_ context.Context, req *dto.ReportRunRequest) error {
			got = req
			return nil
		},
	}
	postID := uuid.New().String()
	body := `{"bot_id":"` + uuid.New().String() + `","status":"success","post_id":"` + postID +
		`","model_used":"gemini-2.5-flash","honored_run_request":true}`

	w := httptest.NewRecorder()
	NewBotHandler(svc).ReportRun(w, newRequest(t, http.MethodPost, "/bot/runs", body))

	assertStatus(t, w, http.StatusNoContent)
	if got == nil || !got.HonoredRunRequest {
		t.Fatalf("decoded request = %+v", got)
	}
	if got.PostID == nil || *got.PostID != postID {
		t.Errorf("PostID = %v, want %v", got.PostID, postID)
	}
}

// The flag must default to false, or an ordinary interval post would clear a
// pending run-now request that the bot never acted on.
func TestReportRun_HonoredFlagDefaultsToFalse(t *testing.T) {
	var got *dto.ReportRunRequest
	svc := &mockBotService{
		reportRun: func(_ context.Context, req *dto.ReportRunRequest) error {
			got = req
			return nil
		},
	}
	body := `{"bot_id":"` + uuid.New().String() + `","status":"error","error":"quota exhausted"}`

	w := httptest.NewRecorder()
	NewBotHandler(svc).ReportRun(w, newRequest(t, http.MethodPost, "/bot/runs", body))

	assertStatus(t, w, http.StatusNoContent)
	if got.HonoredRunRequest {
		t.Error("honored_run_request should be false when the bot omits it")
	}
}

func TestReportRun_ServiceRejectionPropagates(t *testing.T) {
	svc := &mockBotService{
		reportRun: func(context.Context, *dto.ReportRunRequest) error {
			return errors.NewBadRequestError("inconsistent outcome")
		},
	}
	w := httptest.NewRecorder()
	NewBotHandler(svc).ReportRun(w, newRequest(t, http.MethodPost, "/bot/runs", `{"status":"success"}`))
	assertStatus(t, w, http.StatusBadRequest)
}

func TestLinkIdentity_NoContent(t *testing.T) {
	botID := uuid.New()
	userID := uuid.New()
	var gotBot uuid.UUID
	var gotReq *dto.LinkIdentityRequest
	svc := &mockBotService{
		linkIdentity: func(_ context.Context, id uuid.UUID, req *dto.LinkIdentityRequest) error {
			gotBot, gotReq = id, req
			return nil
		},
	}

	r := withID(newRequest(t, http.MethodPost, "/bot/"+botID.String()+"/identity",
		`{"user_id":"`+userID.String()+`"}`), botID.String())
	w := httptest.NewRecorder()
	NewBotHandler(svc).LinkIdentity(w, r)

	assertStatus(t, w, http.StatusNoContent)
	if gotBot != botID || gotReq.UserID != userID.String() {
		t.Errorf("bot = %v user = %q", gotBot, gotReq.UserID)
	}
}

func TestLinkIdentity_InvalidBodyRejected(t *testing.T) {
	botID := uuid.New()
	r := withID(newRequest(t, http.MethodPost, "/bot/"+botID.String()+"/identity", "{not json"), botID.String())
	w := httptest.NewRecorder()
	NewBotHandler(&mockBotService{}).LinkIdentity(w, r)
	assertStatus(t, w, http.StatusBadRequest)
}
