package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	appMiddleware "github.com/jarviisha/darkvoid/internal/app/middleware"
	botHandler "github.com/jarviisha/darkvoid/internal/feature/bot/handler"
)

// passthroughAuth stands in for the real auth middleware. These tests assert the
// shape of the route table, not who is allowed through it.
func passthroughAuth() appMiddleware.AuthMiddleware {
	nop := func(next http.Handler) http.Handler { return next }
	return appMiddleware.AuthMiddleware{Required: nop, Optional: nop}
}

// stubRoleChecker satisfies the RequireRole dependency without a database.
type stubRoleChecker struct{}

func (stubRoleChecker) UserHasAnyRole(context.Context, uuid.UUID, []string) (bool, error) {
	return true, nil
}

// walkRoutes returns every registered route as "METHOD pattern", sorted. chi panics
// at registration time on an overlapping pattern, so a conflict shows up here as a
// failing test rather than a crash on the first boot after a merge.
func walkRoutes(t *testing.T, r chi.Router) []string {
	t.Helper()
	var routes []string
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes = append(routes, method+" "+route)
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk routes: %v", err)
	}
	slices.Sort(routes)
	return routes
}

// newRouteTestBotContext builds a BotContext with a handler over a nil service. The
// routing tests never dispatch, so the service is never called.
func newRouteTestBotContext() *BotContext {
	return &BotContext{botHandler: botHandler.NewBotHandler(nil)}
}

func assertRoutes(t *testing.T, got, want []string) {
	t.Helper()
	if slices.Equal(got, want) {
		return
	}
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("missing route: %s", w)
		}
	}
	for _, g := range got {
		if !slices.Contains(want, g) {
			t.Errorf("unexpected route: %s", g)
		}
	}
}

// The admin surface has to live under /admin so it inherits that group's role
// check. This pins the exact set an operator (and the admin SPA) can call.
func TestBotAdminRoutes_Surface(t *testing.T) {
	router := chi.NewRouter()
	router.Route("/admin", func(r chi.Router) {
		newRouteTestBotContext().registerAdminRoutes(r)
	})

	assertRoutes(t, walkRoutes(t, router), []string{
		"DELETE /admin/bots/topics/{id}",
		"DELETE /admin/bots/{id}",
		"GET /admin/bots/",
		"GET /admin/bots/config",
		"GET /admin/bots/runs",
		"GET /admin/bots/topics/",
		"PATCH /admin/bots/topics/{id}",
		"PATCH /admin/bots/{id}",
		"POST /admin/bots/",
		"POST /admin/bots/pause",
		"POST /admin/bots/resume",
		"POST /admin/bots/topics/",
		"POST /admin/bots/{id}/run-now",
		"PUT /admin/bots/config",
	})
}

// denyAll stands in for the /admin group's auth and role middleware, recording
// whether it was consulted at all.
func denyAll(reached *bool) func(http.Handler) http.Handler {
	return func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			*reached = true
			w.WriteHeader(http.StatusForbidden)
		})
	}
}

// Every operator route has to sit behind the /admin group's guard. This is the
// reason registerAdminRoutes takes the group's router instead of registering its
// own top-level route — see the companion test below for what the alternative costs.
func TestBotAdminRoutes_InheritTheAdminGroupGuard(t *testing.T) {
	var guardReached bool
	router := chi.NewRouter()
	router.Route("/admin", func(r chi.Router) {
		r.Use(denyAll(&guardReached))
		newRouteTestBotContext().registerAdminRoutes(r)
	})

	for _, tt := range []struct{ method, target string }{
		{http.MethodGet, "/admin/bots/"},
		{http.MethodPost, "/admin/bots/"},
		{http.MethodGet, "/admin/bots/config"},
		{http.MethodPut, "/admin/bots/config"},
		{http.MethodPost, "/admin/bots/pause"},
		{http.MethodPost, "/admin/bots/resume"},
		{http.MethodGet, "/admin/bots/runs"},
		{http.MethodGet, "/admin/bots/topics/"},
		{http.MethodPost, "/admin/bots/topics/"},
		{http.MethodPatch, "/admin/bots/topics/8f14e45f-ceea-467a-9c1e-000000000000"},
		{http.MethodDelete, "/admin/bots/topics/8f14e45f-ceea-467a-9c1e-000000000000"},
		{http.MethodPatch, "/admin/bots/8f14e45f-ceea-467a-9c1e-000000000000"},
		{http.MethodDelete, "/admin/bots/8f14e45f-ceea-467a-9c1e-000000000000"},
		{http.MethodPost, "/admin/bots/8f14e45f-ceea-467a-9c1e-000000000000/run-now"},
	} {
		guardReached = false
		w := httptest.NewRecorder()
		req, err := http.NewRequestWithContext(t.Context(), tt.method, tt.target, nil)
		if err != nil {
			t.Fatalf("failed to build request: %v", err)
		}
		router.ServeHTTP(w, req)

		if !guardReached {
			t.Errorf("%s %s bypassed the admin guard entirely", tt.method, tt.target)
		}
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s status = %d, want 403 from the guard", tt.method, tt.target, w.Code)
		}
	}
}

// Registering /admin/bots as a sibling of /admin looks equivalent and is not: chi
// accepts both patterns and resolves them correctly, but a sibling subrouter does
// not inherit the /admin group's middleware, so the whole operator surface would
// answer unauthenticated. The failure is a silent auth bypass rather than a crash,
// which is exactly why this is pinned.
func TestBotAdminRoutes_SiblingRegistrationWouldBypassTheGuard(t *testing.T) {
	var guardReached bool
	router := chi.NewRouter()
	router.Route("/admin", func(r chi.Router) {
		r.Use(denyAll(&guardReached))
		r.Get("/stats", func(http.ResponseWriter, *http.Request) {})
	})
	router.Route("/admin/bots", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	})

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "/admin/bots/", nil)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if guardReached {
		t.Fatal("chi behaviour changed: a sibling subrouter now inherits the parent group's middleware, " +
			"so registerAdminRoutes no longer has to be nested")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 — the point is that the sibling answers unguarded", w.Code)
	}
}

// The guard tests above pin chi's semantics on replica routers; this one pins the
// production call site. AdminContext.RegisterRoutes is exercised for real — a zero
// AdminContext works because handler method values and the RequireRole checker are
// captured at registration and only dereferenced on dispatch, which the denying
// auth middleware never reaches. If bot.registerAdminRoutes ever moves out of the
// r.Route("/admin", ...) closure — or registers as a sibling — the routes still
// resolve, but no longer behind the group's middleware, and this fails.
func TestAdminRegisterRoutes_PutsBotRoutesBehindTheGroupGuard(t *testing.T) {
	var guardReached bool
	auth := appMiddleware.AuthMiddleware{
		Required: denyAll(&guardReached),
		Optional: func(next http.Handler) http.Handler { return next },
	}

	router := chi.NewRouter()
	(&AdminContext{}).RegisterRoutes(router, auth, newRouteTestBotContext())

	for _, tt := range []struct{ method, target string }{
		{http.MethodGet, "/admin/bots/"},
		{http.MethodPost, "/admin/bots/"},
		{http.MethodPut, "/admin/bots/config"},
		{http.MethodPost, "/admin/bots/pause"},
		{http.MethodGet, "/admin/bots/runs"},
		{http.MethodDelete, "/admin/bots/topics/8f14e45f-ceea-467a-9c1e-000000000000"},
		{http.MethodPost, "/admin/bots/8f14e45f-ceea-467a-9c1e-000000000000/run-now"},
	} {
		guardReached = false
		w := httptest.NewRecorder()
		req, err := http.NewRequestWithContext(t.Context(), tt.method, tt.target, nil)
		if err != nil {
			t.Fatalf("failed to build request: %v", err)
		}
		router.ServeHTTP(w, req)

		if !guardReached {
			t.Errorf("%s %s bypassed the admin guard in the production wiring", tt.method, tt.target)
		}
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s status = %d, want 403 from the guard", tt.method, tt.target, w.Code)
		}
	}

	// The guard checks alone would still pass if the bot routes vanished from the
	// group entirely (any /admin/* request runs the group middleware before 404ing),
	// so also pin that the production router actually carries the surface.
	routes := walkRoutes(t, router)
	for _, want := range []string{
		"GET /admin/bots/",
		"POST /admin/bots/",
		"PUT /admin/bots/config",
		"GET /admin/bots/runs",
		"POST /admin/bots/{id}/run-now",
	} {
		if !slices.Contains(routes, want) {
			t.Errorf("production wiring is missing %s", want)
		}
	}
}

// A static segment and /{id} sit at the same depth under /admin/bots, so this
// checks chi resolves /admin/bots/config to the config handler rather than treating
// "config" as a persona id.
func TestBotAdminRoutes_StaticSegmentsBeatTheIDWildcard(t *testing.T) {
	router := chi.NewRouter()
	var hit string
	router.Route("/admin", func(r chi.Router) {
		r.Route("/bots", func(r chi.Router) {
			r.Get("/config", func(http.ResponseWriter, *http.Request) { hit = "config" })
			r.Get("/runs", func(http.ResponseWriter, *http.Request) { hit = "runs" })
			r.Patch("/{id}", func(http.ResponseWriter, *http.Request) { hit = "persona" })
			r.Get("/{id}", func(http.ResponseWriter, *http.Request) { hit = "persona" })
		})
	})

	for _, tt := range []struct {
		method, target, want string
	}{
		{http.MethodGet, "/admin/bots/config", "config"},
		{http.MethodGet, "/admin/bots/runs", "runs"},
		{http.MethodGet, "/admin/bots/" + "8f14e45f-ceea-467a-9c1e-000000000000", "persona"},
	} {
		hit = ""
		req, err := http.NewRequestWithContext(t.Context(), tt.method, tt.target, nil)
		if err != nil {
			t.Fatalf("failed to build request: %v", err)
		}
		router.ServeHTTP(httptest.NewRecorder(), req)
		if hit != tt.want {
			t.Errorf("%s %s reached %q, want %q", tt.method, tt.target, hit, tt.want)
		}
	}
}

// The agent plane is its own group: it is guarded by the bot role, so an admin
// token is rejected here and a bot token is rejected on /admin.
func TestBotAgentRoutes_Surface(t *testing.T) {
	router := chi.NewRouter()
	newRouteTestBotContext().RegisterRoutes(router, passthroughAuth(), stubRoleChecker{})

	assertRoutes(t, walkRoutes(t, router), []string{
		"GET /bot/plan",
		"POST /bot/runs",
		"POST /bot/{id}/identity",
	})
}

// /bot and /admin/bots coexist: the agent plane must not collide with the operator
// routes now that both exist on the same router.
func TestBotRoutes_BothPlanesRegisterTogether(t *testing.T) {
	ctx := newRouteTestBotContext()
	router := chi.NewRouter()
	router.Route("/admin", func(r chi.Router) {
		ctx.registerAdminRoutes(r)
	})
	ctx.RegisterRoutes(router, passthroughAuth(), stubRoleChecker{})

	routes := walkRoutes(t, router)
	for _, want := range []string{"GET /admin/bots/", "GET /bot/plan"} {
		if !slices.Contains(routes, want) {
			t.Errorf("missing route %s after registering both planes", want)
		}
	}
}
