package app

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"
	appMiddleware "github.com/jarviisha/darkvoid/internal/app/middleware"
	settingsHandler "github.com/jarviisha/darkvoid/internal/feature/settings/handler"
)

// newRouteTestSettingsContext builds a SettingsContext with a handler over a nil
// service. The routing tests never dispatch, so the service is never called.
func newRouteTestSettingsContext() *SettingsContext {
	return &SettingsContext{settingsHandler: settingsHandler.NewSettingsHandler(nil)}
}

func TestSettingsRegisterAdminRoutes_MountsFeedSettings(t *testing.T) {
	router := chi.NewRouter()
	router.Route("/admin", func(r chi.Router) {
		newRouteTestSettingsContext().registerAdminRoutes(r)
	})

	assertRoutes(t, walkRoutes(t, router), []string{
		"GET /admin/settings/feed",
		"PATCH /admin/settings/feed",
	})
}

// Every operator route has to sit behind the /admin group's guard. This exercises
// a replica router; the companion below exercises the production call site, and
// TestAdminRoutes_SiblingRegistrationWouldBypassTheGuard pins what the alternative
// registration would cost.
func TestSettingsAdminRoutes_InheritTheAdminGroupGuard(t *testing.T) {
	var guardReached bool
	router := chi.NewRouter()
	router.Route("/admin", func(r chi.Router) {
		r.Use(denyAll(&guardReached))
		newRouteTestSettingsContext().registerAdminRoutes(r)
	})

	for _, tt := range []struct{ method, target string }{
		{http.MethodGet, "/admin/settings/feed"},
		{http.MethodPatch, "/admin/settings/feed"},
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

// The guard tests above pin chi's semantics on replica routers; this one pins the
// production call site. AdminContext.RegisterRoutes is exercised for real — a zero
// AdminContext works because handler method values are captured at registration and
// only dereferenced on dispatch, which the denying auth middleware never reaches.
//
// The reason this matters more here than for any other admin surface: it rewrites
// the feed's ranking weights and rollout percent. If settings.registerAdminRoutes
// ever moves out of the r.Route("/admin", ...) closure the paths still resolve, the
// subrouter inherits none of the group's middleware, and anyone can change how
// every user's feed is ordered.
func TestAdminRegisterRoutes_PutsSettingsRoutesBehindTheGroupGuard(t *testing.T) {
	var guardReached bool
	auth := appMiddleware.AuthMiddleware{
		Required: denyAll(&guardReached),
		Optional: func(next http.Handler) http.Handler { return next },
	}

	router := chi.NewRouter()
	(&AdminContext{}).RegisterRoutes(router, auth, newRouteTestSettingsContext())

	for _, tt := range []struct{ method, target string }{
		{http.MethodGet, "/admin/settings/feed"},
		{http.MethodPatch, "/admin/settings/feed"},
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

	// The guard checks alone would still pass if the routes vanished from the group
	// entirely — any /admin/* request runs the group middleware before 404ing — so
	// pin that the production router actually carries the surface.
	routes := walkRoutes(t, router)
	for _, want := range []string{
		"GET /admin/settings/feed",
		"PATCH /admin/settings/feed",
	} {
		if !slices.Contains(routes, want) {
			t.Errorf("production wiring is missing %s", want)
		}
	}
}
