package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHostGuardAllowed(test *testing.T) {
	cases := []struct {
		name    string
		allowed []string
		host    string
		want    bool
	}{
		{"loopback ip", nil, "127.0.0.1:7373", true},
		{"localhost", nil, "localhost:7373", true},
		{"ipv6 loopback", nil, "[::1]:7373", true},
		{"unknown host blocked", nil, "evil.example.com", false},
		{"lan ip blocked by default", nil, "192.168.1.5:7373", false},
		{"explicit allow", []string{"host.docker.internal"}, "host.docker.internal:7373", true},
		{"wildcard allows any", []string{"*"}, "evil.example.com", true},
	}
	for _, tc := range cases {
		test.Run(tc.name, func(test *testing.T) {
			guard := NewHostGuard(tc.allowed)
			if got := guard.Allowed(tc.host); got != tc.want {
				test.Fatalf("Allowed(%q)=%v want %v", tc.host, got, tc.want)
			}
		})
	}
}

func TestHostGuardWrapBlocks(test *testing.T) {
	guard := NewHostGuard(nil)
	next := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusOK) })
	request := httptest.NewRequest(http.MethodGet, "http://evil.example.com/api/graph", nil)
	request.Host = "evil.example.com"
	recorder := httptest.NewRecorder()
	guard.Wrap(next).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		test.Fatalf("code=%d want 403", recorder.Code)
	}
}
