package bookview

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/germanamz/tusk/internal/webui"
)

// DefaultAddr is the loopback address `tusk book` binds unless --addr says
// otherwise.
const DefaultAddr = "127.0.0.1:7474"

// APIBase is the URL prefix the parent server mounts this package's read API
// under. Every route RegisterRoutes registers hangs off it.
const APIBase = "/api/read"

// Server serves the reading-UI read API and drives the SSE broadcast loop. It
// is a pure API provider: the host guard, security headers, static frontend,
// and healthz belong to the parent server that mounts it. Construct with New,
// register the routes with RegisterRoutes into a parent mux, and run Run(ctx)
// for the SSE broadcast loop.
type Server struct {
	deps Deps

	// hub is the SSE broadcast hub: it serves the stream route and, driven by
	// Run, pushes a fresh change frame to every client whenever the signal
	// advances.
	hub *webui.Hub

	// changes reports the vault's change signal. It is never nil — New
	// substitutes a constant zero signal when Deps.Meta is absent.
	changes webui.ChangeSource
}

// New builds a Server. Routes are registered by the parent via RegisterRoutes;
// Run(ctx) drives the SSE broadcast loop.
func New(deps Deps) *Server {
	srv := &Server{
		deps:    deps,
		changes: newChangeSource(deps),
	}

	// NewHub applies the 2s PollInterval default, so PollInterval passes
	// through unmassaged.
	srv.hub = webui.NewHub(webui.HubOptions{
		EventName:    "change",
		Payload:      srv.changePayload,
		Changes:      srv.changes,
		PollInterval: deps.PollInterval,
	})

	return srv
}

// newChangeSource builds the change source for deps: the real reindex-generation
// + epoch reader, or a constant zero signal when no MetaReader was supplied.
// webui.Hub requires a non-nil ChangeSource and dereferences it without a nil
// check, so a Deps without Meta (tests, and any caller indifferent to live
// updates) would otherwise panic the moment a client connected.
func newChangeSource(deps Deps) webui.ChangeSource {
	if deps.Meta == nil {
		return constantSignal{}
	}

	return webui.NewChangeSource(deps.Root, deps.Meta)
}

// constantSignal reports an unchanging zero signal: the stream route stays open
// and serves a well-formed initial frame, it simply never fires an update.
type constantSignal struct{}

func (constantSignal) Signal() (webui.Signal, error) { return webui.Signal{}, nil }

// RegisterRoutes registers the read-only route table onto mux, every route
// hung under base (no trailing slash). Every route is a GET but the search
// route, which is a read too — it carries a query body rather than mutating
// anything. Nothing here writes to the vault.
//
// This package no longer serves standalone: healthz, the static frontend, the
// Host-header guard, and the security headers are the parent server's job. It
// mounts these routes under APIBase and owns the surrounding policy.
func (srv *Server) RegisterRoutes(mux *http.ServeMux, base string) {
	mux.HandleFunc("GET "+base+"/index", srv.handleIndex)

	mux.HandleFunc("GET "+base+"/node/{id...}", srv.handleNode)

	mux.HandleFunc("GET "+base+"/asset/{path...}", srv.handleAsset)

	mux.HandleFunc("POST "+base+"/search", srv.handleSearch)

	mux.HandleFunc("GET "+base+"/related/{id...}", srv.handleRelated)

	mux.HandleFunc("GET "+base+"/stream", srv.hub.ServeStream)
}

// Run drives the SSE broadcast loop until ctx is cancelled.
func (srv *Server) Run(ctx context.Context) {
	srv.hub.Run(ctx)
}

// ClientCount reports connected SSE clients (for the CLI status line).
func (srv *Server) ClientCount() int {
	return srv.hub.ClientCount()
}

// changePayload builds the SSE frame body: the current change signal, as
// {"generation":N,"epoch":M}.
//
// The Hub calls this concurrently — once per connecting client from
// ServeStream, and from its poll loop — holding no lock. That is safe here
// because it owns no mutable state: srv.changes re-reads the meta row and the
// epoch file on every call, and the result marshals into a fresh buffer.
func (srv *Server) changePayload() ([]byte, error) {
	signal, signalErr := srv.changes.Signal()

	if signalErr != nil {
		return nil, signalErr
	}

	return json.Marshal(signal)
}

// writeJSON encodes value as the response body.
func writeJSON(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(value)
}
