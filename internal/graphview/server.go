package graphview

import (
	"context"
	"io/fs"
	"net/http"
	"sync"
	"time"
)

// Server hosts the graph-view HTTP API and embedded frontend. Construct with
// New, mount Handler(), and run Run(ctx) for the SSE broadcast loop.
type Server struct {
	deps    Deps
	mux     *http.ServeMux
	pollDur time.Duration

	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

// New builds a Server. Handlers are registered immediately; Run(ctx) drives
// the SSE broadcast loop (added in a later task).
func New(deps Deps) *Server {
	poll := deps.PollInterval
	if poll <= 0 {
		poll = 2 * time.Second
	}

	srv := &Server{
		deps:    deps,
		mux:     http.NewServeMux(),
		pollDur: poll,
		clients: make(map[chan []byte]struct{}),
	}

	srv.routes()

	return srv
}

// Handler returns the mountable HTTP handler (API + embedded static assets).
func (srv *Server) Handler() http.Handler {
	return srv.mux
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

	srv.mux.HandleFunc("GET /api/node/{id...}", srv.handleNode)

	srv.mux.HandleFunc("POST /api/query", srv.handleQuery)

	srv.mux.Handle("GET /", srv.staticHandler())
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
