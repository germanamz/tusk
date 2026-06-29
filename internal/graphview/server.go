package graphview

import (
	"context"
	"io/fs"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/germanamz/tusk/internal/graphcluster"
)

// Server hosts the graph-view HTTP API and embedded frontend. Construct with
// New, mount Handler(), and run Run(ctx) for the SSE broadcast loop.
type Server struct {
	deps    Deps
	mux     *http.ServeMux
	pollDur time.Duration

	// allowedHosts is the Host-header allowlist beyond loopback/localhost,
	// derived from Deps.AllowedHosts. allowAnyHost is set when "*" is present.
	allowedHosts map[string]struct{}
	allowAnyHost bool

	// mu guards clients (the SSE hub). It is unrelated to community state.
	mu      sync.Mutex
	clients map[chan []byte]struct{}

	// communityMu guards the four community-memo fields below. It is
	// intentionally separate from mu so the SSE broadcast loop (which holds
	// mu across the entire fan-out) does not block snapshot() from reading
	// community state, and vice-versa. communityLabelsFor holds communityMu
	// for its entire body — no other method may read or write these fields.
	communityMu     sync.Mutex
	prevCommunities map[string]string // nodeID -> last stable label, carried across generations
	communityGen    int64             // generation the memo was computed for
	communityLabels map[string]string // memoized labels for communityGen
	communityGenSet bool              // false until the first community computation

	// detect is the community-detection function. It defaults to
	// graphcluster.Detect and may be replaced in tests to count invocations
	// without the test needing to inspect internal state.
	detect func(nodeIDs []string, edges []graphcluster.Edge, opts graphcluster.Options) map[string]int
}

// New builds a Server. Handlers are registered immediately; Run(ctx) drives
// the SSE broadcast loop (added in a later task).
func New(deps Deps) *Server {
	poll := deps.PollInterval
	if poll <= 0 {
		poll = 2 * time.Second
	}

	srv := &Server{
		deps:         deps,
		mux:          http.NewServeMux(),
		pollDur:      poll,
		clients:      make(map[chan []byte]struct{}),
		detect:       graphcluster.Detect,
		allowedHosts: make(map[string]struct{}),
	}

	for _, host := range deps.AllowedHosts {
		if host == "*" {
			srv.allowAnyHost = true

			continue
		}

		srv.allowedHosts[host] = struct{}{}
	}

	srv.routes()

	return srv
}

// Handler returns the mountable HTTP handler (API + embedded static assets),
// wrapped in a Host-header guard. The server binds loopback by default, but a
// browser the user already runs can rebind an attacker domain to the loopback
// address and read vault file bodies and embeddings same-origin. Only requests
// whose Host names a loopback address, "localhost", or an explicitly allowed
// host are served; everything else gets 403.
func (srv *Server) Handler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !srv.hostAllowed(request.Host) {
			http.Error(writer, "forbidden: untrusted Host header", http.StatusForbidden)

			return
		}

		srv.mux.ServeHTTP(writer, request)
	})
}

// hostAllowed reports whether a request's Host header may be served. Loopback
// addresses and "localhost" always pass; other hosts must be in the configured
// allowlist (or "*" must have disabled the guard).
func (srv *Server) hostAllowed(hostHeader string) bool {
	if srv.allowAnyHost {
		return true
	}

	hostname := hostHeader

	if host, _, splitErr := net.SplitHostPort(hostHeader); splitErr == nil {
		hostname = host
	}

	if hostname == "localhost" {
		return true
	}

	if ip := net.ParseIP(hostname); ip != nil && ip.IsLoopback() {
		return true
	}

	_, allowed := srv.allowedHosts[hostname]

	return allowed
}

// Run drives the SSE broadcast loop until ctx is cancelled.
func (srv *Server) Run(ctx context.Context) {
	srv.runHub(ctx)
}

func (srv *Server) routes() {
	srv.mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok"))
	})

	srv.mux.HandleFunc("GET /api/graph", srv.handleGraph)

	srv.mux.HandleFunc("GET /api/graph/stream", srv.handleStream)

	srv.mux.HandleFunc("GET /api/node/{id...}", srv.handleNodeDetail)

	srv.mux.HandleFunc("GET /api/subunits/{id...}", srv.handleSubunits)

	srv.mux.HandleFunc("POST /api/query", srv.handleQuery)

	srv.mux.HandleFunc("GET /api/embeddings", srv.handleEmbeddings)

	srv.mux.Handle("GET /", srv.staticHandler())
}

// communityLabelsFor returns the stable community labels for the given reindex
// generation. If the memo already holds this generation, it is returned without
// calling compute. Otherwise compute() runs (it returns a fresh nodeID->label
// map computed via Detect + stableLabels against the server's prevCommunities),
// the result becomes both prevCommunities and the memo for gen, and is returned.
// compute must not touch server state.
//
// communityMu is held for the entire call so concurrent snapshot() invocations
// (an /api/graph request racing the SSE poll) run Detect at most once per
// generation and prevCommunities advances monotonically. compute is CPU-only
// (no I/O, no further locks), so the critical section is bounded.
func (srv *Server) communityLabelsFor(gen int64, compute func(prev map[string]string) map[string]string) map[string]string {
	srv.communityMu.Lock()
	defer srv.communityMu.Unlock()

	if srv.communityGenSet && srv.communityGen == gen {
		return srv.communityLabels
	}

	labels := compute(srv.prevCommunities)

	srv.prevCommunities = labels
	srv.communityLabels = labels
	srv.communityGen = gen
	srv.communityGenSet = true

	return labels
}

func (srv *Server) staticHandler() http.Handler {
	sub, subErr := fs.Sub(distFS, "dist")
	if subErr != nil {
		// embed.FS with a known subdir never fails; fall back to 500.
		return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			http.Error(writer, "static assets unavailable", http.StatusInternalServerError)
		})
	}

	return http.FileServerFS(sub)
}
