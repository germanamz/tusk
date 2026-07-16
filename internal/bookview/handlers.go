package bookview

import "net/http"

// The handlers below are registered by routes() so the route table is complete,
// but their bodies land with the tasks that own them. Each reports 501 until
// then.

// handleIndex serves the Contents pane's node index: every file-level node
// (NodeSource.ListFileNodes already excludes sub-units, filtering on
// parent_id IS NULL), each carrying Parent from its ParentID. Parent exists so
// the client can offer hierarchy grouping among files; in the current schema
// a file-level row's parent_id is always NULL (the nodes table's CHECK
// constraint forbids otherwise), so Parent is empty in practice today, but the
// mapping is still applied generically rather than hardcoded to "".
func (srv *Server) handleIndex(writer http.ResponseWriter, _ *http.Request) {
	rows, listErr := srv.deps.Nodes.ListFileNodes()

	if listErr != nil {
		http.Error(writer, listErr.Error(), http.StatusServiceUnavailable)

		return
	}

	out := IndexResponse{Nodes: make([]IndexNode, 0, len(rows))}

	for _, row := range rows {
		parent := ""

		if row.ParentID.Valid {
			parent = row.ParentID.String
		}

		out.Nodes = append(out.Nodes, IndexNode{
			ID:     row.ID,
			Type:   row.Type,
			Title:  row.Title,
			Path:   row.Path,
			Parent: parent,
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
