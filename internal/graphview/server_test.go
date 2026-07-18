package graphview

import "net/http"

// testHandler builds an http.Handler that serves the graph-view API by
// registering the server's routes on a fresh mux under APIBase. Tests use it in
// place of the retired standalone Handler(); the parent webapp owns the host
// guard, healthz, and static frontend in production.
func testHandler(srv *Server) http.Handler {
	mux := http.NewServeMux()

	srv.RegisterRoutes(mux, APIBase)

	return mux
}
