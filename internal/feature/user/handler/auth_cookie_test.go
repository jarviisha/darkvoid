package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Secure, SameSite and Domain now come from configuration, while Path and
// HttpOnly stay fixed. These tests pin both halves of that split, plus the
// agreement between the setting and the clearing cookie — a mismatch there is
// how a logout stops logging out.

func cookieWriter(opts CookieOptions) *AuthHandler {
	return &AuthHandler{cookies: opts}
}

func TestSetRefreshTokenCookie_AppliesConfiguredAttributes(t *testing.T) {
	h := cookieWriter(CookieOptions{
		Secure:   true,
		SameSite: http.SameSiteNoneMode,
		Domain:   "example.com",
	})

	w := httptest.NewRecorder()
	h.setRefreshTokenCookie(w, "token-value", time.Hour)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	c := cookies[0]

	if c.Name != refreshTokenCookieName {
		t.Errorf("Name = %q, want %q", c.Name, refreshTokenCookieName)
	}
	if c.Value != "token-value" {
		t.Errorf("Value = %q, want %q", c.Value, "token-value")
	}
	if !c.Secure {
		t.Errorf("Secure = false, want the configured true")
	}
	if c.SameSite != http.SameSiteNoneMode {
		t.Errorf("SameSite = %v, want SameSiteNoneMode", c.SameSite)
	}
	if c.Domain != "example.com" {
		t.Errorf("Domain = %q, want %q", c.Domain, "example.com")
	}
	if c.MaxAge != int(time.Hour.Seconds()) {
		t.Errorf("MaxAge = %d, want %d", c.MaxAge, int(time.Hour.Seconds()))
	}
}

func TestSetRefreshTokenCookie_PathAndHttpOnlyAreNotConfigurable(t *testing.T) {
	// Both are deliberately absent from CookieOptions. HttpOnly false would make
	// any XSS a session theft; a Path that drifts from where the auth routes are
	// mounted yields a cookie the browser never sends back.
	h := cookieWriter(CookieOptions{})

	w := httptest.NewRecorder()
	h.setRefreshTokenCookie(w, "token-value", time.Hour)

	c := w.Result().Cookies()[0]
	if !c.HttpOnly {
		t.Errorf("HttpOnly = false; the refresh token must not be readable from JavaScript")
	}
	if c.Path != "/api/v1/auth" {
		t.Errorf("Path = %q, want %q — it must match where the auth routes are mounted", c.Path, "/api/v1/auth")
	}
}

func TestClearRefreshTokenCookie_MatchesTheCookieItDeletes(t *testing.T) {
	// A browser matches a deletion on Name, Domain and Path. Disagree on any of
	// the three and the original cookie survives, so logout returns 200 while the
	// refresh token stays usable in the browser.
	opts := CookieOptions{Secure: true, SameSite: http.SameSiteLaxMode, Domain: "example.com"}
	h := cookieWriter(opts)

	setRec := httptest.NewRecorder()
	h.setRefreshTokenCookie(setRec, "token-value", time.Hour)
	set := setRec.Result().Cookies()[0]

	clearRec := httptest.NewRecorder()
	h.clearRefreshTokenCookie(clearRec)
	cleared := clearRec.Result().Cookies()[0]

	if cleared.Name != set.Name {
		t.Errorf("Name = %q, want %q", cleared.Name, set.Name)
	}
	if cleared.Domain != set.Domain {
		t.Errorf("Domain = %q, want %q", cleared.Domain, set.Domain)
	}
	if cleared.Path != set.Path {
		t.Errorf("Path = %q, want %q", cleared.Path, set.Path)
	}
	if cleared.Value != "" {
		t.Errorf("Value = %q, want empty", cleared.Value)
	}
	if cleared.MaxAge != -1 {
		t.Errorf("MaxAge = %d, want -1", cleared.MaxAge)
	}
}

func TestRefreshTokenCookie_EmptyDomainStaysHostOnly(t *testing.T) {
	// The default deployment. An accidental Domain would widen the cookie to
	// every subdomain, which is a larger blast radius than the default asks for.
	h := cookieWriter(CookieOptions{SameSite: http.SameSiteLaxMode})

	w := httptest.NewRecorder()
	h.setRefreshTokenCookie(w, "token-value", time.Hour)

	if domain := w.Result().Cookies()[0].Domain; domain != "" {
		t.Errorf("Domain = %q, want empty so the cookie stays host-only", domain)
	}
}
