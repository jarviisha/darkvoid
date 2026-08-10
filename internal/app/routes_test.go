package app

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"
)

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

// Registering a subgroup as a sibling of /admin looks equivalent to nesting it and
// is not: chi accepts both patterns and resolves them correctly, but a sibling
// subrouter does not inherit the /admin group's middleware, so the whole surface
// would answer unauthenticated. The failure is a silent auth bypass rather than a
// crash, which is exactly why this is pinned — it is the reason every
// registerAdminRoutes takes the group's router instead of registering its own
// top-level route.
func TestAdminRoutes_SiblingRegistrationWouldBypassTheGuard(t *testing.T) {
	var guardReached bool
	router := chi.NewRouter()
	router.Route("/admin", func(r chi.Router) {
		r.Use(denyAll(&guardReached))
		r.Get("/stats", func(http.ResponseWriter, *http.Request) {})
	})
	router.Route("/admin/settings", func(r chi.Router) {
		r.Get("/feed", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	})

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "/admin/settings/feed", nil)
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
