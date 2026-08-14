package middleware

import (
	"fmt"
	"net/http"
	"net/netip"
	"strings"
)

// TrustedRealIP updates RemoteAddr from forwarding headers only when the
// immediate network peer belongs to a configured trusted proxy CIDR.
func TrustedRealIP(trustedCIDRs []string) (func(http.Handler) http.Handler, error) {
	trustedPrefixes := make([]netip.Prefix, 0, len(trustedCIDRs))
	for _, cidr := range trustedCIDRs {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy CIDR %q: %w", cidr, err)
		}
		trustedPrefixes = append(trustedPrefixes, prefix.Masked())
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			peerIP, ok := parseRemoteIP(r.RemoteAddr)
			if !ok || !containsIP(trustedPrefixes, peerIP) {
				next.ServeHTTP(w, r)
				return
			}

			if forwardedValues := r.Header.Values("X-Forwarded-For"); len(forwardedValues) > 0 {
				forwardedIPs, valid := parseForwardedFor(forwardedValues)
				if valid {
					r.RemoteAddr = clientIP(forwardedIPs, trustedPrefixes).String()
				}
				next.ServeHTTP(w, r)
				return
			}

			if realIP, valid := parseHeaderIP(r.Header.Get("X-Real-IP")); valid {
				r.RemoteAddr = realIP.String()
			}

			next.ServeHTTP(w, r)
		})
	}, nil
}

func parseRemoteIP(remoteAddr string) (netip.Addr, bool) {
	if addressPort, err := netip.ParseAddrPort(remoteAddr); err == nil {
		return addressPort.Addr().Unmap(), true
	}
	return parseHeaderIP(remoteAddr)
}

func parseHeaderIP(value string) (netip.Addr, bool) {
	address, err := netip.ParseAddr(strings.Trim(strings.TrimSpace(value), "[]"))
	if err != nil {
		return netip.Addr{}, false
	}
	return address.Unmap(), true
}

func parseForwardedFor(values []string) ([]netip.Addr, bool) {
	parts := strings.Split(strings.Join(values, ","), ",")
	addresses := make([]netip.Addr, 0, len(parts))
	for _, part := range parts {
		address, ok := parseHeaderIP(part)
		if !ok {
			return nil, false
		}
		addresses = append(addresses, address)
	}
	return addresses, len(addresses) > 0
}

func clientIP(forwardedIPs []netip.Addr, trustedPrefixes []netip.Prefix) netip.Addr {
	for i := len(forwardedIPs) - 1; i >= 0; i-- {
		if !containsIP(trustedPrefixes, forwardedIPs[i]) {
			return forwardedIPs[i]
		}
	}
	return forwardedIPs[0]
}

func containsIP(prefixes []netip.Prefix, address netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
