package graphview

import (
	"context"
	"net/http"
	"sync"

	"github.com/germanamz/tusk/internal/graphcluster"
	"github.com/germanamz/tusk/internal/webui"
)

// APIBase is the conventional mount prefix for the graph-view API. The parent
// server passes it (or another prefix) to RegisterRoutes.
const APIBase = "/api/graph"

// Server hosts the graph-view HTTP API. Construct with New, register its routes
// on a parent mux with RegisterRoutes, and run Run(ctx) for the SSE broadcast
// loop. The parent server owns the host guard, CSP, healthz, and static
// frontend; this server is a pure API/SSE provider.
type Server struct {
	deps Deps

	// hub is the SSE broadcast hub: it serves /api/graph/stream and, driven by
	// Run, pushes a fresh snapshot to every client whenever the change signal
	// advances.
	hub *webui.Hub

	// communityMu guards the four community-memo fields below. It is
	// intentionally separate from the hub's internal client lock so the SSE
	// broadcast fan-out (which holds that lock across the entire fan-out) does
	// not block snapshot() from reading community state, and vice-versa.
	// communityLabelsFor holds communityMu for its entire body — no other
	// method may read or write these fields.
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

// New builds a Server. Register its routes on a parent mux with RegisterRoutes;
// Run(ctx) drives the SSE broadcast loop.
func New(deps Deps) *Server {
	srv := &Server{
		deps:   deps,
		detect: graphcluster.Detect,
	}

	// Poll through srv.signal rather than deps.Changes directly: signal
	// tolerates a nil ChangeSource (tests that don't care pass none), so such a
	// Server keeps polling harmlessly instead of panicking in Run. NewHub
	// applies the 2s PollInterval default.
	srv.hub = webui.NewHub(webui.HubOptions{
		EventName:    "graph",
		Payload:      srv.snapshotBytes,
		Changes:      signalFunc(srv.signal),
		PollInterval: deps.PollInterval,
	})

	return srv
}

// signalFunc adapts a Signal-returning func to webui.ChangeSource.
type signalFunc func() (Signal, error)

func (fn signalFunc) Signal() (Signal, error) { return fn() }

// Run drives the SSE broadcast loop until ctx is cancelled.
func (srv *Server) Run(ctx context.Context) {
	srv.hub.Run(ctx)
}

// ClientCount reports connected SSE clients (for the CLI status line).
func (srv *Server) ClientCount() int {
	return srv.hub.ClientCount()
}

// RegisterRoutes registers the six graph-view API routes on mux under base
// (base carries no trailing slash, e.g. APIBase == "/api/graph"). It
// deliberately omits /healthz and the static frontend: the parent server owns
// the host guard, CSP, healthz, and static assets.
func (srv *Server) RegisterRoutes(mux *http.ServeMux, base string) {
	mux.HandleFunc("GET "+base, srv.handleGraph)

	mux.HandleFunc("GET "+base+"/stream", srv.hub.ServeStream)

	mux.HandleFunc("GET "+base+"/node/{id...}", srv.handleNodeDetail)

	mux.HandleFunc("GET "+base+"/subunits/{id...}", srv.handleSubunits)

	mux.HandleFunc("POST "+base+"/query", srv.handleQuery)

	mux.HandleFunc("GET "+base+"/embeddings", srv.handleEmbeddings)
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
