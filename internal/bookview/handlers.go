package bookview

import "net/http"

// The handlers below are registered by routes() so the route table is complete,
// but their bodies land with the tasks that own them. Each reports 501 until
// then.

// handleIndex serves the Contents pane's node index.
func (srv *Server) handleIndex(writer http.ResponseWriter, _ *http.Request) {
	http.Error(writer, "not implemented", http.StatusNotImplemented)
}

// handleNode serves one node as a readable document.
func (srv *Server) handleNode(writer http.ResponseWriter, _ *http.Request) {
	http.Error(writer, "not implemented", http.StatusNotImplemented)
}

// handleAsset serves a vault-relative asset (images referenced from node
// bodies).
func (srv *Server) handleAsset(writer http.ResponseWriter, _ *http.Request) {
	http.Error(writer, "not implemented", http.StatusNotImplemented)
}
