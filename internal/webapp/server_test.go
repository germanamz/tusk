package webapp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRouteComposition pins that both views' API routes are mounted under their
// namespaced prefixes on the composed mux. It reads the mux's matched pattern
// directly (white-box) rather than invoking the handlers, so it needs no view
// deps and cannot panic on empty ones — it verifies wiring, not behavior.
func TestRouteComposition(test *testing.T) {
	srv := New(Deps{})

	cases := []struct {
		method  string
		target  string
		pattern string
	}{
		{http.MethodGet, "/healthz", "GET /healthz"},
		{http.MethodGet, "/api/graph", "GET /api/graph"},
		{http.MethodGet, "/api/graph/stream", "GET /api/graph/stream"},
		{http.MethodGet, "/api/graph/node/notes/a", "GET /api/graph/node/{id...}"},
		{http.MethodGet, "/api/graph/subunits/notes/a", "GET /api/graph/subunits/{id...}"},
		{http.MethodPost, "/api/graph/query", "POST /api/graph/query"},
		{http.MethodGet, "/api/graph/embeddings", "GET /api/graph/embeddings"},
		{http.MethodGet, "/api/read/index", "GET /api/read/index"},
		{http.MethodGet, "/api/read/node/notes/a", "GET /api/read/node/{id...}"},
		{http.MethodGet, "/api/read/asset/img/x.png", "GET /api/read/asset/{path...}"},
		{http.MethodPost, "/api/read/search", "POST /api/read/search"},
		{http.MethodGet, "/api/read/related/notes/a", "GET /api/read/related/{id...}"},
		{http.MethodGet, "/api/read/stream", "GET /api/read/stream"},
	}

	for _, testCase := range cases {
		request := httptest.NewRequest(testCase.method, testCase.target, nil)

		_, pattern := srv.mux.Handler(request)

		if pattern != testCase.pattern {
			test.Errorf("%s %s matched pattern %q, want %q", testCase.method, testCase.target, pattern, testCase.pattern)
		}
	}
}

// TestNodeCollisionResolved proves the merge fixed the one true endpoint
// collision: graph and book both defined GET /api/node/{id...} with different
// payloads. Under the unified app the two node routes live at distinct prefixes,
// and the bare /api/node path no longer resolves to a node handler — it falls
// through to the static frontend catch-all.
func TestNodeCollisionResolved(test *testing.T) {
	srv := New(Deps{})

	_, graphNode := srv.mux.Handler(httptest.NewRequest(http.MethodGet, "/api/graph/node/x", nil))
	_, bookNode := srv.mux.Handler(httptest.NewRequest(http.MethodGet, "/api/read/node/x", nil))

	if graphNode == bookNode {
		test.Fatalf("graph and book node routes share pattern %q; collision not resolved", graphNode)
	}

	_, bareNode := srv.mux.Handler(httptest.NewRequest(http.MethodGet, "/api/node/x", nil))

	if bareNode != "GET /" {
		test.Fatalf("bare /api/node/x matched %q, want the static catch-all %q", bareNode, "GET /")
	}
}

func TestHealthz(test *testing.T) {
	server := httptest.NewServer(New(Deps{}).Handler())
	defer server.Close()

	resp, getErr := http.Get(server.URL + "/healthz")

	if getErr != nil {
		test.Fatalf("GET /healthz: %v", getErr)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		test.Fatalf("GET /healthz status = %d, want 200", resp.StatusCode)
	}
}

func TestServesStaticIndex(test *testing.T) {
	server := httptest.NewServer(New(Deps{}).Handler())
	defer server.Close()

	resp, getErr := http.Get(server.URL + "/")

	if getErr != nil {
		test.Fatalf("GET /: %v", getErr)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		test.Fatalf("GET / status = %d, want 200", resp.StatusCode)
	}
}

// TestUnifiedCSP pins the single security policy that covers both views: the
// worker-src directive the graph's layout worker needs and the 'unsafe-inline'
// style the reading view's KaTeX/mermaid injection needs must both be present,
// alongside the nosniff header.
func TestUnifiedCSP(test *testing.T) {
	server := httptest.NewServer(New(Deps{}).Handler())
	defer server.Close()

	resp, getErr := http.Get(server.URL + "/")

	if getErr != nil {
		test.Fatalf("GET /: %v", getErr)
	}

	defer resp.Body.Close()

	csp := resp.Header.Get("Content-Security-Policy")

	for _, want := range []string{"default-src 'self'", "worker-src 'self' blob:", "style-src 'self' 'unsafe-inline'", "script-src 'self'"} {
		if !strings.Contains(csp, want) {
			test.Errorf("CSP %q missing %q", csp, want)
		}
	}

	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		test.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

// TestHostGuardRejectsUntrustedHost confirms the single guard fronting both
// views blocks a DNS-rebinding Host while loopback passes.
func TestHostGuardRejectsUntrustedHost(test *testing.T) {
	handler := New(Deps{}).Handler()

	untrusted := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	untrusted.Host = "evil.example"
	untrustedRec := httptest.NewRecorder()
	handler.ServeHTTP(untrustedRec, untrusted)

	if untrustedRec.Code != http.StatusForbidden {
		test.Fatalf("untrusted Host status = %d, want 403", untrustedRec.Code)
	}

	loopback := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	loopback.Host = "localhost"
	loopbackRec := httptest.NewRecorder()
	handler.ServeHTTP(loopbackRec, loopback)

	if loopbackRec.Code != http.StatusOK {
		test.Fatalf("loopback Host status = %d, want 200", loopbackRec.Code)
	}
}

func TestClientCountAggregatesFromZero(test *testing.T) {
	if count := New(Deps{}).ClientCount(); count != 0 {
		test.Fatalf("initial ClientCount = %d, want 0", count)
	}
}
