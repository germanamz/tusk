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

// Server hosts the reading-UI HTTP API and the embedded frontend. Construct
// with New, mount Handler(), and run Run(ctx) for the SSE broadcast loop.
type Server struct {
	deps Deps
	mux  *http.ServeMux

	// guard is the Host-header allowlist, built from Deps.AllowedHosts.
	guard *webui.HostGuard

	// hub is the SSE broadcast hub: it serves /api/stream and, driven by Run,
	// pushes a fresh change frame to every client whenever the signal advances.
	hub *webui.Hub

	// changes reports the vault's change signal. It is never nil — New
	// substitutes a constant zero signal when Deps.Meta is absent.
	changes webui.ChangeSource
}

// New builds a Server. Handlers are registered immediately; Run(ctx) drives the
// SSE broadcast loop.
func New(deps Deps) *Server {
	srv := &Server{
		deps:    deps,
		mux:     http.NewServeMux(),
		guard:   webui.NewHostGuard(deps.AllowedHosts),
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

	srv.routes()

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

// constantSignal reports an unchanging zero signal: /api/stream stays open and
// serves a well-formed initial frame, it simply never fires an update.
type constantSignal struct{}

func (constantSignal) Signal() (webui.Signal, error) { return webui.Signal{}, nil }

// Handler returns the mountable HTTP handler (API + embedded static assets),
// wrapped in a Host-header guard and the security headers. The server binds
// loopback by default, but a browser the user already runs can rebind an
// attacker domain to the loopback address and read vault file bodies
// same-origin. Only requests whose Host names a loopback address, "localhost",
// or an explicitly allowed host are served; everything else gets 403.
//
// The guard sits outside the headers so an untrusted Host is rejected before
// any other work; its 403 is http.Error's text/plain, which already carries
// nosniff.
func (srv *Server) Handler() http.Handler {
	return srv.guard.Wrap(withSecurityHeaders(srv.mux))
}

// Run drives the SSE broadcast loop until ctx is cancelled.
func (srv *Server) Run(ctx context.Context) {
	srv.hub.Run(ctx)
}

// ClientCount reports connected SSE clients (for the CLI status line).
func (srv *Server) ClientCount() int {
	return srv.hub.ClientCount()
}

// withSecurityHeaders sets a restrictive CSP on every response so the served
// document, its bundle, and the API all share one policy. The reading UI
// renders untrusted vault content — arbitrary markdown, including raw HTML — as
// the browser's own DOM, so the policy is the backstop behind client-side
// sanitization.
//
// 'unsafe-inline' style is required: KaTeX and mermaid both inject inline
// <style>/style attributes as they render. script-src stays strict 'self' (no
// 'unsafe-inline', no 'unsafe-eval') — the bundle is same-origin, so injected
// <script> in a node body cannot run. connect-src 'self' permits the EventSource
// (/api/stream) and fetch (/api/*) calls. img-src 'self' data: allows vault
// assets (served same-origin via /api/asset) and inline data URIs while blocking
// silent remote image loads from node content, which would otherwise leak what
// the user is reading to a third-party host. base-uri 'none' stops an injected
// <base> from re-pointing the relative ./api/... fetches; object-src 'none'
// drops the legacy plugin vector entirely.
func withSecurityHeaders(next http.Handler) http.Handler {
	const csp = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; " +
		"script-src 'self'; connect-src 'self'; base-uri 'none'; object-src 'none'"

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Security-Policy", csp)
		writer.Header().Set("X-Content-Type-Options", "nosniff")

		next.ServeHTTP(writer, request)
	})
}

// routes registers the read-only route table. Every route is a GET but
// POST /api/search, which is a read too — it carries a query body rather than
// mutating anything. Nothing here writes to the vault.
func (srv *Server) routes() {
	srv.mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok"))
	})

	srv.mux.HandleFunc("GET /api/index", srv.handleIndex)

	srv.mux.HandleFunc("GET /api/node/{id...}", srv.handleNode)

	srv.mux.HandleFunc("GET /api/asset/{path...}", srv.handleAsset)

	srv.mux.HandleFunc("POST /api/search", srv.handleSearch)

	srv.mux.HandleFunc("GET /api/related/{id...}", srv.handleRelated)

	srv.mux.HandleFunc("GET /api/stream", srv.hub.ServeStream)

	srv.mux.Handle("GET /", webui.StaticHandler(distFS, "dist"))
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
