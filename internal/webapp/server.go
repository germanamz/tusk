package webapp

import (
	"context"
	"net/http"

	"github.com/germanamz/tusk/internal/bookview"
	"github.com/germanamz/tusk/internal/graphview"
	"github.com/germanamz/tusk/internal/webui"
)

// Server hosts the unified web app: the graph and reading views behind one mux,
// one Host-header guard, and one CSP. Construct with New, mount Handler(), and
// run Run(ctx) to drive both views' SSE broadcast loops.
type Server struct {
	graph *graphview.Server
	book  *bookview.Server
	mux   *http.ServeMux
	guard *webui.HostGuard
}

// New builds a Server. It constructs the two view providers, mounts their APIs
// under /api/graph/* and /api/read/* (resolving the /api/node path collision
// the two views used to share), and registers healthz plus the embedded unified
// frontend. Run(ctx) drives both SSE hubs.
func New(deps Deps) *Server {
	srv := &Server{
		graph: graphview.New(deps.Graph),
		book:  bookview.New(deps.Book),
		mux:   http.NewServeMux(),
		guard: webui.NewHostGuard(deps.AllowedHosts),
	}

	srv.routes()

	return srv
}

func (srv *Server) routes() {
	srv.mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok"))
	})

	srv.graph.RegisterRoutes(srv.mux, graphview.APIBase)
	srv.book.RegisterRoutes(srv.mux, bookview.APIBase)

	srv.mux.Handle("GET /", webui.StaticHandler(distFS, "dist"))
}

// Handler returns the mountable HTTP handler: the composed API + embedded
// frontend, wrapped in the unified security headers and the Host-header guard.
// The guard sits outermost so an untrusted Host is rejected before any other
// work; its 403 is http.Error's text/plain, which already carries nosniff.
func (srv *Server) Handler() http.Handler {
	return srv.guard.Wrap(withSecurityHeaders(srv.mux))
}

// Run drives both views' SSE broadcast loops until ctx is cancelled, returning
// only once both have stopped.
func (srv *Server) Run(ctx context.Context) {
	done := make(chan struct{}, 2)

	go func() {
		srv.graph.Run(ctx)
		done <- struct{}{}
	}()

	go func() {
		srv.book.Run(ctx)
		done <- struct{}{}
	}()

	<-done
	<-done
}

// ClientCount reports the total connected SSE clients across both views, for
// the CLI status line.
func (srv *Server) ClientCount() int {
	return srv.graph.ClientCount() + srv.book.ClientCount()
}
