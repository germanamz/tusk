package graphview

import (
	"encoding/json"
	"net/http"
)

// snapshot builds the file-level graph: every file node, plus only the edges
// whose BOTH endpoints are file nodes (sub-unit "contains" edges are excluded
// from the top-level view; they surface on drill-down). Degree counts those
// kept edges.
func (srv *Server) snapshot() (Graph, error) {
	fileRows, listErr := srv.deps.Nodes.ListFileNodes()
	if listErr != nil {
		return Graph{}, listErr
	}

	edgeRows, edgeErr := srv.deps.Edges.ListAll()
	if edgeErr != nil {
		return Graph{}, edgeErr
	}

	fileSet := make(map[string]struct{}, len(fileRows))
	for _, row := range fileRows {
		fileSet[row.ID] = struct{}{}
	}

	degree := make(map[string]int, len(fileRows))
	inDegree := make(map[string]int, len(fileRows))

	edges := make([]GraphEdge, 0, len(edgeRows))
	for _, row := range edgeRows {
		_, srcOK := fileSet[row.SourceID]
		_, dstOK := fileSet[row.TargetID]

		if !srcOK || !dstOK {
			continue
		}

		edges = append(edges, GraphEdge{Source: row.SourceID, Target: row.TargetID, Type: row.Type, Kind: row.Kind})
		degree[row.SourceID]++
		degree[row.TargetID]++
		inDegree[row.TargetID]++
	}

	nodes := make([]GraphNode, 0, len(fileRows))
	for _, row := range fileRows {
		nodes = append(nodes, GraphNode{
			ID:       row.ID,
			Type:     row.Type,
			Title:    row.Title,
			Path:     row.Path,
			Tags:     tagsFromProperties(row.PropertiesJSON),
			Degree:   degree[row.ID],
			InDegree: inDegree[row.ID],
		})
	}

	sig, sigErr := srv.signal()
	if sigErr != nil {
		return Graph{}, sigErr
	}

	return Graph{Generation: sig.Generation, Epoch: sig.Epoch, Nodes: nodes, Edges: edges}, nil
}

// signal reads the current change signal, tolerating a nil ChangeSource (tests
// that don't care pass none).
func (srv *Server) signal() (Signal, error) {
	if srv.deps.Changes == nil {
		return Signal{}, nil
	}

	return srv.deps.Changes.Signal()
}

// tagsFromProperties extracts a string "tags" array from a node's raw
// properties JSON. Returns nil when absent or malformed.
func tagsFromProperties(propsJSON string) []string {
	if propsJSON == "" {
		return nil
	}

	var parsed struct {
		Tags []string `json:"tags"`
	}

	if err := json.Unmarshal([]byte(propsJSON), &parsed); err != nil {
		return nil
	}

	return parsed.Tags
}

func (srv *Server) handleGraph(writer http.ResponseWriter, _ *http.Request) {
	graph, err := srv.snapshot()
	if err != nil {
		http.Error(writer, "index unavailable: "+err.Error(), http.StatusServiceUnavailable)

		return
	}

	writeJSON(writer, graph)
}

// writeJSON writes value as application/json. An encode error after the header
// is sent is unrecoverable, so it is intentionally ignored.
func writeJSON(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(value)
}
