package bookview

import "net/http"

// The handlers below are registered by routes() so the route table is complete,
// but their bodies land with the tasks that own them. Each reports 501 until
// then.

// handleIndex serves the Contents pane's node index: every file-level node
// (NodeSource.ListFileNodes already excludes sub-units, filtering on
// parent_id IS NULL).
func (srv *Server) handleIndex(writer http.ResponseWriter, _ *http.Request) {
	rows, listErr := srv.deps.Nodes.ListFileNodes()

	if listErr != nil {
		http.Error(writer, listErr.Error(), http.StatusServiceUnavailable)

		return
	}

	out := IndexResponse{Nodes: make([]IndexNode, 0, len(rows))}

	for _, row := range rows {
		out.Nodes = append(out.Nodes, IndexNode{
			ID:    row.ID,
			Type:  row.Type,
			Title: row.Title,
			Path:  row.Path,
		})
	}

	writeJSON(writer, out)
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
