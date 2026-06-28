package graphview

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The graph server binds loopback by default, but a browser the user already
// runs can rebind an attacker domain to 127.0.0.1 and read vault file bodies
// and embeddings same-origin. The Host guard blocks that: only loopback,
// localhost, and explicitly allowed hosts are served.
func TestServer_HostGuard(test *testing.T) {
	cases := []struct {
		name    string
		allowed []string
		host    string
		wantOK  bool
	}{
		{"loopback ipv4", nil, "127.0.0.1:7373", true},
		{"localhost", nil, "localhost:7373", true},
		{"loopback ipv6", nil, "[::1]:7373", true},
		{"rebinding attacker domain blocked", nil, "evil.example.com:7373", false},
		{"lan ip blocked by default", nil, "192.168.1.5:7373", false},
		{"explicitly allowed host", []string{"graph.local"}, "graph.local:7373", true},
		{"wildcard allows any", []string{"*"}, "evil.example.com", true},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			srv := New(Deps{AllowedHosts: testCase.allowed})

			request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			request.Host = testCase.host
			recorder := httptest.NewRecorder()

			srv.Handler().ServeHTTP(recorder, request)

			gotOK := recorder.Code == http.StatusOK

			if gotOK != testCase.wantOK {
				subtest.Errorf("Host %q: status %d (ok=%v), want ok=%v", testCase.host, recorder.Code, gotOK, testCase.wantOK)
			}
		})
	}
}
