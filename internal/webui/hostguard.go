package webui

import (
	"net"
	"net/http"
)

// HostGuard is the DNS-rebinding Host-header allowlist. Loopback and
// "localhost" always pass; other hosts must be explicitly allowed. A single
// "*" entry disables the guard (the user accepted network exposure).
type HostGuard struct {
	allowed  map[string]struct{}
	allowAny bool
}

func NewHostGuard(allowedHosts []string) *HostGuard {
	guard := &HostGuard{allowed: make(map[string]struct{})}
	for _, host := range allowedHosts {
		if host == "*" {
			guard.allowAny = true
			continue
		}
		guard.allowed[host] = struct{}{}
	}
	return guard
}

func (guard *HostGuard) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !guard.Allowed(request.Host) {
			http.Error(writer, "forbidden: untrusted Host header", http.StatusForbidden)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func (guard *HostGuard) Allowed(hostHeader string) bool {
	if guard.allowAny {
		return true
	}
	hostname := hostHeader
	if host, _, err := net.SplitHostPort(hostHeader); err == nil {
		hostname = host
	}
	if hostname == "localhost" {
		return true
	}
	if ip := net.ParseIP(hostname); ip != nil && ip.IsLoopback() {
		return true
	}
	_, ok := guard.allowed[hostname]
	return ok
}
