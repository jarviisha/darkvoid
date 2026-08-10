// Package handler serves the /admin/settings/* operator surface.
//
// The routes are mounted from inside AdminContext.RegisterRoutes' /admin group so
// they inherit its auth and admin role check. Declaring them as a sibling of
// /admin would resolve correctly and inherit no middleware, leaving a surface
// that changes the feed's ranking answerable to anyone. Pinned in
// internal/app/settings_routes_test.go and internal/app/routes_test.go.
package handler
