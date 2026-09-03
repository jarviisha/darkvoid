package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTrustedRealIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		remoteAddr     string
		forwardedFor   string
		realIP         string
		trustedCIDRs   []string
		wantRemoteAddr string
	}{
		{
			name:           "untrusted peer cannot spoof forwarded headers",
			remoteAddr:     "203.0.113.10:45000",
			forwardedFor:   "198.51.100.20",
			realIP:         "198.51.100.21",
			trustedCIDRs:   []string{"10.0.0.0/8"},
			wantRemoteAddr: "203.0.113.10:45000",
		},
		{
			name:           "trusted peer forwards client IP",
			remoteAddr:     "10.0.0.2:45000",
			forwardedFor:   "198.51.100.20",
			trustedCIDRs:   []string{"10.0.0.0/8"},
			wantRemoteAddr: "198.51.100.20",
		},
		{
			name:           "trusted peer uses real IP fallback",
			remoteAddr:     "10.0.0.2:45000",
			realIP:         "198.51.100.21",
			trustedCIDRs:   []string{"10.0.0.0/8"},
			wantRemoteAddr: "198.51.100.21",
		},
		{
			name:           "rightmost untrusted hop wins",
			remoteAddr:     "10.0.0.2:45000",
			forwardedFor:   "192.0.2.99, 198.51.100.20, 10.0.0.1",
			trustedCIDRs:   []string{"10.0.0.0/8"},
			wantRemoteAddr: "198.51.100.20",
		},
		{
			name:           "invalid forwarded chain fails closed",
			remoteAddr:     "10.0.0.2:45000",
			forwardedFor:   "198.51.100.20, not-an-ip",
			realIP:         "192.0.2.10",
			trustedCIDRs:   []string{"10.0.0.0/8"},
			wantRemoteAddr: "10.0.0.2:45000",
		},
		{
			name:           "IPv6 peer and client",
			remoteAddr:     "[2001:db8:1::2]:45000",
			forwardedFor:   "2001:db8:2::5",
			trustedCIDRs:   []string{"2001:db8:1::/48"},
			wantRemoteAddr: "2001:db8:2::5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			middleware, err := TrustedRealIP(tt.trustedCIDRs)
			if err != nil {
				t.Fatalf("TrustedRealIP() error = %v", err)
			}

			var gotRemoteAddr string
			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotRemoteAddr = r.RemoteAddr
				w.WriteHeader(http.StatusNoContent)
			}))
			request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
			request.RemoteAddr = tt.remoteAddr
			if tt.forwardedFor != "" {
				request.Header.Set("X-Forwarded-For", tt.forwardedFor)
			}
			if tt.realIP != "" {
				request.Header.Set("X-Real-IP", tt.realIP)
			}

			handler.ServeHTTP(httptest.NewRecorder(), request)

			if gotRemoteAddr != tt.wantRemoteAddr {
				t.Fatalf("RemoteAddr = %q, want %q", gotRemoteAddr, tt.wantRemoteAddr)
			}
		})
	}
}

func TestTrustedRealIP_RejectsInvalidCIDR(t *testing.T) {
	t.Parallel()

	if _, err := TrustedRealIP([]string{"not-a-cidr"}); err == nil {
		t.Fatal("TrustedRealIP() error = nil, want invalid CIDR error")
	}
}

func TestRateLimitByIP_UntrustedPeerCannotRotateForwardedIP(t *testing.T) {
	t.Parallel()

	realIP, err := TrustedRealIP([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("TrustedRealIP() error = %v", err)
	}
	handler := realIP(RateLimitByIP(1, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))

	for i, forwardedFor := range []string{"198.51.100.1", "198.51.100.2"} {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
		request.RemoteAddr = "203.0.113.10:45000"
		request.Header.Set("X-Forwarded-For", forwardedFor)
		response := httptest.NewRecorder()

		handler.ServeHTTP(response, request)

		wantStatus := http.StatusNoContent
		if i == 1 {
			wantStatus = http.StatusTooManyRequests
		}
		if response.Code != wantStatus {
			t.Fatalf("request %d status = %d, want %d", i+1, response.Code, wantStatus)
		}
	}
}
