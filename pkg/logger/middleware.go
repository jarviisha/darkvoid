package logger

import (
	"net/http"
	"time"

	"github.com/google/uuid"
)

// HTTPMiddleware logs HTTP requests and injects a correlation logger into the context.
// Only request_id is stored on the context logger so that intermediate log lines carry
// the correlation ID without duplicating HTTP-level fields on every entry.
// All HTTP fields (method, path, status, duration, remote_addr, user_agent) are written
// once in the final access-log line after the handler returns.
func HTTPMiddleware(logger *Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Prefer the client-supplied X-Request-ID for distributed tracing;
			// generate a fresh UUID when absent.
			requestID := r.Header.Get("X-Request-ID")
			if requestID == "" {
				requestID = uuid.New().String()
			}
			w.Header().Set("X-Request-ID", requestID)

			// Inject a request-scoped logger with only request_id.
			// method/path/etc. are NOT added here to avoid duplicate fields in the
			// final LogRequest call below.
			ctx := WithLogger(r.Context(), logger.With("request_id", requestID))
			r = r.WithContext(ctx)

			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(wrapped, r)

			// Single access-log entry written after the handler completes.
			// FromContext picks up the latest logger (which may have user_id added
			// by auth middleware during the request).
			FromContext(r.Context()).LogRequest(
				r.Method,
				r.URL.Path,
				wrapped.statusCode,
				float64(time.Since(start).Milliseconds()),
				"remote_addr", r.RemoteAddr,
				"user_agent", r.UserAgent(),
			)
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.written = true
		rw.ResponseWriter.WriteHeader(code)
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

// Flush implements http.Flusher so SSE and streaming handlers work correctly.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap returns the underlying ResponseWriter, allowing http.ResponseController
// and middleware chains to traverse the wrapper stack.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// RecoveryMiddleware logs panics and recovers
func RecoveryMiddleware(logger *Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					logger.Error("panic recovered",
						"error", err,
						"path", r.URL.Path,
						"method", r.Method,
					)
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
