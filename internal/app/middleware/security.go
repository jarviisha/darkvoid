package middleware

import (
	"net/http"
	"strings"
)

const (
	permissionsPolicy           = "camera=(), geolocation=(), microphone=(), payment=(), usb=()"
	apiContentSecurityPolicy    = "default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
	uploadContentSecurityPolicy = apiContentSecurityPolicy + "; sandbox"
)

// SecurityHeaders adds browser hardening headers that are safe for every API
// response, including locally served user uploads.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setCommonSecurityHeaders(w.Header())
		next.ServeHTTP(w, r)
	})
}

// APIHeaders prevents API responses from creating executable, navigable, or
// embeddable browser resource contexts. Swagger routes intentionally omit this
// policy because their UI requires scripts and styles.
func APIHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", apiContentSecurityPolicy)
		next.ServeHTTP(w, r)
	})
}

// UploadedFileHeaders sandboxes locally served user content if it is opened as
// a top-level document. Valid images remain usable as normal subresources.
func UploadedFileHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setCommonSecurityHeaders(w.Header())
		w.Header().Set("Content-Security-Policy", uploadContentSecurityPolicy)
		next.ServeHTTP(w, r)
	})
}

// HiddenFileGuard refuses any request whose path contains a dot-prefixed
// segment, so nothing the static file server sits on top of can be fetched by a
// hidden name.
//
// Media keys are generated as "media/<uuid><ext>", so no reachable object is
// excluded. What this does exclude is everything the upload directory holds that
// is not an upload — the local storage provider's health probe most of all, which
// is created inside that same directory because that is the only way to prove the
// directory is writable, and would otherwise be briefly fetchable under its own
// name.
func HiddenFileGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, segment := range strings.Split(r.URL.Path, "/") {
			if strings.HasPrefix(segment, ".") {
				http.NotFound(w, r)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func setCommonSecurityHeaders(header http.Header) {
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("Permissions-Policy", permissionsPolicy)
}
